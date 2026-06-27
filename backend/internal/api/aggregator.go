package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
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
	cookie           string
	date             string
	includePublished bool
}

type aggregatePriority int

const (
	aggregatePriorityLow aggregatePriority = iota
	aggregatePriorityHigh
)

// Aggregator 按"自然日"维度异步聚合 session 指标，结果写入 DB。
//
// 触发方式：列表接口收到请求时，用当前请求的 Cookie 异步触发补库，
// 但后端只会尝试最近少量未完成日期，避免随查询窗口线性膨胀。
//
// 执行模型：进程内只保留一个 worker 串行消费，单天内部 detail 拉取并发严格受限。
type Aggregator struct {
	db       *gorm.DB
	upstream *modellog.Client
	fetcher  *tracelog.Fetcher

	highJobs chan aggregateJob
	lowJobs  chan aggregateJob

	flightMu sync.Mutex
	flight   map[string]bool // date -> queued or running

	// cookieMu 保护最近一次用户访问携带的有效 Cookie。
	// 仅驻留内存、绝不落盘，供凌晨 cron 复用以触发全量聚合；
	// 一旦上游鉴权失败会被清空，自动回退到"用户访问触发"模式。
	cookieMu   sync.RWMutex
	lastCookie string

	// startedAt 记录进程启动时刻，用于启动保护期：实例刚启动（如 TCE 升级/重启
	// 流量切换窗口）时不立即触发自动聚合，避免与健康检查、流量切换叠加引发内存尖峰。
	startedAt time.Time

	optionalColumnsOnce sync.Once
	bundleJSONColumnOK  bool
}

// startupGrace 启动保护期：进程启动后该时间窗内不接受访问触发的自动聚合。
const startupGrace = 2 * time.Minute

// 访问触发补库按“最近 7 天高优、最近 30 天低优”分层：
// - 高优：覆盖 24h / 7d 常用窗口，优先让首页和列表尽快变热；
// - 低优：把 30d 剩余日期在后台慢慢补齐。
// 仍保持单 worker + 单天 2 并发 + 内存闸门，避免扩大 OOM 风险。
const (
	accessTriggeredHighPriorityDays = 7
	accessTriggeredLowPriorityDays  = 30
	highPriorityQueueSize           = 8
	lowPriorityQueueSize            = 24
	// 历史 session 增量刷新建议参数：
	// - 热增量扫描：每 10 分钟一次，只扫最近 20 分钟 update_at/source_updated_at 变化；
	// - 详情页兜底：用户打开详情页时做 freshness check，必要时将当前 session 提升优先级入队；
	// - 日级/周级补漏：用于兜底，不走高频链路。
	//
	// 当前提交先落库结构与模型定义，后续再把扫描器/队列消费器按该参数接入，
	// 避免一次性把重逻辑混进现有聚合 worker，扩大 OOM 与回归风险。
	sessionHotIncrementalScanInterval = 10 * time.Minute
	sessionHotIncrementalLookback     = 20 * time.Minute
	sessionRefreshLeaseTTL            = 5 * time.Minute
)

// staleRunningTimeout 判定 running 状态为"陈旧"（残留自被杀实例）的阈值。
const staleRunningTimeout = 15 * time.Minute

// 补数内存闸门：派发每个 session 前读 cgroup 真实内存占用，
//   - 超软阈值：暂停等待回落（背压），不丢数据；
//   - 连续等待仍超硬阈值：中止本次补数并记 paused，下次触发自动续补，优先保命防 OOM。
//
// 阈值用环境变量可配（线上调参不必改代码重发），缺省软 75% / 硬 88%。
const (
	defaultMemSoftLimitPct = 75.0
	defaultMemHardLimitPct = 88.0
	// aggregateFetchConcurrency 单天内 detail 拉取并发。
	aggregateFetchConcurrency = 2
	// aggregateRetryRounds 失败 session 的额外重试轮数。
	// 例如取 2 表示“首轮失败后，再补跑最多 2 轮”。
	aggregateRetryRounds = 2
	// memBackoffInterval 软阈值命中后的等待粒度。
	memBackoffInterval = 2 * time.Second
	// memBackoffMaxRounds 连续等待多少轮仍未回落则判定为硬阈值中止。
	memBackoffMaxRounds = 15
)

// memSoftLimitPct / memHardLimitPct 读环境变量，非法或缺省时回退默认值。
func memSoftLimitPct() float64 { return envPercent("AGG_MEM_SOFT_PCT", defaultMemSoftLimitPct) }
func memHardLimitPct() float64 { return envPercent("AGG_MEM_HARD_PCT", defaultMemHardLimitPct) }

