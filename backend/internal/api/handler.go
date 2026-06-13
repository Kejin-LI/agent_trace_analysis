package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/tracelog"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/ark"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

// Handler 持有数据库连接 / 上游客户端，提供读库 API。
//
// 三种数据源模式：
//   - fornax：旧表 stg_artifact_sessions/traces/spans（默认，依赖 db）
//   - tos：   新表 stg_session_sources + 实时拉 obj_url JSONL（依赖 db + fetcher）
//   - api：   完全不落库，直接调上游模型日志接口 + 实时拉 JSONL（依赖 upstream + fetcher）
type Handler struct {
	db                  *gorm.DB
	fetcher             *tracelog.Fetcher
	upstream            *modellog.Client
	ark                 *ark.Client
	aggregator          *Aggregator
	dbOpenError         string
	aggregatorInitError string
}

// New 构造依赖 DB 的 Handler（fornax / tos 模式）。
func New(db *gorm.DB) *Handler {
	h := &Handler{db: db, fetcher: tracelog.NewFetcher(), ark: ark.NewClient()}
	// TOS 模式下后台批量预热：保证列表页首次加载就有 chip / rules / 雷达数据。
	// 启动跑一次，之后周期性重跑，覆盖启动后新导入但尚未聚合指标的 session，
	// 避免大盘长期出现「指标待分析」的空骨架（异常数被低估）。
	if dataSourceMode() == "tos" {
		go h.backfillMetricsLoop()
	}
	return h
}

// NewAPI 构造 api 模式 Handler。
// DB 为可选依赖：存在时启用 DB-backed 聚合缓存；不存在时仅保留实时上游读取。
func NewAPI(gdb *gorm.DB, dbOpenErr error) (*Handler, error) {
	cli, err := modellog.NewClient()
	if err != nil {
		return nil, err
	}
	fetcher := tracelog.NewFetcher()
	h := &Handler{
		db:       gdb,
		fetcher:  fetcher,
		upstream: cli,
		ark:      ark.NewClient(),
	}
	if dbOpenErr != nil {
		h.dbOpenError = dbOpenErr.Error()
	}
	if gdb != nil {
		agg, err := NewAggregator(gdb, cli, fetcher)
		if err != nil {
			h.aggregatorInitError = err.Error()
			log.Printf("api 模式 DB 聚合器初始化失败，已降级为纯实时读取: %v", err)
		} else {
			h.aggregator = agg
		}
	}
	return h, nil
}

// backfillMetricsLoop 启动后立即跑一次全量补齐，之后每 30 分钟重跑一次，
// 覆盖运行期间新导入、尚未聚合 cached_metrics 的 session。
// backfillAllMetrics 内部已对「已有 chip 的」跳过，因此重跑只处理增量，开销可控。
func (h *Handler) backfillMetricsLoop() {
	h.backfillAllMetrics()
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		h.backfillAllMetrics()
	}
}

// backfillAllMetrics 扫描所有 jsonl session，缺 cached_metrics 的拉取并回写。
// 限流：同时最多 4 个并发拉 TOS，避免打爆带宽。
func (h *Handler) backfillAllMetrics() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("backfillAllMetrics panic: %v\n", r)
		}
	}()
	var rows []model.StgSessionSource
	if err := h.db.Where("obj_format = ?", "jsonl").Find(&rows).Error; err != nil {
		fmt.Printf("backfill: scan failed: %v\n", err)
		return
	}
	fmt.Printf("backfill: scanning %d sessions\n", len(rows))
	sem := make(chan struct{}, 4)
	var done int64
	for _, src := range rows {
		// 已有 cached_metrics 且 chip 字段非空的跳过（避免老缓存只有 features 没 chip 的情况）
		if m, ok := readCachedMetrics(src.Extra); ok && len(m.Rules) > 0 {
			continue
		}
		if src.ObjURL == "" {
			continue
		}
		sem <- struct{}{}
		go func(src model.StgSessionSource) {
			defer func() {
				<-sem
				if r := recover(); r != nil {
					fmt.Printf("backfill one panic: %v\n", r)
				}
			}()
			pr, err := h.fetcher.FetchAndParse(src.ObjURL)
			if err != nil {
				return
			}
			bundle := buildBundleFromTOS(src, pr)
			h.writeBackMetrics(src, bundle)
			done++
			if done%50 == 0 {
				fmt.Printf("backfill: progress %d/%d\n", done, len(rows))
			}
		}(src)
	}
	// 等待最后一批完成
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
	fmt.Printf("backfill: done\n")
}

