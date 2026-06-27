package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

// dashboardLLMCountTimeout 大盘 llm_evaluated_count 这条 COUNT(DISTINCT) 的最长等待时间。
// 它是次要展示指标，超时即降级为 0，确保大盘主数据不被慢查询拖垮。
const dashboardLLMCountTimeout = 1500 * time.Millisecond

type apiDashboardSummaryRadar struct {
	Response      *float64 `json:"response,omitempty"`
	Stability     *float64 `json:"stability,omitempty"`
	Thinking      *float64 `json:"thinking,omitempty"`
	Resource      *float64 `json:"resource,omitempty"`
	Orchestration *float64 `json:"orchestration,omitempty"`
}

// apiDashboardLLMRadar 是 GPT-5.5 六维评估在选定时间窗内的全量均分，
// 维度与前端 LLM_DIMENSIONS 一一对应。仅当该维度在窗口内有评估样本时才非空。
type apiDashboardLLMRadar struct {
	Resolved          *float64 `json:"resolved,omitempty"`
	IntentMatch       *float64 `json:"intent_match,omitempty"`
	EfficiencyFeel    *float64 `json:"efficiency_feel,omitempty"`
	Sentiment         *float64 `json:"sentiment,omitempty"`
	Actionability     *float64 `json:"actionability,omitempty"`
	HallucinationRisk *float64 `json:"hallucination_risk,omitempty"`
}

type apiDashboardSummary struct {
	Total             int                      `json:"total"`
	AnalyzedCount     int                      `json:"analyzed_count"`
	PendingCount      int                      `json:"pending_count"`
	AnomalyCount      int                      `json:"anomaly_count"`
	LLMEvaluatedCount int                      `json:"llm_evaluated_count"`
	P50DurationMs     int64                    `json:"p50_duration_ms,omitempty"`
	P90DurationMs     int64                    `json:"p90_duration_ms,omitempty"`
	AvgScore          *float64                 `json:"avg_score,omitempty"`
	Radar             apiDashboardSummaryRadar `json:"radar"`
	// LLMAvgScore / LLMRadar 是 GPT-5.5 在整段时间窗内的全量均分与六维雷达，
	// 与异常 Top10 样本无关，供大盘 GPT 卡片直接渲染选定范围的真实大盘数据。
	LLMAvgScore *float64             `json:"llm_avg_score,omitempty"`
	LLMRadar    apiDashboardLLMRadar `json:"llm_radar"`
}

type dashboardAnomalyCandidateStats struct {
	filteredTotal  int
	candidateTotal int
	truncated      bool
}

func (h *Handler) getTopAnomalySessions(c *gin.Context) {
	limit := 10
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	tr := timeRangeFromQuery(c)
	bundles, total, stats, err := h.topAnomalySessionsFromAggregates(tr, limit)
	if err != nil {
		fail(c, fmt.Errorf("build top anomaly sessions: %w", err))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":            bundles,
		"limit":           limit,
		"offset":          0,
		"total":           total,
		"filtered_total":  stats.filteredTotal,
		"candidate_total": stats.candidateTotal,
		"truncated":       stats.truncated,
	})
}

func (h *Handler) getDashboardSummary(c *gin.Context) {
	h.getDashboardSummaryDB(c)
}

func (h *Handler) getDashboardSummaryDB(c *gin.Context) {
	tr := timeRangeFromQuery(c)
	if h.aggregator != nil {
		h.aggregator.EnsureDays(h.effectiveCookie(c), daysFromQueryRange(tr))
	}
	summary, err := h.buildDashboardSummaryOptimized(tr)
	if err != nil {
		fail(c, fmt.Errorf("build dashboard summary from db: %w", err))
		return
	}
	if summary.Total == 0 {
		summary.Total = summary.AnalyzedCount
	}
	finalizeDashboardSummary(&summary)
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) getAnomalySessions(c *gin.Context) {
	limit := 600
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 800 {
		limit = v
	}
	tr := timeRangeFromQuery(c)
	// artifact_status=all|published|unpublished，缺省/非法值按 all 处理（不过滤）。
	artifactStatus := normalizeArtifactStatus(c.Query("artifact_status"))
	bundles, total, stats, err := h.anomalySessionsFromAggregates(tr, limit, artifactStatus)
	if err != nil {
		fail(c, fmt.Errorf("build anomaly sessions: %w", err))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":            bundles,
		"limit":           limit,
		"offset":          0,
		"total":           total,
		"filtered_total":  stats.filteredTotal,
		"candidate_total": stats.candidateTotal,
		"truncated":       stats.truncated,
		"artifact_status": artifactStatusLabelForResponse(artifactStatus),
	})
}

// artifactStatusLabelForResponse 把内部空串(=不过滤)回显为 "all"，便于前端缓存键与回显。
func artifactStatusLabelForResponse(status string) string {
	if status == "" {
		return "all"
	}
	return status
}

