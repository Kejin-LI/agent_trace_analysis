package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

type qualityEvaluationRequest struct {
	SessionID                 string          `json:"session_id"`
	TraceID                   string          `json:"trace_id"`
	ArtifactID                string          `json:"artifact_id"`
	SessionTitle              string          `json:"session_title"`
	SessionUser               string          `json:"session_user"`
	SessionUserID             string          `json:"session_user_id"`
	SessionStartedAt          *time.Time      `json:"session_started_at"`
	SessionDurationMs         int64           `json:"session_duration_ms"`
	SessionTurns              int             `json:"session_turns"`
	SessionTraceCount         int             `json:"session_trace_count"`
	RuleScore                 *int            `json:"rule_score"`
	RuleGrade                 string          `json:"rule_grade"`
	RuleTags                  json.RawMessage `json:"rule_tags"`
	RuleSummary               string          `json:"rule_summary"`
	RuleSuggestions           json.RawMessage `json:"rule_suggestions"`
	RuleEvalResult            json.RawMessage `json:"rule_eval_result"`
	LLMScore                  *int            `json:"llm_score"`
	LLMGrade                  string          `json:"llm_grade"`
	LLMModel                  string          `json:"llm_model"`
	LLMTriggeredBy            string          `json:"llm_triggered_by"`
	LLMSentiment              string          `json:"llm_sentiment"`
	LLMSentimentScore         *int            `json:"llm_sentiment_score"`
	LLMResolved               string          `json:"llm_resolved"`
	LLMResolvedScore          *int            `json:"llm_resolved_score"`
	LLMIntentMatch            string          `json:"llm_intent_match"`
	LLMIntentMatchScore       *int            `json:"llm_intent_match_score"`
	LLMEfficiencyFeel         string          `json:"llm_efficiency_feel"`
	LLMEfficiencyFeelScore    *int            `json:"llm_efficiency_feel_score"`
	LLMRepeatLoop             string          `json:"llm_repeat_loop"`
	LLMRepeatLoopScore        *int            `json:"llm_repeat_loop_score"`
	LLMActionability          string          `json:"llm_actionability"`
	LLMActionabilityScore     *int            `json:"llm_actionability_score"`
	LLMHallucinationRisk      string          `json:"llm_hallucination_risk"`
	LLMHallucinationRiskScore *int            `json:"llm_hallucination_risk_score"`
	LLMTags                   json.RawMessage `json:"llm_tags"`
	LLMSummary                string          `json:"llm_summary"`
	LLMScoreBasis             string          `json:"llm_score_basis"`
	LLMSuggestions            json.RawMessage `json:"llm_suggestions"`
	LLMEvidence               json.RawMessage `json:"llm_evidence"`
	LLMEvalResult             json.RawMessage `json:"llm_eval_result"`
	LLMRawResult              json.RawMessage `json:"llm_raw_result"`
	LLMError                  string          `json:"llm_error"`
	CombinedScore             *int            `json:"combined_score"`
	CombinedGrade             string          `json:"combined_grade"`
	CombinedTags              json.RawMessage `json:"combined_tags"`
	CombinedSummary           string          `json:"combined_summary"`
	CombinedSuggestions       json.RawMessage `json:"combined_suggestions"`
	CombinedScoreBasis        string          `json:"combined_score_basis"`
}

func (h *Handler) getQualityEvaluation(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 为空"})
		return
	}
	var row model.StgSessionQualityEvaluation
	err := h.db.Where("session_id = ? AND is_deleted = 0", sessionID).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "quality evaluation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, qualityEvaluationResponse(row))
}

