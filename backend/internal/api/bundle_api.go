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

// 产物发布状态：平台只纳入未发布 template 产物。
const (
	artifactStatusPublished    = "published"
	artifactStatusUnpublished  = "unpublished"
	artifactStatusUnknown      = "unknown"
	currentDetailBundleVersion = 2
	detailLookupHalfWindow     = 2 * time.Hour
)

// listReadTimeout 限定列表/大盘等读路径单次 DB 查询的最长等待时间。
// DB 慢查询或锁等待时快速失败，返回明确错误而非让 HTTP 请求永久 pending。
const listReadTimeout = 4 * time.Second

func normalizeArtifactStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case artifactStatusPublished:
		return artifactStatusPublished
	case artifactStatusUnpublished:
		return artifactStatusUnpublished
	default:
		return ""
	}
}

// normalizePublicationStatusOrUnknown 把任意输入规整成 published/unpublished/unknown 三态之一，
// 供聚合落库使用：无法判定时落 unknown，绝不写空串。
func normalizePublicationStatusOrUnknown(raw string) string {
	if s := normalizeArtifactStatus(raw); s != "" {
		return s
	}
	return artifactStatusUnknown
}

// bundlePublicationStatusFromStored 把聚合表里存的三态值映射成 bundle 对外暴露的口径：
// published/unpublished 原样透出，unknown（或历史空值）映射为空串，前端据此显示“未知/待校准”。
func bundlePublicationStatusFromStored(stored string) string {
	return normalizeArtifactStatus(stored)
}

func detailBundleNeedsRefresh(bundle apiSessionBundle) bool {
	if len(bundle.Traces) == 0 {
		return false
	}
	return bundle.DetailVersion < currentDetailBundleVersion
}

func detailBundleNeedsSourceRefresh(bundle apiSessionBundle, hit *modellog.Session) bool {
	if hit == nil || len(bundle.Traces) == 0 {
		return false
	}
	updatedAt := parseUpstreamTime(hit.UpdateAt)
	if updatedAt.IsZero() {
		updatedAt = parseUpstreamFileTimestamp(*hit)
	}
	if updatedAt.IsZero() {
		return false
	}
	return bundle.SourceUpdatedAtMs < updatedAt.UnixMilli()
}

func detailLookupTimeRange(c *gin.Context, tr modellog.TimeRange, cachedBundle apiSessionBundle, hasCached bool) modellog.TimeRange {
	if !hasCached || cachedBundle.StartedAtMs <= 0 {
		return tr
	}

	sessionAt := time.UnixMilli(cachedBundle.StartedAtMs)
	narrowTR := modellog.TimeRange{
		StartTime: sessionAt.Add(-detailLookupHalfWindow).Format("2006-01-02 15:04:05"),
		EndTime:   sessionAt.Add(detailLookupHalfWindow).Format("2006-01-02 15:04:05"),
	}
	if c.Query("start_time") == "" && c.Query("end_time") == "" {
		return narrowTR
	}

	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return narrowTR
	}
	if sessionAt.Before(startAt) || sessionAt.After(endAt) {
		return tr
	}
	if endAt.Sub(startAt) <= 2*detailLookupHalfWindow {
		return tr
	}
	return narrowTR
}

func (h *Handler) resolveSessionPublicationStatus(ctx context.Context, key, cookie string, tr modellog.TimeRange, statusHint string) (*modellog.Session, string, error) {
	if h == nil || h.upstream == nil || key == "" {
		return nil, "", nil
	}
	appendAttempt := func(dst []struct {
		onlyUnpublished bool
		label           string
	}, onlyUnpublished bool, label string) []struct {
		onlyUnpublished bool
		label           string
	} {
		for _, item := range dst {
			if item.label == label {
				return dst
			}
		}
		return append(dst, struct {
			onlyUnpublished bool
			label           string
		}{onlyUnpublished: onlyUnpublished, label: label})
	}
	attempts := make([]struct {
		onlyUnpublished bool
		label           string
	}, 0, 2)
	switch normalizeArtifactStatus(statusHint) {
	case artifactStatusUnpublished:
		attempts = appendAttempt(attempts, true, artifactStatusUnpublished)
	case artifactStatusPublished:
		attempts = appendAttempt(attempts, false, artifactStatusPublished)
	}
	attempts = appendAttempt(attempts, true, artifactStatusUnpublished)
	attempts = appendAttempt(attempts, false, artifactStatusPublished)
	var lastErr error
	for _, attempt := range attempts {
		const pageSize = 500
		const maxPages = 6
		for pageNo := 1; pageNo <= maxPages; pageNo++ {
			resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
				TimeRange:                tr,
				Page:                     modellog.Page{PageNo: pageNo, PageSize: pageSize},
				OnlyUnpublishedArtifacts: attempt.onlyUnpublished,
			})
			if err != nil {
				lastErr = err
				break
			}
			for i := range resp.Data {
				s := &resp.Data[i]
				if s.SessionID == key || s.ArtifactID == key {
					return s, attempt.label, nil
				}
			}
			total := int(resp.Total)
			if len(resp.Data) < pageSize || total <= pageNo*pageSize {
				break
			}
		}
	}
	return nil, "", lastErr
}