// getAnomalyPublicationStatus 是混合实时策略里的“上游校准”一跳：
// 只做 list-only 的两趟上游查询（不拉 JSONL、不落库），返回时间窗内每个 session 的
// 实时发布状态映射，供前端把 DB 快照状态校准为最新。上游不可用时返回明确错误，
// 前端据此降级到快照状态。该接口刻意保持轻量：只读元信息、不解析产物内容。
func (h *Handler) getAnomalyPublicationStatus(c *gin.Context) {
	if h == nil || h.upstream == nil {
		c.JSON(http.StatusOK, gin.H{"data": map[string]string{}, "available": false})
		return
	}
	tr := timeRangeFromQuery(c)
	cookie := h.effectiveCookie(c)
	if strings.TrimSpace(cookie) == "" {
		c.JSON(http.StatusOK, gin.H{"data": map[string]string{}, "available": false, "reason": "missing_cookie"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	statusBySession := make(map[string]string)
	attempts := []struct {
		onlyUnpublished bool
		label           string
	}{
		{onlyUnpublished: true, label: artifactStatusUnpublished},
		{onlyUnpublished: false, label: artifactStatusPublished},
	}
	for _, attempt := range attempts {
		resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
			TimeRange:                tr,
			Page:                     modellog.Page{},
			OnlyUnpublishedArtifacts: attempt.onlyUnpublished,
		})
		if err != nil {
			log.Printf("anomaly pub-status calibration: upstream list failed status=%s err=%v", attempt.label, err)
			c.JSON(http.StatusOK, gin.H{"data": map[string]string{}, "available": false, "reason": "upstream_unavailable"})
			return
		}
		if resp == nil {
			continue
		}
		for _, s := range resp.Data {
			sid := strings.TrimSpace(s.SessionID)
			if sid == "" {
				continue
			}
			// 两趟互斥：未发布先写，已发布不覆盖，与聚合落库口径保持一致。
			if _, ok := statusBySession[sid]; !ok {
				statusBySession[sid] = attempt.label
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      statusBySession,
		"available": true,
		"total":     len(statusBySession),
	})
}

func finalizeDashboardSummary(summary *apiDashboardSummary) {
	if summary == nil {
		return
	}
	if summary.Total < summary.AnalyzedCount {
		summary.Total = summary.AnalyzedCount
	}
	summary.PendingCount = summary.Total - summary.AnalyzedCount
	if summary.PendingCount < 0 {
		summary.PendingCount = 0
	}
}

func (h *Handler) buildDashboardSummaryOptimized(tr modellog.TimeRange) (apiDashboardSummary, error) {
	exactAnomalyCount := -1
	if apiSessionAggregateHasIssueColumn(h.db) {
		var err error
		exactAnomalyCount, err = h.countDashboardIssueSessions(tr)
		if err != nil {
			return apiDashboardSummary{}, err
		}
	}

	if dashboardSummaryCanUseDailySummary(tr) {
		summary, err := h.buildDashboardSummaryFromDailySummaries(tr)
		if err != nil {
			return apiDashboardSummary{}, err
		}
		if summary.AnalyzedCount > 0 || summary.AnomalyCount > 0 || summary.LLMEvaluatedCount > 0 {
			if exactAnomalyCount >= 0 {
				// 大盘整体摘要仅在完整自然日窗口时复用日汇总；
				// 异常数仍强制对齐精确时间窗 + has_issue 口径。
				summary.AnomalyCount = exactAnomalyCount
			}
			return summary, nil
		}
	}

	summary := apiDashboardSummary{}
	var err error
	summary, err = h.buildDashboardSummaryFromAggregates(tr)
	if err != nil {
		return apiDashboardSummary{}, err
	}
	if exactAnomalyCount >= 0 {
		summary.AnomalyCount = exactAnomalyCount
	}
	return summary, nil
}

func dashboardSummaryCanUseDailySummary(tr modellog.TimeRange) bool {
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return false
	}
	startLocal := startAt.In(time.Local)
	endLocal := endAt.In(time.Local)
	if startLocal.Hour() != 0 || startLocal.Minute() != 0 || startLocal.Second() != 0 {
		return false
	}
	return endLocal.Hour() == 23 && endLocal.Minute() == 59 && endLocal.Second() == 59
}

func (h *Handler) buildDashboardSummaryFromDailySummaries(tr modellog.TimeRange) (apiDashboardSummary, error) {
	summary := apiDashboardSummary{}
	if h == nil || h.db == nil {
		return summary, nil
	}
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return summary, nil
	}
	startDate := startOfLocalDay(startAt)
	endDate := startOfLocalDay(endAt)
	var rows []model.APIDailySummary
	if err := h.db.Model(&model.APIDailySummary{}).
		Where("aggregate_date BETWEEN ? AND ?", startDate, endDate).
		Order("aggregate_date DESC").
		Find(&rows).Error; err != nil {
		return summary, err
	}
	if len(rows) == 0 {
		return summary, nil
	}
	var totalSessions int
	var anomalyCount int
	var weightedAvgScore float64
	var weightedResponse float64
	var weightedThinking float64
	var weightedResource float64
	var weightedStability float64
	var weightedOrchestration float64
	for _, row := range rows {
		if row.SessionCount <= 0 {
			continue
		}
		totalSessions += row.SessionCount
		anomalyCount += row.AbnormalSessionCount
		weightedAvgScore += row.AvgScore * float64(row.SessionCount)
		weightedResponse += row.ResponseScoreAvg * float64(row.SessionCount)
		weightedThinking += row.ThinkingScoreAvg * float64(row.SessionCount)
		weightedResource += row.ResourceScoreAvg * float64(row.SessionCount)
		weightedStability += row.StabilityScoreAvg * float64(row.SessionCount)
		weightedOrchestration += row.OrchestrationScoreAvg * float64(row.SessionCount)
	}
	if totalSessions == 0 {
		return summary, nil
	}
	summary.Total = totalSessions
	summary.AnalyzedCount = totalSessions
	summary.AnomalyCount = anomalyCount
	avgScore := round2(weightedAvgScore / float64(totalSessions))
	summary.AvgScore = &avgScore
	response := round2(weightedResponse / float64(totalSessions))
	thinking := round2(weightedThinking / float64(totalSessions))
	resource := round2(weightedResource / float64(totalSessions))
	stability := round2(weightedStability / float64(totalSessions))
	orchestration := round2(weightedOrchestration / float64(totalSessions))
	summary.Radar.Response = &response
	summary.Radar.Thinking = &thinking
	summary.Radar.Resource = &resource
	summary.Radar.Stability = &stability
	summary.Radar.Orchestration = &orchestration
	llmAgg, err := h.aggregateDashboardLLMFromStaging(tr)
	if err != nil {
		return apiDashboardSummary{}, err
	}
	summary.LLMEvaluatedCount = llmAgg.count
	summary.LLMAvgScore = llmAgg.avgScore
	summary.LLMRadar = llmAgg.radar
	return summary, nil
}