func (h *Handler) upsertQualityEvaluation(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	var req qualityEvaluationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 为空"})
		return
	}

	now := time.Now()
	version := 1
	var old model.StgSessionQualityEvaluation
	if err := h.db.Where("session_id = ? AND is_deleted = 0", req.SessionID).First(&old).Error; err == nil {
		version = old.LLMEvalVersion + 1
	}
	row := model.StgSessionQualityEvaluation{
		SessionID:                 trimLen(req.SessionID, 128),
		TraceID:                   trimLen(req.TraceID, 128),
		ArtifactID:                trimLen(req.ArtifactID, 128),
		SessionTitle:              trimLen(req.SessionTitle, 1024),
		SessionUser:               trimLen(req.SessionUser, 128),
		SessionUserID:             trimLen(req.SessionUserID, 128),
		SessionStartedAt:          req.SessionStartedAt,
		SessionDurationMs:         req.SessionDurationMs,
		SessionTurns:              req.SessionTurns,
		SessionTraceCount:         req.SessionTraceCount,
		RuleScore:                 clampScorePtr(req.RuleScore),
		RuleGrade:                 trimLen(req.RuleGrade, 32),
		RuleTags:                  jsonOrNull(req.RuleTags),
		RuleSummary:               trimLen(req.RuleSummary, 1024),
		RuleSuggestions:           jsonOrNull(req.RuleSuggestions),
		RuleEvalResult:            jsonOrNull(req.RuleEvalResult),
		RuleEvalAt:                &now,
		LLMScore:                  clampScorePtr(req.LLMScore),
		LLMGrade:                  trimLen(req.LLMGrade, 32),
		LLMModel:                  trimLen(firstReviewNonEmpty(req.LLMModel, "gpt-5.5"), 128),
		LLMEvalVersion:            version,
		LLMEvalStatus:             "succeeded",
		LLMTriggeredBy:            trimLen(firstReviewNonEmpty(req.LLMTriggeredBy, req.SessionUser, req.SessionUserID), 128),
		LLMEvaluatedAt:            &now,
		LLMSentiment:              trimLen(req.LLMSentiment, 32),
		LLMSentimentScore:         clampScorePtr(req.LLMSentimentScore),
		LLMResolved:               trimLen(req.LLMResolved, 32),
		LLMResolvedScore:          clampScorePtr(req.LLMResolvedScore),
		LLMIntentMatch:            trimLen(req.LLMIntentMatch, 32),
		LLMIntentMatchScore:       clampScorePtr(req.LLMIntentMatchScore),
		LLMEfficiencyFeel:         trimLen(req.LLMEfficiencyFeel, 32),
		LLMEfficiencyFeelScore:    clampScorePtr(req.LLMEfficiencyFeelScore),
		LLMRepeatLoop:             trimLen(req.LLMRepeatLoop, 32),
		LLMRepeatLoopScore:        clampScorePtr(req.LLMRepeatLoopScore),
		LLMActionability:          trimLen(req.LLMActionability, 32),
		LLMActionabilityScore:     clampScorePtr(req.LLMActionabilityScore),
		LLMHallucinationRisk:      trimLen(req.LLMHallucinationRisk, 32),
		LLMHallucinationRiskScore: clampScorePtr(req.LLMHallucinationRiskScore),
		LLMTags:                   jsonOrNull(req.LLMTags),
		LLMSummary:                trimLen(req.LLMSummary, 1024),
		LLMScoreBasis:             trimLen(req.LLMScoreBasis, 2048),
		LLMSuggestions:            jsonOrNull(req.LLMSuggestions),
		LLMEvidence:               jsonOrNull(req.LLMEvidence),
		LLMEvalResult:             jsonOrNull(req.LLMEvalResult),
		LLMRawResult:              jsonOrNull(req.LLMRawResult),
		LLMError:                  trimLen(req.LLMError, 1024),
		CombinedScore:             clampScorePtr(req.CombinedScore),
		CombinedGrade:             trimLen(req.CombinedGrade, 32),
		CombinedTags:              jsonOrNull(req.CombinedTags),
		CombinedSummary:           trimLen(req.CombinedSummary, 1024),
		CombinedSuggestions:       jsonOrNull(req.CombinedSuggestions),
		CombinedScoreBasis:        trimLen(req.CombinedScoreBasis, 1024),
		IsDeleted:                 0,
	}
	if err := h.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"trace_id", "artifact_id", "session_title", "session_user", "session_user_id",
			"session_started_at", "session_duration_ms", "session_turns", "session_trace_count",
			"rule_score", "rule_grade", "rule_tags", "rule_summary", "rule_suggestions", "rule_eval_result", "rule_eval_at",
			"llm_score", "llm_grade", "llm_model", "llm_eval_version", "llm_eval_status", "llm_triggered_by", "llm_evaluated_at",
			"llm_sentiment", "llm_sentiment_score", "llm_resolved", "llm_resolved_score",
			"llm_intent_match", "llm_intent_match_score", "llm_efficiency_feel", "llm_efficiency_feel_score", "llm_repeat_loop", "llm_repeat_loop_score",
			"llm_actionability", "llm_actionability_score", "llm_hallucination_risk", "llm_hallucination_risk_score",
			"llm_tags", "llm_summary", "llm_score_basis", "llm_suggestions", "llm_evidence", "llm_eval_result", "llm_raw_result", "llm_error",
			"combined_score", "combined_grade", "combined_tags", "combined_summary", "combined_suggestions", "combined_score_basis",
			"is_deleted", "updated_at",
		}),
	}).Create(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, qualityEvaluationResponse(row))
}

