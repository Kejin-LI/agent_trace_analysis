package api

import (
	"encoding/json"
	"testing"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

func TestFilterColumnsByAvailabilityPreservesExistingColumns(t *testing.T) {
	available := map[string]struct{}{
		"session_id":      {},
		"llm_eval_status": {},
		"updated_at":      {},
	}

	got := filterColumnsByAvailability(available, []string{
		"session_id",
		"llm_efficiency_feel",
		"llm_eval_status",
		"updated_at",
	})

	want := []string{"session_id", "llm_eval_status", "updated_at"}
	if len(got) != len(want) {
		t.Fatalf("unexpected filtered column count: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected filtered columns: got=%v want=%v", got, want)
		}
	}
}

func TestFilterAssignmentsByAvailabilityDropsUnknownColumns(t *testing.T) {
	available := map[string]struct{}{
		"session_id":       {},
		"llm_eval_status":  {},
		"llm_evaluated_at": {},
	}

	got := filterAssignmentsByAvailability(available, map[string]interface{}{
		"session_id":                "ses_123",
		"llm_eval_status":           "running",
		"llm_evaluated_at":          "2026-06-16T10:00:00Z",
		"llm_efficiency_feel":       "high",
		"llm_efficiency_feel_score": 91,
	})

	if _, ok := got["llm_efficiency_feel"]; ok {
		t.Fatalf("unexpected legacy-missing column kept in insert/update payload: %v", got)
	}
	if _, ok := got["llm_efficiency_feel_score"]; ok {
		t.Fatalf("unexpected legacy-missing score column kept in insert/update payload: %v", got)
	}
	if got["session_id"] != "ses_123" || got["llm_eval_status"] != "running" {
		t.Fatalf("expected required columns to survive filtering: %v", got)
	}
}

func TestBuildLLMJudgeResultFallsBackToStructuredSummary(t *testing.T) {
	score := 38
	resolvedScore := 20
	row := model.StgSessionQualityEvaluation{
		LLMScore:            &score,
		LLMModel:            "gpt-5.5",
		LLMSummary:          "问题未解决",
		LLMScoreBasis:       "根据会话证据判分",
		LLMResolved:         "未解决",
		LLMResolvedScore:    &resolvedScore,
		LLMIntentMatch:      "部分偏离",
		LLMEfficiencyFeel:   "偏低效",
		LLMSentiment:        "中性",
		LLMActionability:    "空泛不可执行",
		LLMHallucinationRisk:"中",
	}

	got := buildLLMJudgeResult(row)
	raw, ok := got.(json.RawMessage)
	if !ok || len(raw) == 0 {
		t.Fatalf("expected fallback llm judge result, got=%T %#v", got, got)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal fallback result: %v", err)
	}
	if decoded["reason"] != "问题未解决" {
		t.Fatalf("unexpected fallback reason: %#v", decoded)
	}
	if decoded["model_label"] != "gpt-5.5" {
		t.Fatalf("unexpected fallback model label: %#v", decoded)
	}
}
