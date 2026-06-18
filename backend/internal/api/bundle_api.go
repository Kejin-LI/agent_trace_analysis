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
	artifactStatusPublished   = "published"
	artifactStatusUnpublished = "unpublished"
)

// normalizeArtifactStatus 保留兼容旧 query 参数，但平台统一强制未发布口径。
func normalizeArtifactStatus(_ string) string {
	return artifactStatusUnpublished
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
		bundles, total, ok, err := h.listSessionBundlesFromDB(tr, uid, uname, sid, aid, limit, offset)
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

func (h *Handler) listSessionBundlesDBFirst(c *gin.Context) {
	tr := timeRangeFromQuery(c)
	limit, offset := bundlePaginationDefault(c, 50)
	uid := c.Query("user_id")
	uname := c.Query("user_name")
	sid := c.Query("session_id")
	aid := c.Query("artifact_id")

	bundles, total, ok, err := h.listSessionBundlesFromDB(tr, uid, uname, sid, aid, limit, offset)
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
	if h.aggregator != nil {
		h.aggregator.EnsureDays(h.effectiveCookie(c), daysFromQueryRange(tr))
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
	status := normalizeArtifactStatus(c.Query("artifact_status"))
	cachedBundle, hasCached, err := h.getCachedSessionBundle(key, status)
	if err != nil {
		log.Printf("session detail cached lookup failed key=%s err=%v", key, err)
	}
	if hasCached && hasDetailTraces(cachedBundle) {
		// DB 已有完整 bundle 时直接返回，避免详情页再走一次最近 7 天的上游扫描。
		c.JSON(http.StatusOK, h.applyQualityEvaluation(cachedBundle))
		return
	}

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

	if indexedBundle, indexedSrc, ok, err := h.getIndexedSessionBundle(key, status); err != nil {
		log.Printf("session detail indexed lookup failed key=%s err=%v", key, err)
	} else if ok {
		if hasCached {
			indexedBundle = mergeBundleWithCachedBundle(indexedBundle, cachedBundle)
			indexedBundle.ArtifactPublicationStatus = status
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

	// 详情页与列表口径一致：同时扫描 unpublished + published，避免列表可见但详情拿不到完整 trace。
	var attempts []struct {
		onlyUnpublished bool
		label           string
	}
	attempts = append(attempts, struct {
		onlyUnpublished bool
		label           string
	}{true, artifactStatusUnpublished})
	attempts = append(attempts, struct {
		onlyUnpublished bool
		label           string
	}{false, artifactStatusPublished})

	var hit *modellog.Session
	var hitStatus string
	var lastErr error
	for _, attempt := range attempts {
		const pageSize = 500
		const maxPages = 6
		for pageNo := 1; pageNo <= maxPages; pageNo++ {
			resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
				TimeRange:                detailTR,
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
					hit = s
					hitStatus = attempt.label
					break
				}
			}
			if hit != nil {
				break
			}
			total := int(resp.Total)
			if len(resp.Data) < pageSize || total <= pageNo*pageSize {
				break
			}
		}
		if hit != nil {
			break
		}
	}
	if hit == nil && lastErr != nil {
		if hasCached {
			c.JSON(http.StatusOK, h.applyQualityEvaluation(cachedBundle))
			return
		}
		fail(c, fmt.Errorf("upstream list: %w", lastErr))
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
		Where("LEFT(session_id, 4) = ?", "ses_").
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
		return nil, 0, true, nil
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
	return bundles, int64(normalizeBundleListTotal(int(total), len(bundles), limit, offset)), true, nil
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
		ID:                        pickFirstNonEmpty(row.SessionID, row.ArtifactID),
		SessionID:                 row.SessionID,
		ArtifactID:                row.ArtifactID,
		ArtifactPublicationStatus: artifactStatusPublished,
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
	if bundle.SessionID == "" {
		bundle.SessionID = cached.SessionID
	}
	if bundle.ArtifactID == "" {
		bundle.ArtifactID = cached.ArtifactID
	}
	if bundle.ArtifactPublicationStatus == "" {
		bundle.ArtifactPublicationStatus = cached.ArtifactPublicationStatus
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
