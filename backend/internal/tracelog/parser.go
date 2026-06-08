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
	"strings"
	"time"
)

// rawEvent 对应 JSONL 中一行事件。data 保留 RawMessage，按 type 二次解码。
type rawEvent struct {
	Type           string          `json:"type"`
	Timestamp      string          `json:"timestamp"`
	LocalTimestamp string          `json:"local_timestamp"`
	SessionID      string          `json:"sessionID"`
	PromptID       string          `json:"promptId"`
	LogID          string          `json:"logId"`
	Data           json.RawMessage `json:"data"`
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
	PromptID    string
	UserPrompt  string
	StartedMs   int64
	EndedMs     int64
	Calls       []callRec
	InputTokens int64
	OutputTokens int64
	ReasoningTokens int64
}

// Parse 解析 JSONL 字节流，按 promptId 分组返回 ParseResult。
func Parse(raw []byte) (*ParseResult, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty jsonl")
	}

	// 用 bufio 大行容忍（单条事件可能 >64KB，比如 messages 历史很长）。
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024) // 最大 16MB/行

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

	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // 单行损坏不致命
		}
		if sessionID == "" {
			sessionID = ev.SessionID
		}
		if ev.LogID == "" || ev.PromptID == "" {
			continue
		}
		c, ok := calls[ev.LogID]
		if !ok {
			c = &callBuf{promptID: ev.PromptID}
			calls[ev.LogID] = c
			order = append(order, ev.LogID)
		}
		ms := parseTSMillis(ev.Timestamp)
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
		case "RESPONSE_BODY_FINAL":
			c.resp = ev.Data
		case "RESPONSE_META":
			c.meta = ev.Data
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan jsonl: %w", err)
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

	return out, nil
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
