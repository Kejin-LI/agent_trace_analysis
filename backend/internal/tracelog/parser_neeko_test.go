package tracelog

import (
	"fmt"
	"testing"
)

// neekoSample 构造一份最小的 Neeko/Responses 事件流：一组 REQUEST_BODY + STREAM_RESPONSE。
// 用于锁定解析结果：1 轮、1 次调用、真实 user prompt、真实 usage/duration。
func neekoSample() string {
	return `[
  {"type":"REQUEST_BODY","timestamp":"2026-06-03T08:14:39.709Z","data":{
    "model":"gpt-5.5",
    "input":[
      {"role":"developer","content":"You are an interactive CLI agent."},
      {"role":"user","content":[{"type":"input_text","text":"上一个问题"}]},
      {"role":"assistant","content":[{"type":"output_text","text":"上一个回复"}]},
      {"role":"user","content":[{"type":"input_text","text":"分析最大event id的逻辑"}]}
    ]
  }},
  {"type":"STREAM_RESPONSE","timestamp":"2026-06-03T08:14:44.438Z","data":{
    "logId":"log-1",
    "durationMs":4732,
    "allChunks":[
      {"type":"response.created"},
      {"type":"response.completed","response":{
        "id":"resp_1",
        "model":"gpt-5.5",
        "usage":{"input_tokens":32090,"output_tokens":143,"output_tokens_details":{"reasoning_tokens":21}},
        "output":[
          {"type":"reasoning"},
          {"type":"function_call","name":"read_file","call_id":"call_1","arguments":"{\"path\":\"a.ts\"}"}
        ]
      }}
    ]
  }}
]`
}

func TestParseNeekoResponsesLog(t *testing.T) {
	pr, err := Parse([]byte(neekoSample()))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(pr.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1", len(pr.Rounds))
	}
	r := pr.Rounds[0]
	if r.UserPrompt != "分析最大event id的逻辑" {
		t.Errorf("user prompt = %q, want real last user message (not developer/system prompt)", r.UserPrompt)
	}
	if len(r.Calls) != 1 {
		t.Errorf("calls = %d, want 1", len(r.Calls))
	}
	if r.InputTokens != 32090 || r.OutputTokens != 143 || r.ReasoningTokens != 21 {
		t.Errorf("tokens = in:%d out:%d reason:%d, want 32090/143/21", r.InputTokens, r.OutputTokens, r.ReasoningTokens)
	}
	if dur := r.EndedMs - r.StartedMs; dur != 4732 {
		t.Errorf("duration = %d, want 4732", dur)
	}
}

// 同一用户问题连发多次调用（agent 多步）应合并为 1 轮、N 次调用，tokens 累加。
func TestParseNeekoMergesAgentLoop(t *testing.T) {
	pr, err := Parse([]byte(neekoMultiCallSample()))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(pr.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1 (agent loop must merge)", len(pr.Rounds))
	}
	r := pr.Rounds[0]
	if len(r.Calls) != 2 {
		t.Errorf("calls = %d, want 2", len(r.Calls))
	}
	if r.InputTokens != 100+200 {
		t.Errorf("input tokens = %d, want 300 (accumulated)", r.InputTokens)
	}
}

func neekoMultiCallSample() string {
	group := func(in int) string {
		return fmt.Sprintf(`
  {"type":"REQUEST_BODY","timestamp":"2026-06-03T08:14:39.709Z","data":{
    "model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"同一个问题"}]}]}},
  {"type":"STREAM_RESPONSE","timestamp":"2026-06-03T08:14:40.000Z","data":{
    "durationMs":1000,
    "allChunks":[{"type":"response.completed","response":{
      "usage":{"input_tokens":%d,"output_tokens":10},"output":[]}}]}}`, in)
	}
	return "[" + group(100) + "," + group(200) + "]"
}

// 数组末尾混入噪声文本时，解析器应裁剪/容错而非整盘失败。
func TestParseTolerateTrailingJunk(t *testing.T) {
	raw := neekoSample() + "\n解释：以上是日志"
	pr, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(pr.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1 despite trailing junk", len(pr.Rounds))
	}
	if pr.Rounds[0].InputTokens == 0 {
		t.Errorf("input tokens should be non-zero after junk tolerance")
	}
}

// 真实 TOS 日志的另一种格式：Chat Completions 风格——
// REQUEST_BODY.data 用 messages（非 input），usage 直接挂在 chunk 顶层（非 response.completed 内）。
// 这种格式之前会被解析成 0 tokens / 空 prompt，必须锁定。
func TestParseChatCompletionsStyle(t *testing.T) {
	raw := `[
  {"type":"REQUEST_BODY","timestamp":"2026-06-04T03:12:00.000Z","data":{
    "model":"gpt","messages":[
      {"role":"system","content":"You are an interactive CLI agent."},
      {"role":"user","content":"这是上下文设置"},
      {"role":"user","content":"必填项没填却能提交"}
    ]}},
  {"type":"STREAM_RESPONSE","timestamp":"2026-06-04T03:12:06.000Z","data":{
    "logId":"l1","durationMs":6933,"finalContent":"已修复",
    "allChunks":[
      {"id":"c1","choices":[{"delta":{"content":"已"}}]},
      {"id":"c2","model":"gpt","choices":[{"delta":{}}],
       "usage":{"prompt_tokens":33215,"completion_tokens":27,"total_tokens":33645,"reasoning_tokens":403}}
    ]}}
]`
	pr, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(pr.Rounds) != 1 {
		t.Fatalf("rounds = %d, want 1", len(pr.Rounds))
	}
	r := pr.Rounds[0]
	if r.UserPrompt != "必填项没填却能提交" {
		t.Errorf("user prompt = %q, want last user message from messages[]", r.UserPrompt)
	}
	if r.InputTokens != 33215 || r.OutputTokens != 27 || r.ReasoningTokens != 403 {
		t.Errorf("tokens = in:%d out:%d reason:%d, want 33215/27/403 (usage at chunk top-level)",
			r.InputTokens, r.OutputTokens, r.ReasoningTokens)
	}
	if dur := r.EndedMs - r.StartedMs; dur != 6933 {
		t.Errorf("duration = %d, want 6933", dur)
	}
}
