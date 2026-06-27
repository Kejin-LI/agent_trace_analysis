package api

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
)

type bundlePrecomputedMetrics struct {
	Radar          apiRadar
	Score          int
	AbnormalLevel  int
	HasFinalAnswer bool
	NoOpStreak     int
}

func computeBundlePrecomputedMetrics(b apiSessionBundle) bundlePrecomputedMetrics {
	modelSpans := getModelSpansForBundle(b)
	toolSpans := getToolSpansForBundle(b)

	hasFinalAnswer, noOpStreak := modelSignals(modelSpans, toolSpans)
	responseScore := computeResponseScore(b, modelSpans, toolSpans)
	stabilityScore, stabilityApplicable := computeStabilityScore(b, toolSpans)
	thinkingScore := computeThinkingScore(hasFinalAnswer, noOpStreak, b)
	resourceScore := computeResourceScore(b)
	orchestrationScore, orchestrationApplicable := computeOrchestrationScore(b, toolSpans)

	radar := apiRadar{
		Response:      responseScore,
		Thinking:      thinkingScore,
		Resource:      resourceScore,
		Stability:     0,
		Orchestration: 0,
	}
	values := []int{responseScore, thinkingScore, resourceScore}
	if stabilityApplicable {
		radar.Stability = stabilityScore
		values = append(values, stabilityScore)
	}
	if orchestrationApplicable {
		radar.Orchestration = orchestrationScore
		values = append(values, orchestrationScore)
	}

	score := averageInts(values)
	if hasExecutionFailureRule(b.Rules) {
		score = min(score, 50)
	}

	abnormalLevel := 0
	switch {
	case score < 50:
		abnormalLevel = 2
	case score < 85 || (strings.TrimSpace(b.Chip) != "" && b.Chip != "健康"):
		abnormalLevel = 1
	}

	return bundlePrecomputedMetrics{
		Radar:          radar,
		Score:          score,
		AbnormalLevel:  abnormalLevel,
		HasFinalAnswer: hasFinalAnswer,
		NoOpStreak:     noOpStreak,
	}
}

