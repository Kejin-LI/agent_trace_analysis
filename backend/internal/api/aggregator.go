package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/tracelog"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

type aggregateJob struct {
	cookie string
	date   string
}

// Aggregator 按"自然日"维度异步聚合 session 指标，结果写入 DB。
//
// 触发方式：列表接口收到请求时，用当前请求的 Cookie 异步触发补库，
// 但后端会强制把补库范围截断为最近 1 天，避免随查询窗口线性膨胀。
//
// 执行模型：进程内只保留一个 worker 串行消费，单天内部 detail 拉取并发严格受限。
type Aggregator struct {
	db       *gorm.DB
	upstream *modellog.Client
	fetcher  *tracelog.Fetcher

	jobs chan aggregateJob

	flightMu sync.Mutex
	flight   map[string]bool // date -> queued or running

	// cookieMu 保护最近一次用户访问携带的有效 Cookie。
	// 仅驻留内存、绝不落盘，供凌晨 cron 复用以触发全量聚合；
	// 一旦上游鉴权失败会被清空，自动回退到"用户访问触发"模式。
	cookieMu   sync.RWMutex
	lastCookie string
}

// NewAggregator 构造 DB-backed 聚合器。
//
// 表结构由 DBA 工单统一管控（预定义、受控，不允许程序改表），
// 因此这里不再执行 AutoMigrate：线上写账号刻意不授予 ALTER 权限，
// 一旦 model 与现有表存在任何细微差异（comment/collate/index 等）都会
// 触发 ALTER 并因无权限报 1142，导致聚合器整体初始化失败、每次发版必崩。
// 改为直接信任受控表结构，程序只读写、不建表。
func NewAggregator(db *gorm.DB, client *modellog.Client, fetcher *tracelog.Fetcher) (*Aggregator, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	a := &Aggregator{
		db:       db,
		upstream: client,
		fetcher:  fetcher,
		jobs:     make(chan aggregateJob, 8),
		flight:   make(map[string]bool),
	}
	go a.worker()
	go a.nightlyCron()
	return a, nil
}

// rememberCookie 把最近一次用户访问携带的有效 Cookie 缓存到内存，供凌晨 cron 复用。
// 绝不持久化到磁盘/DB，进程退出即丢失。
func (a *Aggregator) rememberCookie(cookie string) {
	if a == nil || strings.TrimSpace(cookie) == "" {
		return
	}
	a.cookieMu.Lock()
	a.lastCookie = cookie
	a.cookieMu.Unlock()
}

// currentCookie 返回内存中缓存的最近 Cookie；无则返回空串。
func (a *Aggregator) currentCookie() string {
	if a == nil {
		return ""
	}
	a.cookieMu.RLock()
	defer a.cookieMu.RUnlock()
	return a.lastCookie
}

// forgetCookie 在检测到 Cookie 失效（上游鉴权失败）时清空缓存，
// 让凌晨 cron 自动回退到"用户访问触发"模式，避免反复用失效凭证打上游。
func (a *Aggregator) forgetCookie() {
	if a == nil {
		return
	}
	a.cookieMu.Lock()
	a.lastCookie = ""
	a.cookieMu.Unlock()
}

// nightlyCron 每天凌晨用内存缓存的最近 Cookie 跑昨天+今天的全量聚合，
// 让大盘指标在用户上班前就已"秒出"。拿不到可用 Cookie 时跳过本轮，
// 自动回退到"用户访问触发"补库（方案 C），不破坏 Cookie 不落盘的安全红线。
func (a *Aggregator) nightlyCron() {
	if a == nil || a.db == nil {
		return
	}
	// 每分钟检查一次是否到达触发时刻（本地时间 03:00），命中即聚合。
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastRunDate := ""
	for range ticker.C {
		now := time.Now()
		if now.Hour() != 3 {
			continue
		}
		today := now.Format("2006-01-02")
		if today == lastRunDate {
			continue // 当天已触发过，避免 03:00~03:59 内重复
		}
		cookie := a.currentCookie()
		if strings.TrimSpace(cookie) == "" {
			log.Printf("aggregator: nightly cron skipped (no cached cookie), fallback to access-triggered backfill")
			continue
		}
		lastRunDate = today
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		for _, date := range []string{yesterday, today} {
			if a.isDateCompleted(date) {
				continue
			}
			if !a.acquireDateFlight(date) {
				continue
			}
			select {
			case a.jobs <- aggregateJob{cookie: cookie, date: date}:
				log.Printf("aggregator: nightly cron enqueued date=%s", date)
			default:
				log.Printf("aggregator: nightly cron queue full, skip date=%s", date)
				a.releaseDateFlight(date)
			}
		}
	}
}