func bundleIdentityKey(sessionID, artifactID string) string {
	sessionID = strings.TrimSpace(sessionID)
	artifactID = strings.TrimSpace(artifactID)
	switch {
	case sessionID != "" && artifactID != "":
		return sessionID + "::" + artifactID
	case sessionID != "":
		return "session::" + sessionID
	case artifactID != "":
		return "artifact::" + artifactID
	default:
		return ""
	}
}

func isUpstreamAuthMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sid is empty") || strings.Contains(msg, "unauthorized")
}

// listSessionBundlesAPI 走上游接口实时拉 session 列表（不落库）。
//
// 请求参数：
//   - limit / offset：分页（offset / limit 换算成 page_no / page_size）
//   - start_time / end_time：可选时间窗，格式 "YYYY-MM-DD HH:mm:ss"；缺省最近 7 天
//   - user_id / user_name / session_id / artifact_id：本地二次过滤（上游接口当前不支持服务端过滤）
//   - artifact_status：兼容旧参数但会被忽略，接口同时返回 published + unpublished sessions。
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
	cookie := h.effectiveCookie(c)
	uid := c.Query("user_id")
	uname := c.Query("user_name")
	sid := c.Query("session_id")
	aid := c.Query("artifact_id")
	respondWithCachedAggregates := func(reason string) bool {
		dbCtx, dbCancel := context.WithTimeout(c.Request.Context(), listReadTimeout)
		defer dbCancel()
		bundles, total, ok, err := h.listSessionBundlesFromDB(dbCtx, tr, uid, uname, sid, aid, limit, offset)
		if err != nil {
			fail(c, fmt.Errorf("list cached session bundles: %w", err))
			return true
		}
		if !ok {
			return false
		}
		log.Printf("bundle list: fallback to cached aggregates reason=%s rows=%d total=%d range=%s~%s", reason, len(bundles), total, tr.StartTime, tr.EndTime)
		bundles = h.applyQualityEvaluations(bundles)
		c.JSON(http.StatusOK, gin.H{
			"data":   bundles,
			"limit":  limit,
			"offset": offset,
			"total":  total,
			"source": "cached_aggregates",
		})
		return true
	}

	if trimmed := strings.TrimSpace(cookie); trimmed == "" {
		log.Printf("bundle list: missing cookie path=%s range=%s~%s", c.Request.URL.Path, tr.StartTime, tr.EndTime)
		if respondWithCachedAggregates("missing_cookie") {
			return
		}
	} else {
		log.Printf("bundle list: cookie received len=%d path=%s range=%s~%s", len(trimmed), c.Request.URL.Path, tr.StartTime, tr.EndTime)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	type listBundleItem struct {
		bundle        apiSessionBundle
		recencyMillis int64
	}

	bundles := make([]apiSessionBundle, 0)

	collectItems := func(resp *modellog.ListResponse, status string) []listBundleItem {
		if resp == nil {
			return nil
		}
		items := make([]listBundleItem, 0, len(resp.Data))
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
			bundle := lightBundleFromAPI(s)
			bundle.ArtifactPublicationStatus = status
			if h.aggregator != nil {
				if m, ok := h.aggregator.Get(s.SessionID); ok {
					bundle = applyCachedMetricsToBundle(bundle, m)
					bundle.ArtifactPublicationStatus = status
				}
			}
			updatedAtMs := parseUpstreamTime(s.UpdateAt).UnixMilli()
			createdAtMs := parseUpstreamTime(s.CreateAt).UnixMilli()
			recencyMillis := updatedAtMs
			if recencyMillis <= 0 {
				recencyMillis = createdAtMs
			}
			if recencyMillis <= 0 {
				recencyMillis = bundle.StartedAtMs
			}
			items = append(items, listBundleItem{
				bundle:        bundle,
				recencyMillis: recencyMillis,
			})
		}
		return items
	}

	bundleMetaByKey := make(map[string]listBundleItem)
	appendUniqueBundles := func(items []listBundleItem) {
		indexByKey := make(map[string]int, len(bundles)+len(items))
		for i, bundle := range bundles {
			key := bundleIdentityKey(bundle.SessionID, bundle.ArtifactID)
			if key != "" {
				indexByKey[key] = i
			}
		}
		for _, item := range items {
			key := bundleIdentityKey(item.bundle.SessionID, item.bundle.ArtifactID)
			if key == "" {
				bundles = append(bundles, item.bundle)
				continue
			}
			if idx, ok := indexByKey[key]; ok {
				prevMeta := bundleMetaByKey[key]
				if item.recencyMillis > prevMeta.recencyMillis {
					bundles[idx] = item.bundle
					bundleMetaByKey[key] = item
				}
				continue
			}
			indexByKey[key] = len(bundles)
			bundleMetaByKey[key] = item
			bundles = append(bundles, item.bundle)
		}
	}

	type listAttempt struct {
		status          string
		onlyUnpublished bool
	}
	attempts := []listAttempt{
		{status: artifactStatusUnpublished, onlyUnpublished: true},
		{status: artifactStatusPublished, onlyUnpublished: false},
	}
	reportedTotal := 0
	for _, attempt := range attempts {
		resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
			TimeRange:                tr,
			Page:                     modellog.Page{PageNo: pageNo, PageSize: pageSize},
			OnlyUnpublishedArtifacts: attempt.onlyUnpublished,
		})
		if err != nil {
			reason := fmt.Sprintf("upstream_list_%s_failed", attempt.status)
			if isUpstreamAuthMissing(err) {
				reason = "upstream_auth_missing"
			}
			log.Printf("bundle list: upstream list failed status=%s err=%v", attempt.status, err)
			if respondWithCachedAggregates(reason) {
				return
			}
			fail(c, fmt.Errorf("upstream list %s: %w", attempt.status, err))
			return
		}
		appendUniqueBundles(collectItems(resp, attempt.status))
		if resp != nil {
			reportedTotal += int(resp.Total)
		}
	}

	bundles = filterBundlesByQueryRange(bundles, tr)
	bundles = h.applyQualityEvaluations(bundles)

	// 异步触发缺失日期的聚合，但后端会强制收敛为最近少量未完成日期，避免随查询窗口放大。
	if h.aggregator != nil {
		h.aggregator.EnsureDays(cookie, daysFromQueryRange(tr))
	}

	if reportedTotal < len(bundles) {
		reportedTotal = len(bundles)
	}

	c.JSON(http.StatusOK, gin.H{
		"data":   bundles,
		"limit":  limit,
		"offset": offset,
		"total":  reportedTotal,
	})
}

