// Package tracelog 把 OpenCode 上传到 TOS 的 JSONL 日志解析为前端可消费的 SessionBundle。
//
// JSONL 每行一个事件，结构稳定：
//
//	{type, timestamp, local_timestamp, sessionID, promptId, logId, data}
//
// 关键约定（基于真实样本验证）：
//   - 一个 promptId == 一次用户轮次（user prompt + 模型回复）
//   - 一个 promptId 内可能有多次 LLM 调用（模型分多步调工具完成任务）
//   - 每次 LLM 调用由一组 REQUEST_*/RESPONSE_* 事件组成，按 logId 关联
//   - 模型最终回复见 RESPONSE_BODY_FINAL.data.final.text
//   - 工具调用见 RESPONSE_BODY_FINAL.data.final.tools[]
//   - reasoning（思考）见 RESPONSE_BODY_FINAL.data.final.reasoning
//   - usage 见 RESPONSE_BODY_FINAL.data.final.usage
package tracelog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
)

// rawEvent 对应 JSONL 中一行事件。data 保留 RawMessage，按 type 二次解码。
type rawEvent struct {
	Type           string          `json:"type"`
	Timestamp      string          `json:"timestamp"`
	LocalTimestamp string          `json:"local_timestamp"`
	SessionID      string          `json:"sessionID"`
	SessionID2     string          `json:"session_id"`
	PromptID       string          `json:"promptId"`
	PromptID2      string          `json:"prompt_id"`
	LogID          string          `json:"logId"`
	LogID2         string          `json:"log_id"`
	Data           json.RawMessage `json:"data"`
}

func (e rawEvent) sessionID() string {
	if e.SessionID != "" {
		return e.SessionID
	}
	return e.SessionID2
}

func (e rawEvent) promptID() string {
	if e.PromptID != "" {
		return e.PromptID
	}
	return e.PromptID2
}

func (e rawEvent) logID() string {
	if e.LogID != "" {
		return e.LogID
	}
	return e.LogID2
}

func (e rawEvent) ts() string {
	if e.Timestamp != "" {
		return e.Timestamp
	}
	return e.LocalTimestamp
}

// callRec 是一次完整的 LLM 调用（一组 REQUEST_*/RESPONSE_* 事件聚合）。
type callRec struct {
	LogID     string
	StartedMs int64
	EndedMs   int64

	// REQUEST_BODY
	Model    string
	Messages []chatMessage // 含 system / user / assistant / tool 历史

	// RESPONSE_BODY_FINAL
	Text         string
	Reasoning    string
	FinishReason string
	Tools        []toolCall
	UsageIn      int64
	UsageOut     int64
	UsageReason  int64
	UsageCached  int64

	// RESPONSE_META
	DurationMs int64
	HTTPStatus int
}

type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type toolCall struct {
	CallID string          `json:"callID"`
	Tool   string          `json:"tool"`
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
	Error  string          `json:"error,omitempty"`
}

// ParseResult 是解析后的轻量结构，由调用方组装成 apiSessionBundle。
type ParseResult struct {
	SessionID string
	Rounds    []Round // 按 promptId 分组，按时间排序
}

// Round 一个用户轮次（promptId）。
type Round struct {
	PromptID        string
	UserPrompt      string
	StartedMs       int64
	EndedMs         int64
	Calls           []callRec
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
}

// Parse 解析 JSONL 字节流，按 promptId 分组返回 ParseResult。
func Parse(raw []byte) (*ParseResult, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty jsonl")
	}

	if raw[0] == '[' {
		var events []rawEvent
		if err := json.Unmarshal(raw, &events); err == nil {
			out := parseStructuredEvents(events)
			if len(out.Rounds) > 0 {
				return out, nil
			}
			fallback := parseJSONArrayFallback(raw)
			if len(fallback.Rounds) > 0 {
				return fallback, nil
			}
			log.Printf("tracelog: json-array parsed but no rounds detected session=%s events=%d", out.SessionID, len(events))
			return out, nil
		}
	}

	// 用 bufio 大行容忍（单条事件可能 >64KB，比如 messages 历史很长）。
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024) // 最大 16MB/行

	events := make([]rawEvent, 0, 256)

	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // 单行损坏不致命
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan jsonl: %w", err)
	}
	return parseStructuredEvents(events), nil
}

