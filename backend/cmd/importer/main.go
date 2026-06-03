// Command importer 读取已跑通的 sessions.json，幂等 upsert 写入 NDB 的 stg_* 表。
//
// 用法（凭证全部来自环境变量，绝不写入代码/仓库）：
//
//	export DB_HOST=xxx DB_PORT=3306 DB_USER=xxx DB_PASSWORD=xxx DB_NAME=xxx
//	go run ./cmd/importer -file ../frontend/data/sessions.json -batch pilot-6
//
// 也可用 DB_DSN 直接提供完整 DSN。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/db"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

const previewLimit = 2000 // input/output 截断长度，符合“raw 克制”原则

// ---- sessions.json 数据结构（仅解析需要的字段） ----

type fileRoot struct {
	Sessions []sessionJSON `json:"sessions"`
}

type sessionJSON struct {
	SessionID   string      `json:"session_id"`
	ArtifactID  string      `json:"artifact_id"`
	UserID      *string     `json:"user_id"`
	StartedAtMs *int64      `json:"started_at_ms"`
	Traces      []traceJSON `json:"traces"`
}

type traceJSON struct {
	TraceID      string     `json:"trace_id"`
	SpanID       string     `json:"span_id"`
	Title        string     `json:"title"`
	ModelName    string     `json:"model_name"`
	Turns        *int       `json:"turns"`
	DurationMs   *int64     `json:"duration_ms"`
	LLMPureMs    *int64     `json:"llm_pure_ms"`
	ToolMs       *int64     `json:"tool_ms"`
	InputTokens  *int64     `json:"input_tokens"`
	OutputTokens *int64     `json:"output_tokens"`
	StartedAtMs  *int64     `json:"started_at_ms"`
	Status       string     `json:"status"`
	Spans        []spanJSON `json:"spans"`
}

type spanJSON struct {
	SpanID       string                 `json:"span_id"`
	ParentID     string                 `json:"parent_id"`
	SpanName     string                 `json:"span_name"`
	SpanType     string                 `json:"span_type"`
	DurationMs   *int64                 `json:"duration_ms"`
	StartedAtMs  *int64                 `json:"started_at_ms"`
	StatusCode   *int                   `json:"status_code"`
	Input        string                 `json:"input"`
	Output       string                 `json:"output"`
	CustomTags   map[string]interface{} `json:"custom_tags"`
	UserPrompt   string                 `json:"-"`
	PromptSource string                 `json:"-"`
	RoundIndex   int                    `json:"-"`
}

type modelInputSummary struct {
	Kind         string `json:"kind"`
	UserPrompt   string `json:"user_prompt,omitempty"`
	PromptSource string `json:"prompt_source,omitempty"`
	RoundIndex   int    `json:"round_index,omitempty"`
	InputPreview string `json:"input_preview,omitempty"`
}

func main() {
	var (
		filePath  string
		batchName string
	)
	flag.StringVar(&filePath, "file", "../frontend/data/sessions.json", "sessions.json 路径")
	flag.StringVar(&batchName, "batch", "manual-import", "批次名称")
	flag.Parse()

	raw, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("读取文件失败 %s: %v", filePath, err)
	}
	var root fileRoot
	if err := json.Unmarshal(raw, &root); err != nil {
		log.Fatalf("解析 JSON 失败: %v", err)
	}
	enrichSessions(root.Sessions)
	log.Printf("已加载 %d 个 session", len(root.Sessions))

	gdb, err := db.Open()
	if err != nil {
		log.Fatalf("%v", err)
	}

	syncJobID := uuid.NewString()
	now := time.Now()
	job := model.StgSyncJob{
		SyncJobID:     syncJobID,
		BatchName:     batchName,
		SourceFile:    filePath,
		ArtifactCount: countArtifacts(root.Sessions),
		Status:        "running",
		StartedAt:     &now,
	}
	if err := gdb.Create(&job).Error; err != nil {
		log.Fatalf("创建同步任务失败: %v", err)
	}
	log.Printf("同步任务 sync_job_id=%s 已创建", syncJobID)

	var sessionCount, traceCount int
	var spanCount int64

	err = gdb.Transaction(func(tx *gorm.DB) error {
		for _, s := range root.Sessions {
			if err := upsertSession(tx, syncJobID, s); err != nil {
				return fmt.Errorf("session %s: %w", s.SessionID, err)
			}
			sessionCount++

			for _, t := range s.Traces {
				if err := upsertTrace(tx, syncJobID, s, t); err != nil {
					return fmt.Errorf("trace %s: %w", t.TraceID, err)
				}
				traceCount++

				for _, sp := range t.Spans {
					if err := upsertSpan(tx, syncJobID, s, t, sp); err != nil {
						return fmt.Errorf("span %s/%s: %w", t.TraceID, sp.SpanID, err)
					}
					spanCount++
				}
			}
		}
		return nil
	})

	fin := time.Now()
	if err != nil {
		gdb.Model(&model.StgSyncJob{}).Where("sync_job_id = ?", syncJobID).Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": err.Error(),
			"finished_at":   fin,
		})
		log.Fatalf("导入失败（已回滚）: %v", err)
	}

	gdb.Model(&model.StgSyncJob{}).Where("sync_job_id = ?", syncJobID).Updates(map[string]interface{}{
		"session_count": sessionCount,
		"trace_count":   traceCount,
		"span_count":    spanCount,
		"status":        "success",
		"finished_at":   fin,
	})

	log.Printf("导入完成: sessions=%d traces=%d spans=%d", sessionCount, traceCount, spanCount)
}

