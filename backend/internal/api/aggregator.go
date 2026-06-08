package api

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/tracelog"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

// Aggregator 按"自然日"维度预聚合 session 指标，结果存内存 + 磁盘。
//
// 触发方式：列表接口收到请求时，用当前请求的 Cookie 异步触发缺失日期的聚合，
// 不需要长期保存 SSO Cookie。同一日期同时只跑一次（去重）。
//
// 持久化：默认 /data/aggregates/<YYYY-MM-DD>.json；目录由 AGGREGATE_DIR 覆盖。
// 没有持久卷时也能跑（每次重启重爬即可）。
type Aggregator struct {
	upstream *modellog.Client
	fetcher  *tracelog.Fetcher
	dir      string

	mu     sync.RWMutex
	perDay map[string]map[string]cachedMetrics // date -> session_id -> metrics

	flightMu sync.Mutex
	flight   map[string]bool // date -> aggregating
}

// NewAggregator 构造聚合器并加载磁盘上已有的日聚合文件。
func NewAggregator(client *modellog.Client, fetcher *tracelog.Fetcher) *Aggregator {
	dir := strings.TrimSpace(os.Getenv("AGGREGATE_DIR"))
	if dir == "" {
		dir = "/data/aggregates"
	}
	a := &Aggregator{
		upstream: client,
		fetcher:  fetcher,
		dir:      dir,
		perDay:   make(map[string]map[string]cachedMetrics),
		flight:   make(map[string]bool),
	}
	a.loadAll()
	return a
}

// Get 查 session 在所有已聚合日期里的指标，命中返回。
func (a *Aggregator) Get(sessionID string) (cachedMetrics, bool) {
	if sessionID == "" {
		return cachedMetrics{}, false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, dayMap := range a.perDay {
		if m, ok := dayMap[sessionID]; ok {
			return m, true
		}
	}
	return cachedMetrics{}, false
}

// EnsureDays 异步保证 dates 列表里的每一天都聚合过；已有缓存或正在跑的日期跳过。
// cookie 用于本次触发的上游调用，仅在内存中传递，不持久化。
func (a *Aggregator) EnsureDays(cookie string, dates []string) {
	for _, d := range dates {
		a.ensureOne(cookie, d)
	}
}

func (a *Aggregator) ensureOne(cookie, date string) {
	a.mu.RLock()
	if m, ok := a.perDay[date]; ok && len(m) > 0 {
		a.mu.RUnlock()
		return
	}
	a.mu.RUnlock()

	a.flightMu.Lock()
	if a.flight[date] {
		a.flightMu.Unlock()
		return
	}
	a.flight[date] = true
	a.flightMu.Unlock()

	go a.runAggregate(cookie, date)
}

// runAggregate 拉指定日期所有 session list → 并发拉 TOS JSONL → 解析 → 写缓存 + 落盘。
func (a *Aggregator) runAggregate(cookie, date string) {
	startedAt := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("aggregator: %s panic: %v", date, r)
		}
		a.flightMu.Lock()
		delete(a.flight, date)
		a.flightMu.Unlock()
	}()

	tr := modellog.TimeRange{
		StartTime: date + " 00:00:00",
		EndTime:   date + " 23:59:59",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	resp, err := a.upstream.List(ctx, cookie, modellog.ListRequest{
		TimeRange: tr,
		Page:      modellog.Page{PageNo: 1, PageSize: 2000},
	})
	if err != nil {
		log.Printf("aggregator: %s list failed: %v", date, err)
		return
	}
	log.Printf("aggregator: %s list ok, total=%d", date, len(resp.Data))

	results := make(map[string]cachedMetrics, len(resp.Data))
	var resultMu sync.Mutex
	sem := make(chan struct{}, 6) // 最多 6 个并发拉 TOS
	var wg sync.WaitGroup

	for i := range resp.Data {
		s := resp.Data[i]
		if len(s.FileList) == 0 || s.FileList[0].URL == "" || s.SessionID == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(s modellog.Session) {
			defer wg.Done()
			defer func() {
				<-sem
				if r := recover(); r != nil {
					log.Printf("aggregator: %s session %s panic: %v", date, s.SessionID, r)
				}
			}()
			pr, err := a.fetcher.FetchAndParse(s.FileList[0].URL)
			if err != nil {
				return
			}
			src := sessionToStgSource(s)
			bundle := buildBundleFromTOS(src, pr)
			m := extractCachedMetrics(bundle)
			// 兜底起始时间：bundle 没算出来时，用 session_id 时间戳。
			if m.DurationMs == 0 && bundle.StartedAtMs == 0 {
				if ts := parseSessionIDTimestamp(s.SessionID); ts > 0 {
					bundle.StartedAtMs = ts
				}
			}
			resultMu.Lock()
			results[s.SessionID] = m
			resultMu.Unlock()
		}(s)
	}
	wg.Wait()

	a.mu.Lock()
	a.perDay[date] = results
	a.mu.Unlock()
	a.saveDate(date, results)

	log.Printf("aggregator: %s done, sessions=%d, took=%s", date, len(results), time.Since(startedAt))
}