func parseStructuredEvents(events []rawEvent) *ParseResult {
	// 先按 logId 聚合事件 —— 一次 LLM 调用的 REQUEST_* / RESPONSE_* 共享同一 logId。
	type callBuf struct {
		promptID string
		startMs  int64
		endMs    int64
		req      json.RawMessage // REQUEST_BODY.data
		resp     json.RawMessage // RESPONSE_BODY_FINAL.data
		meta     json.RawMessage // RESPONSE_META.data
	}
	calls := map[string]*callBuf{}
	order := []string{} // 保留 logId 出现顺序
	var sessionID string

	for _, ev := range events {
		if sessionID == "" {
			sessionID = ev.sessionID()
		}
		logID, promptID := ev.logID(), ev.promptID()
		if logID == "" || promptID == "" {
			continue
		}
		c, ok := calls[logID]
		if !ok {
			c = &callBuf{promptID: promptID}
			calls[logID] = c
			order = append(order, logID)
		}
		ms := parseTSMillis(ev.ts())
		if ms > 0 {
			if c.startMs == 0 || ms < c.startMs {
				c.startMs = ms
			}
			if ms > c.endMs {
				c.endMs = ms
			}
		}
		switch ev.Type {
		case "REQUEST_BODY":
			c.req = ev.Data
		case "RESPONSE_BODY_FINAL", "RESPONSE_BODY":
			c.resp = ev.Data
		case "RESPONSE_META":
			c.meta = ev.Data
		}
	}

	// 按 promptId 分组。
	roundIdx := map[string]int{}
	out := &ParseResult{SessionID: sessionID}
	for _, logID := range order {
		cb := calls[logID]
		if cb.req == nil && cb.resp == nil {
			continue // 噪声
		}
		idx, ok := roundIdx[cb.promptID]
		if !ok {
			idx = len(out.Rounds)
			roundIdx[cb.promptID] = idx
			out.Rounds = append(out.Rounds, Round{PromptID: cb.promptID})
		}
		round := &out.Rounds[idx]

		call := callRec{LogID: logID, StartedMs: cb.startMs, EndedMs: cb.endMs}
		decodeRequest(cb.req, &call)
		decodeResponse(cb.resp, &call)
		decodeMeta(cb.meta, &call)

		if round.UserPrompt == "" {
			round.UserPrompt = extractUserPrompt(call.Messages)
		}
		if round.StartedMs == 0 || (call.StartedMs > 0 && call.StartedMs < round.StartedMs) {
			round.StartedMs = call.StartedMs
		}
		if call.EndedMs > round.EndedMs {
			round.EndedMs = call.EndedMs
		}
		round.InputTokens += call.UsageIn
		round.OutputTokens += call.UsageOut
		round.ReasoningTokens += call.UsageReason
		round.Calls = append(round.Calls, call)
	}

	if len(out.Rounds) == 0 {
		if fallback := parseResponsesWrappedEvents(events); len(fallback.Rounds) > 0 {
			if fallback.SessionID == "" {
				fallback.SessionID = sessionID
			}
			return fallback
		}
	}

	return out
}

func parseResponsesWrappedEvents(events []rawEvent) *ParseResult {
	out := &ParseResult{}
	for i, ev := range events {
		if out.SessionID == "" {
			out.SessionID = ev.sessionID()
		}
		var items []map[string]interface{}
		if err := json.Unmarshal(ev.Data, &items); err != nil || len(items) == 0 {
			continue
		}
		round := buildRoundFromResponseItems(ev, items, i)
		if round == nil {
			continue
		}
		out.Rounds = append(out.Rounds, *round)
	}
	sort.Slice(out.Rounds, func(i, j int) bool { return out.Rounds[i].StartedMs < out.Rounds[j].StartedMs })
	return out
}