// dataSourceMode 决定列表/详情接口走哪种数据源。
//
// 取值：
//   - "api"    走上游模型日志接口，不落库（推荐，PG 同步停在 6/2 后启用）
//   - "tos"    走新表 stg_session_sources + 实时拉 obj_url JSONL
//   - "fornax" 走旧表 stg_artifact_sessions/traces/spans（默认，便于灰度回退）
//
// 由 DATA_SOURCE 环境变量控制；未设置时回退到 fornax。
func dataSourceMode() string {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("DATA_SOURCE"))); v {
	case "api":
		return "api"
	case "tos":
		return "tos"
	default:
		return "fornax"
	}
}

// Register 将读库 API 挂载到 /api 与 /trace_sever/api 两个前缀下。
//
// 双前缀的原因：
//   - 本地开发 / 历史前端代码：/api/...（保持兼容）
//   - 生产网关前缀匹配：/trace_sever/...（注意网关配置拼写就是 trace_sever，少一个 r）
//
// 网关层不做 path rewrite 时，请求会原样带前缀打到我们 Pod，所以两路都注册。
// api 模式仅注册 session-bundles 两个端点（其余端点依赖 DB，没意义）。
func (h *Handler) Register(r *gin.Engine) {
	for _, prefix := range []string{"/api", "/trace_sever/api"} {
		g := r.Group(prefix)
		// 任意 API 请求只要带 Cookie 就刷新缓存，使凌晨 cron 不再依赖
		// 用户恰好访问过某个特定接口（仅内存，绝不落盘）。
		if h.aggregator != nil {
			g.Use(func(c *gin.Context) {
				h.aggregator.RememberCookie(c.GetHeader("Cookie"))
				c.Next()
			})
		}
		g.GET("/session-bundles", h.listSessionBundles)
		g.GET("/session-bundles/:session_id", h.getSessionBundle)
		g.GET("/aggregate-status", h.listAggregateStatus)
		g.GET("/self-check", h.selfCheck)
		g.POST("/backfill-day", h.backfillDay)
		g.POST("/ai-diagnose", h.diagnose)
		if dataSourceMode() == "api" {
			continue
		}
		g.GET("/sessions", h.listSessions)
		g.GET("/sessions/:session_id/traces", h.listTraces)
		g.GET("/traces/:trace_id", h.getTrace)
		g.GET("/traces/:trace_id/spans", h.listSpans)
		g.GET("/sync-jobs", h.listSyncJobs)
	}
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
	ID           string               `json:"id"`
	SessionID    string               `json:"session_id"`
	ArtifactID   string               `json:"artifact_id"`
	User         string               `json:"user"`
	UserID       string               `json:"user_id"`
	Title        string               `json:"title"`
	Trace        string               `json:"trace"`
	StartedAtMs  int64                `json:"started_at_ms"`
	StartedAt    string               `json:"started_at,omitempty"`
	DurationMs   int64                `json:"duration_ms"`
	InputTokens  int64                `json:"input_tokens"`
	OutputTokens int64                `json:"output_tokens"`
	ToolCalls    int                  `json:"tool_calls"`
	Turns        int                  `json:"turns"`
	TraceCount   int                  `json:"trace_count"`
	Score        int                  `json:"score"`
	Color        string               `json:"color"`
	Chip         string               `json:"chip"`
	Features     apiFeatures          `json:"features"`
	Radar        apiRadar             `json:"radar"`
	Rules        []apiRule            `json:"rules"`
	Truncation   *apiTruncationNotice `json:"truncation,omitempty"`
	Traces       []apiTrace           `json:"traces"`
}

