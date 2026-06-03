package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

// Handler 持有数据库连接，提供读库接口。
type Handler struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Handler { return &Handler{db: db} }

// Register 将读库 API 挂载到 /api 下。
func (h *Handler) Register(r *gin.Engine) {
	g := r.Group("/api")
	g.GET("/sessions", h.listSessions)
	g.GET("/session-bundles", h.listSessionBundles)
	g.GET("/session-bundles/:session_id", h.getSessionBundle)
	g.GET("/sessions/:session_id/traces", h.listTraces)
	g.GET("/traces/:trace_id", h.getTrace)
	g.GET("/traces/:trace_id/spans", h.listSpans)
	g.GET("/sync-jobs", h.listSyncJobs)
}

type apiRule struct {
	Name        string `json:"name"`
	Passed      bool   `json:"passed"`
	FailedLabel string `json:"failed_label,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

type apiFeatures struct {
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

type apiRadar struct {
	Response      int `json:"response"`
	Stability     int `json:"stability"`
	Thinking      int `json:"thinking"`
	Resource      int `json:"resource"`
	Orchestration int `json:"orchestration"`
}

type apiSpan struct {
	SpanID       string `json:"span_id"`
	ParentID     string `json:"parent_id"`
	SpanName     string `json:"span_name"`
	SpanType     string `json:"span_type"`
	DurationMs   int64  `json:"duration_ms"`
	StartedAtMs  int64  `json:"started_at_ms"`
	StartedAt    string `json:"started_at,omitempty"`
	StatusCode   int    `json:"status_code"`
	Input        string `json:"input"`
	Output       string `json:"output"`
	CustomTags   string `json:"custom_tags"`
	UserPrompt   string `json:"user_prompt,omitempty"`
	PromptSource string `json:"prompt_source,omitempty"`
	RoundIndex   int    `json:"round_index,omitempty"`
}

type apiTrace struct {
	TraceID      string    `json:"trace_id"`
	SpanID       string    `json:"span_id"`
	Title        string    `json:"title"`
	UserPrompt   string    `json:"user_prompt,omitempty"`
	RoundCount   int       `json:"round_count,omitempty"`
	ModelName    string    `json:"model_name"`
	Turns        int       `json:"turns"`
	DurationMs   int64     `json:"duration_ms"`
	LLMPureMs    int64     `json:"llm_pure_ms"`
	ToolMs       int64     `json:"tool_ms"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	StartedAtMs  int64     `json:"started_at_ms"`
	StartedAt    string    `json:"started_at,omitempty"`
	Status       string    `json:"status"`
	Spans        []apiSpan `json:"spans"`
}

type apiSessionBundle struct {
	ID           string      `json:"id"`
	SessionID    string      `json:"session_id"`
	ArtifactID   string      `json:"artifact_id"`
	User         string      `json:"user"`
	UserID       string      `json:"user_id"`
	Title        string      `json:"title"`
	Trace        string      `json:"trace"`
	StartedAtMs  int64       `json:"started_at_ms"`
	StartedAt    string      `json:"started_at,omitempty"`
	DurationMs   int64       `json:"duration_ms"`
	InputTokens  int64       `json:"input_tokens"`
	OutputTokens int64       `json:"output_tokens"`
	ToolCalls    int         `json:"tool_calls"`
	Turns        int         `json:"turns"`
	TraceCount   int         `json:"trace_count"`
	Score        int         `json:"score"`
	Color        string      `json:"color"`
	Chip         string      `json:"chip"`
	Features     apiFeatures `json:"features"`
	Radar        apiRadar    `json:"radar"`
	Rules        []apiRule   `json:"rules"`
	Traces       []apiTrace  `json:"traces"`
}

func (h *Handler) listSessions(c *gin.Context) {
	limit, offset := pagination(c)
	var rows []model.StgArtifactSession
	q := h.db.Order("session_created_at_ms DESC").Limit(limit).Offset(offset)
	if aid := c.Query("artifact_id"); aid != "" {
		q = q.Where("artifact_id = ?", aid)
	}
	if err := q.Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "limit": limit, "offset": offset})
}