// Get 查 session 的聚合指标，命中返回。
func (a *Aggregator) Get(sessionID string) (cachedMetrics, bool) {
	if a == nil || a.db == nil || sessionID == "" {
		return cachedMetrics{}, false
	}
	var row model.APISessionAggregate
	if err := a.db.Where("session_id = ?", sessionID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return cachedMetrics{}, false
		}
		log.Printf("aggregator: get %s failed: %v", sessionID, err)
		return cachedMetrics{}, false
	}
	return aggregateRowToCachedMetrics(row), true
}

// EnsureDays 异步保证 dates 列表对应的最近一天完成补库；已完成或正在跑的日期跳过。
// cookie 仅用于本次触发的上游调用，仅在内存中传递，不持久化。
func (a *Aggregator) EnsureDays(cookie string, dates []string) {
	if a == nil || a.db == nil || cookie == "" {
		return
	}
	// 记住最近一次用户访问携带的 Cookie，供凌晨 cron 复用（仅内存）。
	a.rememberCookie(cookie)
	date, ok := mostRecentDay(dates)
	if !ok {
		return
	}
	if a.isDateCompleted(date) {
		return
	}
	if !a.acquireDateFlight(date) {
		return
	}

	select {
	case a.jobs <- aggregateJob{cookie: cookie, date: date}:
	default:
		log.Printf("aggregator: queue full, skip date=%s", date)
		a.releaseDateFlight(date)
	}
}

func (a *Aggregator) worker() {
	for job := range a.jobs {
		a.runAggregate(job.cookie, job.date)
		a.releaseDateFlight(job.date)
	}
}

func (a *Aggregator) acquireDateFlight(date string) bool {
	if a == nil || strings.TrimSpace(date) == "" {
		return false
	}
	a.flightMu.Lock()
	defer a.flightMu.Unlock()
	if a.flight == nil {
		a.flight = make(map[string]bool)
	}
	if a.flight[date] {
		return false
	}
	a.flight[date] = true
	return true
}

func (a *Aggregator) releaseDateFlight(date string) {
	if a == nil || strings.TrimSpace(date) == "" {
		return
	}
	a.flightMu.Lock()
	defer a.flightMu.Unlock()
	if a.flight == nil {
		return
	}
	delete(a.flight, date)
}

// isAuthError 粗粒度判断上游错误是否为鉴权失败（Cookie 失效），用于触发清空缓存 Cookie。
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 401") ||
		strings.Contains(msg, "http 403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden")
}

