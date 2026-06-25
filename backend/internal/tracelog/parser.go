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
	"io"
	"log"
	"os"
	"regexp"
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
	SessionID  string
	Rounds     []Round         // 按 promptId 分组，按时间排序
	Truncation *TruncationInfo `json:"truncation,omitempty"`
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

// MaxJSONLLineBytes 是单条 JSONL event 的最大大小。
// 流式解析时文件整体不设总量截断，但必须限制单行，避免某条异常大事件独自撑爆内存。
const MaxJSONLLineBytes = 2 * 1024 * 1024

const (
	// DefaultSessionHardLimitBytes 是单个 session 在解析阶段允许保留的绝对上限。
	// 当前线上 bundle_json 样本最大值 < 64MB，这里给到 128MB 作为兜底保险丝，
	// 只拦截极少数离群的大 session，默认不影响完整还原。
	DefaultSessionHardLimitBytes = 128 * 1024 * 1024
	// DefaultSessionPressureLimitBytes 是内存接近软阈值时启用的收紧上限。
	// 平时不生效，仅在夜间补数等高压场景优先保命，尽量保留前部关键轨迹。
	DefaultSessionPressureLimitBytes = 64 * 1024 * 1024
	// DefaultSessionPressurePct 与聚合器软阈值对齐：达到该水位后，解析器开始更激进地保护单个大 session。
	DefaultSessionPressurePct = 75.0
	// SessionPressureCheckStepBytes 控制大 session 解析过程中回读 cgroup 内存的粒度，避免每行都读文件。
	SessionPressureCheckStepBytes = 4 * 1024 * 1024
)

// TruncationInfo 记录本次解析是否因保护策略而提前停止。
type TruncationInfo struct {
	Truncated     bool    `json:"truncated"`
	Reason        string  `json:"reason,omitempty"`
	LimitBytes    int64   `json:"limit_bytes,omitempty"`
	RetainedBytes int64   `json:"retained_bytes,omitempty"`
	MemoryPct     float64 `json:"memory_pct,omitempty"`
	Message       string  `json:"message,omitempty"`
}

// Parse 解析 JSONL 字节流，按 promptId 分组返回 ParseResult。
func Parse(raw []byte) (*ParseResult, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty jsonl")
	}

	if raw[0] == '[' {
		events, ok := decodeEventArray(raw)
		if ok {
			// 新版 Neeko/Responses 事件流：顶层数组，每组 REQUEST_BODY + STREAM_RESPONSE。
			// 无 logId/promptId/sessionID，必须用专用解析器，否则会退化成 fallback 脏数据。
			if isNeekoResponsesLog(events) {
				if out := parseNeekoResponsesLog(events); len(out.Rounds) > 0 {
					return out, nil
				}
			}
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
	if isNeekoResponsesLog(events) {
		if out := parseNeekoResponsesLog(events); len(out.Rounds) > 0 {
			return out, nil
		}
	}
	return parseStructuredEvents(events), nil
}

// ParseStream 流式解析 JSONL，不再把完整文件读入内存。
//
// 适用线上 TOS JSONL：一行一个 event。该函数只保留按 logId 聚合所需的最小原始片段
// （REQUEST/RESPONSE/META 的 data），避免同时持有完整 raw 文件与完整事件数组。
// 若输入为历史兼容的顶层 JSON 数组，则退回数组 decoder 路径：仍避免保留 raw []byte，
// 但需要暂存 events 以复用 Neeko/Responses 兼容解析逻辑。
func ParseStream(r io.Reader) (*ParseResult, int, error) {
	if r == nil {
		return nil, 0, fmt.Errorf("nil reader")
	}
	cr := &countingReader{r: r}
	br := bufio.NewReaderSize(cr, 256*1024)
	first, err := peekFirstNonSpace(br)
	if err != nil {
		if err == io.EOF {
			return nil, cr.n, fmt.Errorf("empty jsonl")
		}
		return nil, cr.n, err
	}
	if first == '[' {
		out, err := parseEventArrayStream(br)
		return out, cr.n, err
	}

	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 256*1024), MaxJSONLLineBytes)

	collector := newStreamEventCollector()
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // 单行损坏不致命，保持旧 Parse 行为
		}
		if collector.Add(ev) {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, cr.n, fmt.Errorf("scan jsonl around line %d (max line %d bytes): %w", lineNo+1, MaxJSONLLineBytes, err)
	}
	out := collector.Result()
	if len(out.Rounds) == 0 {
		return nil, cr.n, fmt.Errorf("empty jsonl")
	}
	return out, cr.n, nil
}

