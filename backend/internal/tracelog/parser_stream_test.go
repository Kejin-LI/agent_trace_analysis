package tracelog

import (
	"strings"
	"testing"
)

func classicJSONLSample() string {
	return strings.Join([]string{
		`{"type":"REQUEST_BODY","timestamp":"2026-06-04T03:12:00.000Z","sessionID":"s1","promptId":"p1","logId":"l1","data":{"model":"gpt","messages":[{"role":"system","content":"sys"},{"role":"user","content":"第一个问题"}]}}`,
		`{"type":"RESPONSE_BODY_FINAL","timestamp":"2026-06-04T03:12:05.000Z","sessionID":"s1","promptId":"p1","logId":"l1","data":{"final":{"text":"第一个回答","reasoning":"思考","tools":[{"callID":"c1","tool":"web_fetch","input":{"url":"https://example.com"},"output":{"ok":true}}],"usage":{"inputTokens":10,"outputTokens":20,"reasoningTokens":3}}}}`,
		`{"type":"RESPONSE_META","timestamp":"2026-06-04T03:12:06.000Z","sessionID":"s1","promptId":"p1","logId":"l1","data":{"durationMs":6000,"status":200}}`,
		`{"type":"REQUEST_BODY","timestamp":"2026-06-04T03:13:00.000Z","sessionID":"s1","promptId":"p2","logId":"l2","data":{"model":"gpt","messages":[{"role":"user","content":"第二个问题"}]}}`,
		`{"type":"RESPONSE_BODY_FINAL","timestamp":"2026-06-04T03:13:02.000Z","sessionID":"s1","promptId":"p2","logId":"l2","data":{"final":{"text":"第二个回答","usage":{"inputTokens":7,"outputTokens":8}}}}`,
	}, "\n") + "\n"
}

func TestParseStreamMatchesParseForClassicJSONL(t *testing.T) {
	raw := classicJSONLSample()
	want, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got, bytesRead, err := ParseStream(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseStream error: %v", err)
	}
	if bytesRead == 0 {
		t.Fatalf("expected bytesRead > 0")
	}
	if got.SessionID != want.SessionID {
		t.Fatalf("sessionID = %q, want %q", got.SessionID, want.SessionID)
	}
	if len(got.Rounds) != len(want.Rounds) {
		t.Fatalf("rounds = %d, want %d", len(got.Rounds), len(want.Rounds))
	}
	for i := range want.Rounds {
		gr, wr := got.Rounds[i], want.Rounds[i]
		if gr.PromptID != wr.PromptID || gr.UserPrompt != wr.UserPrompt {
			t.Fatalf("round %d prompt = (%q,%q), want (%q,%q)", i, gr.PromptID, gr.UserPrompt, wr.PromptID, wr.UserPrompt)
		}
		if gr.InputTokens != wr.InputTokens || gr.OutputTokens != wr.OutputTokens || gr.ReasoningTokens != wr.ReasoningTokens {
			t.Fatalf("round %d tokens = %d/%d/%d, want %d/%d/%d", i, gr.InputTokens, gr.OutputTokens, gr.ReasoningTokens, wr.InputTokens, wr.OutputTokens, wr.ReasoningTokens)
		}
		if len(gr.Calls) != len(wr.Calls) {
			t.Fatalf("round %d calls = %d, want %d", i, len(gr.Calls), len(wr.Calls))
		}
		if gr.Calls[0].Text != wr.Calls[0].Text || len(gr.Calls[0].Tools) != len(wr.Calls[0].Tools) {
			t.Fatalf("round %d call mismatch", i)
		}
	}
}