func (h *Handler) buildDashboardSummaryFromAggregates(tr modellog.TimeRange) (apiDashboardSummary, error) {
	summary := apiDashboardSummary{}
	if h == nil || h.db == nil {
		return summary, nil
	}
	bundles, err := h.listSummaryBundlesFromDB(tr)
	if err != nil {
		return summary, err
	}
	if len(bundles) == 0 {
		return summary, nil
	}
	bundles = h.applyQualityEvaluations(bundles)
	summary.LLMAvgScore, summary.LLMRadar = accumulateDashboardLLMAggregate(bundles)

	summary.AnalyzedCount = len(bundles)
	summary.Total = len(bundles)
	scores := make([]float64, 0, len(bundles))
	durations := make([]int64, 0, len(bundles))
	var anomalyCount int
	var totalResponse float64
	var totalThinking float64
	var totalResource float64
	var totalStability float64
	var totalOrchestration float64
	var responseCount int
	var thinkingCount int
	var resourceCount int
	var stabilityCount int
	var orchestrationCount int

	for _, bundle := range bundles {
		q := getDashboardQualityScores(bundle)
		if q.CombinedScore != nil {
			score := float64(*q.CombinedScore)
			scores = append(scores, score)
		}
		if dashboardBundleHasIssueTag(bundle) {
			anomalyCount++
		}
		if q.LLMScore != nil {
			summary.LLMEvaluatedCount++
		}
		if bundle.DurationMs > 0 {
			durations = append(durations, bundle.DurationMs)
		}
		totalResponse += float64(bundle.Radar.Response)
		responseCount++
		totalThinking += float64(bundle.Radar.Thinking)
		thinkingCount++
		totalResource += float64(bundle.Radar.Resource)
		resourceCount++
		if bundle.Radar.Stability > 0 {
			totalStability += float64(bundle.Radar.Stability)
			stabilityCount++
		}
		if bundle.Radar.Orchestration > 0 {
			totalOrchestration += float64(bundle.Radar.Orchestration)
			orchestrationCount++
		}
	}

	summary.AnomalyCount = anomalyCount
	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		summary.P50DurationMs = percentile(durations, 0.50)
		summary.P90DurationMs = percentile(durations, 0.90)
	}
	if len(scores) > 0 {
		avgScore := round2(sumFloat64(scores) / float64(len(scores)))
		summary.AvgScore = &avgScore
	}
	if responseCount > 0 {
		v := round2(totalResponse / float64(responseCount))
		summary.Radar.Response = &v
	}
	if thinkingCount > 0 {
		v := round2(totalThinking / float64(thinkingCount))
		summary.Radar.Thinking = &v
	}
	if resourceCount > 0 {
		v := round2(totalResource / float64(resourceCount))
		summary.Radar.Resource = &v
	}
	if stabilityCount > 0 {
		v := round2(totalStability / float64(stabilityCount))
		summary.Radar.Stability = &v
	}
	if orchestrationCount > 0 {
		v := round2(totalOrchestration / float64(orchestrationCount))
		summary.Radar.Orchestration = &v
	}
	return summary, nil
}

