package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

type apiDashboardSummaryRadar struct {
	Response      *float64 `json:"response,omitempty"`
	Stability     *float64 `json:"stability,omitempty"`
	Thinking      *float64 `json:"thinking,omitempty"`
	Resource      *float64 `json:"resource,omitempty"`
	Orchestration *float64 `json:"orchestration,omitempty"`
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
	bundles, total, stats, err := h.anomalySessionsFromAggregates(tr, limit)
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
	summary, err := h.buildDashboardSummaryFromDailySummaries(tr)
	if err != nil {
		return apiDashboardSummary{}, err
	}
	if summary.AnalyzedCount > 0 || summary.AnomalyCount > 0 || summary.LLMEvaluatedCount > 0 {
		return summary, nil
	}
	return h.buildDashboardSummaryFromAggregates(tr)
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
	llmCount, err := h.countDashboardLLMEvaluated(tr)
	if err != nil {
		return apiDashboardSummary{}, err
	}
	summary.LLMEvaluatedCount = llmCount
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
	return h.queryDashboardAnomalyBundles(tr, limit, maxDashboardAnomalyScanLimit(limit, 8, 200))
}

func (h *Handler) anomalySessionsFromAggregates(tr modellog.TimeRange, limit int) ([]apiSessionBundle, int, dashboardAnomalyCandidateStats, error) {
	return h.queryDashboardAnomalyBundles(tr, limit, maxDashboardAnomalyScanLimit(limit, 6, 300))
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

func (h *Handler) queryDashboardAnomalyBundles(tr modellog.TimeRange, limit, scanLimit int) ([]apiSessionBundle, int, dashboardAnomalyCandidateStats, error) {
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
		rows, total, err := h.loadDashboardIssueRows(tr, limit)
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
	rows, candidateTotal, err := h.loadDashboardAnomalyCandidateRows(tr, scanLimit)
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

func (h *Handler) loadDashboardIssueRows(tr modellog.TimeRange, limit int) ([]model.APISessionAggregate, int, error) {
	q, ok := h.dashboardAggregateRangeQuery(tr)
	if !ok {
		return nil, 0, nil
	}
	q = q.Where("session_id LIKE ?", "ses\\_%").Where("has_issue = ?", true)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.APISessionAggregate
	if err := q.Select(dashboardAggregateRowSelectColumns()).
		Order("score ASC").
		Order("started_at_ms DESC").
		Order("id DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, int(total), nil
}

func (h *Handler) loadDashboardAnomalyCandidateRows(tr modellog.TimeRange, scanLimit int) ([]model.APISessionAggregate, int, error) {
	q, ok := h.dashboardAggregateRangeQuery(tr)
	if !ok {
		return nil, 0, nil
	}
	q = q.Where("session_id LIKE ?", "ses\\_%").
		Where("(abnormal_level > 0 OR has_root_fail = ? OR has_loop = ? OR score < ? OR COALESCE(chip, '') <> '')", true, true, 85)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.APISessionAggregate
	cols := dashboardAggregateRowSelectColumns()
	if !apiSessionAggregateHasIssueColumn(h.db) {
		cols = removeString(cols, "has_issue")
	}
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
		"rules_json",
		"features_json",
		"created_at",
		"updated_at",
	}
}

func (h *Handler) dashboardAggregateRangeQuery(tr modellog.TimeRange) (*gorm.DB, bool) {
	if h == nil || h.db == nil {
		return nil, false
	}
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return nil, false
	}
	q := h.db.Model(&model.APISessionAggregate{}).
		Where("started_at_ms BETWEEN ? AND ?", startAt.UnixMilli(), endAt.UnixMilli())
	return q, true
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

func (h *Handler) countDashboardLLMEvaluated(tr modellog.TimeRange) (int, error) {
	if h == nil || h.db == nil {
		return 0, nil
	}
	startAt, endAt, ok := parseTimeRangeBounds(tr)
	if !ok {
		return 0, nil
	}
	type countRow struct {
		Count int
	}
	var row countRow
	if err := h.db.Model(&model.StgSessionQualityEvaluation{}).
		Select("COUNT(DISTINCT session_id) AS count").
		Where("is_deleted = 0").
		Where("LEFT(session_id, 4) = ?", "ses_").
		Where("llm_eval_status = ?", "succeeded").
		Where("session_started_at BETWEEN ? AND ?", startAt, endAt).
		Scan(&row).Error; err != nil {
		return 0, err
	}
	return row.Count, nil
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