type apiTruncationNotice struct {
	Truncated     bool    `json:"truncated"`
	Reason        string  `json:"reason,omitempty"`
	LimitBytes    int64   `json:"limit_bytes,omitempty"`
	RetainedBytes int64   `json:"retained_bytes,omitempty"`
	MemoryPct     float64 `json:"memory_pct,omitempty"`
	Message       string  `json:"message,omitempty"`
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
	switch dataSourceMode() {
	case "api":
		h.listSessionBundlesAPI(c)
		return
	case "tos":
		h.listSessionBundlesTOS(c)
		return
	}
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
	switch dataSourceMode() {
	case "api":
		h.getSessionBundleAPI(c)
		return
	case "tos":
		h.getSessionBundleTOS(c)
		return
	}
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

// listSessionBundlesTOS 走新表 stg_session_sources。
//
// 列表页只展示元信息（user/时间/对话轮次概览），不实时拉 JSONL。
// 默认按 source_updated_at DESC 排序，分页 50 条。
func (h *Handler) listSessionBundlesTOS(c *gin.Context) {
	limit, offset := bundlePaginationDefault(c, 50)
	var rows []model.StgSessionSource
	q := h.db.Where("obj_format = ?", "jsonl").
		Order("source_updated_at DESC, id DESC").
		Limit(limit).Offset(offset)
	if uid := c.Query("user_id"); uid != "" {
		q = q.Where("user_id = ?", uid)
	}
	if name := c.Query("user_name"); name != "" {
		q = q.Where("user_name = ?", name)
	}
	if sid := c.Query("session_id"); sid != "" {
		q = q.Where("session_id = ?", sid)
	}
	if aid := c.Query("artifact_id"); aid != "" {
		q = q.Where("artifact_id = ?", aid)
	}
	if err := q.Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}
	bundles := make([]apiSessionBundle, 0, len(rows))
	prewarmURLs := make([]string, 0, 10)
	for i, r := range rows {
		bundles = append(bundles, lightBundleFromSource(r))
		// 异步预热前 10 条，提升用户点击详情页首屏速度。
		if i < 10 && r.ObjURL != "" {
			prewarmURLs = append(prewarmURLs, r.ObjURL)
		}
	}
	if len(prewarmURLs) > 0 {
		h.fetcher.Prewarm(prewarmURLs)
	}
	c.JSON(http.StatusOK, gin.H{"data": bundles, "limit": limit, "offset": offset})
}

// getSessionBundleTOS 详情页：实时拉 obj_url JSONL，解析后返回完整 bundle。
//
// 路由参数 :session_id 实际上接受 session_id 或 artifact_id（前端列表传哪个都兼容）。
func (h *Handler) getSessionBundleTOS(c *gin.Context) {
	key := c.Param("session_id")
	var src model.StgSessionSource
	q := h.db.Where("session_id = ? OR artifact_id = ?", key, key)
	if err := q.First(&src).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		fail(c, err)
		return
	}
	if src.ObjFormat != "jsonl" || src.ObjURL == "" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{
			"error":       "obj_format not supported",
			"obj_format":  src.ObjFormat,
			"session_id":  src.SessionID,
			"artifact_id": src.ArtifactID,
		})
		return
	}
	pr, err := h.fetcher.FetchAndParse(src.ObjURL)
	if err != nil {
		fail(c, fmt.Errorf("fetch jsonl: %w", err))
		return
	}
	bundle := buildBundleFromTOS(src, pr)
	c.JSON(http.StatusOK, bundle)

	// 异步回写指标缓存到 extra.cached_metrics，列表页雷达将能用上。
	go h.writeBackMetrics(src, bundle)
}

// writeBackMetrics 异步把详情页算好的指标写回 stg_session_sources.extra。
// 失败仅打日志，不影响主流程；同一指标 5 分钟内不重复写。
func (h *Handler) writeBackMetrics(src model.StgSessionSource, b apiSessionBundle) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("writeBackMetrics panic: %v\n", r)
		}
	}()
	if old, ok := readCachedMetrics(src.Extra); ok {
		if time.Since(time.Unix(old.UpdatedAt, 0)) < 5*time.Minute {
			return
		}
	}
	m := extractCachedMetrics(b)
	newExtra, err := mergeCachedMetricsIntoExtra(src.Extra, m)
	if err != nil {
		return
	}
	h.db.Model(&model.StgSessionSource{}).
		Where("id = ?", src.ID).
		Update("extra", newExtra)
}

