package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

// listSessionBundlesAPI 走上游接口实时拉 session 列表（不落库）。
//
// 请求参数：
//   - limit / offset：分页（offset / limit 换算成 page_no / page_size）
//   - start_time / end_time：可选时间窗，格式 "YYYY-MM-DD HH:mm:ss"；缺省最近 7 天
//   - user_id / user_name / session_id / artifact_id：本地二次过滤（上游接口当前不支持服务端过滤）
//
// 列表项不实时拉 JSONL，仅返回元信息概览。当 aggregator 已对涉及日期完成预聚合时，
// 会把缓存指标 join 到 bundle 上（chip / token / 雷达指标全部填充）；缺失的日期
// 会用当前请求的 Cookie 异步触发后台聚合，不阻塞本次返回。
func (h *Handler) listSessionBundlesAPI(c *gin.Context) {
	if h.upstream == nil {
		fail(c, fmt.Errorf("upstream client not initialized"))
		return
	}
	limit, offset := bundlePaginationDefault(c, 50)
	pageSize := limit
	pageNo := offset/limit + 1

	tr := timeRangeFromQuery(c)
	cookie := c.GetHeader("Cookie")
	if trimmed := strings.TrimSpace(cookie); trimmed == "" {
		log.Printf("bundle list: missing cookie path=%s range=%s~%s", c.Request.URL.Path, tr.StartTime, tr.EndTime)
	} else {
		log.Printf("bundle list: cookie received len=%d path=%s range=%s~%s", len(trimmed), c.Request.URL.Path, tr.StartTime, tr.EndTime)
	}
	uid := c.Query("user_id")
	uname := c.Query("user_name")
	sid := c.Query("session_id")
	aid := c.Query("artifact_id")

	if bundles, total, ok, err := h.listSessionBundlesFromDB(tr, uid, uname, sid, aid, limit, offset); err != nil {
		fail(c, fmt.Errorf("db list session bundles: %w", err))
		return
	} else if ok {
		bundles = filterBundlesByQueryRange(bundles, tr)
		total = int64(len(bundles))
		if h.aggregator != nil {
			days := daysFromQueryRange(tr)
			if day, ok := mostRecentDay(days); ok {
				h.aggregator.EnsureDays(cookie, []string{day})
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"data":   bundles,
			"limit":  limit,
			"offset": offset,
			"total":  total,
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
		TimeRange: tr,
		Page:      modellog.Page{PageNo: pageNo, PageSize: pageSize},
	})
	if err != nil {
		fail(c, fmt.Errorf("upstream list: %w", err))
		return
	}

	bundles := make([]apiSessionBundle, 0, len(resp.Data))
	for _, s := range resp.Data {
		if uid != "" && s.UserID != uid {
			continue
		}
		if uname != "" && s.UserName != uname {
			continue
		}
		if sid != "" && s.SessionID != sid {
			continue
		}
		if aid != "" && s.ArtifactID != aid {
			continue
		}
		b := lightBundleFromAPI(s)
		if h.aggregator != nil {
			if m, ok := h.aggregator.Get(s.SessionID); ok {
				b = applyCachedMetricsToBundle(b, m)
			}
		}
		bundles = append(bundles, b)
	}
	bundles = filterBundlesByQueryRange(bundles, tr)

	// 异步触发缺失日期的聚合，但后端会强制收敛为最近 1 天，避免随查询窗口放大。
	if h.aggregator != nil {
		days := daysFromQueryRange(tr)
		if day, ok := mostRecentDay(days); ok {
			h.aggregator.EnsureDays(cookie, []string{day})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data":   bundles,
		"limit":  limit,
		"offset": offset,
		"total":  len(bundles),
	})
}

// getSessionBundleAPI 详情页：按 session_id / artifact_id 命中上游某条记录，
// 实时拉取 file_list[0] 的 JSONL 解析后返回完整 bundle。
//
// 由于上游接口不支持按 ID 直查，这里用同样的时间窗 + page_size 兜底拉一批，
// 在内存里 match。前端正常使用时间窗很紧（详情页在列表的同一时段内），命中率足够。
// 当 DB-backed aggregator 已启用时，会在成功解析后顺手把这条 session 写入聚合表。
func (h *Handler) getSessionBundleAPI(c *gin.Context) {
	if h.upstream == nil {
		fail(c, fmt.Errorf("upstream client not initialized"))
		return
	}
	key := c.Param("session_id")
	tr := timeRangeFromQuery(c)
	cookie := c.GetHeader("Cookie")
	cachedBundle, hasCached, err := h.getCachedSessionBundle(key)
	if err != nil {
		log.Printf("session detail cached lookup failed key=%s err=%v", key, err)
	}
	if hasCached && hasDetailTraces(cachedBundle) {
		// DB 已有完整 bundle 时直接返回，避免详情页再走一次最近 7 天的上游扫描。
		c.JSON(http.StatusOK, cachedBundle)
		return
	}

	// 指标秒出：前端两段式加载，第一段带 meta_only=1 只要 DB 里的指标骨架
	// （分数/tokens/雷达），立即渲染头部与卡片，不等下载解析大文件。
	// 第二段再请求完整 bundle 拉对话流（traces）。
	if isTruthy(c.Query("meta_only")) {
		if hasCached {
			c.JSON(http.StatusOK, cachedBundle)
			return
		}
		c.JSON(http.StatusNoContent, nil)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// 详情页用尽量窄的时间窗在上游定位这条 session，避免默认最近 7 天 ×500 的大扫描。
	// 优先用 query 里显式传的窗；没有就用缓存里这条 session 的起始时间 ±2h 收窄。
	detailTR := tr
	if c.Query("start_time") == "" && c.Query("end_time") == "" {
		if hasCached && cachedBundle.StartedAtMs > 0 {
			st := time.UnixMilli(cachedBundle.StartedAtMs).Add(-2 * time.Hour)
			et := time.UnixMilli(cachedBundle.StartedAtMs).Add(2 * time.Hour)
			detailTR = modellog.TimeRange{
				StartTime: st.Format("2006-01-02 15:04:05"),
				EndTime:   et.Format("2006-01-02 15:04:05"),
			}
		}
	}

	resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
		TimeRange: detailTR,
		Page:      modellog.Page{PageNo: 1, PageSize: 500},
	})
	if err != nil {
		if hasCached {
			c.JSON(http.StatusOK, cachedBundle)
			return
		}
		fail(c, fmt.Errorf("upstream list: %w", err))
		return
	}

	var hit *modellog.Session
	for i := range resp.Data {
		s := &resp.Data[i]
		if s.SessionID == key || s.ArtifactID == key {
			hit = s
			break
		}
	}
	if hit == nil {
		if hasCached {
			c.JSON(http.StatusOK, cachedBundle)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found in upstream window"})
		return
	}
	if len(hit.FileList) == 0 || hit.FileList[0].URL == "" {
		if hasCached {
			c.JSON(http.StatusOK, cachedBundle)
			return
		}
		c.JSON(http.StatusUnsupportedMediaType, gin.H{
			"error":      "no jsonl file in upstream record",
			"session_id": hit.SessionID,
		})
		return
	}

	pr, err := h.fetcher.FetchAndParse(hit.FileList[0].URL)
	if err != nil {
		if hasCached {
			c.JSON(http.StatusOK, cachedBundle)
			return
		}
		fail(c, fmt.Errorf("fetch jsonl: %w", err))
		return
	}
	src := sessionToStgSource(*hit)
	bundle := buildBundleFromTOS(src, pr)
	if hasCached {
		bundle = mergeBundleWithCachedBundle(bundle, cachedBundle)
	}
	// 写库异步化：详情解析完立即返回，写 DB 缓存放后台，不让用户为落库白等。
	if h.aggregator != nil {
		go func(src model.StgSessionSource, bundle apiSessionBundle) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("session detail persist panic session=%s: %v", src.SessionID, r)
				}
			}()
			if err := h.aggregator.PersistBundle(src, bundle); err != nil {
				log.Printf("session detail persist failed session=%s artifact=%s err=%v", src.SessionID, src.ArtifactID, err)
			}
		}(src, bundle)
	}
	c.JSON(http.StatusOK, bundle)
}

