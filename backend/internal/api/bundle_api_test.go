package api

import (
	"strings"
	"testing"
	"time"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/tracelog"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

func TestBundleIdentityKeyUsesSessionAndArtifact(t *testing.T) {
	gotA := bundleIdentityKey("ses_123", "art_a")
	gotB := bundleIdentityKey("ses_123", "art_b")
	if gotA == gotB {
		t.Fatalf("bundle identity key should distinguish artifacts under the same session: gotA=%q gotB=%q", gotA, gotB)
	}
}

func TestBundleIdentityKeyFallsBackSafely(t *testing.T) {
	if got := bundleIdentityKey(" ses_123 ", " "); got != "session::ses_123" {
		t.Fatalf("unexpected session-only key: %q", got)
	}
	if got := bundleIdentityKey("", " art_123 "); got != "artifact::art_123" {
		t.Fatalf("unexpected artifact-only key: %q", got)
	}
	if got := bundleIdentityKey("", ""); got != "" {
		t.Fatalf("expected empty key when both identifiers are missing, got %q", got)
	}
}

func TestNormalizeBundleListTotal(t *testing.T) {
	tests := []struct {
		name          string
		reportedTotal int
		loaded        int
		limit         int
		offset        int
		want          int
	}{
		{
			name:          "underfilled first page uses visible rows",
			reportedTotal: 5998,
			loaded:        1600,
			limit:         2000,
			offset:        0,
			want:          1600,
		},
		{
			name:          "full page keeps larger reported total",
			reportedTotal: 5998,
			loaded:        2000,
			limit:         2000,
			offset:        0,
			want:          5998,
		},
		{
			name:          "later underfilled page includes offset",
			reportedTotal: 5998,
			loaded:        600,
			limit:         2000,
			offset:        2000,
			want:          2600,
		},
		{
			name:          "reported total cannot be smaller than visible rows",
			reportedTotal: 100,
			loaded:        200,
			limit:         200,
			offset:        0,
			want:          200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBundleListTotal(tt.reportedTotal, tt.loaded, tt.limit, tt.offset); got != tt.want {
				t.Fatalf("normalizeBundleListTotal() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDetailBundleNeedsRefreshByVersion(t *testing.T) {
	fresh := apiSessionBundle{
		DetailVersion: currentDetailBundleVersion,
		Traces:        []apiTrace{{TraceID: "trace-1"}},
	}
	if detailBundleNeedsRefresh(fresh) {
		t.Fatalf("fresh detail bundle should not require refresh")
	}

	stale := apiSessionBundle{
		DetailVersion: currentDetailBundleVersion - 1,
		Traces:        []apiTrace{{TraceID: "trace-1"}},
	}
	if !detailBundleNeedsRefresh(stale) {
		t.Fatalf("stale detail bundle should require refresh")
	}
}

func TestBuildBundleFromTOSCarriesPromptAndRoundModel(t *testing.T) {
	raw := `[
  {"type":"REQUEST_BODY","timestamp":"2026-06-23T07:14:00.000Z","data":{
    "model":"gemini-3-flash-preview",
    "input":[
      {"role":"system","content":"sys"},
      {"role":"user","content":"用户真正的问题"}
    ]}},
  {"type":"STREAM_RESPONSE","timestamp":"2026-06-23T07:14:01.000Z","data":{
    "durationMs":1000,
    "allChunks":[{"type":"response.completed","response":{"model":"gemini-3-flash-preview","usage":{"input_tokens":10,"output_tokens":5},"output":[]}}]}},
  {"type":"REQUEST_BODY","timestamp":"2026-06-23T07:14:02.000Z","data":{
    "model":"gpt-5.5-2026-04-24",
    "messages":[
      {"role":"system","content":"sys"},
      {"role":"user","content":"用户真正的问题"}
    ]}},
  {"type":"STREAM_RESPONSE","timestamp":"2026-06-23T07:14:03.000Z","data":{
    "durationMs":1000,
    "allChunks":[{"type":"response.completed","response":{"model":"gpt-5.5-2026-04-24","usage":{"input_tokens":20,"output_tokens":8},"output":[]}}]}}
]`

	pr, err := tracelog.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	bundle := buildBundleFromTOS(model.StgSessionSource{
		SessionID:  "ses_test",
		ArtifactID: "art_test",
		UserName:   "u",
		UserID:     "uid",
	}, pr)
	if bundle.DetailVersion != currentDetailBundleVersion {
		t.Fatalf("detail version = %d, want %d", bundle.DetailVersion, currentDetailBundleVersion)
	}
	if len(bundle.Traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(bundle.Traces))
	}
	tr := bundle.Traces[0]
	if tr.UserPrompt != "用户真正的问题" {
		t.Fatalf("trace user prompt = %q, want %q", tr.UserPrompt, "用户真正的问题")
	}
	if tr.ModelName != "gpt-5.5-2026-04-24" {
		t.Fatalf("trace model = %q, want final round model", tr.ModelName)
	}
	if len(tr.Spans) != 2 {
		t.Fatalf("span count = %d, want 2", len(tr.Spans))
	}
	if tr.Spans[0].Input != "用户真正的问题" {
		t.Fatalf("first model input = %q, want prompt text", tr.Spans[0].Input)
	}
	if tr.Spans[1].Input == "null" || !strings.Contains(tr.Spans[1].Input, "\"messages\"") {
		t.Fatalf("later model input = %q, want serialized messages payload", tr.Spans[1].Input)
	}
}

func TestDetailBundleNeedsSourceRefresh(t *testing.T) {
	bundle := apiSessionBundle{
		DetailVersion:     currentDetailBundleVersion,
		SourceUpdatedAtMs: time.Date(2026, 6, 23, 10, 0, 0, 0, time.Local).UnixMilli(),
		Traces:            []apiTrace{{TraceID: "trace-1"}},
	}
	if detailBundleNeedsSourceRefresh(bundle, &modellog.Session{UpdateAt: "2026-06-23 09:59:59"}) {
		t.Fatalf("older upstream update should not force refresh")
	}
	if !detailBundleNeedsSourceRefresh(bundle, &modellog.Session{UpdateAt: "2026-06-23 10:00:01"}) {
		t.Fatalf("newer upstream update should force refresh")
	}
}
