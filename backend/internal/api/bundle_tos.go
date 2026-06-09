package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/tracelog"
)

// cachedMetrics 写回到 stg_session_sources.extra.cached_metrics 的字段子集。
// 仅缓存列表页雷达计算所需的特征，不缓存对话内容。
type cachedMetrics struct {
	ToolCalls     int     `json:"tool_calls"`
	UniqueTools   int     `json:"unique_tools"`
	MaxSerialRun  int     `json:"max_serial_run"`
	ToolFailures  int     `json:"tool_failures"`
	ToolFailRate  float64 `json:"tool_fail_rate"`
	AvgTokensTurn int64   `json:"avg_tokens_per_turn"`
	ToolRetries   int     `json:"tool_retries"`
	HasRootFail   bool    `json:"has_root_fail"`
	HasLoop       bool    `json:"has_loop"`
	Turns         int     `json:"turns"`
	TraceCount    int     `json:"trace_count"`
	DurationMs    int64   `json:"duration_ms"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	Score         int     `json:"score"`
	Radar         apiRadar `json:"radar"`
	ResponseScore int     `json:"response_score"`
	StabilityScore int    `json:"stability_score"`
	ThinkingScore int     `json:"thinking_score"`
	ResourceScore int     `json:"resource_score"`
	OrchestrationScore int `json:"orchestration_score"`
	AbnormalLevel int     `json:"abnormal_level"`
	HasFinalAnswer bool   `json:"has_final_answer"`
	NoOpStreak    int     `json:"no_op_streak"`
	// 异常标签 & 规则结果（驱动列表页"异常"列）。
	Chip      string    `json:"chip"`
	Rules     []apiRule `json:"rules"`
	Title     string    `json:"title"`
	Trace     string    `json:"trace"`
	UpdatedAt int64     `json:"updated_at"`
}

// extractCachedMetrics 从 buildBundleFromTOS 已经算好的 bundle 中抽出可缓存指标。
func extractCachedMetrics(b apiSessionBundle) cachedMetrics {
	pre := computeBundlePrecomputedMetrics(b)
	return cachedMetrics{
		ToolCalls:     b.Features.ToolCalls,
		UniqueTools:   b.Features.UniqueTools,
		MaxSerialRun:  b.Features.MaxSerialRun,
		ToolFailures:  b.Features.ToolFailures,
		ToolFailRate:  b.Features.ToolFailRate,
		AvgTokensTurn: b.Features.AvgTokensPerTurn,
		ToolRetries:   b.Features.ToolRetries,
		HasRootFail:   b.Features.HasRootFail,
		HasLoop:       b.Features.HasLoop,
		Turns:         b.Turns,
		TraceCount:    b.TraceCount,
		DurationMs:    b.DurationMs,
		InputTokens:   b.InputTokens,
		OutputTokens:  b.OutputTokens,
		TotalTokens:   b.InputTokens + b.OutputTokens,
		Score:         pre.Score,
		Radar:         pre.Radar,
		ResponseScore: pre.Radar.Response,
		StabilityScore: pre.Radar.Stability,
		ThinkingScore: pre.Radar.Thinking,
		ResourceScore: pre.Radar.Resource,
		OrchestrationScore: pre.Radar.Orchestration,
		AbnormalLevel: pre.AbnormalLevel,
		HasFinalAnswer: pre.HasFinalAnswer,
		NoOpStreak:    pre.NoOpStreak,
		Chip:          b.Chip,
		Rules:         b.Rules,
		Title:         b.Title,
		Trace:         b.Trace,
		UpdatedAt:     time.Now().Unix(),
	}
}

// mergeCachedMetricsIntoExtra 把指标 merge 进 extra JSON（保留导入期已有的 obj_size 等字段）。
func mergeCachedMetricsIntoExtra(oldExtra string, m cachedMetrics) (string, error) {
	merged := map[string]interface{}{}
	if oldExtra != "" {
		_ = json.Unmarshal([]byte(oldExtra), &merged)
	}
	merged["cached_metrics"] = m
	buf, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// readCachedMetrics 从 extra JSON 读出 cached_metrics，没有就返回 zero。
func readCachedMetrics(extra string) (cachedMetrics, bool) {
	if extra == "" {
		return cachedMetrics{}, false
	}
	var holder struct {
		CachedMetrics *cachedMetrics `json:"cached_metrics"`
	}
	if err := json.Unmarshal([]byte(extra), &holder); err != nil || holder.CachedMetrics == nil {
		return cachedMetrics{}, false
	}
	return *holder.CachedMetrics, true
}

// buildBundleFromTOS 把 stg_session_sources 行 + 实时拉取的 ParseResult 拼成 apiSessionBundle。
//
// 映射关系：
//   - 1 个 promptId（Round）= 1 个 apiTrace
//   - Round 内每次 LLM 调用（callRec）= 1 个 model span + N 个 tool span
//
// 详情数据完整无截断（直接来自 RESPONSE_BODY_FINAL.text/reasoning/tools）。
func buildBundleFromTOS(src model.StgSessionSource, pr *tracelog.ParseResult) apiSessionBundle {
	traces := make([]apiTrace, 0, len(pr.Rounds))
	var (
		totalDuration int64
		totalIn       int64
		totalOut      int64
		toolCalls     int
	)

	for i, r := range pr.Rounds {
		spans := buildSpansFromRound(r, i)
		modelTurns := 0
		for _, sp := range spans {
			switch sp.SpanType {
			case "model":
				modelTurns++
			case "tool":
				toolCalls++
			}
		}
		traceDur := r.EndedMs - r.StartedMs
		if traceDur < 0 {
			traceDur = 0
		}
		title := r.UserPrompt
		if title == "" {
			title = fmt.Sprintf("Round %d", i+1)
		}
		traces = append(traces, apiTrace{
			TraceID:      r.PromptID,
			SpanID:       r.PromptID,
			Title:        title,
			UserPrompt:   r.UserPrompt,
			RoundCount:   1,
			Turns:        modelTurns,
			DurationMs:   traceDur,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			StartedAtMs:  r.StartedMs,
			StartedAt:    msToString(r.StartedMs),
			Status:       "success",
			Spans:        spans,
		})
		totalDuration += traceDur
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
	}

	startedMs := int64(0)
	if len(pr.Rounds) > 0 {
		startedMs = pr.Rounds[0].StartedMs
	}
	if startedMs == 0 && src.SourceCreatedAt != nil {
		startedMs = src.SourceCreatedAt.UnixMilli()
	}

	title := ""
	firstTraceID := ""
	if len(traces) > 0 {
		title = traces[0].Title
		firstTraceID = traces[0].TraceID
	}
	if title == "" {
		title = "Session " + src.SessionID
	}

	turns := 0
	for _, tr := range traces {
		turns += tr.Turns
	}

	features, rules := deriveSessionSignals(traces, totalDuration, totalIn, totalOut)
	features.ToolCalls = toolCalls

	return apiSessionBundle{
		ID:           pickFirstNonEmpty(src.SessionID, src.ArtifactID),
		SessionID:    src.SessionID,
		ArtifactID:   src.ArtifactID,
		User:         src.UserName,
		UserID:       src.UserID,
		Title:        title,
		Trace:        firstTraceID,
		StartedAtMs:  startedMs,
		StartedAt:    msToString(startedMs),
		DurationMs:   totalDuration,
		InputTokens:  totalIn,
		OutputTokens: totalOut,
		ToolCalls:    toolCalls,
		Turns:        turns,
		TraceCount:   len(traces),
		Score:        0,
		Color:        "green",
		Chip:         pickChip(rules),
		Features:     features,
		Radar:        apiRadar{},
		Rules:        rules,
		Traces:       traces,
	}
}

// buildSpansFromRound 把一个 Round 内的 LLM 调用展开成 model + tool spans。
func buildSpansFromRound(r tracelog.Round, roundIdx int) []apiSpan {
	out := make([]apiSpan, 0, len(r.Calls)*2)
	for ci, c := range r.Calls {
		modelSpanID := fmt.Sprintf("%s-c%d", r.PromptID, ci)
		// model span：input 用首条 user 消息预览，output 用 final.text + reasoning。
		modelOut := assembleModelOutput(c.Text, c.Reasoning)
		modelInput := r.UserPrompt
		if ci > 0 {
			// 后续调用通常是工具结果回填，input 用 messages 末尾整体序列化便于审查。
			if buf, err := json.Marshal(c.Messages); err == nil {
				modelInput = string(buf)
			}
		}
		dur := c.DurationMs
		if dur == 0 && c.EndedMs > c.StartedMs {
			dur = c.EndedMs - c.StartedMs
		}
		modelTags := map[string]string{
			"model_name":       c.Model,
			"input_tokens":     strconv.FormatInt(c.UsageIn, 10),
			"output_tokens":    strconv.FormatInt(c.UsageOut, 10),
			"reasoning_tokens": strconv.FormatInt(c.UsageReason, 10),
		}
		out = append(out, apiSpan{
			SpanID:       modelSpanID,
			ParentID:     r.PromptID,
			SpanName:     c.Model,
			SpanType:     "model",
			DurationMs:   dur,
			StartedAtMs:  c.StartedMs,
			StartedAt:    msToString(c.StartedMs),
			StatusCode:   0,
			Input:        modelInput,
			Output:       modelOut,
			CustomTags:   mustJSON(modelTags),
			UserPrompt:   r.UserPrompt,
			PromptSource: "tos",
			RoundIndex:   roundIdx,
		})
		// tool spans
		for ti, t := range c.Tools {
			toolID := fmt.Sprintf("%s-c%d-t%d", r.PromptID, ci, ti)
			toolIn := string(t.Input)
			toolOut := string(t.Output)
			status := 0
			if t.Error != "" {
				status = 1
			}
			out = append(out, apiSpan{
				SpanID:      toolID,
				ParentID:    modelSpanID,
				SpanName:    t.Tool,
				SpanType:    "tool",
				DurationMs:  0,
				StartedAtMs: c.StartedMs,
				StartedAt:   msToString(c.StartedMs),
				StatusCode:  status,
				Input:       toolIn,
				Output:      toolOut,
				CustomTags:  "{}",
				RoundIndex:  roundIdx,
			})
		}
	}
	return out
}

// assembleModelOutput 把 reasoning + text 组装成可读的 output 预览。
// 前端会按已有逻辑识别并渲染（兼容 reasoning/reasoning_content 字段）。
func assembleModelOutput(text, reasoning string) string {
	payload := map[string]string{}
	if text != "" {
		payload["text"] = text
	}
	if reasoning != "" {
		payload["reasoning"] = reasoning
	}
	if len(payload) == 0 {
		return ""
	}
	return mustJSON(payload)
}

func mustJSON(v interface{}) string {
	if buf, err := json.Marshal(v); err == nil {
		return string(buf)
	}
	return "{}"
}

func msToString(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}