func (h *Handler) topAnomalySessionsFromAggregates(tr modellog.TimeRange, limit int) ([]apiSessionBundle, int, dashboardAnomalyCandidateStats, error) {
	return h.queryDashboardAnomalyBundles(tr, limit, maxDashboardAnomalyScanLimit(limit, 8, 200), "")
}

func (h *Handler) anomalySessionsFromAggregates(tr modellog.TimeRange, limit int, artifactStatus string) ([]apiSessionBundle, int, dashboardAnomalyCandidateStats, error) {
	return h.queryDashboardAnomalyBundles(tr, limit, maxDashboardAnomalyScanLimit(limit, 6, 300), artifactStatus)
}

func maxDashboardAnomalyScanLimit(limit, multiplier, minValue int) int {
	if limit <= 0 {
		limit = minValue
	}
	scanLimit := limit * multiplier
	if scanLimit < minValue {
		scanLimit = minValue
	}
	if scanLimit > 2000 {
		scanLimit = 2000
	}
	return scanLimit
}

func (h *Handler) queryDashboardAnomalyBundles(tr modellog.TimeRange, limit, scanLimit int, artifactStatus string) ([]apiSessionBundle, int, dashboardAnomalyCandidateStats, error) {
	stats := dashboardAnomalyCandidateStats{}
	if h == nil || h.db == nil {
		return nil, 0, stats, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if scanLimit < limit {
		scanLimit = limit
	}
	if apiSessionAggregateHasIssueColumn(h.db) {
		rows, total, err := h.loadDashboardIssueRows(tr, limit, artifactStatus)
		if err != nil {
			return nil, 0, stats, err
		}
		bundles := make([]apiSessionBundle, 0, len(rows))
		for _, row := range rows {
			bundles = append(bundles, buildBundleFromAggregateRow(row))
		}
		bundles = h.applyQualityEvaluations(bundles)
		sortDashboardAnomalyCandidates(bundles)
		stats.filteredTotal = total
		stats.candidateTotal = total
		stats.truncated = false
		return bundles, total, stats, nil
	}
	rows, candidateTotal, err := h.loadDashboardAnomalyCandidateRows(tr, scanLimit, artifactStatus)
	if err != nil {
		return nil, 0, stats, err
	}
	if len(rows) == 0 {
		return nil, 0, stats, nil
	}
	bundles := make([]apiSessionBundle, 0, len(rows))
	for _, row := range rows {
		bundles = append(bundles, buildBundleFromAggregateRow(row))
	}
	bundles = h.applyQualityEvaluations(bundles)
	candidates := make([]apiSessionBundle, 0, len(bundles))
	for _, bundle := range bundles {
		if dashboardBundleHasAnomaly(bundle) {
			candidates = append(candidates, bundle)
		}
	}
	sortDashboardAnomalyCandidates(candidates)
	stats.filteredTotal = len(candidates)
	stats.candidateTotal = candidateTotal
	stats.truncated = candidateTotal > scanLimit
	total := len(candidates)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, total, stats, nil
}

func (h *Handler) loadDashboardIssueRows(tr modellog.TimeRange, limit int, artifactStatus string) ([]model.APISessionAggregate, int, error) {
	q, _, _, ok := h.sessionAggregateRangeQuery(context.Background(), tr, sessionAggregateQueryFilters{HasIssueOnly: true, ArtifactStatus: artifactStatus})
	if !ok {
		return nil, 0, nil
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.APISessionAggregate
	if err := q.Select(h.dashboardAggregateRowSelectColumnsFor()).
		Order("score ASC").
		Order("started_at_ms DESC").
		Order("id DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, int(total), nil
}

func (h *Handler) countDashboardIssueSessions(tr modellog.TimeRange) (int, error) {
	q, _, _, ok := h.sessionAggregateRangeQuery(context.Background(), tr, sessionAggregateQueryFilters{HasIssueOnly: true})
	if !ok {
		return 0, nil
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}

func (h *Handler) loadDashboardAnomalyCandidateRows(tr modellog.TimeRange, scanLimit int, artifactStatus string) ([]model.APISessionAggregate, int, error) {
	q, _, _, ok := h.sessionAggregateRangeQuery(context.Background(), tr, sessionAggregateQueryFilters{ArtifactStatus: artifactStatus})
	if !ok {
		return nil, 0, nil
	}
	q = q.Where("(abnormal_level > 0 OR has_root_fail = ? OR has_loop = ? OR score < ? OR COALESCE(chip, '') <> '')", true, true, 85)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.APISessionAggregate
	cols := h.dashboardAggregateRowSelectColumnsFor()
	if err := q.Select(cols).
		Order("abnormal_level DESC").
		Order("score ASC").
		Order("started_at_ms DESC").
		Order("id DESC").
		Limit(scanLimit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, int(total), nil
}

func dashboardAggregateRowSelectColumns() []string {
	return []string{
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
		"has_issue",
		"artifact_publication_status",
		"rules_json",
		"features_json",
		"created_at",
		"updated_at",
	}
}

// dashboardAggregateRowSelectColumnsFor 返回按当前表结构裁剪后的列清单：
// has_issue / artifact_publication_status 都是后引入的可选列，部分实例尚未 ALTER，
// 直接 SELECT 不存在的列会触发 1054 让整页查询失败，这里探测后剔除以优雅降级。
func (h *Handler) dashboardAggregateRowSelectColumnsFor() []string {
	cols := dashboardAggregateRowSelectColumns()
	if !apiSessionAggregateHasIssueColumn(h.db) {
		cols = removeString(cols, "has_issue")
	}
	if !apiSessionAggregateHasPublicationStatusColumn(h.db) {
		cols = removeString(cols, "artifact_publication_status")
	}
	return cols
}

func (h *Handler) dashboardAggregateRangeQuery(tr modellog.TimeRange) (*gorm.DB, bool) {
	q, _, _, ok := h.sessionAggregateRangeQuery(context.Background(), tr, sessionAggregateQueryFilters{})
	return q, ok
}

func sortDashboardAnomalyCandidates(candidates []apiSessionBundle) {
	sort.SliceStable(candidates, func(i, j int) bool {
		qi := getDashboardQualityScores(candidates[i])
		qj := getDashboardQualityScores(candidates[j])
		si, sj := 101, 101
		if qi.CombinedScore != nil {
			si = *qi.CombinedScore
		}
		if qj.CombinedScore != nil {
			sj = *qj.CombinedScore
		}
		if si == sj {
			return candidates[i].StartedAtMs > candidates[j].StartedAtMs
		}
		return si < sj
	})
}

func dashboardBundleHasAnomaly(bundle apiSessionBundle) bool {
	return dashboardBundleHasIssueTag(bundle)
}

func dashboardBundleHasIssueTag(bundle apiSessionBundle) bool {
	for _, rule := range bundle.Rules {
		if !rule.Passed {
			return true
		}
	}
	return dashboardBundleHasLLMIssueTag(bundle)
}

func dashboardBundleHasLLMIssueTag(bundle apiSessionBundle) bool {
	if !hasDashboardLLMScoreEvidence(bundle) {
		return false
	}
	if optionalScoreBelow(bundle.LLMResolvedScore, 70) {
		return true
	}
	if optionalScoreBelow(bundle.LLMIntentMatchScore, 70) {
		return true
	}
	if optionalScoreBelow(bundle.LLMSentimentScore, 60) {
		return true
	}
	if optionalScoreBelow(bundle.LLMEfficiencyFeelScore, 70) {
		return true
	}
	if optionalScoreBelow(bundle.LLMActionabilityScore, 70) {
		return true
	}
	if optionalScoreBelow(bundle.LLMHallucinationRiskScore, 70) {
		return true
	}
	return false
}

func optionalScoreBelow(score *int, threshold int) bool {
	return score != nil && *score < threshold
}

func (h *Handler) listSummaryBundlesFromDB(tr modellog.TimeRange) ([]apiSessionBundle, error) {
	if h == nil || h.db == nil {
		return nil, nil
	}
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return nil, nil
	}
	startDate := startOfLocalDay(startAt)
	endDate := startOfLocalDay(endAt)

	var rows []model.APISessionAggregate
	if err := h.db.Model(&model.APISessionAggregate{}).
		Where(
			"(started_at_ms BETWEEN ? AND ?) OR "+
				"(started_at_ms = 0 AND started_at BETWEEN ? AND ?) OR "+
				"(started_at_ms = 0 AND started_at IS NULL AND aggregate_date BETWEEN ? AND ?)",
			startAt.UnixMilli(), endAt.UnixMilli(), startAt, endAt, startDate, endDate,
		).
		Select([]string{
			"session_id",
			"artifact_id",
			"duration_ms",
			"score",
			"response_score",
			"stability_score",
			"thinking_score",
			"resource_score",
			"orchestration_score",
		}).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	bundles := make([]apiSessionBundle, 0, len(rows))
	for _, row := range rows {
		bundles = append(bundles, apiSessionBundle{
			SessionID:  row.SessionID,
			ArtifactID: row.ArtifactID,
			DurationMs: row.DurationMs,
			Score:      row.Score,
			Radar: apiRadar{
				Response:      row.ResponseScore,
				Stability:     row.StabilityScore,
				Thinking:      row.ThinkingScore,
				Resource:      row.ResourceScore,
				Orchestration: row.OrchestrationScore,
			},
		})
	}
	return bundles, nil
}

type dashboardLLMAggregate struct {
	count    int
	avgScore *float64
	radar    apiDashboardLLMRadar
}

// aggregateDashboardLLMFromStaging 在日汇总路径下，直接从质量评估表算出整段时间窗的
// GPT-5.5 全量已评估数、均分与六维雷达。口径与 llm_evaluated_count 完全一致
// （is_deleted=0 / session_id 前缀 / llm_eval_status=succeeded / session_started_at 落窗），
// 每个 session 仅保留一行最新评估，故按行 AVG 即等价按 session 均分。
// 这是大盘唯一的重查询，套短超时上下文：DB 慢就整体降级为空，绝不拖垮大盘主数据。
func (h *Handler) aggregateDashboardLLMFromStaging(tr modellog.TimeRange) (dashboardLLMAggregate, error) {
	if h == nil || h.db == nil {
		return dashboardLLMAggregate{}, nil
	}
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return dashboardLLMAggregate{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), dashboardLLMCountTimeout)
	defer cancel()
	type aggRow struct {
		Count                int
		AvgScore             *float64
		AvgResolved          *float64
		AvgIntentMatch       *float64
		AvgEfficiencyFeel    *float64
		AvgSentiment         *float64
		AvgActionability     *float64
		AvgHallucinationRisk *float64
	}
	var row aggRow
	// 用 session_id LIKE 'ses\_%' 替代 LEFT(session_id,4)='ses_'：
	// 函数包裹列会让 session_id 索引失效，LIKE 前缀匹配可走索引。
	if err := h.db.WithContext(ctx).Model(&model.StgSessionQualityEvaluation{}).
		Select("COUNT(DISTINCT session_id) AS count, "+
			"AVG(llm_score) AS avg_score, "+
			"AVG(llm_resolved_score) AS avg_resolved, "+
			"AVG(llm_intent_match_score) AS avg_intent_match, "+
			"AVG(llm_efficiency_feel_score) AS avg_efficiency_feel, "+
			"AVG(llm_sentiment_score) AS avg_sentiment, "+
			"AVG(llm_actionability_score) AS avg_actionability, "+
			"AVG(llm_hallucination_risk_score) AS avg_hallucination_risk").
		Where("is_deleted = 0").
		Where("session_id LIKE ?", "ses\\_%").
		Where("llm_eval_status = ?", "succeeded").
		Where("session_started_at BETWEEN ? AND ?", startAt, endAt).
		Scan(&row).Error; err != nil {
		// 超时或查询失败都降级为空，不让次要指标阻断大盘。
		log.Printf("dashboard: aggregateDashboardLLMFromStaging degraded to empty: %v", err)
		return dashboardLLMAggregate{}, nil
	}
	return dashboardLLMAggregate{
		count:    row.Count,
		avgScore: roundOptionalScoreAvg(row.AvgScore),
		radar: apiDashboardLLMRadar{
			Resolved:          roundOptionalScoreAvg(row.AvgResolved),
			IntentMatch:       roundOptionalScoreAvg(row.AvgIntentMatch),
			EfficiencyFeel:    roundOptionalScoreAvg(row.AvgEfficiencyFeel),
			Sentiment:         roundOptionalScoreAvg(row.AvgSentiment),
			Actionability:     roundOptionalScoreAvg(row.AvgActionability),
			HallucinationRisk: roundOptionalScoreAvg(row.AvgHallucinationRisk),
		},
	}, nil
}

func roundOptionalScoreAvg(v *float64) *float64 {
	if v == nil {
		return nil
	}
	avg := round2(*v)
	return &avg
}

func (h *Handler) fetchDashboardTotalCount(ctx context.Context, cookie string, tr modellog.TimeRange) (int, error) {
	if h == nil || h.upstream == nil {
		return 0, nil
	}
	resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
		TimeRange: tr,
		Page: modellog.Page{
			PageNo:   1,
			PageSize: 1,
		},
	})
	if err != nil {
		return 0, err
	}
	return int(resp.Total), nil
}

func (h *Handler) buildRealtimeDashboardSummary(ctx context.Context, cookie string, tr modellog.TimeRange) (apiDashboardSummary, error) {
	bundles, err := h.realtimeBundlesForDashboard(ctx, cookie, tr, 20)
	if err != nil {
		return apiDashboardSummary{}, err
	}
	return summarizeDashboardBundles(h.applyQualityEvaluations(bundles)), nil
}

func (h *Handler) topAnomalySessionsFromRealtime(c *gin.Context, tr modellog.TimeRange, limit int) ([]apiSessionBundle, int, error) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	bundles, err := h.realtimeBundlesForDashboard(ctx, h.effectiveCookie(c), tr, limit)
	if err != nil {
		return nil, 0, err
	}
	bundles = h.applyQualityEvaluations(bundles)
	candidates := make([]apiSessionBundle, 0, len(bundles))
	for _, bundle := range bundles {
		if dashboardBundleHasAnomaly(bundle) {
			candidates = append(candidates, bundle)
		}
	}
	sortDashboardAnomalyCandidates(candidates)
	total := len(candidates)
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, total, nil
}