func buildRoundFromResponseItems(ev rawEvent, items []map[string]interface{}, idx int) *Round {
	promptID := ""
	userPrompt := ""
	startedMs := parseTSMillis(ev.ts())
	endedMs := startedMs
	payload := assistantPayload{}
	textByItem := map[string]string{}
	assistantOrder := make([]string, 0, 2)
	seenItem := map[string]struct{}{}

	rememberAssistantItem := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seenItem[id]; ok {
			return
		}
		seenItem[id] = struct{}{}
		assistantOrder = append(assistantOrder, id)
	}

	for _, item := range items {
		itemType := strings.ToLower(stringAny(item["type"]))
		if userPrompt == "" {
			userPrompt = extractPromptFromValue(item)
		}
		switch itemType {
		case "response.output_item.added", "response.output_item.done":
			rawItem, _ := item["item"].(map[string]interface{})
			if len(rawItem) == 0 {
				continue
			}
			if userPrompt == "" {
				userPrompt = extractPromptFromValue(rawItem)
			}
			rawItemType := strings.ToLower(stringAny(rawItem["type"]))
			switch rawItemType {
			case "message":
				role := strings.ToLower(stringAny(rawItem["role"]))
				if role == "assistant" || role == "model" {
					itemID := stringAny(firstAny(rawItem, "id", "item_id"))
					rememberAssistantItem(itemID)
					payload = mergeAssistantPayload(payload, payloadFromMessageMap(rawItem))
				}
				if role == "user" && userPrompt == "" {
					userPrompt = contentStringAny(rawItem["content"])
				}
			case "function_call", "tool_call", "custom_tool_call":
				payload.Tools = append(payload.Tools, toolCallFromResponseOutputItem(rawItem))
			}
		case "response.output_text.delta":
			itemID := stringAny(firstAny(item, "item_id", "itemId"))
			rememberAssistantItem(itemID)
			textByItem[itemID] += stringAny(item["delta"])
		case "response.output_text.done":
			itemID := stringAny(firstAny(item, "item_id", "itemId"))
			rememberAssistantItem(itemID)
			if text := stringAny(item["text"]); text != "" {
				textByItem[itemID] = text
			}
		case "response.completed":
			resp, _ := item["response"].(map[string]interface{})
			if len(resp) == 0 {
				continue
			}
			if promptID == "" {
				promptID = stringAny(resp["id"])
			}
			if userPrompt == "" {
				userPrompt = extractPromptFromValue(resp)
			}
			payload = mergeAssistantPayload(payload, payloadFromResponseMap(resp))
			if ms := unixMillisAny(resp["created_at"]); startedMs == 0 || (ms > 0 && ms < startedMs) {
				startedMs = ms
			}
			if ms := unixMillisAny(resp["completed_at"]); ms > endedMs {
				endedMs = ms
			}
		}
	}

	if payload.Text == "" {
		parts := make([]string, 0, len(assistantOrder))
		for _, itemID := range assistantOrder {
			if text := strings.TrimSpace(textByItem[itemID]); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) == 0 {
			for _, text := range textByItem {
				if text = strings.TrimSpace(text); text != "" {
					parts = append(parts, text)
				}
			}
		}
		payload.Text = strings.TrimSpace(strings.Join(parts, "\n"))
	}
	if promptID == "" {
		promptID = fmt.Sprintf("responses-%d", idx+1)
	}
	if startedMs == 0 {
		startedMs = int64(idx+1) * 1000
	}
	if endedMs < startedMs {
		endedMs = startedMs
	}
	if payload.DurationMs == 0 && endedMs > startedMs {
		payload.DurationMs = endedMs - startedMs
	}
	if payload.Text == "" && payload.Reasoning == "" && len(payload.Tools) == 0 {
		return nil
	}

	call := callRec{
		LogID:       promptID,
		StartedMs:   startedMs,
		EndedMs:     endedMs,
		Model:       payload.Model,
		Text:        payload.Text,
		Reasoning:   payload.Reasoning,
		Tools:       payload.Tools,
		UsageIn:     payload.UsageIn,
		UsageOut:    payload.UsageOut,
		UsageReason: payload.UsageReason,
		DurationMs:  payload.DurationMs,
	}
	if userPrompt != "" {
		call.Messages = []chatMessage{{Role: "user", Content: mustRawJSONString(userPrompt)}}
	}
	return &Round{
		PromptID:        promptID,
		UserPrompt:      userPrompt,
		StartedMs:       startedMs,
		EndedMs:         endedMs,
		Calls:           []callRec{call},
		InputTokens:     payload.UsageIn,
		OutputTokens:    payload.UsageOut,
		ReasoningTokens: payload.UsageReason,
	}
}

