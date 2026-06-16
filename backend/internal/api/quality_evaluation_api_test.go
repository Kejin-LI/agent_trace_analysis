package api

import "testing"

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