func (h *Handler) realtimeBundlesForDashboard(ctx context.Context, cookie string, tr modellog.TimeRange, limit int) ([]apiSessionBundle, error) {
	if h == nil || h.upstream == nil || h.fetcher == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
		TimeRange: tr,
		Page: modellog.Page{
			PageNo:   1,
			PageSize: limit,
		},
	})
	if err != nil {
		return nil, err
	}
	bundles := make([]apiSessionBundle, 0, len(resp.Data))
	for _, s := range resp.Data {
		if len(s.FileList) == 0 || s.FileList[0].URL == "" {
			continue
		}
		pr, err := h.fetcher.FetchAndParse(s.FileList[0].URL)
		if err != nil {
			log.Printf("dashboard realtime fallback: fetch session=%s failed err=%v", s.SessionID, err)
			continue
		}
		bundle := buildBundleFromTOS(sessionToStgSource(s), pr)
		bundle.ArtifactPublicationStatus = artifactStatusPublished
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

func summarizeDashboardBundles(bundles []apiSessionBundle) apiDashboardSummary {
	summary := apiDashboardSummary{}
	if len(bundles) == 0 {
		return summary
	}
	summary.LLMAvgScore, summary.LLMRadar = accumulateDashboardLLMAggregate(bundles)
	summary.AnalyzedCount = len(bundles)
	scores := make([]float64, 0, len(bundles))
	durations := make([]int64, 0, len(bundles))
	var anomalyCount int
	var totalResponse float64
	var totalThinking float64
	var totalResource float64
	var totalStability float64
	var totalOrchestration float64
	var responseCount int
	var thinkingCount int
	var resourceCount int
	var stabilityCount int
	var orchestrationCount int
	for _, bundle := range bundles {
		q := getDashboardQualityScores(bundle)
		if q.CombinedScore != nil {
			score := float64(*q.CombinedScore)
			scores = append(scores, score)
		}
		if dashboardBundleHasIssueTag(bundle) {
			anomalyCount++
		}
		if q.LLMScore != nil {
			summary.LLMEvaluatedCount++
		}
		if bundle.DurationMs > 0 {
			durations = append(durations, bundle.DurationMs)
		}
		totalResponse += float64(bundle.Radar.Response)
		responseCount++
		totalThinking += float64(bundle.Radar.Thinking)
		thinkingCount++
		totalResource += float64(bundle.Radar.Resource)
		resourceCount++
		if bundle.Radar.Stability > 0 {
			totalStability += float64(bundle.Radar.Stability)
			stabilityCount++
		}
		if bundle.Radar.Orchestration > 0 {
			totalOrchestration += float64(bundle.Radar.Orchestration)
			orchestrationCount++
		}
	}
	summary.AnomalyCount = anomalyCount
	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		summary.P50DurationMs = percentile(durations, 0.50)
		summary.P90DurationMs = percentile(durations, 0.90)
	}
	if len(scores) > 0 {
		avgScore := round2(sumFloat64(scores) / float64(len(scores)))
		summary.AvgScore = &avgScore
	}
	if responseCount > 0 {
		v := round2(totalResponse / float64(responseCount))
		summary.Radar.Response = &v
	}
	if thinkingCount > 0 {
		v := round2(totalThinking / float64(thinkingCount))
		summary.Radar.Thinking = &v
	}
	if resourceCount > 0 {
		v := round2(totalResource / float64(resourceCount))
		summary.Radar.Resource = &v
	}
	if stabilityCount > 0 {
		v := round2(totalStability / float64(stabilityCount))
		summary.Radar.Stability = &v
	}
	if orchestrationCount > 0 {
		v := round2(totalOrchestration / float64(orchestrationCount))
		summary.Radar.Orchestration = &v
	}
	return summary
}

