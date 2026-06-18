package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

// stg_session_quality_evaluations 列存在性缓存。线上多个 RDS 实例存在 schema 漂移
// (例如曾出现缺失 llm_efficiency_feel_score 的库),GORM 默认 SELECT * 会导致整条
// 查询 1054 报错,详情页持久化结果消失。这里在启动后探测一次,后续 Select 仅保留实际存在的列。
var (
	qualityEvalColsOnce  sync.Once
	qualityEvalColsCache map[string]struct{}
)

func (h *Handler) qualityEvaluationColumns() map[string]struct{} {
	qualityEvalColsOnce.Do(func() {
		qualityEvalColsCache = map[string]struct{}{}
		if h == nil || h.db == nil {
			return
		}
		types, err := h.db.Migrator().ColumnTypes(&model.StgSessionQualityEvaluation{})
		if err != nil {
			return
		}
		for _, t := range types {
			qualityEvalColsCache[t.Name()] = struct{}{}
		}
	})
	return qualityEvalColsCache
}

func filterColumnsByAvailability(available map[string]struct{}, cols []string) []string {
	if len(cols) == 0 {
		return nil
	}
	if len(available) == 0 {
		out := make([]string, len(cols))
		copy(out, cols)
		return out
	}
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if _, ok := available[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

func filterAssignmentsByAvailability(available map[string]struct{}, values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(values))
	if len(available) == 0 {
		for k, v := range values {
			out[k] = v
		}
		return out
	}
	for k, v := range values {
		if _, ok := available[k]; ok {
			out[k] = v
		}
	}
	return out
}

func (h *Handler) filterExistingColumns(cols []string) []string {
	return filterColumnsByAvailability(h.qualityEvaluationColumns(), cols)
}

func (h *Handler) filterExistingAssignments(values map[string]interface{}) map[string]interface{} {
	return filterAssignmentsByAvailability(h.qualityEvaluationColumns(), values)
}

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

func (h *Handler) resolveQualityEvaluationSessionID(sessionID, artifactID string) string {
	sessionID = strings.TrimSpace(sessionID)
	artifactID = strings.TrimSpace(artifactID)
	if h == nil || h.db == nil {
		return sessionID
	}
	if sessionID != "" && (artifactID == "" || sessionID != artifactID) {
		return sessionID
	}
	candidates := make([]string, 0, 2)
	if sessionID != "" {
		candidates = append(candidates, sessionID)
	}
	if artifactID != "" && artifactID != sessionID {
		candidates = append(candidates, artifactID)
	}
	if len(candidates) == 0 {
		return ""
	}
	var agg model.APISessionAggregate
	if err := h.db.
		Select("session_id", "artifact_id", "updated_at").
		Where("session_id IN ? OR artifact_id IN ?", candidates, candidates).
		Order("updated_at DESC, id DESC").
		First(&agg).Error; err == nil && strings.TrimSpace(agg.SessionID) != "" {
		return strings.TrimSpace(agg.SessionID)
	}
	var src model.StgSessionSource
	if err := h.db.
		Select("session_id", "artifact_id", "source_updated_at", "id").
		Where("session_id IN ? OR artifact_id IN ?", candidates, candidates).
		Order("source_updated_at DESC, id DESC").
		First(&src).Error; err == nil && strings.TrimSpace(src.SessionID) != "" {
		return strings.TrimSpace(src.SessionID)
	}
	return sessionID
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
	err := h.db.
		Select(h.filterExistingColumns([]string{
			"id",
			"session_id",
			"artifact_id",
			"trace_id",
			"rule_score",
			"rule_grade",
			"rule_eval_at",
			"llm_score",
			"llm_grade",
			"llm_model",
			"llm_eval_status",
			"llm_eval_version",
			"llm_evaluated_at",
			"llm_sentiment", "llm_sentiment_score",
			"llm_resolved", "llm_resolved_score",
			"llm_intent_match", "llm_intent_match_score",
			"llm_efficiency_feel", "llm_efficiency_feel_score",
			"llm_repeat_loop", "llm_repeat_loop_score",
			"llm_actionability", "llm_actionability_score",
			"llm_hallucination_risk", "llm_hallucination_risk_score",
			"llm_summary",
			"llm_score_basis",
			"llm_eval_result",
			"llm_raw_result",
			"combined_score",
			"combined_grade",
			"combined_score_basis",
			"updated_at",
		})).
		Where("(session_id = ? OR artifact_id = ?) AND is_deleted = 0", sessionID, sessionID).
		Order("updated_at DESC, id DESC").
		First(&row).Error
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
	req.ArtifactID = strings.TrimSpace(req.ArtifactID)
	req.SessionID = h.resolveQualityEvaluationSessionID(req.SessionID, req.ArtifactID)
	if req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 为空"})
		return
	}
	if !isSupportedSessionID(req.SessionID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 session_id 以 ses_ 开头的会话"})
		return
	}

	now := time.Now()
	version := 1
	var old model.StgSessionQualityEvaluation
	if err := h.db.
		Select(h.filterExistingColumns([]string{"id", "session_id", "artifact_id", "llm_eval_version", "updated_at"})).
		Where("(session_id = ? OR artifact_id = ?) AND is_deleted = 0", req.SessionID, req.ArtifactID).
		Order("updated_at DESC, id DESC").
		First(&old).Error; err == nil {
		version = old.LLMEvalVersion + 1
	}
	row := model.StgSessionQualityEvaluation{
		SessionID:                 trimLen(req.SessionID, 128),
		TraceID:                   trimLen(req.TraceID, 128),
		ArtifactID:                trimLen(req.ArtifactID, 128),
		SessionTitle:              safeSessionTitleForDB(req.SessionTitle, 1024),
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
	values := h.filterExistingAssignments(map[string]interface{}{
		"session_id":                   row.SessionID,
		"trace_id":                     row.TraceID,
		"artifact_id":                  row.ArtifactID,
		"session_title":                row.SessionTitle,
		"session_user":                 row.SessionUser,
		"session_user_id":              row.SessionUserID,
		"session_started_at":           row.SessionStartedAt,
		"session_duration_ms":          row.SessionDurationMs,
		"session_turns":                row.SessionTurns,
		"session_trace_count":          row.SessionTraceCount,
		"rule_score":                   row.RuleScore,
		"rule_grade":                   row.RuleGrade,
		"rule_tags":                    row.RuleTags,
		"rule_summary":                 row.RuleSummary,
		"rule_suggestions":             row.RuleSuggestions,
		"rule_eval_result":             row.RuleEvalResult,
		"rule_eval_at":                 row.RuleEvalAt,
		"llm_score":                    row.LLMScore,
		"llm_grade":                    row.LLMGrade,
		"llm_model":                    row.LLMModel,
		"llm_eval_version":             row.LLMEvalVersion,
		"llm_eval_status":              row.LLMEvalStatus,
		"llm_triggered_by":             row.LLMTriggeredBy,
		"llm_evaluated_at":             row.LLMEvaluatedAt,
		"llm_sentiment":                row.LLMSentiment,
		"llm_sentiment_score":          row.LLMSentimentScore,
		"llm_resolved":                 row.LLMResolved,
		"llm_resolved_score":           row.LLMResolvedScore,
		"llm_intent_match":             row.LLMIntentMatch,
		"llm_intent_match_score":       row.LLMIntentMatchScore,
		"llm_efficiency_feel":          row.LLMEfficiencyFeel,
		"llm_efficiency_feel_score":    row.LLMEfficiencyFeelScore,
		"llm_repeat_loop":              row.LLMRepeatLoop,
		"llm_repeat_loop_score":        row.LLMRepeatLoopScore,
		"llm_actionability":            row.LLMActionability,
		"llm_actionability_score":      row.LLMActionabilityScore,
		"llm_hallucination_risk":       row.LLMHallucinationRisk,
		"llm_hallucination_risk_score": row.LLMHallucinationRiskScore,
		"llm_tags":                     row.LLMTags,
		"llm_summary":                  row.LLMSummary,
		"llm_score_basis":              row.LLMScoreBasis,
		"llm_suggestions":              row.LLMSuggestions,
		"llm_evidence":                 row.LLMEvidence,
		"llm_eval_result":              row.LLMEvalResult,
		"llm_raw_result":               row.LLMRawResult,
		"llm_error":                    row.LLMError,
		"combined_score":               row.CombinedScore,
		"combined_grade":               row.CombinedGrade,
		"combined_tags":                row.CombinedTags,
		"combined_summary":             row.CombinedSummary,
		"combined_suggestions":         row.CombinedSuggestions,
		"combined_score_basis":         row.CombinedScoreBasis,
		"is_deleted":                   row.IsDeleted,
	})
	updateColumns := h.filterExistingColumns([]string{
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
	})
	if err := h.db.Model(&model.StgSessionQualityEvaluation{}).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns(updateColumns),
	}).Create(values).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.refreshAggregateIssueForQualityEvaluation(row)
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
	sessionIDs := make([]string, 0, len(bundles))
	artifactIDs := make([]string, 0, len(bundles))
	for _, b := range bundles {
		sessionIDs = appendNonEmptyUnique(sessionIDs, b.SessionID)
		artifactIDs = appendNonEmptyUnique(artifactIDs, b.ArtifactID)
	}
	if len(sessionIDs) == 0 && len(artifactIDs) == 0 {
		return bundles
	}
	var rows []model.StgSessionQualityEvaluation
	q := h.db.Where("is_deleted = 0")
	switch {
	case len(sessionIDs) > 0 && len(artifactIDs) > 0:
		q = q.Where("(session_id IN ? OR artifact_id IN ?)", sessionIDs, artifactIDs)
	case len(sessionIDs) > 0:
		q = q.Where("session_id IN ?", sessionIDs)
	default:
		q = q.Where("artifact_id IN ?", artifactIDs)
	}
	if !includeFullResult {
		q = q.Select(h.filterExistingColumns([]string{
			"session_id",
			"artifact_id",
			"rule_score",
			"llm_score",
			"llm_model",
			"llm_eval_status",
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
			"updated_at",
			"id",
		}))
	} else {
		// 详情页全量模式:也用显式 Select,过滤掉线上缺失的列(schema 漂移容错),
		// 否则 GORM 按 model 全字段拼 SELECT,缺一列即整条 1054,持久化结果丢失。
		q = q.Select(h.filterExistingColumns([]string{
			"id",
			"session_id",
			"artifact_id",
			"trace_id",
			"rule_score",
			"rule_grade",
			"rule_eval_at",
			"llm_score",
			"llm_grade",
			"llm_model",
			"llm_eval_status",
			"llm_eval_version",
			"llm_evaluated_at",
			"llm_sentiment", "llm_sentiment_score",
			"llm_resolved", "llm_resolved_score",
			"llm_intent_match", "llm_intent_match_score",
			"llm_efficiency_feel", "llm_efficiency_feel_score",
			"llm_repeat_loop", "llm_repeat_loop_score",
			"llm_actionability", "llm_actionability_score",
			"llm_hallucination_risk", "llm_hallucination_risk_score",
			"llm_summary",
			"llm_score_basis",
			"llm_eval_result",
			"llm_raw_result",
			"combined_score",
			"combined_grade",
			"combined_score_basis",
			"updated_at",
		}))
	}
	if err := q.Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return bundles
	}
	bySession := make(map[string]model.StgSessionQualityEvaluation, len(rows))
	byArtifact := make(map[string]model.StgSessionQualityEvaluation, len(rows))
	for _, row := range rows {
		if row.SessionID != "" {
			if _, exists := bySession[row.SessionID]; !exists {
				bySession[row.SessionID] = row
			}
		}
		if row.ArtifactID != "" {
			if _, exists := byArtifact[row.ArtifactID]; !exists {
				byArtifact[row.ArtifactID] = row
			}
		}
	}
	for i := range bundles {
		switch {
		case bundles[i].SessionID != "":
			if row, ok := bySession[bundles[i].SessionID]; ok {
				applyQualityEvaluationToBundle(&bundles[i], row, includeFullResult)
				continue
			}
		}
		if bundles[i].ArtifactID != "" {
			if row, ok := byArtifact[bundles[i].ArtifactID]; ok {
				applyQualityEvaluationToBundle(&bundles[i], row, includeFullResult)
			}
		}
	}
	return bundles
}

