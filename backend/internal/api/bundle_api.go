package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

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

	uid := c.Query("user_id")
	uname := c.Query("user_name")
	sid := c.Query("session_id")
	aid := c.Query("artifact_id")

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
		"total":  resp.Total,
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
		TimeRange: tr,
		Page:      modellog.Page{PageNo: 1, PageSize: 500},
	})
	if err != nil {
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
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found in upstream window"})
		return
	}
	if len(hit.FileList) == 0 || hit.FileList[0].URL == "" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{
			"error":      "no jsonl file in upstream record",
			"session_id": hit.SessionID,
		})
		return
	}

	pr, err := h.fetcher.FetchAndParse(hit.FileList[0].URL)
	if err != nil {
		fail(c, fmt.Errorf("fetch jsonl: %w", err))
		return
	}
	src := sessionToStgSource(*hit)
	bundle := buildBundleFromTOS(src, pr)
	if h.aggregator != nil {
		if err := h.aggregator.PersistBundle(src, bundle); err != nil {
			log.Printf("session detail persist failed session=%s artifact=%s err=%v", src.SessionID, src.ArtifactID, err)
		}
	}
	c.JSON(http.StatusOK, bundle)
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