func getModelSpansForBundle(b apiSessionBundle) []apiSpan {
	out := make([]apiSpan, 0)
	for _, tr := range b.Traces {
		for _, sp := range tr.Spans {
			if sp.SpanType == "model" {
				out = append(out, sp)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAtMs < out[j].StartedAtMs })
	return out
}

func getToolSpansForBundle(b apiSessionBundle) []apiSpan {
	out := make([]apiSpan, 0)
	for _, tr := range b.Traces {
		for _, sp := range tr.Spans {
			if sp.SpanType == "tool" {
				out = append(out, sp)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAtMs < out[j].StartedAtMs })
	return out
}

func modelSignals(modelSpans, toolSpans []apiSpan) (bool, int) {
	hasFinalAnswer := false
	noOpStreak := 0
	cur := 0
	for _, sp := range modelSpans {
		childTools := 0
		for _, tool := range toolSpans {
			if tool.ParentID == sp.SpanID {
				childTools++
			}
		}
		content := strings.TrimSpace(extractModelText(sp.Output))
		if content != "" && childTools == 0 {
			hasFinalAnswer = true
		}
		if content == "" && childTools == 0 {
			cur++
			if cur > noOpStreak {
				noOpStreak = cur
			}
		} else {
			cur = 0
		}
	}
	return hasFinalAnswer, noOpStreak
}

func computeResponseScore(b apiSessionBundle, modelSpans, toolSpans []apiSpan) int {
	toolCalls := b.Features.ToolCalls
	durSec := maxInt(0, int(math.Round(float64(b.DurationMs)/1000)))
	full, floor := responseBucket(toolCalls)
	rawExperience := 100.0
	if durSec > full {
		if floor <= full {
			rawExperience = 30
		} else {
			rawExperience = math.Max(30, 100-(float64(durSec-full)/float64(floor-full))*70)
		}
	}
	experienceScore := int(math.Round(rawExperience))
	if hasExecutionFailureRule(b.Rules) {
		experienceScore = min(experienceScore, 50)
	}

	penalty := 0
	if repeatedToolRun(toolSpans) >= 3 {
		penalty += 20
	}
	if hasRepetitiveEdits(toolSpans) {
		penalty += 20
	}
	if hasThinkingRedundancy(modelSpans) {
		penalty += 10
	}
	if toolCalls >= 3 && !hasPlan(modelSpans) {
		penalty += 10
	}
	if planDeviation(modelSpans, len(modelSpans)) {
		penalty += 10
	}
	if hasTestResidue(toolSpans) {
		penalty += 5
	}
	ruleScore := maxInt(0, 100-penalty)
	return int(math.Round(0.4*float64(experienceScore) + 0.6*float64(ruleScore)))
}

func computeStabilityScore(b apiSessionBundle, toolSpans []apiSpan) (int, bool) {
	if len(toolSpans) == 0 && b.Features.ToolCalls == 0 {
		return 0, false
	}
	failRate := b.Features.ToolFailRate
	if failRate > 1 {
		failRate = failRate / 100
	}
	if failRate < 0 {
		failRate = 0
	}
	if failRate > 1 {
		failRate = 1
	}
	score := int(math.Round((1-failRate)*100 - float64(b.Features.ToolFailures*5)))
	if hasExecutionFailureRule(b.Rules) {
		score = min(score, 50)
	}
	return clamp(score, 0, 100), true
}

func computeThinkingScore(hasFinalAnswer bool, noOpStreak int, b apiSessionBundle) int {
	score := 100
	if !hasFinalAnswer {
		score -= 35
	}
	score -= min(noOpStreak*10, 30)
	if b.Features.HasLoop {
		score -= 15
	}
	return clamp(score, 0, 100)
}

func computeResourceScore(b apiSessionBundle) int {
	avgTokens := b.Features.AvgTokensPerTurn
	score := 100.0
	if avgTokens > 76000 {
		if avgTokens >= 128000 {
			score = 60
		} else {
			score = 100 - (float64(avgTokens-76000)/float64(128000-76000))*40
		}
	}
	return clamp(int(math.Round(score)), 0, 100)
}

func computeOrchestrationScore(b apiSessionBundle, toolSpans []apiSpan) (int, bool) {
	toolCalls := maxInt(b.Features.ToolCalls, len(toolSpans))
	if toolCalls == 0 {
		return 0, false
	}
	score := 100
	switch {
	case b.Features.MaxSerialRun >= 7:
		score -= 30
	case b.Features.MaxSerialRun >= 5:
		score -= 20
	case b.Features.MaxSerialRun >= 3:
		score -= 10
	}
	diversity := 0.0
	if toolCalls > 0 {
		diversity = float64(b.Features.UniqueTools) / float64(toolCalls)
	}
	if diversity >= 0.6 {
		score += 20
	} else if diversity >= 0.4 {
		score += 10
	}
	return clamp(score, 0, 100), true
}

func hasExecutionFailureRule(rules []apiRule) bool {
	for _, r := range rules {
		if r.Passed {
			continue
		}
		if r.FailedLabel == "执行失败" || r.FailedLabel == "trace 失败" || r.Name == "执行失败" {
			return true
		}
	}
	return false
}

func repeatedToolRun(toolSpans []apiSpan) int {
	prev := ""
	cur := 0
	best := 0
	for _, sp := range toolSpans {
		key := sp.SpanName + "::" + sp.Input
		if key == prev {
			cur++
		} else {
			cur = 1
		}
		prev = key
		if cur > best {
			best = cur
		}
	}
	return best
}

func hasRepetitiveEdits(toolSpans []apiSpan) bool {
	counts := map[string]int{}
	for _, sp := range toolSpans {
		name := strings.ToLower(sp.SpanName)
		if !strings.Contains(name, "write") &&
			!strings.Contains(name, "create") &&
			!strings.Contains(name, "edit") &&
			!strings.Contains(name, "rewrite") &&
			!strings.Contains(name, "save") &&
			!strings.Contains(name, "update") &&
			!strings.Contains(name, "append") &&
			!strings.Contains(name, "insert") &&
			!strings.Contains(name, "replace") &&
			!strings.Contains(name, "apply_patch") {
			continue
		}
		path := toolPathFromInput(sp.Input)
		if path == "" {
			continue
		}
		counts[path]++
		if counts[path] >= 3 {
			return true
		}
	}
	return false
}

func hasThinkingRedundancy(modelSpans []apiSpan) bool {
	maxLen := 0
	total := 0
	for _, sp := range modelSpans {
		text := extractModelText(sp.Output)
		l := len(text)
		if l > maxLen {
			maxLen = l
		}
		total += l
	}
	if len(modelSpans) == 0 {
		return false
	}
	avg := total / len(modelSpans)
	return maxLen > 12000 || (len(modelSpans) >= 3 && maxLen > avg*3 && maxLen > 4000)
}

func hasPlan(modelSpans []apiSpan) bool {
	var sb strings.Builder
	for i, sp := range modelSpans {
		if i >= 3 {
			break
		}
		sb.WriteString(extractModelText(sp.Output))
		sb.WriteString("\n")
	}
	first := sb.String()
	return strings.Contains(first, "Plan:") ||
		strings.Contains(first, "计划") ||
		strings.Contains(first, "步骤") ||
		strings.Contains(first, "首先")
}

func planDeviation(modelSpans []apiSpan, actualSteps int) bool {
	var sb strings.Builder
	for i, sp := range modelSpans {
		if i >= 3 {
			break
		}
		sb.WriteString(extractModelText(sp.Output))
		sb.WriteString("\n")
	}
	first := sb.String()
	planCount := strings.Count(first, "1.") + strings.Count(first, "2.") + strings.Count(first, "3.")
	if planCount < 2 {
		return false
	}
	return actualSteps < planCount || float64(actualSteps) > float64(planCount)*1.5
}

func hasTestResidue(toolSpans []apiSpan) bool {
	hasTempCreate := false
	for _, sp := range toolSpans {
		merged := strings.ToLower(sp.SpanName + " " + sp.Input)
		if (strings.Contains(merged, "mkdir") || strings.Contains(merged, "touch") || strings.Contains(merged, "write") || strings.Contains(merged, "create")) &&
			(strings.Contains(merged, "tmp") || strings.Contains(merged, "temp") || strings.Contains(merged, "mock") || strings.Contains(merged, "debug") || strings.Contains(merged, "test")) {
			hasTempCreate = true
			break
		}
	}
	if !hasTempCreate {
		return false
	}
	start := maxInt(0, len(toolSpans)-3)
	for _, sp := range toolSpans[start:] {
		merged := strings.ToLower(sp.SpanName + " " + sp.Input)
		if strings.Contains(merged, "rm") || strings.Contains(merged, "delete") || strings.Contains(merged, "cleanup") || strings.Contains(merged, "remove") {
			return false
		}
	}
	return true
}

func toolPathFromInput(input string) string {
	m := safeJSONMap(input)
	for _, key := range []string{"filePath", "file_path", "path", "outputPath", "output_path", "target_path"} {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func extractModelText(output string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	m := safeJSONMap(output)
	for _, key := range []string{"text", "reasoning", "reasoning_content"} {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	if choices, ok := m["choices"].([]interface{}); ok {
		parts := make([]string, 0, len(choices))
		for _, raw := range choices {
			choice, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			msg, _ := choice["message"].(map[string]interface{})
			if content, ok := msg["content"].(string); ok && strings.TrimSpace(content) != "" {
				parts = append(parts, content)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	var s string
	if err := json.Unmarshal([]byte(output), &s); err == nil {
		return s
	}
	return strings.TrimSpace(output)
}

func averageInts(vals []int) int {
	if len(vals) == 0 {
		return 0
	}
	sum := 0
	for _, v := range vals {
		sum += v
	}
	return int(math.Round(float64(sum) / float64(len(vals))))
}

func clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