type countingReader struct {
	r io.Reader
	n int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += n
	return n, err
}

func peekFirstNonSpace(r *bufio.Reader) (byte, error) {
	for i := 0; ; i++ {
		buf, err := r.Peek(i + 1)
		if err != nil {
			return 0, err
		}
		b := buf[i]
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			return b, nil
		}
	}
}

func parseEventArrayStream(r io.Reader) (*ParseResult, error) {
	var events []rawEvent
	if err := json.NewDecoder(r).Decode(&events); err != nil {
		return nil, fmt.Errorf("decode json array stream: %w", err)
	}
	if isNeekoResponsesLog(events) {
		if out := parseNeekoResponsesLog(events); len(out.Rounds) > 0 {
			return out, nil
		}
	}
	out := parseStructuredEvents(events)
	if len(out.Rounds) > 0 {
		return out, nil
	}
	return nil, fmt.Errorf("empty json array log")
}

type streamCallBuf struct {
	promptID string
	startMs  int64
	endMs    int64
	req      json.RawMessage
	resp     json.RawMessage
	meta     json.RawMessage
}

type streamEventCollector struct {
	calls          map[string]*streamCallBuf
	order          []string
	fallbackEvents []rawEvent
	sessionID      string
	totalBytes     int64
	hardLimitBytes int64
	pressureBytes  int64
	pressurePct    float64
	nextMemCheck   int64
	truncation     *TruncationInfo
}

func newStreamEventCollector() *streamEventCollector {
	hardLimit := sessionHardLimitBytes()
	pressureLimit := sessionPressureLimitBytes(hardLimit)
	return &streamEventCollector{
		calls:          make(map[string]*streamCallBuf),
		order:          make([]string, 0, 256),
		hardLimitBytes: hardLimit,
		pressureBytes:  pressureLimit,
		pressurePct:    sessionPressurePct(),
		nextMemCheck:   pressureLimit,
	}
}

func (c *streamEventCollector) Add(ev rawEvent) bool {
	if c.truncation != nil {
		return true
	}
	if c.sessionID == "" {
		c.sessionID = ev.sessionID()
	}
	logID, promptID := ev.logID(), ev.promptID()
	if logID == "" || promptID == "" {
		c.fallbackEvents = append(c.fallbackEvents, ev)
		return c.applyPayloadBudget(int64(len(ev.Data)))
	}
	cb, ok := c.calls[logID]
	if !ok {
		cb = &streamCallBuf{promptID: promptID}
		c.calls[logID] = cb
		c.order = append(c.order, logID)
	}
	ms := parseTSMillis(ev.ts())
	if ms > 0 {
		if cb.startMs == 0 || ms < cb.startMs {
			cb.startMs = ms
		}
		if ms > cb.endMs {
			cb.endMs = ms
		}
	}
	switch ev.Type {
	case "REQUEST_BODY":
		if c.replacePayload(&cb.req, ev.Data) {
			return true
		}
	case "RESPONSE_BODY_FINAL", "RESPONSE_BODY":
		if c.replacePayload(&cb.resp, ev.Data) {
			return true
		}
	case "RESPONSE_META":
		if c.replacePayload(&cb.meta, ev.Data) {
			return true
		}
	}
	return false
}