// llmDimensionAccumulator 在遍历 bundle 时累计单个 GPT 维度的有效分数之和与样本数，
// 最终求得该维度在整段时间窗内的全量均分（无样本则返回 nil）。
type llmDimensionAccumulator struct {
	sum   float64
	count int
}

func (a *llmDimensionAccumulator) add(score *int) {
	v := clampOptionalScore(score)
	if v == nil {
		return
	}
	a.sum += float64(*v)
	a.count++
}

func (a llmDimensionAccumulator) average() *float64 {
	if a.count == 0 {
		return nil
	}
	avg := round2(a.sum / float64(a.count))
	return &avg
}

// accumulateDashboardLLMAggregate 从已评估的 bundle 全量计算 GPT-5.5 均分与六维雷达。
// 数据源是时间窗内的全部 session（非异常 Top10 样本），保证大盘 GPT 卡片反映选定范围真实大盘。
func accumulateDashboardLLMAggregate(bundles []apiSessionBundle) (*float64, apiDashboardLLMRadar) {
	var score llmDimensionAccumulator
	var resolved, intentMatch, efficiencyFeel, sentiment, actionability, hallucinationRisk llmDimensionAccumulator
	for _, bundle := range bundles {
		q := getDashboardQualityScores(bundle)
		score.add(q.LLMScore)
		if !hasDashboardLLMScoreEvidence(bundle) {
			continue
		}
		resolved.add(bundle.LLMResolvedScore)
		intentMatch.add(bundle.LLMIntentMatchScore)
		efficiencyFeel.add(bundle.LLMEfficiencyFeelScore)
		sentiment.add(bundle.LLMSentimentScore)
		actionability.add(bundle.LLMActionabilityScore)
		hallucinationRisk.add(bundle.LLMHallucinationRiskScore)
	}
	return score.average(), apiDashboardLLMRadar{
		Resolved:          resolved.average(),
		IntentMatch:       intentMatch.average(),
		EfficiencyFeel:    efficiencyFeel.average(),
		Sentiment:         sentiment.average(),
		Actionability:     actionability.average(),
		HallucinationRisk: hallucinationRisk.average(),
	}
}