func (h *Handler) listSessionBundles(c *gin.Context) {
	limit, offset := bundlePagination(c)
	var sessions []model.StgArtifactSession
	q := h.db.Order("session_created_at_ms DESC").Limit(limit).Offset(offset)
	if aid := c.Query("artifact_id"); aid != "" {
		q = q.Where("artifact_id = ?", aid)
	}
	if sid := c.Query("session_id"); sid != "" {
		q = q.Where("session_id = ?", sid)
	}
	if err := q.Find(&sessions).Error; err != nil {
		fail(c, err)
		return
	}
	bundles, err := h.loadSessionBundles(sessions)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": bundles, "limit": limit, "offset": offset})
}

func (h *Handler) getSessionBundle(c *gin.Context) {
	var session model.StgArtifactSession
	if err := h.db.Where("session_id = ?", c.Param("session_id")).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		fail(c, err)
		return
	}
	bundles, err := h.loadSessionBundles([]model.StgArtifactSession{session})
	if err != nil {
		fail(c, err)
		return
	}
	if len(bundles) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, bundles[0])
}

func (h *Handler) listTraces(c *gin.Context) {
	var rows []model.StgArtifactTrace
	if err := h.db.Where("session_id = ?", c.Param("session_id")).
		Order("started_at_ms ASC").Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) getTrace(c *gin.Context) {
	var trace model.StgArtifactTrace
	if err := h.db.Where("trace_id = ?", c.Param("trace_id")).First(&trace).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "trace not found"})
			return
		}
		fail(c, err)
		return
	}
	var spans []model.StgArtifactSpan
	if err := h.db.Where("trace_id = ?", trace.TraceID).
		Order("started_at_ms ASC").Find(&spans).Error; err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"trace": trace, "spans": spans})
}

func (h *Handler) listSpans(c *gin.Context) {
	var rows []model.StgArtifactSpan
	if err := h.db.Where("trace_id = ?", c.Param("trace_id")).
		Order("started_at_ms ASC").Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) listSyncJobs(c *gin.Context) {
	limit, offset := pagination(c)
	var rows []model.StgSyncJob
	if err := h.db.Order("id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) loadSessionBundles(sessions []model.StgArtifactSession) ([]apiSessionBundle, error) {
	if len(sessions) == 0 {
		return []apiSessionBundle{}, nil
	}
	sessionIDs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		sessionIDs = append(sessionIDs, s.SessionID)
	}

	var traceRows []model.StgArtifactTrace
	if err := h.db.Where("session_id IN ?", sessionIDs).Order("started_at_ms ASC").Find(&traceRows).Error; err != nil {
		return nil, err
	}
	traceIDs := make([]string, 0, len(traceRows))
	for _, t := range traceRows {
		traceIDs = append(traceIDs, t.TraceID)
	}

	var spanRows []model.StgArtifactSpan
	if len(traceIDs) > 0 {
		if err := h.db.Where("trace_id IN ?", traceIDs).Order("started_at_ms ASC").Find(&spanRows).Error; err != nil {
			return nil, err
		}
	}

	spansByTrace := make(map[string][]model.StgArtifactSpan, len(traceIDs))
	for _, sp := range spanRows {
		spansByTrace[sp.TraceID] = append(spansByTrace[sp.TraceID], sp)
	}
	tracesBySession := make(map[string][]model.StgArtifactTrace, len(sessions))
	for _, tr := range traceRows {
		tracesBySession[tr.SessionID] = append(tracesBySession[tr.SessionID], tr)
	}

	out := make([]apiSessionBundle, 0, len(sessions))
	for _, s := range sessions {
		traces := tracesBySession[s.SessionID]
		bundle := buildSessionBundle(s, traces, spansByTrace)
		out = append(out, bundle)
	}
	return out, nil
}