// listSessionBundlesDBFirst 列表主路径：只读 DB，保证快速、可超时、不悬挂。
//
// 读路径不做任何上游调用或同步补库——补库（EnsureDays）放到后台 goroutine，
// 即便它内部要查“当天是否已聚合”也不会拖慢本次响应。DB 查询带 listReadTimeout，
// 超时立刻返回明确错误，由前端展示“加载失败”而非永久 pending。
func (h *Handler) listSessionBundlesDBFirst(c *gin.Context) {
	tr := timeRangeFromQuery(c)
	limit, offset := bundlePaginationDefault(c, 50)
	uid := c.Query("user_id")
	uname := c.Query("user_name")
	sid := c.Query("session_id")
	aid := c.Query("artifact_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), listReadTimeout)
	defer cancel()

	bundles, total, ok, err := h.listSessionBundlesFromDB(ctx, tr, uid, uname, sid, aid, limit, offset)
	if err != nil {
		fail(c, fmt.Errorf("list session bundles from db: %w", err))
		return
	}
	if !ok {
		if h.upstream != nil {
			h.listSessionBundlesAPI(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data":   []apiSessionBundle{},
			"limit":  limit,
			"offset": offset,
			"total":  0,
			"source": "db_aggregates",
		})
		return
	}
	// 补库异步触发：从请求上下文取出 Cookie 后丢给后台，绝不阻塞列表响应。
	if h.aggregator != nil {
		cookie := h.effectiveCookie(c)
		days := daysFromQueryRange(tr)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("list ensure days panic: %v", r)
				}
			}()
			h.aggregator.EnsureDays(cookie, days)
		}()
	}
	bundles = h.applyQualityEvaluations(bundles)
	c.JSON(http.StatusOK, gin.H{
		"data":   bundles,
		"limit":  limit,
		"offset": offset,
		"total":  total,
		"source": "db_aggregates",
	})
}