func upsertSession(tx *gorm.DB, jobID string, s sessionJSON) error {
	row := model.StgArtifactSession{
		SyncJobID:          jobID,
		ArtifactID:         s.ArtifactID,
		SessionID:          s.SessionID,
		UserID:             strOrEmpty(s.UserID),
		SessionCreatedAtMs: s.StartedAtMs,
		SessionCreatedAt:   msToTime(s.StartedAtMs),
		Status:             "synced",
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "artifact_id"}, {Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"sync_job_id", "user_id", "session_created_at_ms",
			"session_created_at", "status", "updated_at",
		}),
	}).Create(&row).Error
}

func upsertTrace(tx *gorm.DB, jobID string, s sessionJSON, t traceJSON) error {
	row := model.StgArtifactTrace{
		SyncJobID:          jobID,
		ArtifactID:         s.ArtifactID,
		SessionID:          s.SessionID,
		TraceID:            t.TraceID,
		RootSpanID:         t.SpanID,
		UserID:             strOrEmpty(s.UserID),
		StartedAtMs:        t.StartedAtMs,
		StartedAt:          msToTime(t.StartedAtMs),
		DurationMs:         t.DurationMs,
		LLMDurationMs:      t.LLMPureMs,
		ToolDurationMs:     t.ToolMs,
		TurnCount:          t.Turns,
		SpanCount:          intPtr(len(t.Spans)),
		Status:             t.Status,
		ModelName:          t.ModelName,
		InputTokens:        t.InputTokens,
		OutputTokens:       t.OutputTokens,
		TotalTokens:        sumTokens(t.InputTokens, t.OutputTokens),
		UserRequestPreview: truncate(t.Title, previewLimit),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "trace_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"sync_job_id", "artifact_id", "session_id", "root_span_id", "user_id",
			"started_at_ms", "started_at", "duration_ms", "llm_duration_ms",
			"tool_duration_ms", "turn_count", "span_count", "status", "model_name",
			"input_tokens", "output_tokens", "total_tokens", "user_request_preview", "updated_at",
		}),
	}).Create(&row).Error
}

func upsertSpan(tx *gorm.DB, jobID string, s sessionJSON, t traceJSON, sp spanJSON) error {
	in := tagInt64(sp.CustomTags, "input_tokens")
	out := tagInt64(sp.CustomTags, "output_tokens")
	row := model.StgArtifactSpan{
		SyncJobID:     jobID,
		ArtifactID:    s.ArtifactID,
		SessionID:     s.SessionID,
		TraceID:       t.TraceID,
		SpanID:        sp.SpanID,
		ParentID:      sp.ParentID,
		SpanName:      sp.SpanName,
		SpanType:      sp.SpanType,
		StartedAtMs:   sp.StartedAtMs,
		StartedAt:     msToTime(sp.StartedAtMs),
		DurationMs:    sp.DurationMs,
		Status:        statusFromCode(sp.StatusCode),
		ModelName:     tagStr(sp.CustomTags, "model_name"),
		InputTokens:   in,
		OutputTokens:  out,
		TotalTokens:   sumTokens(in, out),
		HasInput:      sp.Input != "",
		HasOutput:     sp.Output != "",
		InputPreview:  buildInputPreview(sp),
		OutputPreview: truncate(sp.Output, previewLimit),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "trace_id"}, {Name: "span_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"sync_job_id", "artifact_id", "session_id", "parent_id", "span_name",
			"span_type", "started_at_ms", "started_at", "duration_ms", "status",
			"model_name", "input_tokens", "output_tokens", "total_tokens",
			"has_input", "has_output", "input_preview", "output_preview", "updated_at",
		}),
	}).Create(&row).Error
}

// ---- helpers ----