func (h *Handler) listSessionBundlesFromDB(tr modellog.TimeRange, uid, uname, sid, aid string, limit, offset int) ([]apiSessionBundle, int64, bool, error) {
	if h == nil || h.db == nil {
		return nil, 0, false, nil
	}
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return nil, 0, false, nil
	}
	startDate := startOfLocalDay(startAt)
	endDate := startOfLocalDay(endAt)
	q := h.db.Model(&model.APISessionAggregate{}).
		Where(
			"(started_at_ms BETWEEN ? AND ?) OR "+
				"(started_at_ms = 0 AND started_at BETWEEN ? AND ?) OR "+
				"(started_at_ms = 0 AND started_at IS NULL AND aggregate_date BETWEEN ? AND ?)",
			startAt.UnixMilli(), endAt.UnixMilli(), startAt, endAt, startDate, endDate,
		)
	if uid != "" {
		q = q.Where("user_id = ?", uid)
	}
	if uname != "" {
		q = q.Where("user_name = ?", uname)
	}
	if sid != "" {
		q = q.Where("session_id = ?", sid)
	}
	if aid != "" {
		q = q.Where("artifact_id = ?", aid)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, false, err
	}
	if total == 0 {
		return nil, 0, false, nil
	}
	var rows []model.APISessionAggregate
	if err := q.
		Select([]string{
			"id",
			"session_id",
			"artifact_id",
			"aggregate_date",
			"user_id",
			"user_name",
			"started_at_ms",
			"started_at",
			"duration_ms",
			"trace_id",
			"title",
			"chip",
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
			"score",
			"response_score",
			"stability_score",
			"thinking_score",
			"resource_score",
			"orchestration_score",
			"abnormal_level",
			"rules_json",
			"features_json",
			"created_at",
			"updated_at",
		}).
		Order("started_at_ms DESC, updated_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, false, err
	}
	bundles := make([]apiSessionBundle, 0, len(rows))
	for _, row := range rows {
		bundles = append(bundles, buildBundleFromAggregateRow(row))
	}
	return bundles, total, true, nil
}