func normalizeBundleListTotal(reportedTotal, loaded, limit, offset int) int {
	if loaded < 0 {
		loaded = 0
	}
	if limit <= 0 {
		limit = loaded
	}
	minVisibleTotal := offset + loaded
	if loaded < limit {
		return minVisibleTotal
	}
	if reportedTotal < minVisibleTotal {
		return minVisibleTotal
	}
	return reportedTotal
}

// getSessionBundleAPI 详情页：优先按 session_id / artifact_id 直查本地 stg_session_sources 索引，
// 命中时实时拉取 obj_url JSONL；未命中时再回退到上游列表扫描。
//
// 由于上游接口不支持按 ID 直查，回退路径仍使用时间窗 + page_size 拉一批再内存匹配。
// 当 DB-backed aggregator 已启用时，会在成功解析后顺手把这条 session 写入聚合表。
func (h *Handler) getSessionBundleAPI(c *gin.Context) {
	if h.upstream == nil {
		fail(c, fmt.Errorf("upstream client not initialized"))
		return
	}
	key := c.Param("session_id")
	tr := timeRangeFromQuery(c)
	cookie := h.effectiveCookie(c)
	statusHint := normalizeArtifactStatus(c.Query("artifact_status"))
	cachedBundle, hasCached, err := h.getCachedSessionBundle(key, "")
	if err != nil {
		log.Printf("session detail cached lookup failed key=%s err=%v", key, err)
	}
	if statusHint != "" {
		cachedBundle.ArtifactPublicationStatus = statusHint
	}
	detailTR := detailLookupTimeRange(c, tr, cachedBundle, hasCached)

	// 指标秒出：前端两段式加载，第一段带 meta_only=1 只要 DB 里的指标骨架
	// （分数/tokens/雷达），立即渲染头部与卡片，不等下载解析大文件。
	// 第二段再请求完整 bundle 拉对话流（traces）。
	if isTruthy(c.Query("meta_only")) {
		if hasCached {
			c.JSON(http.StatusOK, h.applyQualityEvaluationLight(cachedBundle))
			return
		}
		c.JSON(http.StatusNoContent, nil)
		return
	}

	if indexedBundle, indexedSrc, ok, err := h.getIndexedSessionBundle(key, statusHint); err != nil {
		log.Printf("session detail indexed lookup failed key=%s err=%v", key, err)
	} else if ok {
		if hasCached {
			indexedBundle = mergeBundleWithCachedBundle(indexedBundle, cachedBundle)
		}
		if h.aggregator != nil {
			go func(src model.StgSessionSource, bundle apiSessionBundle) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("session detail indexed persist panic session=%s: %v", src.SessionID, r)
					}
				}()
				if !isSupportedSessionID(src.SessionID) {
					return
				}
				if err := h.aggregator.PersistBundle(src, bundle); err != nil {
					log.Printf("session detail indexed persist failed session=%s artifact=%s err=%v", src.SessionID, src.ArtifactID, err)
				}
			}(indexedSrc, indexedBundle)
		}
		c.JSON(http.StatusOK, h.applyQualityEvaluation(indexedBundle))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	hit, hitStatus, resolveErr := h.resolveSessionPublicationStatus(ctx, key, cookie, detailTR, statusHint)

	if hitStatus != "" {
		cachedBundle.ArtifactPublicationStatus = hitStatus
	}
	if hasCached && !cachedBundle.AggregateInvalidated && hasDetailTraces(cachedBundle) &&
		!detailBundleNeedsRefresh(cachedBundle) &&
		!detailBundleNeedsSourceRefresh(cachedBundle, hit) {
		// DB 已有完整 bundle 时仍要实时刷新发布状态，避免详情页命中旧缓存。
		c.JSON(http.StatusOK, h.applyQualityEvaluation(cachedBundle))
		return
	}

	if hit == nil && resolveErr != nil {
		if hasCached {
			c.JSON(http.StatusOK, h.applyQualityEvaluation(cachedBundle))
			return
		}
		fail(c, fmt.Errorf("upstream list: %w", resolveErr))
		return
	}
	if hit == nil {
		if hasCached {
			c.JSON(http.StatusOK, h.applyQualityEvaluation(cachedBundle))
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found in upstream window"})
		return
	}
	if len(hit.FileList) == 0 || hit.FileList[0].URL == "" {
		if hasCached {
			c.JSON(http.StatusOK, h.applyQualityEvaluation(cachedBundle))
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
			c.JSON(http.StatusOK, h.applyQualityEvaluation(cachedBundle))
			return
		}
		fail(c, fmt.Errorf("fetch jsonl: %w", err))
		return
	}
	src := sessionToStgSource(*hit)
	bundle := buildBundleFromTOS(src, pr)
	bundle.ArtifactPublicationStatus = hitStatus
	if hasCached {
		bundle = mergeBundleWithCachedBundle(bundle, cachedBundle)
		bundle.ArtifactPublicationStatus = hitStatus
	}
	if realTraceID, err := h.lookupRealTraceID(bundle.SessionID, bundle.ArtifactID); err != nil {
		log.Printf("session detail real trace lookup failed session=%s artifact=%s err=%v", bundle.SessionID, bundle.ArtifactID, err)
	} else if realTraceID != "" {
		bundle.Trace = realTraceID
	}
	// 写库异步化：详情解析完立即返回，写 DB 缓存放后台，不让用户为落库白等。
	if h.aggregator != nil {
		go func(src model.StgSessionSource, bundle apiSessionBundle) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("session detail persist panic session=%s: %v", src.SessionID, r)
				}
			}()
			if !isSupportedSessionID(src.SessionID) {
				return
			}
			if err := h.aggregator.PersistBundle(src, bundle); err != nil {
				log.Printf("session detail persist failed session=%s artifact=%s err=%v", src.SessionID, src.ArtifactID, err)
			}
		}(src, bundle)
	}
	c.JSON(http.StatusOK, h.applyQualityEvaluation(bundle))
}