func envPercent(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 || f > 100 {
		return def
	}
	return f
}

// cgroupMemoryUsagePct 返回当前容器内存占用百分比（0~100）与是否成功读取。
// 必须读 cgroup 真实值（与 OOM killer 同口径），不能用 runtime.ReadMemStats，
// 后者只反映 Go 堆，看不到 TOS 下载 buffer 等堆外内存，会漏判 OOM 风险。
// 同时兼容 cgroup v2（memory.current/memory.max）与 v1（usage_in_bytes/limit_in_bytes）。
func cgroupMemoryUsagePct() (float64, bool) {
	type pair struct{ usage, max string }
	candidates := []pair{
		{"/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory.max"},
		{"/sys/fs/cgroup/memory/memory.usage_in_bytes", "/sys/fs/cgroup/memory/memory.limit_in_bytes"},
	}
	for _, c := range candidates {
		used, ok1 := readUintFile(c.usage)
		limit, ok2 := readUintFile(c.max)
		if !ok1 || !ok2 || limit == 0 {
			continue
		}
		return float64(used) / float64(limit) * 100, true
	}
	return 0, false
}

func readUintFile(path string) (uint64, bool) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(buf))
	// cgroup v2 在无上限时 memory.max 为 "max"，视为读取失败让上层跳过限制。
	if s == "" || s == "max" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// memGateDecision 描述派发前内存闸门的判定结果。
type memGateDecision int

const (
	memGateProceed memGateDecision = iota // 内存安全或无法读取，正常派发
	memGateAbort                          // 连续等待仍超硬阈值，中止补数
)

// waitForMemory 在派发下一个 session 前检查内存：低于软阈值立即放行；
// 介于软硬阈值之间则 sleep 等待回落；连续 memBackoffMaxRounds 轮仍超硬阈值则中止。
// 读不到 cgroup 数值时不阻塞（放行），避免在非容器环境下卡死。
func waitForMemory(date string) memGateDecision {
	soft := memSoftLimitPct()
	hard := memHardLimitPct()
	for round := 0; round < memBackoffMaxRounds; round++ {
		pct, ok := cgroupMemoryUsagePct()
		if !ok {
			return memGateProceed
		}
		if pct < soft {
			return memGateProceed
		}
		if pct >= hard {
			log.Printf("aggregator: %s memory gate over hard limit pct=%.1f%% hard=%.1f%% round=%d, aborting", date, pct, hard, round)
			return memGateAbort
		}
		log.Printf("aggregator: %s memory gate paused pct=%.1f%% soft=%.1f%% round=%d, waiting", date, pct, soft, round)
		time.Sleep(memBackoffInterval)
	}
	// 等满所有轮次仍处于软硬阈值之间：保守中止，避免长时间逼近 OOM。
	log.Printf("aggregator: %s memory gate still high after %d rounds, aborting", date, memBackoffMaxRounds)
	return memGateAbort
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
		db:        db,
		upstream:  client,
		fetcher:   fetcher,
		highJobs:  make(chan aggregateJob, highPriorityQueueSize),
		lowJobs:   make(chan aggregateJob, lowPriorityQueueSize),
		flight:    make(map[string]bool),
		startedAt: time.Now(),
	}
	// 启动时清理上一个实例残留的 running 状态：实例被 OOM/升级杀掉后，
	// DB 里的聚合状态会永远停在 running，既误导监控也会阻塞同日期重试。
	a.cleanupStaleRunning()
	go a.worker()
	go a.refreshQueueLoop()
	go a.nightlyCron()
	return a, nil
}

// rememberCookie 把最近一次用户访问携带的有效 Cookie 缓存到内存，供凌晨 cron 复用。
// 绝不持久化到磁盘/DB，进程退出即丢失。
func (a *Aggregator) rememberCookie(cookie string) {
	if a == nil {
		return
	}
	trimmed := strings.TrimSpace(cookie)
	if trimmed == "" {
		log.Printf("aggregator: rememberCookie skipped empty cookie")
		return
	}
	a.cookieMu.Lock()
	prevEmpty := strings.TrimSpace(a.lastCookie) == ""
	changed := a.lastCookie != trimmed
	a.lastCookie = trimmed
	a.cookieMu.Unlock()
	// 仅在"从空变非空"或"内容变化"时打印，避免每个 API 请求都刷屏。
	if prevEmpty || changed {
		log.Printf("aggregator: rememberCookie updated prev_empty=%t new_len=%d", prevEmpty, len(trimmed))
	}
}