func (h *Handler) applyQualityEvaluations(bundles []apiSessionBundle) []apiSessionBundle {
	return h.applyQualityEvaluationsWithMode(bundles, false)
}

func (h *Handler) applyQualityEvaluationsFull(bundles []apiSessionBundle) []apiSessionBundle {
	return h.applyQualityEvaluationsWithMode(bundles, true)
}

func (h *Handler) applyQualityEvaluationsWithMode(bundles []apiSessionBundle, includeFullResult bool) []apiSessionBundle {
	if h == nil || h.db == nil || len(bundles) == 0 {
		return bundles
	}
	ids := make([]string, 0, len(bundles))
	for _, b := range bundles {
		if b.SessionID != "" {
			ids = append(ids, b.SessionID)
		}
	}
	if len(ids) == 0 {
		return bundles
	}
	var rows []model.StgSessionQualityEvaluation
	q := h.db.Where("session_id IN ? AND is_deleted = 0", ids)
	if !includeFullResult {
		q = q.Select([]string{
			"session_id",
			"rule_score",
			"llm_score",
			"llm_model",
			"llm_eval_version",
			"llm_evaluated_at",
			"llm_sentiment_score",
			"llm_resolved_score",
			"llm_intent_match_score",
			"llm_efficiency_feel_score",
			"llm_repeat_loop_score",
			"llm_actionability_score",
			"llm_hallucination_risk_score",
			"combined_score",
		})
	}
	if err := q.Find(&rows).Error; err != nil {
		return bundles
	}
	bySession := make(map[string]model.StgSessionQualityEvaluation, len(rows))
	for _, row := range rows {
		bySession[row.SessionID] = row
	}
	for i := range bundles {
		if row, ok := bySession[bundles[i].SessionID]; ok {
			applyQualityEvaluationToBundle(&bundles[i], row, includeFullResult)
		}
	}
	return bundles
}

func (h *Handler) applyQualityEvaluation(bundle apiSessionBundle) apiSessionBundle {
	enriched := h.applyQualityEvaluationsFull([]apiSessionBundle{bundle})
	if len(enriched) == 1 {
		return enriched[0]
	}
	return bundle
}

func (h *Handler) applyQualityEvaluationLight(bundle apiSessionBundle) apiSessionBundle {
	enriched := h.applyQualityEvaluations([]apiSessionBundle{bundle})
	if len(enriched) == 1 {
		return enriched[0]
	}
	return bundle
}

func applyQualityEvaluationToBundle(b *apiSessionBundle, row model.StgSessionQualityEvaluation, includeFullResult bool) {
	if b == nil {
		return
	}
	b.RuleScore = row.RuleScore
	b.LLMScore = row.LLMScore
	b.LLMJudgeScore = row.LLMScore
	b.CombinedScore = row.CombinedScore
	b.LLMJudgeModel = row.LLMModel
	b.LLMEvalVersion = row.LLMEvalVersion
	b.LLMEvaluatedAt = timeToString(row.LLMEvaluatedAt)
	b.LLMSentimentScore = row.LLMSentimentScore
	b.LLMResolvedScore = row.LLMResolvedScore
	b.LLMIntentMatchScore = row.LLMIntentMatchScore
	b.LLMEfficiencyFeelScore = row.LLMEfficiencyFeelScore
	b.LLMRepeatLoopScore = row.LLMRepeatLoopScore
	b.LLMActionabilityScore = row.LLMActionabilityScore
	b.LLMHallucinationRiskScore = row.LLMHallucinationRiskScore
	if includeFullResult {
		if raw := strings.TrimSpace(row.LLMEvalResult); raw != "" && raw != "null" {
			b.LLMJudgeResult = json.RawMessage(raw)
		}
	}
}

func qualityEvaluationResponse(row model.StgSessionQualityEvaluation) gin.H {
	return gin.H{
		"session_id":           row.SessionID,
		"trace_id":             row.TraceID,
		"artifact_id":          row.ArtifactID,
		"rule_score":           row.RuleScore,
		"llm_score":            row.LLMScore,
		"llm_judge_score":      row.LLMScore,
		"llm_judge_model":      row.LLMModel,
		"llm_eval_version":     row.LLMEvalVersion,
		"llm_evaluated_at":     timeToString(row.LLMEvaluatedAt),
		"llm_judge_result":     rawJSONOrNil(row.LLMEvalResult),
		"combined_score":       row.CombinedScore,
		"combined_score_basis": row.CombinedScoreBasis,
	}
}

func rawJSONOrNil(raw string) any {
	s := strings.TrimSpace(raw)
	if s == "" || s == "null" {
		return nil
	}
	return json.RawMessage(s)
}

func clampScorePtr(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	return &n
}