// listSessionBundlesFromDB 列表读路径：必须保证“快、走索引、不挂死”。
//
// 关键设计（避免页面长时间 pending）：
//  1. session_id 前缀过滤用 LIKE 'ses\_%' 而非 LEFT(session_id,4)，
//     不再对列套函数，保留 session_id 索引可用性，仅作残差判断。
//  2. 时间窗收敛为单列 started_at_ms BETWEEN，配合 ORDER BY started_at_ms DESC
//     直接走 idx_started_at_ms 做有序范围扫描，无 filesort、无跨列 OR 全表扫。
//     started_at_ms=0 的“未知时间”行本就无法落到时间窗内，不在列表展示更符合口径。
//  3. 不做全表 Count()：多取 1 条（limit+1）判断是否被截断，total 从
//     api_daily_summary 的日汇总累加（O(天数)），既便宜又能给出有意义的提示。
//  4. ctx 带超时：DB 慢/锁等待时快速失败返回明确错误，绝不让 HTTP 请求悬挂。
func (h *Handler) listSessionBundlesFromDB(ctx context.Context, tr modellog.TimeRange, uid, uname, sid, aid string, limit, offset int) ([]apiSessionBundle, int64, bool, error) {
	q, startAt, endAt, ok := h.sessionAggregateRangeQuery(ctx, tr, sessionAggregateQueryFilters{
		UserID:     uid,
		UserName:   uname,
		SessionID:  sid,
		ArtifactID: aid,
	})
	if !ok {
		return nil, 0, false, nil
	}
	var rows []model.APISessionAggregate
	cols := dashboardAggregateRowSelectColumns()
	if !apiSessionAggregateHasIssueColumn(h.db) {
		cols = removeString(cols, "has_issue")
	}
	if err := q.
		Select(cols).
		Order("started_at_ms DESC, id DESC").Limit(limit + 1).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, false, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	bundles := make([]apiSessionBundle, 0, len(rows))
	for _, row := range rows {
		bundles = append(bundles, buildBundleFromAggregateRow(row))
	}
	total := offset + len(bundles)
	if truncated {
		// 还有更多数据：优先用日汇总给出范围内真实总量，拿不到再退化为“至少 +1”。
		if summed := h.countSessionsFromDailySummary(ctx, startAt, endAt); summed > total {
			total = summed
		} else if total <= offset+limit {
			total = offset + limit + 1
		}
	}
	return bundles, int64(total), true, nil
}

