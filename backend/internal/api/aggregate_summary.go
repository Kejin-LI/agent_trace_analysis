package api

import (
	"math"
	"sort"
	"sync"
	"time"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

type dailySummaryAccumulator struct {
	mu sync.Mutex

	userIDs map[string]struct{}

	sessionCount         int
	abnormalSessionCount int
	failedSessionCount   int
	loopSessionCount     int

	totalInputTokens   int64
	totalOutputTokens  int64
	totalTokens        int64
	totalToolCalls     int64
	totalToolFailures  int64
	totalDurationMs    int64
	totalTurns         int64
	totalScore         int64
	totalResponse      int64
	totalStability     int64
	totalThinking      int64
	totalResource      int64
	totalOrchestration int64

	stabilityCount     int
	orchestrationCount int

	durations []int64
}

func newDailySummaryAccumulator() *dailySummaryAccumulator {
	return &dailySummaryAccumulator{
		userIDs:   make(map[string]struct{}),
		durations: make([]int64, 0, 256),
	}
}

func (a *dailySummaryAccumulator) Add(s modellog.Session, m cachedMetrics) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.sessionCount++
	if s.UserID != "" {
		a.userIDs[s.UserID] = struct{}{}
	}
	if m.AbnormalLevel > 0 {
		a.abnormalSessionCount++
	}
	if m.HasRootFail {
		a.failedSessionCount++
	}
	if m.HasLoop {
		a.loopSessionCount++
	}

	a.totalInputTokens += m.InputTokens
	a.totalOutputTokens += m.OutputTokens
	a.totalTokens += m.TotalTokens
	a.totalToolCalls += int64(m.ToolCalls)
	a.totalToolFailures += int64(m.ToolFailures)
	a.totalDurationMs += m.DurationMs
	a.totalTurns += int64(m.Turns)
	a.totalScore += int64(m.Score)
	a.totalResponse += int64(m.ResponseScore)
	a.totalThinking += int64(m.ThinkingScore)
	a.totalResource += int64(m.ResourceScore)
	a.durations = append(a.durations, m.DurationMs)
	if m.StabilityScore > 0 {
		a.totalStability += int64(m.StabilityScore)
		a.stabilityCount++
	}
	if m.OrchestrationScore > 0 {
		a.totalOrchestration += int64(m.OrchestrationScore)
		a.orchestrationCount++
	}
}

func (a *dailySummaryAccumulator) ToModel(date time.Time) model.APIDailySummary {
	a.mu.Lock()
	defer a.mu.Unlock()

	sessionCount := a.sessionCount
	if sessionCount == 0 {
		return model.APIDailySummary{
			AggregateDate: date,
			AggregatedAt:  time.Now(),
		}
	}

	durations := append([]int64(nil), a.durations...)
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	summary := model.APIDailySummary{
		AggregateDate:        date,
		SessionCount:         sessionCount,
		ActiveUserCount:      len(a.userIDs),
		AbnormalSessionCount: a.abnormalSessionCount,
		FailedSessionCount:   a.failedSessionCount,
		LoopSessionCount:     a.loopSessionCount,
		TotalInputTokens:     a.totalInputTokens,
		TotalOutputTokens:    a.totalOutputTokens,
		TotalTokens:          a.totalTokens,
		TotalToolCalls:       a.totalToolCalls,
		TotalToolFailures:    a.totalToolFailures,
		AvgDurationMs:        int64(math.Round(float64(a.totalDurationMs) / float64(sessionCount))),
		AvgTurns:             round2(float64(a.totalTurns) / float64(sessionCount)),
		AvgScore:             round2(float64(a.totalScore) / float64(sessionCount)),
		P50DurationMs:        percentile(durations, 0.50),
		P90DurationMs:        percentile(durations, 0.90),
		P95DurationMs:        percentile(durations, 0.95),
		ResponseScoreAvg:     round2(float64(a.totalResponse) / float64(sessionCount)),
		ThinkingScoreAvg:     round2(float64(a.totalThinking) / float64(sessionCount)),
		ResourceScoreAvg:     round2(float64(a.totalResource) / float64(sessionCount)),
		AggregatedAt:         time.Now(),
	}
	if a.stabilityCount > 0 {
		summary.StabilityScoreAvg = round2(float64(a.totalStability) / float64(a.stabilityCount))
	}
	if a.orchestrationCount > 0 {
		summary.OrchestrationScoreAvg = round2(float64(a.totalOrchestration) / float64(a.orchestrationCount))
	}
	return summary
}