func buildSessionBundle(session model.StgArtifactSession, traceRows []model.StgArtifactTrace, spansByTrace map[string][]model.StgArtifactSpan) apiSessionBundle {
	apiTraces := make([]apiTrace, 0, len(traceRows))
	var (
		totalDuration int64
		totalIn       int64
		totalOut      int64
		startedAtMs   = nullableInt64(session.SessionCreatedAtMs)
		startedAt     = timeToString(session.SessionCreatedAt)
		title         = ""
		firstTraceID  = ""
	)

	for i, tr := range traceRows {
		spans := buildTraceSpans(spansByTrace[tr.TraceID])
		userPrompt := extractTraceUserPrompt(tr.UserRequestPreview, spans)
		roundCount := countRounds(spans)
		turns := countModelSpans(spans)
		apiTraces = append(apiTraces, apiTrace{
			TraceID:      tr.TraceID,
			SpanID:       tr.RootSpanID,
			Title:        pickFirstNonEmpty(tr.UserRequestPreview, tr.FinalResult, tr.TraceID),
			UserPrompt:   userPrompt,
			RoundCount:   roundCount,
			ModelName:    tr.ModelName,
			Turns:        max(turns, nullableInt(tr.TurnCount)),
			DurationMs:   nullableInt64(tr.DurationMs),
			LLMPureMs:    nullableInt64(tr.LLMDurationMs),
			ToolMs:       nullableInt64(tr.ToolDurationMs),
			InputTokens:  nullableInt64(tr.InputTokens),
			OutputTokens: nullableInt64(tr.OutputTokens),
			StartedAtMs:  nullableInt64(tr.StartedAtMs),
			StartedAt:    timeToString(tr.StartedAt),
			Status:       tr.Status,
			Spans:        spans,
		})
		totalDuration += nullableInt64(tr.DurationMs)
		totalIn += nullableInt64(tr.InputTokens)
		totalOut += nullableInt64(tr.OutputTokens)
		if i == 0 {
			firstTraceID = tr.TraceID
			title = pickFirstNonEmpty(tr.UserRequestPreview, tr.FinalResult, tr.TraceID)
			if nullableInt64(tr.StartedAtMs) > 0 {
				startedAtMs = nullableInt64(tr.StartedAtMs)
				startedAt = timeToString(tr.StartedAt)
			}
		}
	}

	if title == "" {
		title = "Session " + session.SessionID
	}

	features, rules := deriveSessionSignals(apiTraces, totalDuration, totalIn, totalOut)
	turns := 0
	for _, tr := range apiTraces {
		turns += tr.Turns
	}

	return apiSessionBundle{
		ID:           session.SessionID,
		SessionID:    session.SessionID,
		ArtifactID:   session.ArtifactID,
		User:         session.UserID,
		UserID:       session.UserID,
		Title:        title,
		Trace:        firstTraceID,
		StartedAtMs:  startedAtMs,
		StartedAt:    startedAt,
		DurationMs:   totalDuration,
		InputTokens:  totalIn,
		OutputTokens: totalOut,
		ToolCalls:    features.ToolCalls,
		Turns:        turns,
		TraceCount:   len(apiTraces),
		Score:        0,
		Color:        "green",
		Chip:         pickChip(rules),
		Features:     features,
		Radar:        apiRadar{},
		Rules:        rules,
		Traces:       apiTraces,
	}
}