func (h *Handler) getCachedSessionBundle(key string) (apiSessionBundle, bool, error) {
	if h == nil || h.db == nil || key == "" {
		return apiSessionBundle{}, false, nil
	}
	var row model.APISessionAggregate
	err := h.db.
		Where("session_id = ? OR artifact_id = ?", key, key).
		Order("updated_at DESC").
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return apiSessionBundle{}, false, nil
		}
		return apiSessionBundle{}, false, err
	}
	return buildDetailBundleFromAggregateRow(row), true, nil
}

func buildBundleFromAggregateRow(row model.APISessionAggregate) apiSessionBundle {
	rules := []apiRule{}
	if row.RulesJSON != "" {
		_ = json.Unmarshal([]byte(row.RulesJSON), &rules)
	}
	features := apiFeatures{
		ToolCalls:        row.ToolCalls,
		UniqueTools:      row.UniqueTools,
		MaxSerialRun:     row.MaxSerialRun,
		ToolFailures:     row.ToolFailures,
		ToolFailRate:     float64(row.ToolFailRateBP) / 10000,
		AvgTokensPerTurn: row.AvgTokensPerTurn,
		ToolRetries:      row.ToolRetries,
		HasRootFail:      row.HasRootFail,
		HasLoop:          row.HasLoop,
	}
	return apiSessionBundle{
		ID:           pickFirstNonEmpty(row.SessionID, row.ArtifactID),
		SessionID:    row.SessionID,
		ArtifactID:   row.ArtifactID,
		User:         row.UserName,
		UserID:       row.UserID,
		Title:        pickFirstNonEmpty(row.Title, "Session "+pickFirstNonEmpty(row.SessionID, row.ArtifactID)),
		Trace:        row.TraceID,
		StartedAtMs:  row.StartedAtMs,
		StartedAt:    msToString(row.StartedAtMs),
		DurationMs:   row.DurationMs,
		InputTokens:  row.InputTokens,
		OutputTokens: row.OutputTokens,
		ToolCalls:    row.ToolCalls,
		Turns:        row.Turns,
		TraceCount:   row.TraceCount,
		Score:        row.Score,
		Color:        "green",
		Chip:         row.Chip,
		Features:     features,
		Radar: apiRadar{
			Response:      row.ResponseScore,
			Stability:     row.StabilityScore,
			Thinking:      row.ThinkingScore,
			Resource:      row.ResourceScore,
			Orchestration: row.OrchestrationScore,
		},
		Rules:  rules,
		Traces: []apiTrace{},
	}
}