// RememberCookie 是 rememberCookie 的公开入口，供 HTTP 中间件在任意 API 请求时
// 缓存当前请求携带的 Cookie，使凌晨 cron 不再依赖用户恰好访问过某个特定接口。
func (a *Aggregator) RememberCookie(cookie string) {
	a.rememberCookie(cookie)
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

// nightlyCron 每天凌晨用内存缓存的最近 Cookie 跑昨天+今天的 session 聚合，
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
	lastSkipLogDate := ""
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
			// 仅每天提示一次，避免 03:00~03:59 内每分钟刷屏 60 条 skipped 日志。
			if today != lastSkipLogDate {
				log.Printf("aggregator: nightly cron skipped (no cached cookie), fallback to access-triggered backfill started_at=%s", a.startedAt.Format(time.RFC3339))
				lastSkipLogDate = today
			}
			continue
		}
		lastRunDate = today
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		for _, date := range []string{yesterday, today} {
			if a.shouldSkipNightlyDate(date, now) {
				continue
			}
			if !a.acquireDateFlight(date) {
				continue
			}
			if a.enqueueJob(aggregateJob{cookie: cookie, date: date, includePublished: true}, aggregatePriorityHigh) {
				log.Printf("aggregator: nightly cron enqueued high-priority date=%s", date)
			} else {
				log.Printf("aggregator: nightly cron queue full, skip date=%s", date)
				a.releaseDateFlight(date)
			}
		}
	}
}

// shouldSkipNightlyDate 仅服务于凌晨 cron 的"跳过条件"判断：
// - 今天：沿用"当天已完成就跳过"语义，避免 03:00 同日重复排队；
// - 昨天：只有在进入今天之后仍被重新聚合过，才允许跳过。
// 这样可以避免"昨天晚上提前空跑一次就被标记 completed"，从而阻断次日凌晨回补。
func (a *Aggregator) shouldSkipNightlyDate(date string, now time.Time) bool {
	if a == nil || a.db == nil {
		return false
	}
	targetDate := parseAggregateDate(date)
	if a.hasIncompleteSessionAggregates(targetDate) {
		return false
	}
	var row model.APIDailyAggregateStatus
	if err := a.db.Where("aggregate_date = ?", targetDate).First(&row).Error; err != nil {
		return false
	}
	if row.FailCount > 0 {
		return false
	}
	return isNightlyFreshCompletion(row, targetDate, now)
}

func isNightlyFreshCompletion(row model.APIDailyAggregateStatus, targetDate, now time.Time) bool {
	if row.Status != "completed" {
		return false
	}
	completedAt, ok := aggregateStatusCompletedAt(row)
	if !ok {
		return false
	}
	nowDay := startOfLocalDay(now)
	targetDay := startOfLocalDay(targetDate)
	yesterday := nowDay.AddDate(0, 0, -1)
	if targetDay.Equal(yesterday) {
		return !completedAt.Before(nowDay)
	}
	return startOfLocalDay(completedAt).Equal(targetDay)
}