// lightBundleFromSource 列表项不实时拉 JSONL，只用索引表本身的字段拼概览。
// 若详情页访问过，extra.cached_metrics 已被回写，则填充 features/turns 等让雷达可算。
func lightBundleFromSource(src model.StgSessionSource) apiSessionBundle {
	startedMs := int64(0)
	if src.SourceCreatedAt != nil {
		startedMs = src.SourceCreatedAt.UnixMilli()
	}
	endedMs := int64(0)
	if src.SourceUpdatedAt != nil {
		endedMs = src.SourceUpdatedAt.UnixMilli()
	}
	dur := endedMs - startedMs
	if dur < 0 {
		dur = 0
	}
	id := pickFirstNonEmpty(src.SessionID, src.ArtifactID)
	title := "Session " + id

	bundle := apiSessionBundle{
		ID:          id,
		SessionID:   src.SessionID,
		ArtifactID:  src.ArtifactID,
		User:        src.UserName,
		UserID:      src.UserID,
		Title:       title,
		Trace:       "",
		StartedAtMs: startedMs,
		StartedAt:   msToString(startedMs),
		DurationMs:  dur,
		TraceCount:  0,
		Color:       "green",
		Traces:      []apiTrace{},
		Rules:       []apiRule{},
	}

	// 若有缓存指标（来自详情页一次访问），把雷达计算所需字段填上。
	if m, ok := readCachedMetrics(src.Extra); ok {
		bundle.ToolCalls = m.ToolCalls
		bundle.Turns = m.Turns
		bundle.TraceCount = m.TraceCount
		bundle.InputTokens = m.InputTokens
		bundle.OutputTokens = m.OutputTokens
		if m.DurationMs > 0 {
			bundle.DurationMs = m.DurationMs
		}
		bundle.Features = apiFeatures{
			ToolCalls:        m.ToolCalls,
			UniqueTools:      m.UniqueTools,
			MaxSerialRun:     m.MaxSerialRun,
			ToolFailures:     m.ToolFailures,
			ToolFailRate:     m.ToolFailRate,
			AvgTokensPerTurn: m.AvgTokensTurn,
		}
		// 异常标签 & 规则（驱动列表页"异常"列）。
		if m.Chip != "" {
			bundle.Chip = m.Chip
		}
		if len(m.Rules) > 0 {
			bundle.Rules = m.Rules
		}
		if m.Title != "" {
			bundle.Title = m.Title
		}
		if m.Trace != "" {
			bundle.Trace = m.Trace
		}
	}
	return bundle
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

// extractRawLastUserContent 取 model span input.messages 中最后一条 user 消息的原文（未剥注入块）。
// 用于「合成 prompt 识别」：指标计算时若该 prompt 是框架内部消息（工具回填 / 转义自检 / 续接 / 重试），
// 应当从轮次/空转/耗时分母中剔除，避免污染效率指标。
func extractRawLastUserContent(input string) string {
	inp := safeJSONMap(input)
	rawMsgs, ok := inp["messages"].([]interface{})
	if !ok || len(rawMsgs) == 0 {
		return ""
	}
	for i := len(rawMsgs) - 1; i >= 0; i-- {
		msg, ok := rawMsgs[i].(map[string]interface{})
		if !ok || !strings.EqualFold(fmt.Sprint(msg["role"]), "user") {
			continue
		}
		if c := messageContentToText(msg["content"]); c != "" {
			return c
		}
	}
	return ""
}

// isSyntheticModelSpan 判断该 model span 的最近一条 user 消息是否为框架内部合成 prompt。
// 命中后该 span 不应计入轮次/空转等效率指标分母。
func isSyntheticModelSpan(sp apiSpan) bool {
	// 优先看已抽取出的 prompt 摘要（importer 阶段写入的 model_input_summary）。
	// summary.UserPrompt 经 cleanUserPrompt 清洗，若为空通常意味着该消息是控制词或合成 prompt；
	// 这种情况下需要回到原始 input 复核 synthetic 签名以避免误判。
	if summary, ok := parseModelInputSummary(sp.Input); ok {
		if summary.UserPrompt != "" {
			return false
		}
	}
	raw := extractRawLastUserContent(sp.Input)
	if raw == "" {
		return false
	}
	return isSyntheticToolPrompt(stripInjectedContext(raw))
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
	// 过滤掉「框架内部 prompt」对应的 model spans：这些 span 不是真实用户轮次的产物，
	// 不应计入轮次、空转、单轮 Token 等效率指标分母，否则会出现「连续 N 步空转 / 严重偏慢」
	// 之类的指标污染（实际上用户只发了 1 条真消息）。
	realModelSpans := make([]apiSpan, 0, len(modelSpans))
	for _, sp := range modelSpans {
		if isSyntheticModelSpan(sp) {
			continue
		}
		realModelSpans = append(realModelSpans, sp)
	}
	for _, sp := range realModelSpans {
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

	turns := len(realModelSpans)
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
	return bundlePaginationDefault(c, 1000)
}

// bundlePaginationDefault 与 bundlePagination 一致，但允许指定默认 limit。
// 用于 TOS 模式列表页：默认 50 条更适合按更新时间倒序浏览。
func bundlePaginationDefault(c *gin.Context, defaultLimit int) (limit, offset int) {
	limit = defaultLimit
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 2000 {
		limit = v
	}
	if v, err := strconv.Atoi(c.Query("offset")); err == nil && v >= 0 {
		offset = v
	}
	return
}

// isTruthy 把常见的真值字符串（1/true/yes/on）判为 true，用于解析 query 开关。
func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
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
	stripped := stripInjectedContext(raw)
	prompt := normalizePromptText(stripped)
	if prompt == "" || isControlLikePrompt(prompt) || isSyntheticToolPrompt(stripped) {
		return ""
	}
	return prompt
}

// injectedContextRe 匹配框架注入的上下文包裹块（含跨行内容）。
var injectedContextRe = regexp.MustCompile(`(?is)<(system-reminder|project-memory|related-conversations)>.*?</(system-reminder|project-memory|related-conversations)>`)

// injectedCloseTagRe 匹配注入块的闭合标签（兼容单复数），用作切分锚点。
var injectedCloseTagRe = regexp.MustCompile(`(?i)</(system-reminders?|project-memory|related-conversations?)>`)

// stripInjectedContext 剥离框架注入的上下文包裹块，保留其后真实用户输入。
// 新版 Agent 框架会把 <system-reminder> 等注入块拼在真实提问之前塞进同一条 user 消息，
// 不剥离会导致 prompt 显示成系统注入、多轮塌缩。
// 优先按"最后一个闭合标签"锚点切分：闭合标签（含）之前整体视为注入上下文，之后才是真实用户输入。
// 这样即使上游只截到残缺标签（如缺开标签、只剩 </system-reminder>）也能正确分离。
func stripInjectedContext(raw string) string {
	if locs := injectedCloseTagRe.FindAllStringIndex(raw, -1); len(locs) > 0 {
		return strings.TrimSpace(raw[locs[len(locs)-1][1]:])
	}
	return strings.TrimSpace(injectedContextRe.ReplaceAllString(raw, ""))
}

// isSyntheticToolPrompt 识别工具回填的"合成 user 消息"（如 web_fetch / web_search
// 执行后框架以 user 角色回填的结果），这类不是真实用户提问，提取 prompt 时必须排除。
//
// 同时识别四类「框架内部 prompt」（同样以 role:user 通道发送）：
//  1. Edit 转义自检（Context: A text replacement operation is planned ... new_string ...）
//  2. 长上下文压缩 / 跨 Agent 续接（Provide a detailed prompt for continuing ...）
//  3. Edit 工具失败后的自我修复（# Goal of the Original Edit / # Failed Attempt Details ...）
//  4. 子代理任务下发（Your task is to do a deep investigation ... <objective>...</objective>）
//  5. 工具中断后的收尾控制（You have stopped calling tools without finishing ... complete_task ...）
//
// 这些消息会污染轮次/空转/耗时等指标，必须在指标计算时识别并剔除。
func isSyntheticToolPrompt(raw string) bool {
	lower := strings.ToLower(raw)
	for _, sig := range []string{
		"the user requested the following",
		"i have fetched the raw content",
		"i was unable to access the url",
		"<tool_call_result>",
		"<function_results>",
		"<system-reminder>",
		// 编辑转义自检
		"context: a text replacement operation is planned",
		"potentially_problematic_new_string",
		// 长上下文续接 / 压缩
		"provide a detailed prompt for continuing our conversation above",
		// 编辑失败重试自修复
		"# goal of the original edit",
		"# failed attempt details",
	} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	// 重试 prompt 的强组合特征：search/replace 双标签 + 完整文件内容
	if strings.Contains(lower, "<search>") && strings.Contains(lower, "</search>") &&
		strings.Contains(lower, "<replace>") && strings.Contains(lower, "</replace>") &&
		strings.Contains(lower, "# full file content") {
		return true
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
	return false
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