func buildDetailBundleFromAggregateRow(row model.APISessionAggregate) apiSessionBundle {
	cached := buildBundleFromAggregateRow(row)
	if row.BundleJSON == "" || row.BundleJSON == "{}" {
		return cached
	}
	var bundle apiSessionBundle
	if err := json.Unmarshal([]byte(row.BundleJSON), &bundle); err != nil {
		return cached
	}
	if bundle.ID == "" {
		bundle.ID = pickFirstNonEmpty(bundle.SessionID, bundle.ArtifactID, row.SessionID, row.ArtifactID)
	}
	return mergeBundleWithCachedBundle(bundle, cached)
}

func hasDetailTraces(bundle apiSessionBundle) bool {
	return len(bundle.Traces) > 0
}

func mergeBundleWithCachedBundle(bundle, cached apiSessionBundle) apiSessionBundle {
	if bundle.SessionID == "" {
		bundle.SessionID = cached.SessionID
	}
	if bundle.ArtifactID == "" {
		bundle.ArtifactID = cached.ArtifactID
	}
	if bundle.User == "" {
		bundle.User = cached.User
	}
	if bundle.UserID == "" {
		bundle.UserID = cached.UserID
	}
	if bundle.Title == "" {
		bundle.Title = cached.Title
	}
	if bundle.Trace == "" {
		bundle.Trace = cached.Trace
	}
	if bundle.StartedAtMs == 0 {
		bundle.StartedAtMs = cached.StartedAtMs
		bundle.StartedAt = cached.StartedAt
	}
	if bundle.DurationMs == 0 {
		bundle.DurationMs = cached.DurationMs
	}
	if bundle.InputTokens == 0 {
		bundle.InputTokens = cached.InputTokens
	}
	if bundle.OutputTokens == 0 {
		bundle.OutputTokens = cached.OutputTokens
	}
	if bundle.ToolCalls == 0 {
		bundle.ToolCalls = cached.ToolCalls
	}
	if bundle.Turns == 0 {
		bundle.Turns = cached.Turns
	}
	if bundle.TraceCount == 0 {
		bundle.TraceCount = cached.TraceCount
	}
	if bundle.Score == 0 {
		bundle.Score = cached.Score
	}
	if bundle.Chip == "" {
		bundle.Chip = cached.Chip
	}
	if bundle.Features == (apiFeatures{}) {
		bundle.Features = cached.Features
	}
	if bundle.Radar == (apiRadar{}) {
		bundle.Radar = cached.Radar
	}
	if len(bundle.Rules) == 0 {
		bundle.Rules = cached.Rules
	}
	if len(bundle.Traces) == 0 {
		bundle.Traces = cached.Traces
	}
	return bundle
}

func parseTimeRangeBounds(tr modellog.TimeRange) (time.Time, time.Time, bool) {
	st, err1 := time.ParseInLocation("2006-01-02 15:04:05", tr.StartTime, time.Local)
	et, err2 := time.ParseInLocation("2006-01-02 15:04:05", tr.EndTime, time.Local)
	if err1 != nil || err2 != nil || et.Before(st) {
		return time.Time{}, time.Time{}, false
	}
	return st, et, true
}

func filterBundlesByQueryRange(bundles []apiSessionBundle, tr modellog.TimeRange) []apiSessionBundle {
	st, et, ok := parseTimeRangeBounds(tr)
	if !ok || len(bundles) == 0 {
		return bundles
	}
	startMs, endMs := st.UnixMilli(), et.UnixMilli()
	out := bundles[:0]
	for _, b := range bundles {
		if b.StartedAtMs >= startMs && b.StartedAtMs <= endMs {
			out = append(out, b)
		}
	}
	return out
}