func (c *streamEventCollector) Result() *ParseResult {
	roundIdx := map[string]int{}
	out := &ParseResult{SessionID: c.sessionID, Truncation: c.truncation}
	for _, logID := range c.order {
		cb := c.calls[logID]
		if cb.req == nil && cb.resp == nil {
			continue
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
	if len(out.Rounds) == 0 && len(c.fallbackEvents) > 0 {
		fallback := parseResponsesWrappedEvents(c.fallbackEvents)
		if len(fallback.Rounds) > 0 {
			if fallback.SessionID == "" {
				fallback.SessionID = c.sessionID
			}
			return fallback
		}
	}
	return out
}

func (c *streamEventCollector) replacePayload(dst *json.RawMessage, data json.RawMessage) bool {
	c.totalBytes -= int64(len(*dst))
	*dst = data
	return c.applyPayloadBudget(int64(len(data)))
}

func (c *streamEventCollector) applyPayloadBudget(delta int64) bool {
	if delta <= 0 {
		return false
	}
	c.totalBytes += delta
	if c.totalBytes > c.hardLimitBytes {
		c.markTruncated("session_size_limit", c.hardLimitBytes, 0)
		return true
	}
	if c.totalBytes < c.pressureBytes || c.pressureBytes <= 0 {
		return false
	}
	if c.totalBytes < c.nextMemCheck {
		return false
	}
	c.nextMemCheck = c.totalBytes + SessionPressureCheckStepBytes
	pct, ok := cgroupMemoryUsagePct()
	if ok && pct >= c.pressurePct {
		c.markTruncated("memory_pressure", c.pressureBytes, pct)
		return true
	}
	return false
}

func (c *streamEventCollector) markTruncated(reason string, limitBytes int64, memoryPct float64) {
	if c.truncation != nil {
		return
	}
	msg := "该会话轨迹较大，为保障服务稳定性已提前停止解析，当前仅展示部分内容。"
	if reason == "memory_pressure" {
		msg = "夜间补数时检测到内存接近阈值，为保障服务稳定性已提前停止解析该会话，当前仅展示部分内容。"
	}
	c.truncation = &TruncationInfo{
		Truncated:     true,
		Reason:        reason,
		LimitBytes:    limitBytes,
		RetainedBytes: c.totalBytes,
		MemoryPct:     memoryPct,
		Message:       msg,
	}
}

func sessionHardLimitBytes() int64 {
	return envBytes("TRACELOG_SESSION_MAX_BYTES", DefaultSessionHardLimitBytes)
}

func sessionPressureLimitBytes(hardLimit int64) int64 {
	v := envBytes("TRACELOG_SESSION_PRESSURE_BYTES", DefaultSessionPressureLimitBytes)
	if v <= 0 || v > hardLimit {
		return hardLimit
	}
	return v
}

func sessionPressurePct() float64 {
	return envPercent("AGG_MEM_SOFT_PCT", DefaultSessionPressurePct)
}

func envBytes(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envPercent(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 || f > 100 {
		return def
	}
	return f
}

func cgroupMemoryUsagePct() (float64, bool) {
	type pair struct{ usage, max string }
	candidates := []pair{
		{"/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory.max"},
		{"/sys/fs/cgroup/memory/memory.usage_in_bytes", "/sys/fs/cgroup/memory/memory.limit_in_bytes"},
	}
	for _, c := range candidates {
		used, ok1 := readUintFile(c.usage)
		limit, ok2 := readUintFile(c.max)
		if !ok1 || !ok2 || limit == 0 {
			continue
		}
		return float64(used) / float64(limit) * 100, true
	}
	return 0, false
}

func readUintFile(path string) (uint64, bool) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(buf))
	if s == "" || s == "max" {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
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
	if applyRequestBody(raw, c) {
		return
	}
	// 兜底：data 可能再嵌一层 {body: ...}。
	var wrap struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Body) > 0 {
		applyRequestBody(wrap.Body, c)
	}
}

// applyRequestBody 从单次 LLM 调用的请求体里提取 model 与历史消息。
//
// 兼容两种线上格式：
//   - Chat Completions：{model, messages:[{role,content},...]}
//   - Responses API   ：{model, input:[{role,content},...]}（GPT-5.x 等新模型走这套）
//
// 同一 session 可能逐轮切换格式（首轮 gemini 用 messages，后续 gpt 用 input），
// 旧实现只认 messages，导致 Responses 轮的 user prompt 解析为空、多轮在详情页塌缩成一轮。
// input 数组里还混有 function_call / function_call_output 等无 role 的项，它们不影响
// extractUserPrompt（只回溯 role==user 的消息），原样保留即可。
func applyRequestBody(raw json.RawMessage, c *callRec) bool {
	var body struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Input    []chatMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return false
	}
	msgs := body.Messages
	if len(msgs) == 0 {
		msgs = body.Input
	}
	if body.Model == "" && len(msgs) == 0 {
		return false
	}
	c.Model = body.Model
	c.Messages = msgs
	return true
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
		t := extractBusinessWrappedOriginalQuery(stripQuestionAnswerResultPayload(stripInjectedContext(contentText(m.Content))))
		if t != "" && !isSyntheticToolPrompt(t) {
			return t
		}
	}
	return ""
}