// runAggregate 拉指定日期所有 session list -> 低并发拉 TOS JSONL -> 解析 -> 直接写 DB。
func (a *Aggregator) runAggregate(cookie, date string) {
	startedAt := time.Now()
	dateValue := parseAggregateDate(date)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("aggregator: %s panic: %v", date, r)
			a.upsertDayStatus(dateValue, "failed", 0, 0, 0, 0, fmt.Sprintf("panic: %v", r), &startedAt, nil, nil, 0, 2)
		}
	}()

	a.upsertDayStatus(dateValue, "running", 0, 0, 0, 0, "", &startedAt, nil, nil, 0, 2)

	tr := modellog.TimeRange{
		StartTime: date + " 00:00:00",
		EndTime:   date + " 23:59:59",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	resp, err := a.upstream.List(ctx, cookie, modellog.ListRequest{
		TimeRange: tr,
		// page_size <= 0：上游不分页，返回时间范围内全部 session（按接口契约）。
		Page: modellog.Page{},
	})
	if err != nil {
		errAt := time.Now()
		a.upsertDayStatus(dateValue, "failed", 0, 0, 0, 0, err.Error(), &startedAt, nil, &errAt, 0, 2)
		log.Printf("aggregator: %s list failed: %v", date, err)
		// 鉴权失败说明缓存 Cookie 已失效，清空它让凌晨 cron 回退到用户访问触发。
		if isAuthError(err) {
			a.forgetCookie()
			log.Printf("aggregator: cached cookie invalidated due to auth error")
		}
		return
	}
	listTotal := len(resp.Data)
	log.Printf("aggregator: %s list ok, total=%d", date, listTotal)

	var successCount atomic.Int64
	var failCount atomic.Int64
	acc := newDailySummaryAccumulator()
	sem := make(chan struct{}, 2) // 单天 detail 拉取最多 2 并发，避免内存尖峰
	var wg sync.WaitGroup

	for i := range resp.Data {
		s := resp.Data[i]
		if len(s.FileList) == 0 || s.FileList[0].URL == "" || s.SessionID == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(s modellog.Session) {
			defer wg.Done()
			defer func() {
				<-sem
				if r := recover(); r != nil {
					log.Printf("aggregator: %s session %s panic: %v", date, s.SessionID, r)
				}
			}()

			pr, err := a.fetcher.FetchAndParse(s.FileList[0].URL)
			if err != nil {
				log.Printf("aggregator: %s session %s fetch failed: %v", date, s.SessionID, err)
				failCount.Add(1)
				return
			}
			src := sessionToStgSource(s)
			bundle := buildBundleFromTOS(src, pr)
			m := extractCachedMetrics(bundle)
			if err := a.upsertSessionAggregate(dateValue, src, bundle, m); err != nil {
				log.Printf("aggregator: %s session %s upsert failed: %v", date, s.SessionID, err)
				failCount.Add(1)
				return
			}
			acc.Add(s, m)
			successCount.Add(1)
		}(s)
	}
	wg.Wait()

	completedAt := time.Now()
	if err := a.upsertDailySummary(acc.ToModel(dateValue)); err != nil {
		log.Printf("aggregator: %s upsert daily summary failed: %v", date, err)
	}
	a.upsertDayStatus(
		dateValue,
		"completed",
		int(successCount.Load()),
		int(successCount.Load()),
		int(failCount.Load()),
		listTotal,
		"",
		&startedAt,
		&completedAt,
		nil,
		time.Since(startedAt).Milliseconds(),
		2,
	)
	skipCount := listTotal - int(successCount.Load()) - int(failCount.Load())
	if skipCount < 0 {
		skipCount = 0
	}
	log.Printf(
		"aggregator: %s done, success=%d fail=%d skip=%d total=%d completion=%.1f%% failure=%.1f%% took=%s",
		date,
		successCount.Load(),
		failCount.Load(),
		skipCount,
		listTotal,
		percent(int(successCount.Load()), listTotal),
		percent(int(failCount.Load()), listTotal),
		time.Since(startedAt),
	)
}

func (a *Aggregator) isDateCompleted(date string) bool {
	var row model.APIDailyAggregateStatus
	if err := a.db.Where("aggregate_date = ?", parseAggregateDate(date)).First(&row).Error; err != nil {
		return false
	}
	return row.Status == "completed"
}