type sessionAggregateQueryFilters struct {
	UserID       string
	UserName     string
	SessionID    string
	ArtifactID   string
	HasIssueOnly bool
	// ArtifactStatus 限定发布状态：published / unpublished；空串表示不过滤（全部）。
	// 仅当聚合表已具备 artifact_publication_status 列时生效，否则静默忽略以优雅降级。
	ArtifactStatus string
}

func (h *Handler) sessionAggregateRangeQuery(ctx context.Context, tr modellog.TimeRange, filters sessionAggregateQueryFilters) (*gorm.DB, time.Time, time.Time, bool) {
	if h == nil || h.db == nil {
		return nil, time.Time{}, time.Time{}, false
	}
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return nil, time.Time{}, time.Time{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	q := h.db.WithContext(ctx).Model(&model.APISessionAggregate{}).
		Where("session_id LIKE ?", "ses\\_%").
		Where("started_at_ms BETWEEN ? AND ?", startAt.UnixMilli(), endAt.UnixMilli())
	if filters.UserID != "" {
		q = q.Where("user_id = ?", filters.UserID)
	}
	if filters.UserName != "" {
		q = q.Where("user_name = ?", filters.UserName)
	}
	if filters.SessionID != "" {
		q = q.Where("session_id = ?", filters.SessionID)
	}
	if filters.ArtifactID != "" {
		q = q.Where("artifact_id = ?", filters.ArtifactID)
	}
	if filters.HasIssueOnly && apiSessionAggregateHasIssueColumn(h.db) {
		q = q.Where("has_issue = ?", true)
	}
	if status := normalizeArtifactStatus(filters.ArtifactStatus); status != "" && apiSessionAggregateHasPublicationStatusColumn(h.db) {
		q = q.Where("artifact_publication_status = ?", status)
	}
	return q, startAt, endAt, true
}

// countSessionsFromDailySummary 用 api_daily_summary 的日级 session_count 累加，
// 给列表截断提示一个范围内的近似总量。仅扫天级聚合，命中 uk_aggregate_date，开销极小。
func (h *Handler) countSessionsFromDailySummary(ctx context.Context, startAt, endAt time.Time) int {
	if h == nil || h.db == nil {
		return 0
	}
	startDate := startOfLocalDay(startAt)
	endDate := startOfLocalDay(endAt)
	var total int64
	if err := h.db.WithContext(ctx).Model(&model.APIDailySummary{}).
		Where("aggregate_date BETWEEN ? AND ?", startDate, endDate).
		Select("COALESCE(SUM(session_count), 0)").
		Scan(&total).Error; err != nil {
		return 0
	}
	return int(total)
}

func (h *Handler) getCachedSessionBundle(key, statusHint string) (apiSessionBundle, bool, error) {
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
	bundle := buildDetailBundleFromAggregateRow(row)
	if statusHint == artifactStatusUnpublished {
		bundle.ArtifactPublicationStatus = statusHint
	}
	if realTraceID, err := h.lookupRealTraceID(row.SessionID, row.ArtifactID); err != nil {
		log.Printf("session detail cached real trace lookup failed session=%s artifact=%s err=%v", row.SessionID, row.ArtifactID, err)
	} else if realTraceID != "" {
		bundle.Trace = realTraceID
	}
	return bundle, true, nil
}

func (h *Handler) getIndexedSessionBundle(key, statusHint string) (apiSessionBundle, model.StgSessionSource, bool, error) {
	if h == nil || h.db == nil || h.fetcher == nil || key == "" {
		return apiSessionBundle{}, model.StgSessionSource{}, false, nil
	}
	var src model.StgSessionSource
	err := h.db.
		Where("session_id = ? OR artifact_id = ?", key, key).
		Order("source_updated_at DESC, id DESC").
		First(&src).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return apiSessionBundle{}, model.StgSessionSource{}, false, nil
		}
		return apiSessionBundle{}, model.StgSessionSource{}, false, err
	}
	if src.ObjFormat != "jsonl" || src.ObjURL == "" {
		return apiSessionBundle{}, src, false, fmt.Errorf("indexed source invalid obj_format=%s obj_url_empty=%t", src.ObjFormat, src.ObjURL == "")
	}
	pr, err := h.fetcher.FetchAndParse(src.ObjURL)
	if err != nil {
		return apiSessionBundle{}, src, false, fmt.Errorf("fetch indexed jsonl: %w", err)
	}
	bundle := buildBundleFromTOS(src, pr)
	if statusHint != "" {
		bundle.ArtifactPublicationStatus = statusHint
	}
	if realTraceID, err := h.lookupRealTraceID(bundle.SessionID, bundle.ArtifactID); err != nil {
		log.Printf("session detail indexed real trace lookup failed session=%s artifact=%s err=%v", bundle.SessionID, bundle.ArtifactID, err)
	} else if realTraceID != "" {
		bundle.Trace = realTraceID
	}
	return bundle, src, true, nil
}

