package api

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

type qualityTraceFingerprintSpan struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	StartedAtMs int64  `json:"started_at_ms"`
	DurationMs  int64  `json:"duration_ms"`
	StatusCode  int    `json:"status_code"`
}

type qualityTraceFingerprintTrace struct {
	ID          string                        `json:"id"`
	StartedAtMs int64                         `json:"started_at_ms"`
	DurationMs  int64                         `json:"duration_ms"`
	Status      string                        `json:"status"`
	Spans       []qualityTraceFingerprintSpan `json:"spans"`
}

type qualityFingerprintMeta struct {
	TraceFingerprint string `json:"_trace_fingerprint"`
}

func computeQualityTraceFingerprint(bundle apiSessionBundle) string {
	traces := make([]qualityTraceFingerprintTrace, 0, len(bundle.Traces))
	for _, trace := range bundle.Traces {
		spans := make([]qualityTraceFingerprintSpan, 0, len(trace.Spans))
		for _, span := range trace.Spans {
			spans = append(spans, qualityTraceFingerprintSpan{
				ID:          strings.TrimSpace(span.SpanID),
				Type:        strings.TrimSpace(span.SpanType),
				Name:        strings.TrimSpace(span.SpanName),
				StartedAtMs: span.StartedAtMs,
				DurationMs:  span.DurationMs,
				StatusCode:  span.StatusCode,
			})
		}
		traces = append(traces, qualityTraceFingerprintTrace{
			ID:          strings.TrimSpace(firstReviewNonEmpty(trace.TraceID, trace.SpanID)),
			StartedAtMs: trace.StartedAtMs,
			DurationMs:  trace.DurationMs,
			Status:      strings.TrimSpace(trace.Status),
			Spans:       spans,
		})
	}
	payload, err := json.Marshal(traces)
	if err != nil {
		return ""
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write(payload)
	return fmt.Sprintf("%d:%x", len(traces), hasher.Sum32())
}

func embedTraceFingerprintJSON(raw string, fingerprint string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "null" || strings.TrimSpace(fingerprint) == "" {
		return s
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return s
	}
	obj["_trace_fingerprint"] = strings.TrimSpace(fingerprint)
	buf, err := json.Marshal(obj)
	if err != nil {
		return s
	}
	return string(buf)
}

func buildTraceFingerprintMetaJSON(fingerprint string) string {
	if strings.TrimSpace(fingerprint) == "" {
		return ""
	}
	buf, err := json.Marshal(qualityFingerprintMeta{TraceFingerprint: strings.TrimSpace(fingerprint)})
	if err != nil {
		return ""
	}
	return string(buf)
}

func extractTraceFingerprintFromQualityRow(row model.StgSessionQualityEvaluation) string {
	for _, raw := range []string{row.LLMEvalResult, row.LLMRawResult, row.RuleEvalResult} {
		if fingerprint := extractTraceFingerprintFromJSON(raw); fingerprint != "" {
			return fingerprint
		}
	}
	return ""
}

func extractTraceFingerprintFromJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "null" {
		return ""
	}
	var meta qualityFingerprintMeta
	if err := json.Unmarshal([]byte(s), &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.TraceFingerprint)
}

func qualityEvaluationColumnExists(h *Handler, name string) bool {
	if h == nil {
		return false
	}
	_, ok := h.qualityEvaluationColumns()[name]
	return ok
}