// injectedContextRe 匹配框架注入的上下文包裹块（含跨行内容）。
var injectedContextRe = regexp.MustCompile(`(?is)<(system-reminder|project-memory|related-conversations)>.*?</(system-reminder|project-memory|related-conversations)>`)

// injectedCloseTagRe 匹配注入块的闭合标签（兼容单复数），用作切分锚点。
var injectedCloseTagRe = regexp.MustCompile(`(?i)</(system-reminders?|project-memory|related-conversations?)>`)

// stripInjectedContext 剥离框架注入的上下文包裹块，保留其后真实的用户输入。
//
// 新版 Agent 框架会把 <system-reminder>...</system-reminder>（内含 project-memory /
// related-conversations 等）拼在真实用户输入之前，塞进同一条 role==user 消息里。
// 若直接把整条判为合成丢弃，会导致：① UI 气泡显示成一大坨系统注入；② 多轮因
// prompt 被判空而塌缩成一轮。
// 优先按"最后一个闭合标签"锚点切分：闭合标签（含）之前整体视为注入上下文，之后才是真实输入。
// 这样即使上游只截到残缺标签（如缺开标签、只剩 </system-reminder>）也能正确分离。
func stripInjectedContext(text string) string {
	if locs := injectedCloseTagRe.FindAllStringIndex(text, -1); len(locs) > 0 {
		return strings.TrimSpace(text[locs[len(locs)-1][1]:])
	}
	return strings.TrimSpace(injectedContextRe.ReplaceAllString(text, ""))
}

func stripQuestionAnswerResultPayload(text string) string {
	start, end, ok := findQuestionAnswerResultPayload(text)
	if !ok {
		return text
	}
	return strings.TrimSpace(text[:start] + " " + text[end:])
}

func extractBusinessWrappedOriginalQuery(text string) string {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	idx := strings.Index(lower, "用户原始查询")
	if idx < 0 {
		return raw
	}
	segment := strings.TrimSpace(raw[idx+len("用户原始查询"):])
	segment = strings.TrimLeft(segment, "：: \n\t")
	if segment == "" {
		return raw
	}
	cut := len(segment)
	for _, marker := range []string{
		" batch_id:",
		" batch_id：",
		"\nbatch_id:",
		"\nbatch_id：",
		" 候选列表:",
		" 候选列表：",
		" 专家候选列表:",
		" 专家候选列表：",
		"\n候选列表:",
		"\n候选列表：",
		"\n专家候选列表:",
		"\n专家候选列表：",
	} {
		if pos := strings.Index(segment, marker); pos >= 0 && pos < cut {
			cut = pos
		}
	}
	if cut < len(segment) {
		segment = segment[:cut]
	}
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return raw
	}
	return segment
}