// mixedFormatJSONLSample 复刻线上真实 session：首轮用 Chat Completions 的 messages，
// 后续轮切到 Responses API 的 input 数组（新模型 GPT-5.x），每轮带 logId/promptId、
// 用 REQUEST_BODY + RESPONSE_BODY_FINAL（而非 STREAM_RESPONSE）。
// 旧实现 decodeRequest 只认 messages，会把第二轮的 user prompt 解析为空，
// 导致详情页多轮塌缩成一轮。
func mixedFormatJSONLSample() string {
	return strings.Join([]string{
		`{"type":"REQUEST_BODY","timestamp":"2026-06-04T03:12:00.000Z","sessionID":"s1","promptId":"p1","logId":"l1","data":{"model":"gemini","messages":[{"role":"system","content":"sys"},{"role":"user","content":"第一个问题"}]}}`,
		`{"type":"RESPONSE_BODY_FINAL","timestamp":"2026-06-04T03:12:05.000Z","sessionID":"s1","promptId":"p1","logId":"l1","data":{"final":{"text":"第一个回答","usage":{"inputTokens":10,"outputTokens":20}}}}`,
		`{"type":"REQUEST_BODY","timestamp":"2026-06-04T03:13:00.000Z","sessionID":"s1","promptId":"p2","logId":"l2","data":{"model":"gpt-5.5","input":[{"role":"developer","content":"You are OpenCode"},{"role":"user","content":[{"type":"input_text","text":"第二个问题"}]}]}}`,
		`{"type":"RESPONSE_BODY_FINAL","timestamp":"2026-06-04T03:13:02.000Z","sessionID":"s1","promptId":"p2","logId":"l2","data":{"final":{"text":"第二个回答","usage":{"inputTokens":7,"outputTokens":8}}}}`,
	}, "\n") + "\n"
}

func TestParseStreamExtractsResponsesAPIUserPrompt(t *testing.T) {
	raw := mixedFormatJSONLSample()
	got, _, err := ParseStream(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseStream error: %v", err)
	}
	if len(got.Rounds) != 2 {
		t.Fatalf("rounds = %d, want 2 (Responses-format round must not collapse)", len(got.Rounds))
	}
	if got.Rounds[0].UserPrompt != "第一个问题" {
		t.Fatalf("round 0 prompt = %q, want 第一个问题", got.Rounds[0].UserPrompt)
	}
	if got.Rounds[1].UserPrompt != "第二个问题" {
		t.Fatalf("round 1 prompt = %q, want 第二个问题 (Responses input not parsed)", got.Rounds[1].UserPrompt)
	}
	if got.Rounds[1].Calls[0].Model != "gpt-5.5" {
		t.Fatalf("round 1 model = %q, want gpt-5.5", got.Rounds[1].Calls[0].Model)
	}
}

func TestParseStreamRejectsOversizedLine(t *testing.T) {
	oversized := `{"type":"REQUEST_BODY","timestamp":"2026-06-04T03:12:00.000Z","sessionID":"s1","promptId":"p1","logId":"l1","data":{"model":"gpt","messages":[{"role":"user","content":"` + strings.Repeat("x", MaxJSONLLineBytes) + `"}]}}`
	if _, _, err := ParseStream(strings.NewReader(oversized)); err == nil {
		t.Fatalf("expected oversized line error")
	}
}

func TestParseStreamSupportsJSONArrayCompatibility(t *testing.T) {
	raw := classicJSONLSample()
	array := "[" + strings.Join(strings.Split(strings.TrimSpace(raw), "\n"), ",") + "]"

	want, err := Parse([]byte(array))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got, _, err := ParseStream(strings.NewReader(array))
	if err != nil {
		t.Fatalf("ParseStream error: %v", err)
	}
	if len(got.Rounds) != len(want.Rounds) {
		t.Fatalf("rounds = %d, want %d", len(got.Rounds), len(want.Rounds))
	}
}

func TestParseStreamTruncatesOversizedSessionByHardLimit(t *testing.T) {
	t.Setenv("TRACELOG_SESSION_MAX_BYTES", "2048")
	t.Setenv("TRACELOG_SESSION_PRESSURE_BYTES", "1024")
	t.Setenv("AGG_MEM_SOFT_PCT", "100")

	raw := classicJSONLSample() + `{"type":"RESPONSE_BODY_FINAL","timestamp":"2026-06-04T03:13:03.000Z","sessionID":"s1","promptId":"p2","logId":"l2","data":{"final":{"text":"` + strings.Repeat("超大输出", 600) + `"}}}` + "\n"

	got, _, err := ParseStream(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseStream error: %v", err)
	}
	if got.Truncation == nil || !got.Truncation.Truncated {
		t.Fatalf("expected truncation metadata")
	}
	if got.Truncation.Reason != "session_size_limit" {
		t.Fatalf("reason = %q, want session_size_limit", got.Truncation.Reason)
	}
	if got.Truncation.LimitBytes != 2048 {
		t.Fatalf("limit = %d, want 2048", got.Truncation.LimitBytes)
	}
	if len(got.Rounds) == 0 {
		t.Fatalf("expected partial rounds after truncation")
	}
}