func parseJSONArrayFallback(raw []byte) *ParseResult {
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return &ParseResult{}
	}
	out := &ParseResult{}
	currentPrompt := ""
	for i, item := range arr {
		if out.SessionID == "" {
			out.SessionID = stringAny(firstAny(item, "sessionID", "session_id"))
		}
		ts := parseTSMillis(stringAny(firstAny(item, "timestamp", "local_timestamp")))
		data, _ := item["data"]

		turns := extractConversationTurns(data)
		if len(turns) == 0 {
			turns = extractConversationTurns(item)
		}
		if len(turns) == 0 {
			if up := extractPromptFromValue(data); up != "" {
				currentPrompt = up
			} else if up := extractPromptFromValue(item); up != "" {
				currentPrompt = up
			}
			payload, ok := extractAssistantPayload(data)
			if !ok {
				payload, ok = extractAssistantPayload(item)
			}
			if !ok {
				continue
			}
			turns = []conversationTurn{{UserPrompt: currentPrompt, Payload: payload}}
		}

		if len(turns) == 0 {
			continue
		}
		if ts == 0 {
			ts = int64(i+1) * 1000
		}
		for ti, turn := range turns {
			if turn.UserPrompt != "" {
				currentPrompt = turn.UserPrompt
			} else {
				turn.UserPrompt = currentPrompt
			}
			callTS := ts + int64(ti)
			callID := fmt.Sprintf("fallback-%d-%d", i+1, ti+1)
			call := callRec{
				LogID:       callID,
				StartedMs:   callTS,
				EndedMs:     callTS + max64(turn.Payload.DurationMs, 1),
				Model:       turn.Payload.Model,
				Text:        turn.Payload.Text,
				Reasoning:   turn.Payload.Reasoning,
				Tools:       turn.Payload.Tools,
				UsageIn:     turn.Payload.UsageIn,
				UsageOut:    turn.Payload.UsageOut,
				UsageReason: turn.Payload.UsageReason,
			}
			if turn.UserPrompt != "" {
				call.Messages = []chatMessage{{Role: "user", Content: mustRawJSONString(turn.UserPrompt)}}
			}
			out.Rounds = append(out.Rounds, Round{
				PromptID:        callID,
				UserPrompt:      turn.UserPrompt,
				StartedMs:       call.StartedMs,
				EndedMs:         call.EndedMs,
				Calls:           []callRec{call},
				InputTokens:     call.UsageIn,
				OutputTokens:    call.UsageOut,
				ReasoningTokens: call.UsageReason,
			})
		}
	}
	sort.Slice(out.Rounds, func(i, j int) bool { return out.Rounds[i].StartedMs < out.Rounds[j].StartedMs })
	return out
}

type conversationTurn struct {
	UserPrompt string
	Payload    assistantPayload
}

func extractConversationTurns(v interface{}) []conversationTurn {
	switch x := v.(type) {
	case map[string]interface{}:
		if msgs, ok := x["messages"].([]interface{}); ok {
			if turns := turnsFromMessages(msgs); len(turns) > 0 {
				return turns
			}
		}
		if p, ok := payloadFromChoices(x); ok {
			return []conversationTurn{{UserPrompt: extractPromptFromValue(x), Payload: p}}
		}
		out := make([]conversationTurn, 0)
		for _, val := range x {
			out = append(out, extractConversationTurns(val)...)
		}
		return out
	case []interface{}:
		out := make([]conversationTurn, 0)
		for _, it := range x {
			out = append(out, extractConversationTurns(it)...)
		}
		return out
	default:
		return nil
	}
}

func turnsFromMessages(msgs []interface{}) []conversationTurn {
	out := make([]conversationTurn, 0)
	currentPrompt := ""
	for _, raw := range msgs {
		msg, _ := raw.(map[string]interface{})
		if len(msg) == 0 {
			continue
		}
		role := strings.ToLower(stringAny(msg["role"]))
		switch role {
		case "user":
			if prompt := contentStringAny(msg["content"]); prompt != "" {
				currentPrompt = prompt
			}
		case "assistant", "model":
			p := payloadFromMessageMap(msg)
			if p.Text == "" && p.Reasoning == "" && len(p.Tools) == 0 {
				continue
			}
			out = append(out, conversationTurn{UserPrompt: currentPrompt, Payload: p})
		}
	}
	return out
}