type dashboardQualityScores struct {
	RuleScore     *int
	LLMScore      *int
	CombinedScore *int
}

func getDashboardQualityScores(bundle apiSessionBundle) dashboardQualityScores {
	ruleScore := clampOptionalScore(bundle.RuleScore)
	if ruleScore == nil {
		ruleScore = clampScoreValuePtr(bundle.Score)
	}

	hasLLMEvidence := hasDashboardLLMScoreEvidence(bundle)
	llmScore := firstOptionalScore(bundle.LLMScore, bundle.LLMJudgeScore)
	llmScore = takeOptionalScoreIfEvidenced(llmScore, hasLLMEvidence)

	storedCombined := sanitizeDashboardCombinedScore(bundle.CombinedScore, ruleScore, llmScore, true, hasLLMEvidence)
	combinedScore := storedCombined
	if combinedScore == nil {
		switch {
		case llmScore != nil && ruleScore != nil:
			combinedScore = clampScoreValuePtr(int(math.Round(float64(*ruleScore)*0.5 + float64(*llmScore)*0.5)))
		case ruleScore != nil:
			combinedScore = ruleScore
		}
	}

	return dashboardQualityScores{
		RuleScore:     ruleScore,
		LLMScore:      llmScore,
		CombinedScore: combinedScore,
	}
}