func buildDailySummaryFromAggregateRows(date time.Time, rows []model.APISessionAggregate) model.APIDailySummary {
	if len(rows) == 0 {
		return model.APIDailySummary{
			AggregateDate: date,
			AggregatedAt:  time.Now(),
		}
	}

	userIDs := make(map[string]struct{}, len(rows))
	durations := make([]int64, 0, len(rows))

	var abnormalSessionCount int
	var failedSessionCount int
	var loopSessionCount int

	var totalInputTokens int64
	var totalOutputTokens int64
	var totalTokens int64
	var totalToolCalls int64
	var totalToolFailures int64
	var totalDurationMs int64
	var totalTurns int64
	var totalScore int64
	var totalResponse int64
	var totalThinking int64
	var totalResource int64
	var totalStability int64
	var totalOrchestration int64
	var stabilityCount int
	var orchestrationCount int

	for _, row := range rows {
		if row.UserID != "" {
			userIDs[row.UserID] = struct{}{}
		}
		if row.AbnormalLevel > 0 {
			abnormalSessionCount++
		}
		if row.HasRootFail {
			failedSessionCount++
		}
		if row.HasLoop {
			loopSessionCount++
		}

		totalInputTokens += row.InputTokens
		totalOutputTokens += row.OutputTokens
		totalTokens += row.TotalTokens
		totalToolCalls += int64(row.ToolCalls)
		totalToolFailures += int64(row.ToolFailures)
		totalDurationMs += row.DurationMs
		totalTurns += int64(row.Turns)
		totalScore += int64(row.Score)
		totalResponse += int64(row.ResponseScore)
		totalThinking += int64(row.ThinkingScore)
		totalResource += int64(row.ResourceScore)
		durations = append(durations, row.DurationMs)
		if row.StabilityScore > 0 {
			totalStability += int64(row.StabilityScore)
			stabilityCount++
		}
		if row.OrchestrationScore > 0 {
			totalOrchestration += int64(row.OrchestrationScore)
			orchestrationCount++
		}
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	summary := model.APIDailySummary{
		AggregateDate:        date,
		SessionCount:         len(rows),
		ActiveUserCount:      len(userIDs),
		AbnormalSessionCount: abnormalSessionCount,
		FailedSessionCount:   failedSessionCount,
		LoopSessionCount:     loopSessionCount,
		TotalInputTokens:     totalInputTokens,
		TotalOutputTokens:    totalOutputTokens,
		TotalTokens:          totalTokens,
		TotalToolCalls:       totalToolCalls,
		TotalToolFailures:    totalToolFailures,
		AvgDurationMs:        int64(math.Round(float64(totalDurationMs) / float64(len(rows)))),
		AvgTurns:             round2(float64(totalTurns) / float64(len(rows))),
		AvgScore:             round2(float64(totalScore) / float64(len(rows))),
		P50DurationMs:        percentile(durations, 0.50),
		P90DurationMs:        percentile(durations, 0.90),
		P95DurationMs:        percentile(durations, 0.95),
		ResponseScoreAvg:     round2(float64(totalResponse) / float64(len(rows))),
		ThinkingScoreAvg:     round2(float64(totalThinking) / float64(len(rows))),
		ResourceScoreAvg:     round2(float64(totalResource) / float64(len(rows))),
		AggregatedAt:         time.Now(),
	}
	if stabilityCount > 0 {
		summary.StabilityScoreAvg = round2(float64(totalStability) / float64(stabilityCount))
	}
	if orchestrationCount > 0 {
		summary.OrchestrationScoreAvg = round2(float64(totalOrchestration) / float64(orchestrationCount))
	}
	return summary
}
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(math.Ceil(float64(len(sorted))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func parseAggregateDate(date string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return time.Now().Truncate(24 * time.Hour)
	}
	return t
}

func msToTimePtr(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}
