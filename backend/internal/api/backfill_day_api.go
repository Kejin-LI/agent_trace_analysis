package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

type backfillDayResponse struct {
	Date               string                 `json:"date"`
	Started            bool                   `json:"started"`
	Message            string                 `json:"message"`
	UsedTempRunner     bool                   `json:"used_temp_runner"`
	AggregatorEnabled  bool                   `json:"aggregator_enabled"`
	SessionCountBefore int64                  `json:"session_count_before"`
	DayStatus          *apiAggregateStatusRow `json:"day_status,omitempty"`
}

func (h *Handler) backfillDay(c *gin.Context) {
	if h == nil || h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	if h.upstream == nil || h.fetcher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "upstream unavailable"})
		return
	}
	cookie := strings.TrimSpace(c.GetHeader("Cookie"))
	if cookie == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing Cookie header"})
		return
	}

	date, ok := normalizeBackfillDate(c.Query("date"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date, expected YYYY-MM-DD"})
		return
	}

	before, err := countAggregatesByDate(h, date)
	if err != nil {
		fail(c, err)
		return
	}

	runner := h.aggregator
	usedTempRunner := false
	if runner == nil {
		runner = &Aggregator{
			db:       h.db,
			upstream: h.upstream,
			fetcher:  h.fetcher,
			flight:   make(map[string]bool),
		}
		usedTempRunner = true
	}
	// 单日期互斥：拿不到 flight 说明该日期补库正在进行中。
	if !runner.acquireDateFlight(date) {
		row, _ := loadAggregateStatusRow(h, date)
		c.JSON(http.StatusConflict, gin.H{
			"error":      "backfill already running for date",
			"date":       date,
			"day_status": row,
		})
		return
	}

	// 异步执行：补库是重操作（拉全天 session + 下载解析 TOS，可达数分钟），
	// 同步执行会撞网关超时（504）。这里后台跑，接口立即返回，
	// 前端通过 /api/aggregate-status 轮询该日期 status 进度。
	go func() {
		defer runner.releaseDateFlight(date)
		runner.runAggregate(cookie, date)
	}()

	row, _ := loadAggregateStatusRow(h, date)
	c.JSON(http.StatusAccepted, backfillDayResponse{
		Date:               date,
		Started:            true,
		Message:            "backfill started in background, poll /api/aggregate-status for progress",
		UsedTempRunner:     usedTempRunner,
		AggregatorEnabled:  h.aggregator != nil,
		SessionCountBefore: before,
		DayStatus:          row,
	})
}

func normalizeBackfillDate(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().Format("2006-01-02"), true
	}
	t, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

func countAggregatesByDate(h *Handler, date string) (int64, error) {
	var total int64
	err := h.db.Model(&model.APISessionAggregate{}).
		Where("aggregate_date = ?", parseAggregateDate(date)).
		Count(&total).Error
	return total, err
}

func loadAggregateStatusRow(h *Handler, date string) (*apiAggregateStatusRow, error) {
	var row model.APIDailyAggregateStatus
	if err := h.db.Where("aggregate_date = ?", parseAggregateDate(date)).First(&row).Error; err != nil {
		return nil, err
	}
	skipCount := row.ListTotal - row.SuccessCount - row.FailCount
	if skipCount < 0 {
		skipCount = 0
	}
	return &apiAggregateStatusRow{
		AggregateDate:    row.AggregateDate.Format("2006-01-02"),
		Status:           row.Status,
		SessionCount:     row.SessionCount,
		SuccessCount:     row.SuccessCount,
		FailCount:        row.FailCount,
		SkipCount:        skipCount,
		ListTotal:        row.ListTotal,
		CompletionRate:   percent(row.SuccessCount, row.ListTotal),
		FailureRate:      percent(row.FailCount, row.ListTotal),
		SkipRate:         percent(skipCount, row.ListTotal),
		RetryCount:       row.RetryCount,
		FetchConcurrency: row.FetchConcurrency,
		LastError:        row.LastError,
		CostMs:           row.CostMs,
		StartedAt:        formatTimePtr(row.StartedAt),
		FinishedAt:       formatTimePtr(row.FinishedAt),
		LastAggregatedAt: formatTimePtr(row.LastAggregatedAt),
	}, nil
}