func appendNonEmptyUnique(values []string, raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return values
	}
	for _, existing := range values {
		if existing == raw {
			return values
		}
	}
	return append(values, raw)
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
	b.LLMEvalStatus = row.LLMEvalStatus
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
		if llmJudgeResult := buildLLMJudgeResult(row); llmJudgeResult != nil {
			if raw, ok := llmJudgeResult.(json.RawMessage); ok {
				b.LLMJudgeResult = raw
			} else if raw, err := json.Marshal(llmJudgeResult); err == nil {
				b.LLMJudgeResult = raw
			}
		}
	}
}

func buildLLMJudgeResult(row model.StgSessionQualityEvaluation) any {
	// llm_judge_result 优先用结构化的 llm_eval_result;
	// 该字段缺失时(老数据 / schema 漂移 / 异步任务只回写 raw),
	// fallback 到 llm_raw_result;再不行就用 6 维分项分 + summary 兜底拼一个最小对象,
	// 保证前端永远不会因为读不到 result 对象而误显示"未评估"。
	llmJudgeResult := rawJSONOrNil(row.LLMEvalResult)
	if llmJudgeResult == nil {
		llmJudgeResult = rawJSONOrNil(row.LLMRawResult)
	}
	if llmJudgeResult == nil && row.LLMScore != nil {
		fallback := gin.H{
			"score":                    row.LLMScore,
			"reason":                   row.LLMSummary,
			"score_basis":              row.LLMScoreBasis,
			"resolved":                 row.LLMResolved,
			"resolved_score":           row.LLMResolvedScore,
			"intent_match":             row.LLMIntentMatch,
			"intent_match_score":       row.LLMIntentMatchScore,
			"efficiency_feel":          row.LLMEfficiencyFeel,
			"efficiency_feel_score":    row.LLMEfficiencyFeelScore,
			"sentiment":                row.LLMSentiment,
			"sentiment_score":          row.LLMSentimentScore,
			"actionability":            row.LLMActionability,
			"actionability_score":      row.LLMActionabilityScore,
			"hallucination_risk":       row.LLMHallucinationRisk,
			"hallucination_risk_score": row.LLMHallucinationRiskScore,
			"model_label":              row.LLMModel,
		}
		if b, err := json.Marshal(fallback); err == nil {
			llmJudgeResult = json.RawMessage(b)
		}
	}
	return llmJudgeResult
}

func qualityEvaluationResponse(row model.StgSessionQualityEvaluation) gin.H {
	llmJudgeResult := buildLLMJudgeResult(row)
	return gin.H{
		"session_id":           row.SessionID,
		"trace_id":             row.TraceID,
		"artifact_id":          row.ArtifactID,
		"rule_score":           row.RuleScore,
		"llm_score":            row.LLMScore,
		"llm_judge_score":      row.LLMScore,
		"llm_judge_model":      row.LLMModel,
		"llm_eval_status":      row.LLMEvalStatus,
		"llm_eval_version":     row.LLMEvalVersion,
		"llm_evaluated_at":     timeToString(row.LLMEvaluatedAt),
		"llm_judge_result":     llmJudgeResult,
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
