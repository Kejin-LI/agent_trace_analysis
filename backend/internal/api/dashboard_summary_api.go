package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

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
	Total            int                      `json:"total"`
	PublishedCount   int                      `json:"published_count"`
	UnpublishedCount int                      `json:"unpublished_count"`
	PublishedRate    int                      `json:"published_rate"`
	AnalyzedCount    int                      `json:"analyzed_count"`
	PendingCount     int                      `json:"pending_count"`
	AnomalyCount     int                      `json:"anomaly_count"`
	AvgScore         *float64                 `json:"avg_score,omitempty"`
	Radar            apiDashboardSummaryRadar `json:"radar"`
}

func (h *Handler) getDashboardSummary(c *gin.Context) {
	switch dataSourceMode() {
	case "api":
		h.getDashboardSummaryAPI(c)
	default:
		h.getDashboardSummaryDB(c)
	}
}

func (h *Handler) getDashboardSummaryAPI(c *gin.Context) {
	tr := timeRangeFromQuery(c)
	status := normalizeArtifactStatus(c.Query("artifact_status"))

	summary, err := h.buildDashboardSummaryFromAggregates(tr, status)
	if err != nil {
		fail(c, fmt.Errorf("build dashboard summary from aggregates: %w", err))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	publishedCount, unpublishedCount, err := h.fetchDashboardPublicationCounts(ctx, h.effectiveCookie(c), tr, status)
	if err != nil {
		if isUpstreamAuthMissing(err) {
			log.Printf("dashboard summary: upstream publication counts unavailable, fallback to aggregate cache err=%v", err)
			finalizeDashboardSummary(&summary)
			c.JSON(http.StatusOK, summary)
			return
		}
		fail(c, fmt.Errorf("fetch dashboard publication counts: %w", err))
		return
	}

	switch status {
	case artifactStatusPublished:
		if summary.Total == 0 {
			summary.Total = publishedCount
		}
		summary.PublishedCount = publishedCount
		summary.UnpublishedCount = 0
	case artifactStatusUnpublished:
		summary.Total = unpublishedCount
		summary.PublishedCount = 0
		summary.UnpublishedCount = unpublishedCount
	default:
		// 首页 total 统一使用聚合真值层的唯一 session 数，不再把上游 published/unpublished 两路 total 直接相加。
		summary.PublishedCount = publishedCount
		summary.UnpublishedCount = unpublishedCount
	}
	finalizeDashboardSummary(&summary)
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) getDashboardSummaryDB(c *gin.Context) {
	tr := timeRangeFromQuery(c)
	summary, err := h.buildDashboardSummaryFromAggregates(tr, artifactStatusPublished)
	if err != nil {
		fail(c, fmt.Errorf("build dashboard summary from db: %w", err))
		return
	}
	if summary.Total == 0 {
		summary.Total = summary.AnalyzedCount
	}
	if summary.PublishedCount == 0 {
		summary.PublishedCount = summary.Total
	}
	finalizeDashboardSummary(&summary)
	c.JSON(http.StatusOK, summary)
}

func finalizeDashboardSummary(summary *apiDashboardSummary) {
	if summary == nil {
		return
	}
	if summary.Total < summary.AnalyzedCount {
		summary.Total = summary.AnalyzedCount
	}
	if summary.PublishedCount < 0 {
		summary.PublishedCount = 0
	}
	if summary.UnpublishedCount < 0 {
		summary.UnpublishedCount = 0
	}
	summary.PendingCount = summary.Total - summary.AnalyzedCount
	if summary.PendingCount < 0 {
		summary.PendingCount = 0
	}
	publicationBase := summary.PublishedCount + summary.UnpublishedCount
	if publicationBase > 0 {
		summary.PublishedRate = int(math.Round(float64(summary.PublishedCount) * 100 / float64(publicationBase)))
	} else if summary.Total > 0 {
		summary.PublishedRate = int(math.Round(float64(summary.PublishedCount) * 100 / float64(summary.Total)))
	}
}

func (h *Handler) buildDashboardSummaryFromAggregates(tr modellog.TimeRange, status string) (apiDashboardSummary, error) {
	summary := apiDashboardSummary{}
	if h == nil || h.db == nil || status == artifactStatusUnpublished {
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
	summary.PublishedCount = len(bundles)

	scores := make([]float64, 0, len(bundles))
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
			if dashboardScoreBand(*q.CombinedScore) != "green" {
				anomalyCount++
			}
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

func (h *Handler) fetchDashboardPublicationCounts(ctx context.Context, cookie string, tr modellog.TimeRange, status string) (publishedCount int, unpublishedCount int, err error) {
	if h == nil || h.upstream == nil {
		return 0, 0, nil
	}
	fetchTotal := func(onlyUnpublished bool) (int, error) {
		resp, err := h.upstream.List(ctx, cookie, modellog.ListRequest{
			TimeRange: tr,
			Page: modellog.Page{
				PageNo:   1,
				PageSize: 1,
			},
			OnlyUnpublishedArtifacts: onlyUnpublished,
		})
		if err != nil {
			return 0, err
		}
		return int(resp.Total), nil
	}

	switch status {
	case artifactStatusPublished:
		publishedCount, err = fetchTotal(false)
		return
	case artifactStatusUnpublished:
		unpublishedCount, err = fetchTotal(true)
		return
	default:
		var wg sync.WaitGroup
		var pubErr, unpubErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			publishedCount, pubErr = fetchTotal(false)
		}()
		go func() {
			defer wg.Done()
			unpublishedCount, unpubErr = fetchTotal(true)
		}()
		wg.Wait()
		if pubErr != nil {
			return 0, 0, pubErr
		}
		if unpubErr != nil {
			return 0, 0, unpubErr
		}
		return
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