func (a *Aggregator) upsertDayStatus(date time.Time, status string, sessionCount, successCount, failCount, listTotal int, lastErr string, startedAt, completedAt, lastErrorAt *time.Time, costMs int64, fetchConcurrency int) {
	row := model.APIDailyAggregateStatus{
		AggregateDate:    date,
		Status:           status,
		SessionCount:     sessionCount,
		SuccessCount:     successCount,
		FailCount:        failCount,
		ListTotal:        listTotal,
		FetchConcurrency: fetchConcurrency,
		LastError:        lastErr,
		LastErrorAt:      lastErrorAt,
		StartedAt:        startedAt,
		FinishedAt:       completedAt,
		CostMs:           costMs,
		LastAggregatedAt: completedAt,
	}
	if err := a.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "aggregate_date"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"status":             row.Status,
			"session_count":      row.SessionCount,
			"success_count":      row.SuccessCount,
			"fail_count":         row.FailCount,
			"list_total":         row.ListTotal,
			"fetch_concurrency":  row.FetchConcurrency,
			"last_error":         row.LastError,
			"last_error_at":      row.LastErrorAt,
			"started_at":         row.StartedAt,
			"finished_at":        row.FinishedAt,
			"cost_ms":            row.CostMs,
			"last_aggregated_at": row.LastAggregatedAt,
			"updated_at":         time.Now(),
		}),
	}).Create(&row).Error; err != nil {
		log.Printf("aggregator: upsert day status failed date=%s err=%v", date.Format("2006-01-02"), err)
	}
}

func (a *Aggregator) upsertSessionAggregate(date time.Time, src model.StgSessionSource, bundle apiSessionBundle, m cachedMetrics) error {
	rulesJSON := "[]"
	if len(m.Rules) > 0 {
		buf, err := json.Marshal(m.Rules)
		if err != nil {
			return err
		}
		rulesJSON = string(buf)
	}
	featuresJSON := "{}"
	if buf, err := json.Marshal(bundle.Features); err == nil {
		featuresJSON = string(buf)
	}
	bundleJSON := "{}"
	if buf, err := json.Marshal(bundle); err == nil {
		bundleJSON = string(buf)
	}
	row := model.APISessionAggregate{
		SessionID:          src.SessionID,
		ArtifactID:         src.ArtifactID,
		AggregateDate:      date,
		UserID:             src.UserID,
		UserName:           src.UserName,
		StartedAtMs:        bundle.StartedAtMs,
		StartedAt:          msToTimePtr(bundle.StartedAtMs),
		DurationMs:         m.DurationMs,
		TraceID:            m.Trace,
		Title:              m.Title,
		Chip:               m.Chip,
		InputTokens:        m.InputTokens,
		OutputTokens:       m.OutputTokens,
		TotalTokens:        m.TotalTokens,
		AvgTokensPerTurn:   m.AvgTokensTurn,
		Turns:              m.Turns,
		TraceCount:         m.TraceCount,
		ToolCalls:          m.ToolCalls,
		UniqueTools:        m.UniqueTools,
		ToolFailures:       m.ToolFailures,
		ToolFailRateBP:     int(m.ToolFailRate * 10000),
		ToolRetries:        m.ToolRetries,
		MaxSerialRun:       m.MaxSerialRun,
		HasRootFail:        m.HasRootFail,
		HasLoop:            m.HasLoop,
		HasFinalAnswer:     m.HasFinalAnswer,
		NoOpStreak:         m.NoOpStreak,
		Score:              m.Score,
		ResponseScore:      m.ResponseScore,
		StabilityScore:     m.StabilityScore,
		ThinkingScore:      m.ThinkingScore,
		ResourceScore:      m.ResourceScore,
		OrchestrationScore: m.OrchestrationScore,
		AbnormalLevel:      m.AbnormalLevel,
		RulesJSON:          rulesJSON,
		FeaturesJSON:       featuresJSON,
		BundleJSON:         bundleJSON,
		SourceCreateAt:     src.SourceCreatedAt,
		SourceUpdateAt:     src.SourceUpdatedAt,
		AggregatedAt:       time.Now(),
	}
	return a.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"aggregate_date",
			"artifact_id",
			"user_id",
			"user_name",
			"started_at_ms",
			"started_at",
			"duration_ms",
			"input_tokens",
			"output_tokens",
			"total_tokens",
			"avg_tokens_per_turn",
			"turns",
			"trace_count",
			"tool_calls",
			"unique_tools",
			"tool_failures",
			"tool_fail_rate_bp",
			"tool_retries",
			"max_serial_run",
			"has_root_fail",
			"has_loop",
			"has_final_answer",
			"no_op_streak",
			"score",
			"response_score",
			"stability_score",
			"thinking_score",
			"resource_score",
			"orchestration_score",
			"abnormal_level",
			"chip",
			"rules_json",
			"features_json",
			"bundle_json",
			"title",
			"trace_id",
			"source_create_at",
			"source_update_at",
			"aggregated_at",
			"updated_at",
		}),
	}).Create(&row).Error
}