func (h *Handler) lookupRealTraceID(sessionID, artifactID string) (string, error) {
	if h == nil || h.db == nil {
		return "", nil
	}
	sessionID = strings.TrimSpace(sessionID)
	artifactID = strings.TrimSpace(artifactID)
	if sessionID == "" && artifactID == "" {
		return "", nil
	}
	var row model.StgArtifactTrace
	q := h.db.Model(&model.StgArtifactTrace{})
	switch {
	case sessionID != "" && artifactID != "":
		q = q.Where("session_id = ? OR artifact_id = ?", sessionID, artifactID)
	case sessionID != "":
		q = q.Where("session_id = ?", sessionID)
	default:
		q = q.Where("artifact_id = ?", artifactID)
	}
	err := q.Order("started_at_ms ASC, id ASC").First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(row.TraceID), nil
}

func buildBundleFromAggregateRow(row model.APISessionAggregate) apiSessionBundle {
	rules := []apiRule{}
	if row.RulesJSON != "" {
		_ = json.Unmarshal([]byte(row.RulesJSON), &rules)
	}
	features := apiFeatures{}
	if row.FeaturesJSON != "" {
		_ = json.Unmarshal([]byte(row.FeaturesJSON), &features)
	}
	features.ToolCalls = row.ToolCalls
	features.UniqueTools = row.UniqueTools
	features.MaxSerialRun = row.MaxSerialRun
	features.ToolFailures = row.ToolFailures
	features.ToolFailRate = float64(row.ToolFailRateBP) / 10000
	features.AvgTokensPerTurn = row.AvgTokensPerTurn
	features.ToolRetries = row.ToolRetries
	features.HasRootFail = row.HasRootFail
	features.HasLoop = row.HasLoop
	return apiSessionBundle{
		DetailVersion:             currentDetailBundleVersion,
		SourceUpdatedAtMs:         msFromTimePtr(row.SourceUpdateAt),
		SourceUpdatedAt:           timeToString(row.SourceUpdateAt),
		ID:                        pickFirstNonEmpty(row.SessionID, row.ArtifactID),
		SessionID:                 row.SessionID,
		ArtifactID:                row.ArtifactID,
		ArtifactPublicationStatus: bundlePublicationStatusFromStored(row.ArtifactPublicationStatus),
		TraceFingerprint:          strings.TrimSpace(row.TraceFingerprint),
		AggregateInvalidated:      row.AggregateInvalidated,
		User:                      row.UserName,
		UserID:                    row.UserID,
		Title:                     pickFirstNonEmpty(row.Title, "Session "+pickFirstNonEmpty(row.SessionID, row.ArtifactID)),
		Trace:                     row.TraceID,
		StartedAtMs:               row.StartedAtMs,
		StartedAt:                 msToString(row.StartedAtMs),
		DurationMs:                row.DurationMs,
		InputTokens:               row.InputTokens,
		OutputTokens:              row.OutputTokens,
		ToolCalls:                 row.ToolCalls,
		Turns:                     row.Turns,
		EffectiveRounds:           features.EffectiveRounds,
		TraceCount:                row.TraceCount,
		Score:                     row.Score,
		Color:                     "green",
		Chip:                      row.Chip,
		Features:                  features,
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
	if bundle.DetailVersion == 0 {
		bundle.DetailVersion = cached.DetailVersion
	}
	if bundle.SourceUpdatedAtMs == 0 {
		bundle.SourceUpdatedAtMs = cached.SourceUpdatedAtMs
		bundle.SourceUpdatedAt = cached.SourceUpdatedAt
	}
	if bundle.SessionID == "" {
		bundle.SessionID = cached.SessionID
	}
	if bundle.ArtifactID == "" {
		bundle.ArtifactID = cached.ArtifactID
	}
	if bundle.ArtifactPublicationStatus == "" {
		bundle.ArtifactPublicationStatus = cached.ArtifactPublicationStatus
	}
	if bundle.TraceFingerprint == "" {
		bundle.TraceFingerprint = cached.TraceFingerprint
	}
	if !bundle.AggregateInvalidated {
		bundle.AggregateInvalidated = cached.AggregateInvalidated
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
	if bundle.EffectiveRounds == 0 {
		bundle.EffectiveRounds = cached.EffectiveRounds
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
		// api 模式下上游已先按时间窗过滤过一轮；若列表项缺少 create_at / update_at，
		// StartedAtMs 会保持 0，此时不应在本地二次过滤时把真实数据误删。
		if b.StartedAtMs == 0 {
			out = append(out, b)
			continue
		}
		if b.StartedAtMs >= startMs && b.StartedAtMs <= endMs {
			out = append(out, b)
		}
	}
	return out
}

// timeRangeFromQuery 从 query 解析时间窗，缺省最近 30 天。
// 格式严格 "YYYY-MM-DD HH:mm:ss"，前端可任意精度，由 sanitize 补齐。
func timeRangeFromQuery(c *gin.Context) modellog.TimeRange {
	now := time.Now()
	defaultEnd := now.Format("2006-01-02 15:04:05")
	defaultStart := now.AddDate(0, 0, -30).Format("2006-01-02 15:04:05")

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
	} else if t := parseUpstreamFileTimestamp(s); !t.IsZero() {
		src.SourceCreatedAt = &t
	}
	if t := parseUpstreamTime(s.UpdateAt); !t.IsZero() {
		src.SourceUpdatedAt = &t
	}
	return src
}

// lightBundleFromAPI 列表项概览：与 lightBundleFromSource 等价，只是字段来源不同。
//
// started_at_ms 四级兜底：
//  1. ses_* 优先取 file_list URL 文件名里的时间戳（如 ses_xxx_20260618122215.jsonl）
//  2. 上游 create_at
//  3. 老格式 session_id 自带时间戳（如 20260608_095347_*）
//  4. 0（让前端按"未知时间"渲染）
//
// 后续如命中 JSONL 解析/聚合缓存，applyCachedMetricsToBundle 会用解析出的 StartedAtMs 覆盖这里的兜底时间。
func lightBundleFromAPI(s modellog.Session) apiSessionBundle {
	startedAt := parseUpstreamTime(s.CreateAt)
	endedAt := parseUpstreamTime(s.UpdateAt)
	startedMs, endedMs := int64(0), int64(0)
	if strings.HasPrefix(strings.TrimSpace(s.SessionID), "ses_") {
		if t := parseUpstreamFileTimestamp(s); !t.IsZero() {
			startedMs = t.UnixMilli()
		}
	}
	if startedMs == 0 && !startedAt.IsZero() {
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
	if startedMs == 0 {
		if t := parseUpstreamFileTimestamp(s); !t.IsZero() {
			startedMs = t.UnixMilli()
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

// parseUpstreamTime 兼容上游可能的时间格式：RFC3339 / 常见本地时间字符串 / 秒或毫秒时间戳字符串。
func parseUpstreamTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05.999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006/01/02 15:04:05.999999999",
		"2006/01/02 15:04:05.999999",
		"2006/01/02 15:04:05.999",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
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

func parseUpstreamFileTimestamp(s modellog.Session) time.Time {
	for _, f := range s.FileList {
		if t := parseTimestampFromFileURL(f.URL); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func parseTimestampFromFileURL(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}
	if idx := strings.LastIndex(raw, "/"); idx >= 0 {
		raw = raw[idx+1:]
	}
	raw = strings.TrimSuffix(raw, ".jsonl")
	idx := strings.LastIndex(raw, "_")
	if idx < 0 || idx+15 > len(raw) {
		return time.Time{}
	}
	candidate := raw[idx+1 : idx+15]
	t, err := time.ParseInLocation("20060102150405", candidate, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}