type assistantPayload struct {
	Model       string
	Text        string
	Reasoning   string
	Tools       []toolCall
	UsageIn     int64
	UsageOut    int64
	UsageReason int64
	DurationMs  int64
}

func extractAssistantPayload(v interface{}) (assistantPayload, bool) {
	switch x := v.(type) {
	case map[string]interface{}:
		if p, ok := payloadFromChoices(x); ok {
			return p, true
		}
		if role := strings.ToLower(stringAny(x["role"])); role == "assistant" || role == "model" {
			if p := payloadFromMessageMap(x); p.Text != "" || p.Reasoning != "" || len(p.Tools) > 0 {
				return p, true
			}
		}
		if p := payloadFromSimpleMap(x); p.Text != "" || p.Reasoning != "" || len(p.Tools) > 0 {
			return p, true
		}
		for _, val := range x {
			if p, ok := extractAssistantPayload(val); ok {
				return p, true
			}
		}
	case []interface{}:
		for _, it := range x {
			if p, ok := extractAssistantPayload(it); ok {
				return p, true
			}
		}
	}
	return assistantPayload{}, false
}

func payloadFromChoices(m map[string]interface{}) (assistantPayload, bool) {
	choices, _ := m["choices"].([]interface{})
	if len(choices) == 0 {
		return assistantPayload{}, false
	}
	choice, _ := choices[0].(map[string]interface{})
	msg, _ := choice["message"].(map[string]interface{})
	if len(msg) == 0 {
		return assistantPayload{}, false
	}
	p := payloadFromMessageMap(msg)
	p.Model = stringAny(firstAny(m, "model", "model_name"))
	p.UsageIn, p.UsageOut, p.UsageReason = extractUsage(m)
	if d := int64Any(firstAny(m, "durationMs", "duration_ms")); d > 0 {
		p.DurationMs = d
	}
	return p, true
}

func payloadFromMessageMap(msg map[string]interface{}) assistantPayload {
	return assistantPayload{
		Text:      contentStringAny(msg["content"]),
		Reasoning: stringAny(firstAny(msg, "reasoning", "reasoning_content")),
		Tools:     extractToolCalls(firstAny(msg, "toolCalls", "tool_calls")),
	}
}

func payloadFromSimpleMap(m map[string]interface{}) assistantPayload {
	p := assistantPayload{
		Model:      stringAny(firstAny(m, "model", "model_name", "name")),
		Text:       contentStringAny(firstAny(m, "content", "text", "output", "answer", "response")),
		Reasoning:  stringAny(firstAny(m, "reasoning", "reasoning_content", "thought")),
		Tools:      extractToolCalls(firstAny(m, "toolCalls", "tool_calls", "tools")),
		DurationMs: int64Any(firstAny(m, "durationMs", "duration_ms")),
	}
	p.UsageIn, p.UsageOut, p.UsageReason = extractUsage(m)
	return p
}

func extractPromptFromValue(v interface{}) string {
	switch x := v.(type) {
	case map[string]interface{}:
		if msgs, ok := x["messages"].([]interface{}); ok {
			for i := len(msgs) - 1; i >= 0; i-- {
				if msg, ok := msgs[i].(map[string]interface{}); ok && strings.ToLower(stringAny(msg["role"])) == "user" {
					if t := contentStringAny(msg["content"]); t != "" {
						return t
					}
				}
			}
		}
		for _, key := range []string{"user_prompt", "userPrompt", "prompt", "query", "input"} {
			if t := contentStringAny(x[key]); t != "" {
				return t
			}
		}
		if role := strings.ToLower(stringAny(x["role"])); role == "user" {
			if t := contentStringAny(x["content"]); t != "" {
				return t
			}
		}
		for _, val := range x {
			if t := extractPromptFromValue(val); t != "" {
				return t
			}
		}
	case []interface{}:
		for _, it := range x {
			if t := extractPromptFromValue(it); t != "" {
				return t
			}
		}
	}
	return ""
}