func aggregateStatusCompletedAt(row model.APIDailyAggregateStatus) (time.Time, bool) {
	for _, candidate := range []*time.Time{row.LastAggregatedAt, row.FinishedAt} {
		if candidate == nil || candidate.IsZero() {
			continue
		}
		return candidate.In(time.Local), true
	}
	return time.Time{}, false
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

// EnsureDays 异步保证 dates 列表中最近未完成日期进入补库队列；已完成或正在跑的日期跳过。
// cookie 仅用于本次触发的上游调用，仅在内存中传递，不持久化。
func (a *Aggregator) EnsureDays(cookie string, dates []string) {
	if a == nil || a.db == nil || cookie == "" {
		return
	}
	// 记住最近一次用户访问携带的 Cookie，供凌晨 cron 复用（仅内存）。
	a.rememberCookie(cookie)
	// 启动保护期：实例刚启动（TCE 升级/重启流量切换窗口）时不立即触发聚合，
	// 避免与健康检查、流量预热叠加导致内存尖峰。Cookie 已记住，过保护期后的
	// 后续访问或凌晨 cron 仍会触发补库，不影响数据最终补齐。
	if time.Since(a.startedAt) < startupGrace {
		return
	}
	candidates := normalizedDaysDesc(dates)
	if len(candidates) == 0 {
		return
	}
	highQueued := make([]string, 0, accessTriggeredHighPriorityDays)
	lowQueued := make([]string, 0, accessTriggeredLowPriorityDays-accessTriggeredHighPriorityDays)
	for _, date := range candidates {
		if a.isDateCompleted(date) {
			continue
		}
		priority, ok := aggregatePriorityForDate(date)
		if !ok {
			continue
		}
		if priority == aggregatePriorityHigh && len(highQueued) >= accessTriggeredHighPriorityDays {
			continue
		}
		if priority == aggregatePriorityLow && len(lowQueued) >= (accessTriggeredLowPriorityDays-accessTriggeredHighPriorityDays) {
			continue
		}
		if !a.acquireDateFlight(date) {
			continue
		}
		if a.enqueueJob(aggregateJob{cookie: cookie, date: date, includePublished: true}, priority) {
			if priority == aggregatePriorityHigh {
				highQueued = append(highQueued, date)
			} else {
				lowQueued = append(lowQueued, date)
			}
			continue
		}
		log.Printf("aggregator: queue full, skip date=%s priority=%s", date, aggregatePriorityLabel(priority))
		a.releaseDateFlight(date)
		if priority == aggregatePriorityHigh {
			break
		}
	}
	if len(highQueued) > 0 || len(lowQueued) > 0 {
		log.Printf("aggregator: ensure queued high=%v low=%v requested=%v", highQueued, lowQueued, candidates)
	}
}

func (a *Aggregator) ForceQueueDays(cookie string, dates []string) (queued []string, alreadyRunning []string, skipped []string) {
	if a == nil || a.db == nil || strings.TrimSpace(cookie) == "" {
		return nil, nil, normalizedDaysDesc(dates)
	}
	a.rememberCookie(cookie)
	for _, date := range normalizedDaysDesc(dates) {
		priority, ok := aggregatePriorityForDate(date)
		if !ok {
			skipped = append(skipped, date)
			continue
		}
		if !a.acquireDateFlight(date) {
			alreadyRunning = append(alreadyRunning, date)
			continue
		}
		if a.enqueueJob(aggregateJob{cookie: cookie, date: date, includePublished: true}, priority) {
			queued = append(queued, date)
			continue
		}
		a.releaseDateFlight(date)
		skipped = append(skipped, date)
	}
	if len(queued) > 0 || len(alreadyRunning) > 0 || len(skipped) > 0 {
		log.Printf("aggregator: manual refresh queued=%v running=%v skipped=%v", queued, alreadyRunning, skipped)
	}
	return queued, alreadyRunning, skipped
}

func (a *Aggregator) enqueueJob(job aggregateJob, priority aggregatePriority) bool {
	if a == nil {
		return false
	}
	switch priority {
	case aggregatePriorityHigh:
		select {
		case a.highJobs <- job:
			return true
		default:
			return false
		}
	default:
		select {
		case a.lowJobs <- job:
			return true
		default:
			return false
		}
	}
}

func (a *Aggregator) nextJob() (aggregateJob, bool) {
	if a == nil {
		return aggregateJob{}, false
	}
	select {
	case job, ok := <-a.highJobs:
		return job, ok
	default:
	}
	select {
	case job, ok := <-a.highJobs:
		return job, ok
	case job, ok := <-a.lowJobs:
		return job, ok
	}
}

func aggregatePriorityForDate(date string) (aggregatePriority, bool) {
	target, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), time.Local)
	if err != nil {
		return aggregatePriorityLow, false
	}
	today := startOfLocalDay(time.Now())
	target = startOfLocalDay(target)
	if target.After(today) {
		return aggregatePriorityLow, false
	}
	ageDays := int(today.Sub(target).Hours() / 24)
	switch {
	case ageDays < accessTriggeredHighPriorityDays:
		return aggregatePriorityHigh, true
	case ageDays < accessTriggeredLowPriorityDays:
		return aggregatePriorityLow, true
	default:
		return aggregatePriorityLow, false
	}
}

func aggregatePriorityLabel(priority aggregatePriority) string {
	if priority == aggregatePriorityHigh {
		return "high"
	}
	return "low"
}