func countArtifacts(sessions []sessionJSON) int {
	set := map[string]struct{}{}
	for _, s := range sessions {
		set[s.ArtifactID] = struct{}{}
	}
	return len(set)
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func intPtr(v int) *int { return &v }

func msToTime(ms *int64) *time.Time {
	if ms == nil || *ms == 0 {
		return nil
	}
	t := time.UnixMilli(*ms)
	return &t
}

func sumTokens(a, b *int64) *int64 {
	if a == nil && b == nil {
		return nil
	}
	var sum int64
	if a != nil {
		sum += *a
	}
	if b != nil {
		sum += *b
	}
	return &sum
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…[truncated]"
}

func buildInputPreview(sp spanJSON) string {
	if sp.SpanType != "model" {
		return truncate(sp.Input, previewLimit)
	}
	summary := modelInputSummary{
		Kind:         "model_input_summary",
		UserPrompt:   sp.UserPrompt,
		PromptSource: sp.PromptSource,
		RoundIndex:   sp.RoundIndex,
		InputPreview: truncate(sp.Input, 320),
	}
	buf, err := json.Marshal(summary)
	if err != nil {
		return truncate(sp.Input, previewLimit)
	}
	return string(buf)
}

func enrichSessions(sessions []sessionJSON) {
	for si := range sessions {
		for ti := range sessions[si].Traces {
			enrichTrace(&sessions[si].Traces[ti])
		}
	}
}

func enrichTrace(t *traceJSON) {
	curPrompt := ""
	roundIndex := 0
	modelIdx := modelSpanIndices(t.Spans)
	for _, idx := range modelIdx {
		sp := &t.Spans[idx]
		prompt, source := extractPromptFromModelInput(sp.Input)
		if prompt == "" {
			prompt = cleanPromptText(t.Title)
			if prompt != "" {
				source = "trace_title"
			}
		}
		if prompt != "" && prompt != curPrompt {
			roundIndex++
			curPrompt = prompt
		} else if roundIndex == 0 && prompt != "" {
			roundIndex = 1
			curPrompt = prompt
		}
		if prompt == "" {
			prompt = curPrompt
		}
		sp.UserPrompt = prompt
		sp.PromptSource = source
		sp.RoundIndex = roundIndex
	}
}

func modelSpanIndices(spans []spanJSON) []int {
	indices := make([]int, 0)
	for i := range spans {
		if spans[i].SpanType == "model" {
			indices = append(indices, i)
		}
	}
	return indices
}

func extractPromptFromModelInput(input string) (string, string) {
	if strings.TrimSpace(input) == "" {
		return "", ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		return "", ""
	}
	rawMsgs, ok := payload["messages"].([]interface{})
	if !ok || len(rawMsgs) == 0 {
		return "", ""
	}
	fallback := ""
	for i := len(rawMsgs) - 1; i >= 0; i-- {
		msg, ok := rawMsgs[i].(map[string]interface{})
		if !ok || strings.ToLower(fmt.Sprint(msg["role"])) != "user" {
			continue
		}
		content := contentToText(msg["content"])
		if content == "" {
			continue
		}
		if prompt := cleanPromptText(content); prompt != "" {
			return prompt, "messages_last_user"
		}
		if fallback == "" {
			fallback = normalizeText(content)
		}
	}
	if fallback != "" {
		return fallback, "messages_last_user_fallback"
	}
	return "", ""
}

func contentToText(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return normalizeText(v)
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch p := item.(type) {
			case string:
				if s := normalizeText(p); s != "" {
					parts = append(parts, s)
				}
			case map[string]interface{}:
				if text, ok := p["text"].(string); ok {
					if s := normalizeText(text); s != "" {
						parts = append(parts, s)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func cleanPromptText(raw string) string {
	prompt := normalizeText(raw)
	if prompt == "" || isControlPrompt(prompt) {
		return ""
	}
	return prompt
}

func normalizeText(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	return strings.TrimSpace(strings.Join(fields, " "))
}

func isControlPrompt(raw string) bool {
	compact := strings.ToLower(raw)
	replacer := strings.NewReplacer(
		" ", "", "\n", "", "\t", "",
		"(", "", ")", "", "（", "", "）", "",
		"[", "", "]", "", "【", "", "】", "",
		"<", "", ">", "", "\"", "", "'", "",
		"“", "", "”", "", ".", "", "。", "",
		",", "", "，", "", ":", "", "：", "",
		"!", "", "！", "", "?", "", "？", "",
	)
	switch replacer.Replace(compact) {
	case "继续执行", "请继续执行", "继续", "continue", "pleasecontinue", "resume", "resumeexecution", "goon", "proceed":
		return true
	default:
		return false
	}
}

func statusFromCode(code *int) string {
	if code == nil {
		return ""
	}
	if *code == 0 {
		return "success"
	}
	return "error"
}

func tagStr(tags map[string]interface{}, key string) string {
	if tags == nil {
		return ""
	}
	if v, ok := tags[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func tagInt64(tags map[string]interface{}, key string) *int64 {
	s := tagStr(tags, key)
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