func (a *Aggregator) upsertDailySummary(summary model.APIDailySummary) error {
	return a.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "aggregate_date"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"session_count",
			"active_user_count",
			"abnormal_session_count",
			"failed_session_count",
			"loop_session_count",
			"total_input_tokens",
			"total_output_tokens",
			"total_tokens",
			"total_tool_calls",
			"total_tool_failures",
			"avg_duration_ms",
			"avg_turns",
			"avg_score",
			"p50_duration_ms",
			"p90_duration_ms",
			"p95_duration_ms",
			"response_score_avg",
			"stability_score_avg",
			"thinking_score_avg",
			"resource_score_avg",
			"orchestration_score_avg",
			"aggregated_at",
			"updated_at",
		}),
	}).Create(&summary).Error
}

// PersistBundle 在详情页命中并成功解析 JSONL 后，顺手把这条 session 的聚合结果写入 DB。
// 该写入是 best-effort：只更新当前 session 及所属自然日 summary，不阻塞页面返回。
func (a *Aggregator) PersistBundle(src model.StgSessionSource, bundle apiSessionBundle) error {
	if a == nil || a.db == nil {
		return nil
	}
	if src.SessionID == "" {
		return fmt.Errorf("empty session id")
	}
	date := aggregateDateForBundle(src, bundle)
	m := extractCachedMetrics(bundle)
	if err := a.upsertSessionAggregate(date, src, bundle, m); err != nil {
		return err
	}
	return a.refreshDailySummary(date)
}

func (a *Aggregator) refreshDailySummary(date time.Time) error {
	if a == nil || a.db == nil {
		return nil
	}
	var rows []model.APISessionAggregate
	if err := a.db.Where("aggregate_date = ?", date).Find(&rows).Error; err != nil {
		return err
	}
	return a.upsertDailySummary(buildDailySummaryFromAggregateRows(date, rows))
}
func aggregateRowToCachedMetrics(row model.APISessionAggregate) cachedMetrics {
	var rules []apiRule
	if row.RulesJSON != "" {
		_ = json.Unmarshal([]byte(row.RulesJSON), &rules)
	}
	return cachedMetrics{
		ToolCalls:          row.ToolCalls,
		UniqueTools:        row.UniqueTools,
		MaxSerialRun:       row.MaxSerialRun,
		ToolFailures:       row.ToolFailures,
		ToolFailRate:       float64(row.ToolFailRateBP) / 10000,
		AvgTokensTurn:      row.AvgTokensPerTurn,
		ToolRetries:        row.ToolRetries,
		HasRootFail:        row.HasRootFail,
		HasLoop:            row.HasLoop,
		Turns:              row.Turns,
		TraceCount:         row.TraceCount,
		DurationMs:         row.DurationMs,
		InputTokens:        row.InputTokens,
		OutputTokens:       row.OutputTokens,
		TotalTokens:        row.TotalTokens,
		Score:              row.Score,
		Radar:              apiRadar{Response: row.ResponseScore, Stability: row.StabilityScore, Thinking: row.ThinkingScore, Resource: row.ResourceScore, Orchestration: row.OrchestrationScore},
		ResponseScore:      row.ResponseScore,
		StabilityScore:     row.StabilityScore,
		ThinkingScore:      row.ThinkingScore,
		ResourceScore:      row.ResourceScore,
		OrchestrationScore: row.OrchestrationScore,
		AbnormalLevel:      row.AbnormalLevel,
		HasFinalAnswer:     row.HasFinalAnswer,
		NoOpStreak:         row.NoOpStreak,
		Chip:               row.Chip,
		Rules:              rules,
		Title:              row.Title,
		Trace:              row.TraceID,
		UpdatedAt:          row.UpdatedAt.Unix(),
	}
}