func findQuestionAnswerResultPayload(text string) (int, int, bool) {
	idx := strings.Index(text, `"type":"question_answer_result"`)
	if idx < 0 {
		idx = strings.Index(text, `"type": "question_answer_result"`)
	}
	if idx < 0 {
		return 0, 0, false
	}
	start := strings.LastIndex(text[:idx], "{")
	if start < 0 {
		return 0, 0, false
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inStr {
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				var payload struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal([]byte(text[start:i+1]), &payload); err == nil && payload.Type == "question_answer_result" {
					return start, i + 1, true
				}
				return 0, 0, false
			}
		}
	}
	return 0, 0, false
}

// isSyntheticToolPrompt 识别工具回填的"合成 user 消息"（如 web_fetch / web_search
// 执行后框架以 user 角色把结果回填给模型），以及其他框架内部 prompt（如子代理任务下发、工具中断收尾），
// 这类都不是真实用户提问，提取轮次 prompt 时排除。
func isSyntheticToolPrompt(text string) bool {
	if strings.TrimSpace(stripQuestionAnswerResultPayload(text)) == "" {
		if _, _, ok := findQuestionAnswerResultPayload(text); ok {
			return true
		}
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "continue if you have next steps") &&
		strings.Contains(lower, "stop and ask for clarification") &&
		strings.Contains(lower, "unsure how to proceed") {
		return true
	}
	if isTitleGenerationPrompt(text) {
		return true
	}
	for _, sig := range []string{
		"the user requested the following",
		"i have fetched the raw content",
		"i was unable to access the url",
		"<tool_call_result>",
		"<function_results>",
		"<system-reminder>",
	} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	if (strings.HasPrefix(lower, "your task is to do a deep investigation") || strings.HasPrefix(lower, "your task is to")) &&
		strings.Contains(lower, "<objective>") && strings.Contains(lower, "</objective>") {
		return true
	}
	if strings.Contains(lower, "you have stopped calling tools without finishing") ||
		(strings.Contains(lower, "you have one final chance") && strings.Contains(lower, "short grace period")) ||
		(strings.Contains(lower, "must call `complete_task` immediately") && strings.Contains(lower, "do not call any other tools")) ||
		(strings.Contains(lower, "must call complete_task immediately") && strings.Contains(lower, "do not call any other tools")) {
		return true
	}
	if strings.Contains(lower, "/root/neeko-workspace/delivery/") &&
		strings.Contains(lower, "process only entries with sheetrow") &&
		strings.Contains(lower, "return only json") {
		return true
	}
	return false
}