func hasDashboardLLMScoreEvidence(bundle apiSessionBundle) bool {
	if bundle.LLMScore != nil && *bundle.LLMScore > 0 {
		return true
	}
	if bundle.LLMJudgeScore != nil && *bundle.LLMJudgeScore > 0 {
		return true
	}
	for _, v := range []*int{
		bundle.LLMSentimentScore,
		bundle.LLMResolvedScore,
		bundle.LLMIntentMatchScore,
		bundle.LLMEfficiencyFeelScore,
		bundle.LLMActionabilityScore,
		bundle.LLMHallucinationRiskScore,
	} {
		if clampOptionalScore(v) != nil {
			return true
		}
	}
	return false
}

func sanitizeDashboardCombinedScore(raw, ruleScore, llmScore *int, hasRuleEvidence, hasLLMEvidence bool) *int {
	n := clampOptionalScore(raw)
	if n == nil {
		return nil
	}
	if *n != 0 {
		return n
	}
	if llmScore != nil {
		return n
	}
	if !hasRuleEvidence && !hasLLMEvidence {
		return nil
	}
	if ruleScore != nil && *ruleScore > 0 {
		return nil
	}
	return n
}

func clampOptionalScore(v *int) *int {
	if v == nil {
		return nil
	}
	return clampScoreValuePtr(*v)
}

func clampScoreValuePtr(v int) *int {
	n := v
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	return &n
}

func firstOptionalScore(values ...*int) *int {
	for _, v := range values {
		if n := clampOptionalScore(v); n != nil {
			return n
		}
	}
	return nil
}

func takeOptionalScoreIfEvidenced(v *int, hasEvidence bool) *int {
	if v == nil {
		return nil
	}
	if *v == 0 && !hasEvidence {
		return nil
	}
	return v
}

func dashboardScoreBand(score int) string {
	switch {
	case score >= 85:
		return "green"
	case score >= 70:
		return "orange"
	case score >= 50:
		return "purple"
	default:
		return "red"
	}
}

func sumFloat64(values []float64) float64 {
	var total float64
	for _, v := range values {
		total += v
	}
	return total
}
