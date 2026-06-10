// Command parsecheck 离线验证日志解析链路：喂一份样本日志文件，
// 打印 Parse + buildBundle 的关键指标，无需 DB / 网络 / Cookie / 发版。
//
// 用法：
//
//	go run ./cmd/parsecheck <样本文件路径>
//	go run ./cmd/parsecheck ../testurl
//
// 输出：trace_count / turns / tool_calls / tokens / duration，以及每一轮的摘要。
// 用于在本地复现"详情页空白 / 指标全 0"问题，定位是解析层还是上层。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/tracelog"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: go run ./cmd/parsecheck <样本文件路径> [-bundle 输出.json]")
		os.Exit(2)
	}
	path := os.Args[1]
	bundleOut := ""
	for i := 2; i < len(os.Args)-1; i++ {
		if os.Args[i] == "-bundle" {
			bundleOut = os.Args[i+1]
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("读取文件失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("== 文件: %s (%d 字节) ==\n", path, len(raw))
	if len(raw) == 0 {
		fmt.Println("文件为空。请把一份真实日志样本粘贴进该文件后重试。")
		os.Exit(1)
	}

	pr, err := tracelog.Parse(raw)
	if err != nil {
		fmt.Printf("解析失败(Parse error): %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("session_id = %q\n", pr.SessionID)
	fmt.Printf("rounds(轮次) = %d\n", len(pr.Rounds))

	if len(pr.Rounds) == 0 {
		fmt.Println("\n[诊断] 解析成功但 rounds=0 —— 这就是详情页空白/指标全 0 的根因。")
		fmt.Println("说明日志格式与 parser 期望的事件结构不匹配（logId/promptId/RESPONSE_BODY_FINAL 等）。")
		fmt.Println("下一步：检查样本里事件的 type 字段和层级，对照 parser.go 的解析分支。")
		return
	}

	var totalIn, totalOut, totalReason, totalDur int64
	var totalCalls int
	for i, r := range pr.Rounds {
		dur := r.EndedMs - r.StartedMs
		if dur < 0 {
			dur = 0
		}
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
		totalReason += r.ReasoningTokens
		totalDur += dur
		totalCalls += len(r.Calls)

		prompt := r.UserPrompt
		if rs := []rune(prompt); len(rs) > 40 {
			prompt = string(rs[:40]) + "…"
		}
		fmt.Printf("  round[%d] prompt=%q calls(LLM调用)=%d dur=%dms in=%d out=%d reason=%d\n",
			i, prompt, len(r.Calls), dur, r.InputTokens, r.OutputTokens, r.ReasoningTokens)
	}

	fmt.Println("\n== 汇总 ==")
	fmt.Printf("trace_count = %d\n", len(pr.Rounds))
	fmt.Printf("llm_calls   = %d\n", totalCalls)
	fmt.Printf("duration_ms = %d\n", totalDur)
	fmt.Printf("tokens      = in:%d out:%d reason:%d\n", totalIn, totalOut, totalReason)

	// DB 写入预览：模拟 buildBundleFromTOS -> api_session_aggregates 的核心列。
	// turns = 各轮 LLM 调用数之和（每次调用对应一个 model span）。
	totalTurns := totalCalls
	totalTokens := totalIn + totalOut
	fmt.Println("\n== 即将写入 api_session_aggregates 的核心列(预览) ==")
	fmt.Printf("  turns        = %d\n", totalTurns)
	fmt.Printf("  duration_ms  = %d\n", totalDur)
	fmt.Printf("  input_tokens = %d\n", totalIn)
	fmt.Printf("  output_tokens= %d\n", totalOut)
	fmt.Printf("  total_tokens = %d\n", totalTokens)

	// 全零脏数据校验：这正是历史上污染 DB、导致 0ms/0步、详情页卡死的元凶。
	if totalDur == 0 && totalTurns == 0 && totalTokens == 0 {
		fmt.Println("\n[拦截] 该 session 解析出全零(duration/turns/tokens 均为 0) —— 属于脏数据，")
		fmt.Println("       严禁入库。线上补库逻辑应跳过此类记录。请检查解析是否未匹配真实格式。")
		os.Exit(3)
	}

	if totalDur == 0 && totalIn == 0 && totalOut == 0 && totalCalls == 0 {
		fmt.Println("\n[诊断] 有 rounds 但指标全 0 —— 说明轮次被识别了，但内部 LLM 调用/usage 没被提取。")
		fmt.Println("重点检查：RESPONSE_BODY_FINAL.data.final.usage 与 logId 关联逻辑。")
	} else {
		fmt.Println("\n[诊断] 解析链路正常，指标非 0、非全零脏数据。")
		fmt.Println("        即将写入 DB 的核心列均有有效值，发版后 DB 应能看到对应数据。")
	}

	if bundleOut != "" {
		b := buildPreviewBundle(pr)
		buf, _ := json.MarshalIndent(b, "", "  ")
		if err := os.WriteFile(bundleOut, buf, 0o644); err != nil {
			fmt.Printf("\n写入 bundle 失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n[bundle] 已写出前端可消费的 bundle JSON -> %s (traces=%d)\n", bundleOut, len(b.Traces))
	}
}

// 以下结构与 internal/api 的 apiSpan/apiTrace/apiSessionBundle JSON 字段对齐，
// 仅用于本地用真实样本驱动前端 session-detail 页做可视化验证（多轮/计划卡片/澄清卡片）。
type previewSpan struct {
	SpanID      string `json:"span_id"`
	ParentID    string `json:"parent_id"`
	SpanName    string `json:"span_name"`
	SpanType    string `json:"span_type"`
	DurationMs  int64  `json:"duration_ms"`
	StartedAtMs int64  `json:"started_at_ms"`
	StatusCode  int    `json:"status_code"`
	Input       string `json:"input"`
	Output      string `json:"output"`
	CustomTags  string `json:"custom_tags"`
	UserPrompt  string `json:"user_prompt,omitempty"`
	RoundIndex  int    `json:"round_index,omitempty"`
}

type previewTrace struct {
	TraceID      string        `json:"trace_id"`
	SpanID       string        `json:"span_id"`
	Title        string        `json:"title"`
	UserPrompt   string        `json:"user_prompt,omitempty"`
	RoundCount   int           `json:"round_count,omitempty"`
	Turns        int           `json:"turns"`
	DurationMs   int64         `json:"duration_ms"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	StartedAtMs  int64         `json:"started_at_ms"`
	Status       string        `json:"status"`
	Spans        []previewSpan `json:"spans"`
}

// previewFeatures 与 internal/api 的 apiFeatures JSON 字段对齐，
// 前端 KPI（工具调用次数等）直接读 features.tool_calls，必须忠实复刻后端。
type previewFeatures struct {
	ToolCalls        int     `json:"tool_calls"`
	UniqueTools      int     `json:"unique_tools"`
	MaxSerialRun     int     `json:"max_serial_run"`
	ToolFailures     int     `json:"tool_failures"`
	ToolFailRate     float64 `json:"tool_fail_rate"`
	AvgTokensPerTurn int64   `json:"avg_tokens_per_turn"`
	ToolRetries      int     `json:"tool_retries"`
	HasRootFail      bool    `json:"has_root_fail"`
	HasLoop          bool    `json:"has_loop"`
}

// previewRule 与 internal/api 的 apiRule JSON 字段对齐，
// 前端「规则评估」卡片直接读 bundle.rules，必须忠实复刻后端 deriveSessionSignals。
type previewRule struct {
	Name        string `json:"name"`
	Passed      bool   `json:"passed"`
	FailedLabel string `json:"failed_label"`
	Detail      string `json:"detail"`
}

type previewBundle struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	Title        string          `json:"title"`
	Trace        string          `json:"trace"`
	TraceCount   int             `json:"trace_count"`
	Turns        int             `json:"turns"`
	DurationMs   int64           `json:"duration_ms"`
	InputTokens  int64           `json:"input_tokens"`
	OutputTokens int64           `json:"output_tokens"`
	ToolCalls    int             `json:"tool_calls"`
	Color        string          `json:"color"`
	Chip         string          `json:"chip"`
	Features     previewFeatures `json:"features"`
	Rules        []previewRule   `json:"rules"`
	Traces       []previewTrace  `json:"traces"`
}

func buildPreviewBundle(pr *tracelog.ParseResult) previewBundle {
	traces := make([]previewTrace, 0, len(pr.Rounds))
	totalTurns := 0
	var totalDur, totalIn, totalOut int64
	totalToolCalls := 0
	uniqueTools := map[string]struct{}{}
	dupKey := map[string]int{}
	// 规则评估所需统计（复刻后端 deriveSessionSignals）
	maxSerialRun, curSerialRun := 0, 0
	prevToolKey := ""
	toolFailures := 0
	hasFinalAnswer := false
	noOpStreak, curNoOp := 0, 0
	for i, r := range pr.Rounds {
		spans := make([]previewSpan, 0, len(r.Calls)*2)
		modelTurns := 0
		for ci, c := range r.Calls {
			modelSpanID := fmt.Sprintf("%s-c%d", r.PromptID, ci)
			tags, _ := json.Marshal(map[string]string{
				"model_name":    c.Model,
				"input_tokens":  strconv.FormatInt(c.UsageIn, 10),
				"output_tokens": strconv.FormatInt(c.UsageOut, 10),
			})
			pts := make([]previewTool, 0, len(c.Tools))
			for _, t := range c.Tools {
				pts = append(pts, previewTool{CallID: t.CallID, Tool: t.Tool, Input: t.Input})
			}
			spans = append(spans, previewSpan{
				SpanID:      modelSpanID,
				ParentID:    r.PromptID,
				SpanName:    c.Model,
				SpanType:    "model",
				DurationMs:  c.DurationMs,
				StartedAtMs: c.StartedMs,
				Input:       r.UserPrompt,
				Output:      assembleModelOutput(c.Text, c.Reasoning, pts),
				CustomTags:  string(tags),
				UserPrompt:  r.UserPrompt,
				RoundIndex:  i,
			})
			modelTurns++
			// 复刻后端：有文本且无工具调用视为产出最终答复；既无文本又无工具视为空转。
			if strings.TrimSpace(c.Text) != "" && len(c.Tools) == 0 {
				hasFinalAnswer = true
			}
			if len(c.Tools) == 0 && strings.TrimSpace(c.Text) == "" {
				curNoOp++
				if curNoOp > noOpStreak {
					noOpStreak = curNoOp
				}
			} else {
				curNoOp = 0
			}
			for ti, t := range c.Tools {
				spans = append(spans, previewSpan{
					SpanID:      fmt.Sprintf("%s-c%d-t%d", r.PromptID, ci, ti),
					ParentID:    modelSpanID,
					SpanName:    t.Tool,
					SpanType:    "tool",
					StartedAtMs: c.StartedMs,
					Input:       string(t.Input),
					Output:      string(t.Output),
					CustomTags:  "{}",
					RoundIndex:  i,
				})
				totalToolCalls++
				uniqueTools[t.Tool] = struct{}{}
				k := t.Tool + "::" + string(t.Input)
				dupKey[k]++
				if k == prevToolKey {
					curSerialRun++
				} else {
					curSerialRun = 1
				}
				prevToolKey = k
				if curSerialRun > maxSerialRun {
					maxSerialRun = curSerialRun
				}
				if t.Error != "" {
					toolFailures++
				}
			}
		}
		title := r.UserPrompt
		if title == "" {
			title = fmt.Sprintf("Round %d", i+1)
		}
		dur := r.EndedMs - r.StartedMs
		if dur < 0 {
			dur = 0
		}
		traces = append(traces, previewTrace{
			TraceID:      r.PromptID,
			SpanID:       r.PromptID,
			Title:        title,
			UserPrompt:   r.UserPrompt,
			RoundCount:   1,
			Turns:        modelTurns,
			DurationMs:   dur,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			StartedAtMs:  r.StartedMs,
			Status:       "success",
			Spans:        spans,
		})
		totalTurns += modelTurns
		totalDur += dur
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
	}
	// 复刻后端 features：重复调用 = 同工具+同入参出现 >=2 次的多余次数之和。
	toolRetries := 0
	for _, n := range dupKey {
		if n > 1 {
			toolRetries += n - 1
		}
	}
	avgTok := int64(0)
	if totalTurns > 0 {
		avgTok = (totalIn + totalOut) / int64(totalTurns)
	}
	toolFailRate := 0.0
	if totalToolCalls > 0 {
		toolFailRate = float64(toolFailures) / float64(totalToolCalls)
	}
	features := previewFeatures{
		ToolCalls:        totalToolCalls,
		UniqueTools:      len(uniqueTools),
		MaxSerialRun:     maxSerialRun,
		ToolFailures:     toolFailures,
		ToolFailRate:     toolFailRate,
		AvgTokensPerTurn: avgTok,
		ToolRetries:      toolRetries,
		HasLoop:          maxSerialRun >= 3,
	}
	// 复刻后端 deriveSessionSignals 的 5 条规则，供前端「规则评估」卡片展示。
	durSec := int(totalDur / 1000)
	respFull, respFloor := responsePreviewBucket(totalToolCalls)
	rules := []previewRule{
		{Name: "执行效率健康", Passed: hasFinalAnswer && noOpStreak < 3,
			FailedLabel: ternaryPreviewLabel(!(hasFinalAnswer && noOpStreak < 3), "轨迹异常"),
			Detail:      efficiencyPreviewDetail(totalTurns, hasFinalAnswer, noOpStreak)},
		{Name: "响应耗时合理", Passed: durSec <= respFloor,
			FailedLabel: ternaryPreviewLabel(durSec > respFloor, "关键路径过长"),
			Detail:      fmt.Sprintf("%d 秒，按当前复杂度阈值 %ds/%ds 评估", durSec, respFull, respFloor)},
		{Name: "工具稳定性", Passed: toolFailures == 0,
			FailedLabel: ternaryPreviewLabel(toolFailures > 0, "工具失败"),
			Detail:      fmt.Sprintf("工具失败 %d 次，失败率 %.0f%%", toolFailures, toolFailRate*100)},
		{Name: "资源使用健康", Passed: avgTok <= 76000,
			FailedLabel: ternaryPreviewLabel(avgTok > 76000, "长上下文超限"),
			Detail:      fmt.Sprintf("单轮平均 %dk Token", avgTok/1000)},
		{Name: "工具编排健康", Passed: maxSerialRun < 3,
			FailedLabel: ternaryPreviewLabel(maxSerialRun >= 3, "行为死循环"),
			Detail:      fmt.Sprintf("同名工具最长连续 %d 次，重复调用 %d 次", maxSerialRun, toolRetries)},
	}
	chip := "健康"
	for _, rr := range rules {
		if !rr.Passed && rr.FailedLabel != "" {
			chip = rr.FailedLabel
			break
		}
	}
	id := pr.SessionID
	if id == "" {
		id = "local-preview"
	}
	title := "Local Preview"
	traceID := ""
	if len(traces) > 0 {
		title = traces[0].Title
		traceID = traces[0].TraceID
	}
	return previewBundle{
		ID:           id,
		SessionID:    id,
		Title:        title,
		Trace:        traceID,
		TraceCount:   len(traces),
		Turns:        totalTurns,
		DurationMs:   totalDur,
		InputTokens:  totalIn,
		OutputTokens: totalOut,
		ToolCalls:    totalToolCalls,
		Features:     features,
		Rules:        rules,
		Color:        "green",
		Chip:         chip,
		Traces:       traces,
	}
}

// 以下三个辅助函数复刻 internal/api 的 responseBucket / efficiencyDetail / ternaryLabel。
func responsePreviewBucket(toolCalls int) (full, floor int) {
	switch {
	case toolCalls < 3:
		return 30, 90
	case toolCalls < 10:
		return 90, 240
	default:
		return 240, 600
	}
}

func efficiencyPreviewDetail(turns int, hasFinalAnswer bool, noOpStreak int) string {
	checks := make([]string, 0, 3)
	if !hasFinalAnswer {
		checks = append(checks, "未产出最终答复")
	}
	if noOpStreak >= 3 {
		checks = append(checks, fmt.Sprintf("连续 %d 步空转", noOpStreak))
	}
	if len(checks) == 0 {
		return fmt.Sprintf("%d 步，轨迹健康（已答复 / 无空转）", turns)
	}
	return fmt.Sprintf("%d 步 · %s", turns, strings.Join(checks, "、"))
}

func ternaryPreviewLabel(cond bool, label string) string {
	if cond {
		return label
	}
	return ""
}

// previewTool 是从 tracelog 未导出的 toolCall 拷贝出的可序列化轻量结构，
// 因 tracelog.toolCall 未导出无法在签名中直接引用，故在 main 包内转译一份。
type previewTool struct {
	CallID string
	Tool   string
	Input  json.RawMessage
}

// assembleModelOutput 复刻 internal/api 的同名逻辑：把模型输出组装成前端
// 详情页可直接消费的 choices[0].message 结构（含 content/reasoning_content/tool_calls）。
func assembleModelOutput(text, reasoning string, tools []previewTool) string {
	type toolFn struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type toolCallPayload struct {
		ID       string `json:"id,omitempty"`
		Type     string `json:"type"`
		Function toolFn `json:"function"`
	}
	payloadTools := make([]toolCallPayload, 0, len(tools))
	for _, t := range tools {
		payloadTools = append(payloadTools, toolCallPayload{
			ID:       t.CallID,
			Type:     "function",
			Function: toolFn{Name: t.Tool, Arguments: string(t.Input)},
		})
	}
	if text == "" && reasoning == "" && len(payloadTools) == 0 {
		return ""
	}
	msg := map[string]interface{}{}
	if text != "" {
		msg["content"] = text
	}
	if reasoning != "" {
		msg["reasoning_content"] = reasoning
	}
	if len(payloadTools) > 0 {
		msg["tool_calls"] = payloadTools
	}
	buf, _ := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{{"message": msg}},
	})
	return string(buf)
}
