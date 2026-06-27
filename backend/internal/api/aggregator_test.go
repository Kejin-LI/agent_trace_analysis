package api

import (
	"testing"
	"time"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

func TestIsNightlyFreshCompletionForYesterdayRequiresAfterMidnightRefresh(t *testing.T) {
	now := time.Date(2026, 6, 12, 3, 0, 0, 0, time.Local)
	targetDate := time.Date(2026, 6, 11, 0, 0, 0, 0, time.Local)

	row := model.APIDailyAggregateStatus{
		Status:           "completed",
		LastAggregatedAt: timePtr(time.Date(2026, 6, 11, 19, 22, 54, 0, time.Local)),
	}
	if isNightlyFreshCompletion(row, targetDate, now) {
		t.Fatalf("expected stale completion for yesterday to require rerun")
	}

	row.LastAggregatedAt = timePtr(time.Date(2026, 6, 12, 0, 5, 0, 0, time.Local))
	if !isNightlyFreshCompletion(row, targetDate, now) {
		t.Fatalf("expected post-midnight completion for yesterday to be treated as fresh")
	}
}

func TestIsNightlyFreshCompletionForTodayUsesSameDayCompletion(t *testing.T) {
	now := time.Date(2026, 6, 12, 3, 0, 0, 0, time.Local)
	targetDate := time.Date(2026, 6, 12, 0, 0, 0, 0, time.Local)

	row := model.APIDailyAggregateStatus{
		Status:     "completed",
		FinishedAt: timePtr(time.Date(2026, 6, 12, 1, 30, 0, 0, time.Local)),
	}
	if !isNightlyFreshCompletion(row, targetDate, now) {
		t.Fatalf("expected same-day completion for today to be treated as fresh")
	}

	row.FinishedAt = timePtr(time.Date(2026, 6, 11, 23, 50, 0, 0, time.Local))
	if isNightlyFreshCompletion(row, targetDate, now) {
		t.Fatalf("expected previous-day completion for today to require rerun")
	}
}

func TestAggregateStatusCompletedAtPrefersLastAggregatedAt(t *testing.T) {
	lastAggregatedAt := time.Date(2026, 6, 12, 3, 1, 0, 0, time.Local)
	finishedAt := time.Date(2026, 6, 12, 3, 0, 0, 0, time.Local)
	got, ok := aggregateStatusCompletedAt(model.APIDailyAggregateStatus{
		LastAggregatedAt: &lastAggregatedAt,
		FinishedAt:       &finishedAt,
	})
	if !ok {
		t.Fatalf("expected completion time to be available")
	}
	if !got.Equal(lastAggregatedAt) {
		t.Fatalf("expected last_aggregated_at to win, got %v", got)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