func extractUsage(v interface{}) (int64, int64, int64) {
	m, _ := v.(map[string]interface{})
	if len(m) == 0 {
		return 0, 0, 0
	}
	if usage, ok := firstAny(m, "usage", "token_usage").(map[string]interface{}); ok {
		in := int64Any(firstAny(usage, "inputTokens", "prompt_tokens", "input_tokens"))
		out := int64Any(firstAny(usage, "outputTokens", "completion_tokens", "output_tokens"))
		reason := int64Any(firstAny(usage, "reasoningTokens", "reasoning_tokens"))
		if reason == 0 {
			if details, ok := firstAny(usage, "output_tokens_details", "outputTokensDetails").(map[string]interface{}); ok {
				reason = int64Any(firstAny(details, "reasoning_tokens", "reasoningTokens"))
			}
		}
		return in, out, reason
	}
	return 0, 0, 0
}

func payloadFromResponseMap(m map[string]interface{}) assistantPayload {
	p := assistantPayload{
		Model: stringAny(firstAny(m, "model", "model_name")),
	}
	p.UsageIn, p.UsageOut, p.UsageReason = extractUsage(m)
	if output, ok := m["output"].([]interface{}); ok {
		for _, raw := range output {
			item, _ := raw.(map[string]interface{})
			if len(item) == 0 {
				continue
			}
			switch strings.ToLower(stringAny(item["type"])) {
			case "message":
				if role := strings.ToLower(stringAny(item["role"])); role == "assistant" || role == "model" {
					p = mergeAssistantPayload(p, payloadFromMessageMap(item))
				}
			case "function_call", "tool_call", "custom_tool_call":
				p.Tools = append(p.Tools, toolCallFromResponseOutputItem(item))
			}
		}
	}
	return p
}

func mergeAssistantPayload(dst, src assistantPayload) assistantPayload {
	dst.Model = firstNonEmptyString(dst.Model, src.Model)
	dst.Text = firstNonEmptyString(dst.Text, src.Text)
	dst.Reasoning = firstNonEmptyString(dst.Reasoning, src.Reasoning)
	if len(dst.Tools) == 0 && len(src.Tools) > 0 {
		dst.Tools = src.Tools
	}
	if dst.UsageIn == 0 {
		dst.UsageIn = src.UsageIn
	}
	if dst.UsageOut == 0 {
		dst.UsageOut = src.UsageOut
	}
	if dst.UsageReason == 0 {
		dst.UsageReason = src.UsageReason
	}
	if dst.DurationMs == 0 {
		dst.DurationMs = src.DurationMs
	}
	return dst
}

func toolCallFromResponseOutputItem(m map[string]interface{}) toolCall {
	input := firstAny(m, "arguments", "input")
	return toolCall{
		CallID: stringAny(firstAny(m, "call_id", "callID", "id")),
		Tool:   stringAny(firstAny(m, "name", "toolName")),
		Input:  mustMarshalRaw(input),
		Output: mustMarshalRaw(firstAny(m, "output", "result")),
		Error:  stringAny(firstAny(m, "error", "error_message")),
	}
}

func extractToolCalls(v interface{}) []toolCall {
	arr, _ := v.([]interface{})
	if len(arr) == 0 {
		return nil
	}
	out := make([]toolCall, 0, len(arr))
	for _, raw := range arr {
		m, _ := raw.(map[string]interface{})
		if len(m) == 0 {
			continue
		}
		fn, _ := m["function"].(map[string]interface{})
		in := firstAny(m, "arguments", "input")
		if fnArgs, ok := fn["arguments"]; ok && in == nil {
			in = fnArgs
		}
		out = append(out, toolCall{
			CallID: stringAny(firstAny(m, "toolCallId", "callID", "id")),
			Tool:   firstNonEmptyString(stringAny(firstAny(m, "toolName", "name")), stringAny(fn["name"])),
			Input:  mustMarshalRaw(in),
			Output: mustMarshalRaw(firstAny(m, "output", "result")),
			Error:  stringAny(firstAny(m, "error", "error_message")),
		})
	}
	return out
}

func firstAny(m map[string]interface{}, keys ...string) interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func stringAny(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

func int64Any(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		return 0
	}
}

func contentStringAny(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []interface{}:
		parts := make([]string, 0, len(x))
		for _, it := range x {
			switch p := it.(type) {
			case string:
				if s := strings.TrimSpace(p); s != "" {
					parts = append(parts, s)
				}
			case map[string]interface{}:
				if s := stringAny(firstAny(p, "text", "content", "value")); s != "" {
					parts = append(parts, s)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]interface{}:
		return strings.TrimSpace(stringAny(firstAny(x, "text", "content", "value")))
	default:
		return ""
	}
}

func mustMarshalRaw(v interface{}) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(buf)
}