func (a *Aggregator) worker() {
	for {
		job, ok := a.nextJob()
		if !ok {
			return
		}
		a.runAggregate(job.cookie, job.date, job.includePublished)
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

func (a *Aggregator) listAggregateSessions(ctx context.Context, cookie string, tr modellog.TimeRange, includePublished bool) ([]modellog.Session, map[string]string, error) {
	requests := []struct {
		onlyUnpublished bool
		label           string
	}{
		{onlyUnpublished: true, label: artifactStatusUnpublished},
	}
	if includePublished {
		requests = append(requests, struct {
			onlyUnpublished bool
			label           string
		}{onlyUnpublished: false, label: artifactStatusPublished})
	}

	sessions := make([]modellog.Session, 0)
	statusBySession := make(map[string]string)
	seen := make(map[string]struct{})
	for _, req := range requests {
		resp, err := a.upstream.List(ctx, cookie, modellog.ListRequest{
			TimeRange: tr,
			// page_size <= 0：上游不分页，返回时间范围内全部 session（按接口契约）。
			Page:                     modellog.Page{},
			OnlyUnpublishedArtifacts: req.onlyUnpublished,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("upstream list %s: %w", req.label, err)
		}
		if resp == nil {
			continue
		}
		for _, s := range resp.Data {
			if !isSupportedSessionID(s.SessionID) {
				continue
			}
			key := aggregateSessionKey(s)
			if key != "" {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
			}
			if sid := strings.TrimSpace(s.SessionID); sid != "" {
				// 两趟互斥：未发布先于已发布，dedup 保证每个 session 只标一次。
				statusBySession[sid] = req.label
			}
			sessions = append(sessions, s)
		}
	}
	return sessions, statusBySession, nil
}

func aggregateSessionKey(s modellog.Session) string {
	if sessionID := strings.TrimSpace(s.SessionID); sessionID != "" {
		return "session:" + sessionID
	}
	if artifactID := strings.TrimSpace(s.ArtifactID); artifactID != "" {
		return "artifact:" + artifactID
	}
	return ""
}

// runAggregate 拉指定日期 session list -> 低并发拉 TOS JSONL -> 解析 -> 直接写 DB。
func (a *Aggregator) runAggregate(cookie, date string, includePublished bool) {
	startedAt := time.Now()
	dateValue := parseAggregateDate(date)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("aggregator: %s panic: %v", date, r)
			a.upsertDayStatus(dateValue, "failed", 0, 0, 0, 0, 0, fmt.Sprintf("panic: %v", r), &startedAt, nil, nil, 0, aggregateFetchConcurrency)
		}
	}()

	a.upsertDayStatus(dateValue, "running", 0, 0, 0, 0, 0, "", &startedAt, nil, nil, 0, aggregateFetchConcurrency)

	tr := modellog.TimeRange{
		StartTime: date + " 00:00:00",
		EndTime:   date + " 23:59:59",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	sessions, statusBySession, err := a.listAggregateSessions(ctx, cookie, tr, includePublished)
	if err != nil {
		errAt := time.Now()
		a.upsertDayStatus(dateValue, "failed", 0, 0, 0, 0, 0, err.Error(), &startedAt, nil, &errAt, 0, aggregateFetchConcurrency)
		log.Printf("aggregator: %s list failed: %v", date, err)
		// 鉴权失败说明缓存 Cookie 已失效，清空它让凌晨 cron 回退到用户访问触发。
		if isAuthError(err) {
			a.forgetCookie()
			log.Printf("aggregator: cached cookie invalidated due to auth error")
		}
		return
	}
	listTotal := len(sessions)
	mode := "unpublished"
	if includePublished {
		mode = "unpublished+published"
	}
	log.Printf("aggregator: %s %s list ok, total=%d", date, mode, listTotal)

	// 断点续补：跳过 DB 里当天已落库的 session，使上次因内存中止（paused）的补数
	// 下次触发时只补剩余部分，不重复拉取已完成的 session。
	done := a.aggregatedSessionIDs(dateValue)
	if len(done) > 0 {
		log.Printf("aggregator: %s resume, already aggregated=%d", date, len(done))
	}

	var successCount atomic.Int64
	var failCount atomic.Int64
	var skippedExisting atomic.Int64
	aborted := false
	pending := make([]modellog.Session, 0, len(sessions))
	for i := range sessions {
		s := sessions[i]
		if len(s.FileList) == 0 || s.FileList[0].URL == "" || s.SessionID == "" {
			continue
		}
		if _, ok := done[s.SessionID]; ok {
			skippedExisting.Add(1)
			continue // 续补：已补过，跳过
		}
		pending = append(pending, s)
	}
	retryRoundsUsed := 0
	for attempt := 0; attempt <= aggregateRetryRounds && len(pending) > 0; attempt++ {
		current := pending
		pending = nil
		if attempt > 0 {
			retryRoundsUsed = attempt
			log.Printf("aggregator: %s retry round=%d pending=%d", date, attempt, len(current))
		}

		var roundMu sync.Mutex
		nextPending := make([]modellog.Session, 0)
		sem := make(chan struct{}, aggregateFetchConcurrency) // 单天 detail 拉取最多 2 并发，避免内存尖峰
		var wg sync.WaitGroup

		for i := range current {
			s := current[i]
			// 派发前内存闸门：超软阈值暂停等待，连续仍超硬阈值则中止本次补数。
			if waitForMemory(date) == memGateAbort {
				aborted = true
				roundMu.Lock()
				nextPending = append(nextPending, current[i:]...)
				roundMu.Unlock()
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(s modellog.Session) {
				defer wg.Done()
				defer func() {
					<-sem
					if r := recover(); r != nil {
						log.Printf("aggregator: %s session %s panic: %v", date, s.SessionID, r)
						roundMu.Lock()
						nextPending = append(nextPending, s)
						roundMu.Unlock()
					}
				}()

				pr, err := a.fetcher.FetchAndParse(s.FileList[0].URL)
				if err != nil {
					log.Printf("aggregator: %s session %s fetch failed attempt=%d: %v", date, s.SessionID, attempt+1, err)
					roundMu.Lock()
					nextPending = append(nextPending, s)
					roundMu.Unlock()
					return
				}
				src := sessionToStgSource(s)
				bundle := buildBundleFromTOS(src, pr)
				m := extractCachedMetrics(bundle)
				pubStatus := normalizePublicationStatusOrUnknown(statusBySession[s.SessionID])
				if err := a.upsertSessionAggregate(dateValue, src, bundle, m, pubStatus); err != nil {
					log.Printf("aggregator: %s session %s upsert failed attempt=%d: %v", date, s.SessionID, attempt+1, err)
					roundMu.Lock()
					nextPending = append(nextPending, s)
					roundMu.Unlock()
					return
				}
				successCount.Add(1)
			}(s)
		}
		wg.Wait()
		pending = nextPending
		if aborted {
			break
		}
	}
	failCount.Store(int64(len(pending)))

	completedAt := time.Now()
	// 始终用 DB 已落库行重算 daily summary：续补/中止场景下也能保证大盘数字
	// 与已补数据一致（不依赖内存累加器，天然支持断点续补）。
	if err := a.refreshDailySummary(dateValue); err != nil {
		log.Printf("aggregator: %s refresh daily summary failed: %v", date, err)
	}

	// 已补总数 = 本次成功 + 历史已存在（跳过的）。
	aggregatedTotal := int(successCount.Load()) + int(skippedExisting.Load())
	status := "completed"
	lastErr := ""
	if aborted {
		// 内存中止：记 paused，isDateCompleted 对其返回 false，下次触发自动续补。
		status = "paused"
		lastErr = "paused by memory guard, will resume on next trigger"
	}
	a.upsertDayStatus(
		dateValue,
		status,
		aggregatedTotal,
		aggregatedTotal,
		int(failCount.Load()),
		listTotal,
		retryRoundsUsed,
		lastErr,
		&startedAt,
		&completedAt,
		nil,
		time.Since(startedAt).Milliseconds(),
		aggregateFetchConcurrency,
	)
	skipCount := listTotal - aggregatedTotal - int(failCount.Load())
	if skipCount < 0 {
		skipCount = 0
	}
	log.Printf(
		"aggregator: %s %s, success=%d resume_skip=%d fail=%d remain=%d total=%d completion=%.1f%% took=%s",
		date,
		status,
		successCount.Load(),
		skippedExisting.Load(),
		failCount.Load(),
		skipCount,
		listTotal,
		percent(aggregatedTotal, listTotal),
		time.Since(startedAt),
	)
}

// cleanupStaleRunning 把残留的 running 状态（通常来自被 OOM/升级杀掉的旧实例）
// 标记为 failed，避免 /api/aggregate-status 永远显示 running，也避免误导排查。
// 只处理早于阈值且确实仍是 running 的行，不会影响当前实例正在跑的任务。
func (a *Aggregator) cleanupStaleRunning() {
	if a == nil || a.db == nil {
		return
	}
	cutoff := time.Now().Add(-staleRunningTimeout)
	res := a.db.Model(&model.APIDailyAggregateStatus{}).
		Where("status = ? AND (started_at IS NULL OR started_at < ?)", "running", cutoff).
		Updates(map[string]interface{}{
			"status":     "failed",
			"last_error": "interrupted: instance restarted while running",
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		log.Printf("aggregator: cleanup stale running failed: %v", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		log.Printf("aggregator: cleaned up %d stale running aggregate status", res.RowsAffected)
	}
}

func (a *Aggregator) isDateCompleted(date string) bool {
	var row model.APIDailyAggregateStatus
	if err := a.db.Where("aggregate_date = ?", parseAggregateDate(date)).First(&row).Error; err != nil {
		return false
	}
	if row.Status != "completed" {
		return false
	}
	targetDate := parseAggregateDate(date)
	if a.hasIncompleteSessionAggregates(targetDate) {
		log.Printf("aggregator: date=%s marked completed but still has incomplete session aggregates, will retry", targetDate.Format("2006-01-02"))
		return false
	}
	if row.FailCount > 0 {
		log.Printf("aggregator: date=%s marked completed but still has failed sessions fail_count=%d, will retry", targetDate.Format("2006-01-02"), row.FailCount)
		return false
	}
	return true
}

func isIncompleteAggregateCondition(db *gorm.DB) *gorm.DB {
	return db.Where(
		"trace_count = 0 AND turns = 0 AND duration_ms = 0 AND input_tokens = 0 AND output_tokens = 0 AND tool_calls = 0 AND COALESCE(bundle_json, '') IN ('', '{}')",
	)
}

func (a *Aggregator) hasIncompleteSessionAggregates(date time.Time) bool {
	if a == nil || a.db == nil {
		return false
	}
	var count int64
	if err := isIncompleteAggregateCondition(
		a.db.Model(&model.APISessionAggregate{}).Where("aggregate_date = ?", date),
	).Count(&count).Error; err != nil {
		log.Printf("aggregator: check incomplete aggregates failed date=%s err=%v", date.Format("2006-01-02"), err)
		return false
	}
	return count > 0
}

func (a *Aggregator) upsertDayStatus(date time.Time, status string, sessionCount, successCount, failCount, listTotal, retryCount int, lastErr string, startedAt, completedAt, lastErrorAt *time.Time, costMs int64, fetchConcurrency int) {
	row := model.APIDailyAggregateStatus{
		AggregateDate:    date,
		Status:           status,
		SessionCount:     sessionCount,
		SuccessCount:     successCount,
		FailCount:        failCount,
		RetryCount:       retryCount,
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
			"retry_count":        row.RetryCount,
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

func (a *Aggregator) upsertSessionAggregate(date time.Time, src model.StgSessionSource, bundle apiSessionBundle, m cachedMetrics, pubStatus string) error {
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
	updateColumns := []string{
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
		"has_issue",
		"artifact_publication_status",
		"chip",
		"rules_json",
		"features_json",
		"title",
		"trace_id",
		"source_create_at",
		"source_update_at",
		"trace_fingerprint",
		"aggregate_invalidated",
		"aggregate_invalidated_at",
		"aggregated_at",
		"updated_at",
	}
	traceFingerprint := computeQualityTraceFingerprint(bundle)
	row := model.APISessionAggregate{
		SessionID:                 trimDBString(src.SessionID, 128),
		ArtifactID:                trimDBString(src.ArtifactID, 64),
		AggregateDate:             date,
		UserID:                    trimDBString(src.UserID, 64),
		UserName:                  trimDBString(src.UserName, 128),
		StartedAtMs:               bundle.StartedAtMs,
		StartedAt:                 msToTimePtr(bundle.StartedAtMs),
		DurationMs:                m.DurationMs,
		TraceID:                   trimDBString(m.Trace, 128),
		Title:                     trimDBString(m.Title, sessionAggregateTitleMaxLen),
		Chip:                      trimDBString(m.Chip, 64),
		InputTokens:               m.InputTokens,
		OutputTokens:              m.OutputTokens,
		TotalTokens:               m.TotalTokens,
		AvgTokensPerTurn:          m.AvgTokensTurn,
		Turns:                     m.Turns,
		TraceCount:                m.TraceCount,
		ToolCalls:                 m.ToolCalls,
		UniqueTools:               m.UniqueTools,
		ToolFailures:              m.ToolFailures,
		ToolFailRateBP:            int(m.ToolFailRate * 10000),
		ToolRetries:               m.ToolRetries,
		MaxSerialRun:              m.MaxSerialRun,
		HasRootFail:               m.HasRootFail,
		HasLoop:                   m.HasLoop,
		HasFinalAnswer:            m.HasFinalAnswer,
		NoOpStreak:                m.NoOpStreak,
		Score:                     m.Score,
		ResponseScore:             m.ResponseScore,
		StabilityScore:            m.StabilityScore,
		ThinkingScore:             m.ThinkingScore,
		ResourceScore:             m.ResourceScore,
		OrchestrationScore:        m.OrchestrationScore,
		AbnormalLevel:             m.AbnormalLevel,
		HasIssue:                  aggregateIssueFlagForSession(a.db, src.SessionID, src.ArtifactID, rulesJSON),
		ArtifactPublicationStatus: trimDBString(normalizePublicationStatusOrUnknown(pubStatus), 16),
		RulesJSON:                 rulesJSON,
		FeaturesJSON:              featuresJSON,
		SourceCreateAt:            src.SourceCreatedAt,
		SourceUpdateAt:            src.SourceUpdatedAt,
		TraceFingerprint:          trimDBString(traceFingerprint, 64),
		AggregateInvalidated:      false,
		AggregateInvalidatedAt:    nil,
		AggregatedAt:              time.Now(),
	}
	tx := a.db
	if a.hasBundleJSONColumn() {
		row.BundleJSON = bundleJSON
		updateColumns = append(updateColumns, "bundle_json")
	} else {
		tx = tx.Omit("bundle_json")
	}
	if !apiSessionAggregateHasIssueColumn(a.db) {
		tx = tx.Omit("has_issue")
		updateColumns = removeString(updateColumns, "has_issue")
	}
	if !apiSessionAggregateHasPublicationStatusColumn(a.db) {
		tx = tx.Omit("artifact_publication_status")
		updateColumns = removeString(updateColumns, "artifact_publication_status")
	} else if row.ArtifactPublicationStatus == artifactStatusUnknown {
		// 防覆盖：本次无法判定发布状态时，新插入仍写 unknown，但绝不在 OnConflict 更新里
		// 用 unknown 把已校准的 published/unpublished 覆盖回 unknown（混合实时策略下尤为重要）。
		updateColumns = removeString(updateColumns, "artifact_publication_status")
	}
	if !apiSessionAggregateHasTraceFingerprintColumn(a.db) {
		tx = tx.Omit("trace_fingerprint")
		updateColumns = removeString(updateColumns, "trace_fingerprint")
	}
	if !apiSessionAggregateHasAggregateInvalidatedColumn(a.db) {
		tx = tx.Omit("aggregate_invalidated", "aggregate_invalidated_at")
		updateColumns = removeString(updateColumns, "aggregate_invalidated")
		updateColumns = removeString(updateColumns, "aggregate_invalidated_at")
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns(updateColumns),
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
	if !isSupportedSessionID(src.SessionID) {
		return nil
	}
	date := aggregateDateForBundle(src, bundle)
	m := extractCachedMetrics(bundle)
	if err := a.upsertSessionAggregate(date, src, bundle, m, bundle.ArtifactPublicationStatus); err != nil {
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

// aggregatedSessionIDs 返回当天已完整落库的 session_id 集合，供断点续补时跳过。
// 对明显未补齐的 0 行（trace/turns/duration/tokens/tool_calls 全 0 且无 bundle_json）不视为完成，
// 这样下次触发补库时会自动重刷，而不是永久卡在 0ms/0 turns。
func (a *Aggregator) aggregatedSessionIDs(date time.Time) map[string]struct{} {
	out := make(map[string]struct{})
	if a == nil || a.db == nil {
		return out
	}
	var ids []string
	q := a.db.Model(&model.APISessionAggregate{}).
		Where("aggregate_date = ?", date)
	if err := q.Where(
		"NOT (trace_count = 0 AND turns = 0 AND duration_ms = 0 AND input_tokens = 0 AND output_tokens = 0 AND tool_calls = 0 AND COALESCE(bundle_json, '') IN ('', '{}'))",
	).
		Pluck("session_id", &ids).Error; err != nil {
		log.Printf("aggregator: load aggregated session ids failed date=%s err=%v", date.Format("2006-01-02"), err)
		return out
	}
	for _, id := range ids {
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
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
		StartedAtMs:        row.StartedAtMs,
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

func normalizedDaysDesc(dates []string) []string {
	if len(dates) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(dates))
	uniq := make([]string, 0, len(dates))
	for _, d := range dates {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		uniq = append(uniq, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(uniq)))
	return uniq
}

func (a *Aggregator) hasBundleJSONColumn() bool {
	if a == nil || a.db == nil {
		return false
	}
	a.optionalColumnsOnce.Do(func() {
		exists := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("aggregator: detect optional column bundle_json panic: %v", r)
				}
			}()
			exists = a.db.Migrator().HasColumn(&model.APISessionAggregate{}, "bundle_json")
		}()
		a.bundleJSONColumnOK = exists
		if !exists {
			log.Printf("aggregator: optional column bundle_json missing, detail bundle cache write disabled")
		}
	})
	return a.bundleJSONColumnOK
}

func removeString(values []string, target string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
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

// applyCachedMetricsToBundle 把日聚合的指标 join 到 bundle 上，覆盖轻量列表字段。
// 起始时间以 JSONL 解析/聚合缓存为准；轻量列表里的文件名时间只作为聚合前兜底。
func applyCachedMetricsToBundle(b apiSessionBundle, m cachedMetrics) apiSessionBundle {
	if m.StartedAtMs > 0 {
		b.StartedAtMs = m.StartedAtMs
		b.StartedAt = msToString(m.StartedAtMs)
	}
	if m.DurationMs > 0 {
		b.DurationMs = m.DurationMs
	}
	b.InputTokens = m.InputTokens
	b.OutputTokens = m.OutputTokens
	b.ToolCalls = m.ToolCalls
	b.Turns = m.Turns
	b.EffectiveRounds = m.EffectiveRounds
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
		EffectiveRounds:  m.EffectiveRounds,
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