func buildTraceSpans(rows []model.StgArtifactSpan) []apiSpan {
	out := make([]apiSpan, 0, len(rows))
	for _, sp := range rows {
		tags := map[string]string{}
		if sp.ModelName != "" {
			tags["model_name"] = sp.ModelName
		}
		if sp.InputTokens != nil {
			tags["input_tokens"] = strconv.FormatInt(*sp.InputTokens, 10)
		}
		if sp.OutputTokens != nil {
			tags["output_tokens"] = strconv.FormatInt(*sp.OutputTokens, 10)
		}
		customTags := "{}"
		if len(tags) > 0 {
			if buf, err := json.Marshal(tags); err == nil {
				customTags = string(buf)
			}
		}
		inputPreview := sp.InputPreview
		userPrompt := ""
		promptSource := ""
		roundIndex := 0
		if summary, ok := parseModelInputSummary(sp.InputPreview); ok {
			inputPreview = summary.InputPreview
			userPrompt = summary.UserPrompt
			promptSource = summary.PromptSource
			roundIndex = summary.RoundIndex
		}
		out = append(out, apiSpan{
			SpanID:       sp.SpanID,
			ParentID:     sp.ParentID,
			SpanName:     sp.SpanName,
			SpanType:     sp.SpanType,
			DurationMs:   nullableInt64(sp.DurationMs),
			StartedAtMs:  nullableInt64(sp.StartedAtMs),
			StartedAt:    timeToString(sp.StartedAt),
			StatusCode:   statusCodeFromStatus(sp.Status),
			Input:        inputPreview,
			Output:       sp.OutputPreview,
			CustomTags:   customTags,
			UserPrompt:   userPrompt,
			PromptSource: promptSource,
			RoundIndex:   roundIndex,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAtMs == out[j].StartedAtMs {
			return out[i].SpanID < out[j].SpanID
		}
		return out[i].StartedAtMs < out[j].StartedAtMs
	})
	return out
}

func deriveSessionSignals(traces []apiTrace, totalDuration, totalIn, totalOut int64) (apiFeatures, []apiRule) {
	allSpans := make([]apiSpan, 0)
	modelSpans := make([]apiSpan, 0)
	toolSpans := make([]apiSpan, 0)
	for _, tr := range traces {
		allSpans = append(allSpans, tr.Spans...)
		for _, sp := range tr.Spans {
			switch sp.SpanType {
			case "model":
				modelSpans = append(modelSpans, sp)
			case "tool":
				toolSpans = append(toolSpans, sp)
			}
		}
	}
	sort.Slice(modelSpans, func(i, j int) bool { return modelSpans[i].StartedAtMs < modelSpans[j].StartedAtMs })
	sort.Slice(toolSpans, func(i, j int) bool { return toolSpans[i].StartedAtMs < toolSpans[j].StartedAtMs })

	uniqueTools := map[string]struct{}{}
	toolKeyCount := map[string]int{}
	maxSerialRun := 0
	curSerialRun := 0
	prevToolKey := ""
	toolFailures := 0
	hasRootFail := false
	for _, tr := range traces {
		if tr.Status != "success" {
			hasRootFail = true
		}
	}
	for _, sp := range toolSpans {
		if sp.SpanName != "" {
			uniqueTools[sp.SpanName] = struct{}{}
		}
		key := sp.SpanName + "::" + sp.Input
		toolKeyCount[key]++
		if key == prevToolKey {
			curSerialRun++
		} else {
			curSerialRun = 1
		}
		prevToolKey = key
		if curSerialRun > maxSerialRun {
			maxSerialRun = curSerialRun
		}
		if sp.StatusCode != 0 {
			toolFailures++
		}
	}
	toolRetries := 0
	for _, cnt := range toolKeyCount {
		if cnt > 1 {
			toolRetries += cnt - 1
		}
	}

	hasFinalAnswer := false
	noOpStreak := 0
	curNoOp := 0
	for _, sp := range modelSpans {
		out := safeJSONMap(sp.Output)
		choices, _ := out["choices"].([]interface{})
		for _, rawChoice := range choices {
			choice, ok := rawChoice.(map[string]interface{})
			if !ok {
				continue
			}
			msg, _ := choice["message"].(map[string]interface{})
			content, _ := msg["content"].(string)
			toolCalls, _ := msg["tool_calls"].([]interface{})
			if strings.TrimSpace(content) != "" && len(toolCalls) == 0 {
				hasFinalAnswer = true
			}
			if len(toolCalls) == 0 && strings.TrimSpace(content) == "" {
				curNoOp++
				if curNoOp > noOpStreak {
					noOpStreak = curNoOp
				}
			} else {
				curNoOp = 0
			}
		}
	}

	turns := len(modelSpans)
	if turns == 0 {
		turns = max(1, len(traces))
	}
	totalTokens := totalIn + totalOut
	avgTokensPerTurn := int64(0)
	if turns > 0 {
		avgTokensPerTurn = totalTokens / int64(turns)
	}
	toolFailRate := 0.0
	if len(toolSpans) > 0 {
		toolFailRate = float64(toolFailures) / float64(len(toolSpans))
	}

	features := apiFeatures{
		ToolCalls:        len(toolSpans),
		UniqueTools:      len(uniqueTools),
		MaxSerialRun:     maxSerialRun,
		ToolFailures:     toolFailures,
		ToolFailRate:     toolFailRate,
		AvgTokensPerTurn: avgTokensPerTurn,
		ToolRetries:      toolRetries,
		HasRootFail:      hasRootFail,
		HasLoop:          maxSerialRun >= 3,
	}

	responseBucketFull, responseBucketFloor := responseBucket(len(toolSpans))
	durSec := int(totalDuration / 1000)
	rules := []apiRule{
		{
			Name:        "执行效率健康",
			Passed:      hasFinalAnswer && noOpStreak < 3,
			FailedLabel: ternaryLabel(!(hasFinalAnswer && noOpStreak < 3), "轨迹异常"),
			Detail:      efficiencyDetail(turns, hasFinalAnswer, noOpStreak),
		},
		{
			Name:        "响应耗时合理",
			Passed:      durSec <= responseBucketFloor,
			FailedLabel: ternaryLabel(durSec > responseBucketFloor, "关键路径过长"),
			Detail:      fmt.Sprintf("%d 秒，按当前复杂度阈值 %ds/%ds 评估", durSec, responseBucketFull, responseBucketFloor),
		},
		{
			Name:        "工具稳定性",
			Passed:      toolFailures == 0,
			FailedLabel: ternaryLabel(toolFailures > 0, "工具失败"),
			Detail:      fmt.Sprintf("工具失败 %d 次，失败率 %.0f%%", toolFailures, toolFailRate*100),
		},
		{
			Name:        "资源使用健康",
			Passed:      avgTokensPerTurn <= 76000,
			FailedLabel: ternaryLabel(avgTokensPerTurn > 76000, "长上下文超限"),
			Detail:      fmt.Sprintf("单轮平均 %dk Token", avgTokensPerTurn/1000),
		},
		{
			Name:        "工具编排健康",
			Passed:      maxSerialRun < 3,
			FailedLabel: ternaryLabel(maxSerialRun >= 3, "行为死循环"),
			Detail:      fmt.Sprintf("同名工具最长连续 %d 次，重复调用 %d 次", maxSerialRun, toolRetries),
		},
	}
	return features, rules
}

func countModelSpans(spans []apiSpan) int {
	n := 0
	for _, sp := range spans {
		if sp.SpanType == "model" {
			n++
		}
	}
	return n
}

func countRounds(spans []apiSpan) int {
	maxRound := 0
	for _, sp := range spans {
		if sp.RoundIndex > maxRound {
			maxRound = sp.RoundIndex
		}
	}
	return maxRound
}

func bundlePagination(c *gin.Context) (limit, offset int) {
	limit = 1000
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 2000 {
		limit = v
	}
	if v, err := strconv.Atoi(c.Query("offset")); err == nil && v >= 0 {
		offset = v
	}
	return
}

func pagination(c *gin.Context) (limit, offset int) {
	limit = 50
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	if v, err := strconv.Atoi(c.Query("offset")); err == nil && v >= 0 {
		offset = v
	}
	return
}

func nullableInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func nullableInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func timeToString(v *time.Time) string {
	if v == nil {
		return ""
	}
	return v.Format(time.RFC3339)
}

func statusCodeFromStatus(status string) int {
	if status == "" || strings.EqualFold(status, "success") || status == "ok" {
		return 0
	}
	return 2
}

func safeJSONMap(s string) map[string]interface{} {
	if strings.TrimSpace(s) == "" {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func extractTraceUserPrompt(tracePreview string, spans []apiSpan) string {
	for _, sp := range spans {
		if sp.SpanType != "model" {
			continue
		}
		if prompt := cleanUserPrompt(sp.UserPrompt); prompt != "" {
			return prompt
		}
		if prompt := extractUserPromptFromInput(sp.Input); prompt != "" {
			return prompt
		}
	}
	if prompt := cleanUserPrompt(tracePreview); prompt != "" {
		return prompt
	}
	return ""
}

type modelInputSummary struct {
	Kind         string `json:"kind"`
	UserPrompt   string `json:"user_prompt"`
	PromptSource string `json:"prompt_source"`
	RoundIndex   int    `json:"round_index"`
	InputPreview string `json:"input_preview"`
}

func parseModelInputSummary(input string) (modelInputSummary, bool) {
	var summary modelInputSummary
	if strings.TrimSpace(input) == "" {
		return summary, false
	}
	if err := json.Unmarshal([]byte(input), &summary); err != nil {
		return summary, false
	}
	if summary.Kind != "model_input_summary" {
		return summary, false
	}
	return summary, true
}

func extractUserPromptFromInput(input string) string {
	inp := safeJSONMap(input)
	rawMsgs, ok := inp["messages"].([]interface{})
	if !ok || len(rawMsgs) == 0 {
		return ""
	}
	fallback := ""
	for i := len(rawMsgs) - 1; i >= 0; i-- {
		msg, ok := rawMsgs[i].(map[string]interface{})
		if !ok || !strings.EqualFold(fmt.Sprint(msg["role"]), "user") {
			continue
		}
		content := messageContentToText(msg["content"])
		if content == "" {
			continue
		}
		if prompt := cleanUserPrompt(content); prompt != "" {
			return prompt
		}
		if fallback == "" {
			fallback = normalizePromptText(content)
		}
	}
	return fallback
}

func messageContentToText(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return normalizePromptText(v)
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch p := item.(type) {
			case string:
				if t := normalizePromptText(p); t != "" {
					parts = append(parts, t)
				}
			case map[string]interface{}:
				if text, ok := p["text"].(string); ok {
					if t := normalizePromptText(text); t != "" {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func cleanUserPrompt(raw string) string {
	prompt := normalizePromptText(raw)
	if prompt == "" || isControlLikePrompt(prompt) {
		return ""
	}
	return prompt
}

func normalizePromptText(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	return strings.TrimSpace(strings.Join(fields, " "))
}

func isControlLikePrompt(raw string) bool {
	if raw == "" {
		return false
	}
	compact := strings.ToLower(raw)
	replacer := strings.NewReplacer(
		" ", "",
		"\n", "",
		"\t", "",
		"(", "",
		")", "",
		"（", "",
		"）", "",
		"[", "",
		"]", "",
		"【", "",
		"】", "",
		"<", "",
		">", "",
		"“", "",
		"”", "",
		"\"", "",
		"'", "",
		"。", "",
		".", "",
		"，", "",
		",", "",
		":", "",
		"：", "",
		"!", "",
		"！", "",
		"?", "",
		"？", "",
	)
	compact = replacer.Replace(compact)
	switch compact {
	case "继续执行", "请继续执行", "继续", "continue", "pleasecontinue", "resume", "resumeexecution", "goon", "proceed":
		return true
	default:
		return false
	}
}

func responseBucket(toolCalls int) (full, floor int) {
	switch {
	case toolCalls < 3:
		return 30, 90
	case toolCalls < 10:
		return 90, 240
	default:
		return 240, 600
	}
}

func efficiencyDetail(turns int, hasFinalAnswer bool, noOpStreak int) string {
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

func pickFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func pickChip(rules []apiRule) string {
	for _, r := range rules {
		if !r.Passed && r.FailedLabel != "" {
			return r.FailedLabel
		}
	}
	return "健康"
}

func ternaryLabel(cond bool, label string) string {
	if cond {
		return label
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fail(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