func aggregateDateForBundle(src model.StgSessionSource, bundle apiSessionBundle) time.Time {
	if bundle.StartedAtMs > 0 {
		return startOfLocalDay(time.UnixMilli(bundle.StartedAtMs))
	}
	if src.SourceCreatedAt != nil && !src.SourceCreatedAt.IsZero() {
		return startOfLocalDay(*src.SourceCreatedAt)
	}
	if src.SourceUpdatedAt != nil && !src.SourceUpdatedAt.IsZero() {
		return startOfLocalDay(*src.SourceUpdatedAt)
	}
	return startOfLocalDay(time.Now())
}

func startOfLocalDay(t time.Time) time.Time {
	y, m, d := t.In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
func mostRecentDay(dates []string) (string, bool) {
	if len(dates) == 0 {
		return "", false
	}
	latest := ""
	for _, d := range dates {
		if d == "" {
			continue
		}
		if latest == "" || d > latest {
			latest = d
		}
	}
	if latest == "" {
		return "", false
	}
	return latest, true
}

// LastNDays 返回最近 n 天（含今天）的日期列表，格式 "YYYY-MM-DD"。
func LastNDays(n int) []string {
	out := make([]string, 0, n)
	now := time.Now()
	for i := 0; i < n; i++ {
		out = append(out, now.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	return out
}

// parseSessionIDTimestamp 尝试从 "YYYYMMDD_HHMMSS_*" 形式的 session_id 解析时间戳。
// 不匹配返回 0。OpenCode 的 "ses_*" 格式没有时间信息，靠后续聚合补齐。
func parseSessionIDTimestamp(sid string) int64 {
	if len(sid) < 16 {
		return 0
	}
	if sid[8] != '_' || sid[15] != '_' {
		return 0
	}
	t, err := time.ParseInLocation("20060102_150405", sid[:15], time.Local)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// applyCachedMetricsToBundle 把日聚合的指标 join 到 bundle 上，覆盖空字段。
// 起始时间不从缓存恢复（缓存里没存），由 lightBundleFromAPI 优先从 session_id 推算。
func applyCachedMetricsToBundle(b apiSessionBundle, m cachedMetrics) apiSessionBundle {
	if m.DurationMs > 0 {
		b.DurationMs = m.DurationMs
	}
	b.InputTokens = m.InputTokens
	b.OutputTokens = m.OutputTokens
	b.ToolCalls = m.ToolCalls
	b.Turns = m.Turns
	b.TraceCount = m.TraceCount
	b.Score = m.Score
	b.Radar = m.Radar
	b.Features = apiFeatures{
		ToolCalls:        m.ToolCalls,
		UniqueTools:      m.UniqueTools,
		MaxSerialRun:     m.MaxSerialRun,
		ToolFailures:     m.ToolFailures,
		ToolFailRate:     m.ToolFailRate,
		AvgTokensPerTurn: m.AvgTokensTurn,
		ToolRetries:      m.ToolRetries,
		HasRootFail:      m.HasRootFail,
		HasLoop:          m.HasLoop,
	}
	if m.Chip != "" {
		b.Chip = m.Chip
	}
	if len(m.Rules) > 0 {
		b.Rules = m.Rules
	}
	if m.Title != "" {
		b.Title = m.Title
	}
	if m.Trace != "" {
		b.Trace = m.Trace
	}
	return b
}

// rangeDays 返回 [start, end] 闭区间内涉及的所有自然日（按 Local 时区）。
func rangeDays(startMs, endMs int64) []string {
	if startMs <= 0 || endMs < startMs {
		return nil
	}
	cursor := time.UnixMilli(startMs).Local()
	endCursor := time.UnixMilli(endMs).Local()
	out := make([]string, 0, 8)
	for d := cursor; !d.After(endCursor); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
		if len(out) > 31 { // 防呆，最多 31 天
			break
		}
	}
	return out
}