func isTitleGenerationPrompt(raw string) bool {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.Join(strings.Fields(normalized), " ")
	switch normalized {
	case "generate a title for this conversation",
		"generate a title for this conversation:",
		"write a title for this conversation",
		"write a title for this conversation:",
		"summarize this conversation in a title",
		"summarize this conversation in a title:",
		"generate a concise title for this conversation",
		"generate a concise title for this conversation:",
		"write a concise title for this conversation",
		"write a concise title for this conversation:":
		return true
	default:
		return false
	}
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

// decodeEventArray 把顶层 JSON 数组解码为事件列表，并尽量容错：
//  1. 先严格 Unmarshal；
//  2. 失败则裁掉数组外的前后噪声（如末尾多余的 "解释" 文本、BOM 等）后重试；
//  3. 仍失败则用流式 Decoder 逐元素解析，保留能解出的有效事件，
//     避免数组外/末尾噪声导致整份日志解析失败、详情页整页空白。
func decodeEventArray(raw []byte) ([]rawEvent, bool) {
	var events []rawEvent
	if err := json.Unmarshal(raw, &events); err == nil {
		return events, true
	}

	// 裁剪到第一个 '[' 与最后一个 ']' 之间，去掉数组外噪声后再试一次。
	if start := bytes.IndexByte(raw, '['); start >= 0 {
		if end := bytes.LastIndexByte(raw, ']'); end > start {
			trimmed := raw[start : end+1]
			events = nil
			if err := json.Unmarshal(trimmed, &events); err == nil {
				return events, true
			}
		}
	}

	// 逐元素流式解析：保留能解出的有效事件，遇到损坏元素则停止（已收集的仍可用）。
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token() // 期望 '['
	if err != nil {
		return nil, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, false
	}
	collected := make([]rawEvent, 0, 64)
	for dec.More() {
		var ev rawEvent
		if err := dec.Decode(&ev); err != nil {
			break
		}
		collected = append(collected, ev)
	}
	if len(collected) > 0 {
		return collected, true
	}
	return nil, false
}

// isNeekoResponsesLog 识别新版 Neeko/Responses 事件流：
// 顶层 JSON 数组，事件以 type 区分（REQUEST_BODY / STREAM_RESPONSE 等），
// 且没有 logId/promptId/sessionID 字段（老版解析器据此聚合，这里聚合不出来）。
func isNeekoResponsesLog(events []rawEvent) bool {
	hasReq, hasStream := false, false
	for _, ev := range events {
		switch ev.Type {
		case "REQUEST_BODY":
			hasReq = true
		case "STREAM_RESPONSE":
			hasStream = true
		}
		if hasReq && hasStream {
			return true
		}
	}
	return false
}

// parseNeekoResponsesLog 解析新版事件流。
//
// 关键事实（基于真实样本验证）：
//   - 事件按组出现：REQUEST_BODY 之后紧跟 STREAM_RESPONSE，构成一次 LLM 调用。
//   - REQUEST_BODY.data = {model, input:[{role,content},...]}；用户问题取 input 中
//     最后一条 role==user 的 input_text（developer 是 system prompt，不能当用户问题）。
//   - STREAM_RESPONSE.data = {durationMs, allChunks:[...], finalContent}；
//     usage / 最终 output 在 allChunks 里 type==response.completed 的 response 内。
//   - Agent 多步调工具时，同一个用户问题会连发多次调用（input_tokens 递增）；
//     这些调用应合并为同一个 Round（按 userPrompt 连续相同分组），而非多轮。
func parseNeekoResponsesLog(events []rawEvent) *ParseResult {
	out := &ParseResult{}
	var pendingReq json.RawMessage
	var pendingReqTS string

	flushPair := func(req json.RawMessage, reqTS string, sr json.RawMessage, srTS string) {
		if len(req) == 0 {
			return
		}
		userPrompt, model, msgs := decodeNeekoRequest(req)
		call := callRec{Model: model}
		call.Messages = msgs
		if userPrompt == "" {
			userPrompt = extractUserPrompt(msgs)
		}
		call.StartedMs = parseTSMillis(reqTS)
		decodeNeekoStream(sr, &call)
		if call.StartedMs == 0 {
			call.StartedMs = parseTSMillis(srTS)
		}
		if call.DurationMs > 0 {
			call.EndedMs = call.StartedMs + call.DurationMs
		} else {
			call.EndedMs = call.StartedMs
		}

		// 同一用户问题的连续调用合并进同一 Round。
		var round *Round
		if n := len(out.Rounds); n > 0 && (userPrompt == "" || out.Rounds[n-1].UserPrompt == userPrompt) {
			round = &out.Rounds[n-1]
		} else {
			out.Rounds = append(out.Rounds, Round{
				PromptID:   fmt.Sprintf("round-%d", len(out.Rounds)+1),
				UserPrompt: userPrompt,
				StartedMs:  call.StartedMs,
			})
			round = &out.Rounds[len(out.Rounds)-1]
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

	for _, ev := range events {
		switch ev.Type {
		case "REQUEST_BODY":
			// 若上一组只有 REQUEST 没等到 STREAM，也先落一笔。
			if len(pendingReq) > 0 {
				flushPair(pendingReq, pendingReqTS, nil, "")
			}
			pendingReq = ev.Data
			pendingReqTS = ev.ts()
		case "STREAM_RESPONSE":
			flushPair(pendingReq, pendingReqTS, ev.Data, ev.ts())
			pendingReq = nil
			pendingReqTS = ""
		}
	}
	if len(pendingReq) > 0 {
		flushPair(pendingReq, pendingReqTS, nil, "")
	}
	return out
}

// decodeNeekoRequest 从 REQUEST_BODY.data 提取真实用户问题与模型名。
// 兼容两种线上格式：
//   - Responses 风格：data.input = [{role, content}]
//   - Chat Completions 风格：data.messages = [{role, content}]
//
// 用户问题取最后一条 role==user 的文本（system/developer 是系统提示词，不能当用户问题）。
func decodeNeekoRequest(raw json.RawMessage) (userPrompt, model string, msgs []chatMessage) {
	var body struct {
		Model    string         `json:"model"`
		Input    []neekoMessage `json:"input"`
		Messages []neekoMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", "", nil
	}
	model = body.Model
	sourceMsgs := body.Input
	if len(sourceMsgs) == 0 {
		sourceMsgs = body.Messages
	}
	msgs = make([]chatMessage, 0, len(sourceMsgs))
	for _, msg := range sourceMsgs {
		msgs = append(msgs, chatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.ToLower(msgs[i].Role) != "user" {
			continue
		}
		// 先剥离 <system-reminder> 等框架注入块，再取真实用户输入。注入块常被拼在
		// 真实提问之前塞进同一条 user 消息，不剥离会导致 prompt 显示成系统注入、多轮塌缩。
		t := extractBusinessWrappedOriginalQuery(stripQuestionAnswerResultPayload(stripInjectedContext(neekoContentText(msgs[i].Content))))
		if t != "" && !isSyntheticToolPrompt(t) {
			return t, model, msgs
		}
	}
	return "", model, msgs
}

type neekoMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// neekoContentText 解析 Responses 风格 content：string 或 [{type,text}]。
func neekoContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
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
	return ""
}

// decodeNeekoStream 从 STREAM_RESPONSE.data 提取耗时、usage、模型回复与工具调用。
func decodeNeekoStream(raw json.RawMessage, c *callRec) {
	if len(raw) == 0 {
		return
	}
	var sr struct {
		LogID        string                   `json:"logId"`
		DurationMs   int64                    `json:"durationMs"`
		AllChunks    []map[string]interface{} `json:"allChunks"`
		FinalContent string                   `json:"finalContent"`
	}
	if err := json.Unmarshal(raw, &sr); err != nil {
		return
	}
	if c.LogID == "" {
		c.LogID = sr.LogID
	}
	c.DurationMs = sr.DurationMs
	for _, chunk := range sr.AllChunks {
		// Responses 风格：usage 与最终 output 在 type==response.completed 的 response 内。
		if strings.ToLower(stringAny(chunk["type"])) == "response.completed" {
			resp, _ := chunk["response"].(map[string]interface{})
			if len(resp) > 0 {
				if c.Model == "" {
					c.Model = stringAny(firstAny(resp, "model", "model_name"))
				}
				in, out, reason := extractUsage(resp)
				if in > 0 || out > 0 || reason > 0 {
					c.UsageIn, c.UsageOut, c.UsageReason = in, out, reason
				}
				payload := payloadFromResponseMap(resp)
				c.Text = firstNonEmptyString(c.Text, payload.Text)
				c.Reasoning = firstNonEmptyString(c.Reasoning, payload.Reasoning)
				if len(c.Tools) == 0 {
					c.Tools = payload.Tools
				}
			}
			continue
		}
		// Chat Completions 风格：usage 直接挂在 chunk 顶层，回复在 choices[].delta/message。
		if chunk["usage"] != nil {
			if c.Model == "" {
				c.Model = stringAny(firstAny(chunk, "model", "model_name"))
			}
			in, out, reason := extractUsage(chunk)
			if in > 0 || out > 0 || reason > 0 {
				c.UsageIn, c.UsageOut, c.UsageReason = in, out, reason
			}
		}
	}
	// finalContent 作为模型回复的兜底（Chat 风格常把完整回复放在这里）。
	if c.Text == "" {
		c.Text = strings.TrimSpace(sr.FinalContent)
	}
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
