package api

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

var (
	apiSessionAggregateColsMu    sync.Mutex
	apiSessionAggregateColsCache map[string]struct{}
	apiSessionAggregateColsAt    time.Time
)

func apiSessionAggregateColumns(db *gorm.DB) map[string]struct{} {
	if db == nil {
		return nil
	}
	apiSessionAggregateColsMu.Lock()
	defer apiSessionAggregateColsMu.Unlock()
	if apiSessionAggregateColsCache != nil {
		if _, ok := apiSessionAggregateColsCache["has_issue"]; ok || time.Since(apiSessionAggregateColsAt) < time.Minute {
			return apiSessionAggregateColsCache
		}
	}
	apiSessionAggregateColsCache = map[string]struct{}{}
	apiSessionAggregateColsAt = time.Now()
	types, err := db.Migrator().ColumnTypes(&model.APISessionAggregate{})
	if err != nil {
		log.Printf("issue flag: detect api_session_aggregates columns failed: %v", err)
		return apiSessionAggregateColsCache
	}
	for _, t := range types {
		apiSessionAggregateColsCache[t.Name()] = struct{}{}
	}
	return apiSessionAggregateColsCache
}

func apiSessionAggregateHasIssueColumn(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	_, ok := apiSessionAggregateColumns(db)["has_issue"]
	return ok
}

func apiSessionAggregateHasPublicationStatusColumn(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	_, ok := apiSessionAggregateColumns(db)["artifact_publication_status"]
	return ok
}

func rulesJSONHasIssue(raw string) bool {
	if raw == "" {
		return false
	}
	var rules []apiRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return false
	}
	return rulesHaveIssue(rules)
}

func rulesHaveIssue(rules []apiRule) bool {
	for _, rule := range rules {
		if !rule.Passed {
			return true
		}
	}
	return false
}

func qualityEvaluationHasLLMIssue(row model.StgSessionQualityEvaluation) bool {
	bundle := apiSessionBundle{
		LLMScore:                  row.LLMScore,
		LLMJudgeScore:             row.LLMScore,
		LLMSentimentScore:         row.LLMSentimentScore,
		LLMResolvedScore:          row.LLMResolvedScore,
		LLMIntentMatchScore:       row.LLMIntentMatchScore,
		LLMEfficiencyFeelScore:    row.LLMEfficiencyFeelScore,
		LLMRepeatLoopScore:        row.LLMRepeatLoopScore,
		LLMActionabilityScore:     row.LLMActionabilityScore,
		LLMHallucinationRiskScore: row.LLMHallucinationRiskScore,
		CombinedScore:             row.CombinedScore,
		LLMEvalStatus:             row.LLMEvalStatus,
	}
	return dashboardBundleHasLLMIssueTag(bundle)
}

func aggregateRowHasIssue(row model.APISessionAggregate) bool {
	return row.HasIssue || rulesJSONHasIssue(row.RulesJSON)
}

func latestQualityEvaluationForAggregate(db *gorm.DB, sessionID, artifactID string) (model.StgSessionQualityEvaluation, bool) {
	if db == nil {
		return model.StgSessionQualityEvaluation{}, false
	}
	var row model.StgSessionQualityEvaluation
	q := db.Where("is_deleted = 0")
	switch {
	case sessionID != "" && artifactID != "":
		q = q.Where("(session_id = ? OR artifact_id = ?)", sessionID, artifactID)
	case sessionID != "":
		q = q.Where("session_id = ?", sessionID)
	case artifactID != "":
		q = q.Where("artifact_id = ?", artifactID)
	default:
		return model.StgSessionQualityEvaluation{}, false
	}
	cols := (&Handler{db: db}).filterExistingColumns([]string{
		"id",
		"session_id",
		"artifact_id",
		"llm_score",
		"llm_eval_status",
		"llm_resolved_score",
		"llm_intent_match_score",
		"llm_sentiment_score",
		"llm_efficiency_feel_score",
		"llm_actionability_score",
		"llm_hallucination_risk_score",
		"combined_score",
		"updated_at",
	})
	if err := q.Select(cols).Order("updated_at DESC, id DESC").First(&row).Error; err != nil {
		return model.StgSessionQualityEvaluation{}, false
	}
	return row, true
}

func aggregateIssueFlagForSession(db *gorm.DB, sessionID, artifactID, rulesJSON string) bool {
	if rulesJSONHasIssue(rulesJSON) {
		return true
	}
	if row, ok := latestQualityEvaluationForAggregate(db, sessionID, artifactID); ok {
		return qualityEvaluationHasLLMIssue(row)
	}
	return false
}

func (h *Handler) refreshAggregateIssueForSessionID(sessionID string) {
	if h == nil || h.db == nil || sessionID == "" || !apiSessionAggregateHasIssueColumn(h.db) {
		return
	}
	eval, ok := latestQualityEvaluationForAggregate(h.db, sessionID, "")
	if !ok {
		return
	}
	h.refreshAggregateIssueForQualityEvaluation(eval)
}

func (h *Handler) refreshAggregateIssueForQualityEvaluation(eval model.StgSessionQualityEvaluation) {
	if h == nil || h.db == nil || !apiSessionAggregateHasIssueColumn(h.db) {
		return
	}
	var row model.APISessionAggregate
	q := h.db.Select("id", "session_id", "artifact_id", "aggregate_date", "rules_json", "has_issue")
	switch {
	case eval.SessionID != "" && eval.ArtifactID != "":
		q = q.Where("(session_id = ? OR artifact_id = ?)", eval.SessionID, eval.ArtifactID)
	case eval.SessionID != "":
		q = q.Where("session_id = ?", eval.SessionID)
	case eval.ArtifactID != "":
		q = q.Where("artifact_id = ?", eval.ArtifactID)
	default:
		return
	}
	if err := q.Order("updated_at DESC, id DESC").First(&row).Error; err != nil {
		return
	}
	hasIssue := rulesJSONHasIssue(row.RulesJSON) || qualityEvaluationHasLLMIssue(eval)
	if err := h.db.Model(&model.APISessionAggregate{}).
		Where("id = ?", row.ID).
		Updates(map[string]interface{}{"has_issue": hasIssue, "updated_at": time.Now()}).Error; err != nil {
		log.Printf("issue flag: update session=%s failed: %v", row.SessionID, err)
		return
	}
	runner := h.aggregator
	if runner == nil {
		runner = &Aggregator{db: h.db}
	}
	if err := runner.refreshDailySummary(row.AggregateDate); err != nil {
		log.Printf("issue flag: refresh summary date=%s failed: %v", row.AggregateDate.Format("2006-01-02"), err)
	}
}