// saveDate 写一份当天的日聚合 JSON 到磁盘（先 .tmp 再 rename）。
func (a *Aggregator) saveDate(date string, sessions map[string]cachedMetrics) {
	if a.dir == "" {
		return
	}
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		log.Printf("aggregator: mkdir %s failed (no persistent volume?): %v", a.dir, err)
		return
	}
	payload := map[string]interface{}{
		"date":          date,
		"generated_at":  time.Now().Format(time.RFC3339),
		"session_count": len(sessions),
		"sessions":      sessions,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return
	}
	tmp := filepath.Join(a.dir, date+".json.tmp")
	final := filepath.Join(a.dir, date+".json")
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		log.Printf("aggregator: write %s failed: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		log.Printf("aggregator: rename %s -> %s failed: %v", tmp, final, err)
		return
	}
}

// loadAll 启动时把磁盘上所有日聚合 JSON 读进内存。
func (a *Aggregator) loadAll() {
	if a.dir == "" {
		return
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		log.Printf("aggregator: read dir %s skip: %v", a.dir, err)
		return
	}
	loaded := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp.json") {
			continue
		}
		date := strings.TrimSuffix(name, ".json")
		buf, err := os.ReadFile(filepath.Join(a.dir, name))
		if err != nil {
			continue
		}
		var p struct {
			Sessions map[string]cachedMetrics `json:"sessions"`
		}
		if err := json.Unmarshal(buf, &p); err != nil {
			continue
		}
		a.perDay[date] = p.Sessions
		loaded++
	}
	if loaded > 0 {
		log.Printf("aggregator: loaded %d day files from %s", loaded, a.dir)
	}
}

// LastNDays 返回最近 n 天（含今天）的日期列表，格式 "YYYY-MM-DD"。
func LastNDays(n int) []string {
	out := make([]string, 0, n)
	now := time.Now()
	for i := 0; i < n; i++ {
		out = append(out, now.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	return out
}

// parseSessionIDTimestamp 尝试从 "YYYYMMDD_HHMMSS_*" 形式的 session_id 解析时间戳。
// 不匹配返回 0。OpenCode 的 "ses_*" 格式没有时间信息，靠后续聚合补齐。
func parseSessionIDTimestamp(sid string) int64 {
	if len(sid) < 16 {
		return 0
	}
	if sid[8] != '_' || sid[15] != '_' {
		return 0
	}
	t, err := time.ParseInLocation("20060102_150405", sid[:15], time.Local)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// applyCachedMetricsToBundle 把日聚合的指标 join 到 bundle 上，覆盖空字段。
// 起始时间不从缓存恢复（缓存里没存），由 lightBundleFromAPI 优先从 session_id 推算。
func applyCachedMetricsToBundle(b apiSessionBundle, m cachedMetrics) apiSessionBundle {
	if m.DurationMs > 0 {
		b.DurationMs = m.DurationMs
	}
	b.InputTokens = m.InputTokens
	b.OutputTokens = m.OutputTokens
	b.ToolCalls = m.ToolCalls
	b.Turns = m.Turns
	b.TraceCount = m.TraceCount
	b.Features = apiFeatures{
		ToolCalls:        m.ToolCalls,
		UniqueTools:      m.UniqueTools,
		MaxSerialRun:     m.MaxSerialRun,
		ToolFailures:     m.ToolFailures,
		ToolFailRate:     m.ToolFailRate,
		AvgTokensPerTurn: m.AvgTokensTurn,
	}
	if m.Chip != "" {
		b.Chip = m.Chip
	}
	if len(m.Rules) > 0 {
		b.Rules = m.Rules
	}
	if m.Title != "" {
		b.Title = m.Title
	}
	if m.Trace != "" {
		b.Trace = m.Trace
	}
	return b
}

// rangeDays 返回 [start, end] 闭区间内涉及的所有自然日（按 Local 时区）。
func rangeDays(startMs, endMs int64) []string {
	if startMs <= 0 || endMs < startMs {
		return nil
	}
	cursor := time.UnixMilli(startMs).Local()
	endCursor := time.UnixMilli(endMs).Local()
	out := make([]string, 0, 8)
	for d := cursor; !d.After(endCursor); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
		if len(out) > 31 { // 防呆，最多 31 天
			break
		}
	}
	return out
}