func mustRawJSONString(s string) json.RawMessage {
	buf, _ := json.Marshal(s)
	return json.RawMessage(buf)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func unixMillisAny(v interface{}) int64 {
	n := int64Any(v)
	if n == 0 {
		return 0
	}
	if n < 1e12 {
		return n * 1000
	}
	return n
}

// decodeRequest 从 REQUEST_BODY.data 提取 model 与 messages。
//
// 实际样本中 data 为请求体本身，形如 {model, messages, ...}。
func decodeRequest(raw json.RawMessage, c *callRec) {
	if len(raw) == 0 {
		return
	}
	var body struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		c.Model = body.Model
		c.Messages = body.Messages
		return
	}
	// 兜底：data 可能再嵌一层 {body: ...}。
	var wrap struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Body) > 0 {
		_ = json.Unmarshal(wrap.Body, &body)
		c.Model = body.Model
		c.Messages = body.Messages
	}
}

// decodeResponse 从 RESPONSE_BODY_FINAL.data 提取 final 块。
func decodeResponse(raw json.RawMessage, c *callRec) {
	if len(raw) == 0 {
		return
	}
	var outer struct {
		Final struct {
			Text         string     `json:"text"`
			Reasoning    string     `json:"reasoning"`
			FinishReason string     `json:"finishReason"`
			Tools        []toolCall `json:"tools"`
			Usage        struct {
				InputTokens     int64 `json:"inputTokens"`
				OutputTokens    int64 `json:"outputTokens"`
				ReasoningTokens int64 `json:"reasoningTokens"`
				CachedTokens    int64 `json:"cachedTokens"`
			} `json:"usage"`
		} `json:"final"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return
	}
	c.Text = outer.Final.Text
	c.Reasoning = outer.Final.Reasoning
	c.FinishReason = outer.Final.FinishReason
	c.Tools = outer.Final.Tools
	c.UsageIn = outer.Final.Usage.InputTokens
	c.UsageOut = outer.Final.Usage.OutputTokens
	c.UsageReason = outer.Final.Usage.ReasoningTokens
	c.UsageCached = outer.Final.Usage.CachedTokens
	if c.Text != "" || c.Reasoning != "" || len(c.Tools) > 0 || c.UsageIn > 0 || c.UsageOut > 0 {
		return
	}
	// 兼容直接返回 OpenAI/Responses 风格 payload：{choices:[{message:{...}}], usage:{...}}
	var generic map[string]interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return
	}
	if payload, ok := payloadFromChoices(generic); ok {
		c.Model = firstNonEmptyString(c.Model, payload.Model)
		c.Text = payload.Text
		c.Reasoning = payload.Reasoning
		c.Tools = payload.Tools
		c.UsageIn = payload.UsageIn
		c.UsageOut = payload.UsageOut
		c.UsageReason = payload.UsageReason
	}
}

// decodeMeta 从 RESPONSE_META.data 提取 durationMs / status。
func decodeMeta(raw json.RawMessage, c *callRec) {
	if len(raw) == 0 {
		return
	}
	var meta struct {
		DurationMs int64 `json:"durationMs"`
		Status     int   `json:"status"`
	}
	if err := json.Unmarshal(raw, &meta); err == nil {
		c.DurationMs = meta.DurationMs
		c.HTTPStatus = meta.Status
	}
}

// extractUserPrompt 从 messages 末尾向前找最近一条 user 消息文本。
//
// content 可能是 string，也可能是 [{type:"text", text:"..."}]。
func extractUserPrompt(msgs []chatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != "user" {
			continue
		}
		if t := contentText(m.Content); t != "" {
			return t
		}
	}
	return ""
}

// contentText 把多种 content 形态压成纯文本预览。
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// 字符串
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	// 数组
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var b strings.Builder
		for _, p := range arr {
			if p.Text != "" {
				b.WriteString(p.Text)
				b.WriteByte('\n')
			}
		}
		return strings.TrimSpace(b.String())
	}
	return strings.TrimSpace(string(raw))
}

// parseTSMillis 把 ISO8601 / 含毫秒时间戳转成毫秒。
func parseTSMillis(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}
