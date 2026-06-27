package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

type apiAggregateStatusRow struct {
	AggregateDate   string  `json:"aggregate_date"`
	Status          string  `json:"status"`
	SessionCount    int     `json:"session_count"`
	SuccessCount    int     `json:"success_count"`
	FailCount       int     `json:"fail_count"`
	SkipCount       int     `json:"skip_count"`
	ListTotal       int     `json:"list_total"`
	CompletionRate  float64 `json:"completion_rate"`
	FailureRate     float64 `json:"failure_rate"`
	SkipRate        float64 `json:"skip_rate"`
	RetryCount      int     `json:"retry_count"`
	FetchConcurrency int    `json:"fetch_concurrency"`
	LastError       string  `json:"last_error,omitempty"`
	CostMs          int64   `json:"cost_ms"`
	StartedAt       string  `json:"started_at,omitempty"`
	FinishedAt      string  `json:"finished_at,omitempty"`
	LastAggregatedAt string `json:"last_aggregated_at,omitempty"`
}

func (h *Handler) listAggregateStatus(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	limit := 7
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 31 {
			limit = n
		}
	}

	var rows []model.APIDailyAggregateStatus
	if err := h.db.Order("aggregate_date DESC").Limit(limit).Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}

	out := make([]apiAggregateStatusRow, 0, len(rows))
	for _, row := range rows {
		skipCount := row.ListTotal - row.SuccessCount - row.FailCount
		if skipCount < 0 {
			skipCount = 0
		}
		out = append(out, apiAggregateStatusRow{
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
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "limit": limit})
}

func percent(num, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(num) * 100 / float64(total)
}

func formatTimePtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