// timeRangeFromQuery 从 query 解析时间窗，缺省最近 7 天。
// 格式严格 "YYYY-MM-DD HH:mm:ss"，前端可任意精度，由 sanitize 补齐。
func timeRangeFromQuery(c *gin.Context) modellog.TimeRange {
	now := time.Now()
	defaultEnd := now.Format("2006-01-02 15:04:05")
	defaultStart := now.AddDate(0, 0, -7).Format("2006-01-02 15:04:05")

	st := c.Query("start_time")
	if st == "" {
		st = defaultStart
	}
	et := c.Query("end_time")
	if et == "" {
		et = defaultEnd
	}
	return modellog.TimeRange{StartTime: st, EndTime: et}
}

// sessionToStgSource 把上游 Session 转成 buildBundleFromTOS 需要的 StgSessionSource 伪造实例。
// 仅 api 模式临时使用，不入库。
func sessionToStgSource(s modellog.Session) model.StgSessionSource {
	src := model.StgSessionSource{
		ArtifactID: s.ArtifactID,
		SessionID:  s.SessionID,
		UserID:     s.UserID,
		UserName:   s.UserName,
		ObjFormat:  "jsonl",
	}
	if len(s.FileList) > 0 {
		src.ObjURL = s.FileList[0].URL
	}
	if t := parseUpstreamTime(s.CreateAt); !t.IsZero() {
		src.SourceCreatedAt = &t
	}
	if t := parseUpstreamTime(s.UpdateAt); !t.IsZero() {
		src.SourceUpdatedAt = &t
	}
	return src
}

// lightBundleFromAPI 列表项概览：与 lightBundleFromSource 等价，只是字段来源不同。
//
// started_at_ms 三级兜底：
//  1. 上游 create_at（最准确）
//  2. session_id 自带时间戳（OpenCode 之外的格式如 20260608_095347_*）
//  3. 0（让前端按"未知时间"渲染）
func lightBundleFromAPI(s modellog.Session) apiSessionBundle {
	startedAt := parseUpstreamTime(s.CreateAt)
	endedAt := parseUpstreamTime(s.UpdateAt)
	startedMs, endedMs := int64(0), int64(0)
	if !startedAt.IsZero() {
		startedMs = startedAt.UnixMilli()
	}
	if !endedAt.IsZero() {
		endedMs = endedAt.UnixMilli()
	}
	if startedMs == 0 {
		if ts := parseSessionIDTimestamp(s.SessionID); ts > 0 {
			startedMs = ts
		}
	}
	dur := endedMs - startedMs
	if dur < 0 {
		dur = 0
	}
	id := pickFirstNonEmpty(s.SessionID, s.ArtifactID)
	return apiSessionBundle{
		ID:          id,
		SessionID:   s.SessionID,
		ArtifactID:  s.ArtifactID,
		User:        s.UserName,
		UserID:      s.UserID,
		Title:       "Session " + id,
		Trace:       "",
		StartedAtMs: startedMs,
		StartedAt:   msToString(startedMs),
		DurationMs:  dur,
		TraceCount:  0,
		Color:       "green",
		Traces:      []apiTrace{},
		Rules:       []apiRule{},
	}
}

// daysFromQueryRange 从上游 TimeRange（"YYYY-MM-DD HH:mm:ss"）解析出涉及的自然日。
// 解析失败返回今天单日，避免空触发。
func daysFromQueryRange(tr modellog.TimeRange) []string {
	st, err1 := time.ParseInLocation("2006-01-02 15:04:05", tr.StartTime, time.Local)
	et, err2 := time.ParseInLocation("2006-01-02 15:04:05", tr.EndTime, time.Local)
	if err1 != nil || err2 != nil || et.Before(st) {
		return []string{time.Now().Format("2006-01-02")}
	}
	return rangeDays(st.UnixMilli(), et.UnixMilli())
}

// parseUpstreamTime 兼容上游可能的时间格式：RFC3339 / "YYYY-MM-DD HH:mm:ss" / 毫秒时间戳字符串。
func parseUpstreamTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
		if v > 1e12 { // 毫秒
			return time.UnixMilli(v)
		}
		return time.Unix(v, 0)
	}
	return time.Time{}
}
