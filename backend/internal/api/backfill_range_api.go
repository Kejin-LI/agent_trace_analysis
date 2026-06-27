package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type backfillRangeResponse struct {
	Dates              []string `json:"dates"`
	StartedDays        []string `json:"started_days"`
	AlreadyRunningDays []string `json:"already_running_days,omitempty"`
	SkippedDays        []string `json:"skipped_days,omitempty"`
	Message            string   `json:"message"`
	UsedTempRunner     bool     `json:"used_temp_runner"`
	AggregatorEnabled  bool     `json:"aggregator_enabled"`
}

func (h *Handler) backfillRange(c *gin.Context) {
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

	dates := normalizedDaysDesc(daysFromQueryRange(timeRangeFromQuery(c)))
	if len(dates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid time range"})
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
		go func(days []string) {
			for _, date := range days {
				runner.runAggregate(cookie, date, true)
			}
		}(append([]string(nil), dates...))
		c.JSON(http.StatusAccepted, backfillRangeResponse{
			Dates:             dates,
			StartedDays:       dates,
			Message:           "range backfill started in background",
			UsedTempRunner:    true,
			AggregatorEnabled: false,
		})
		return
	}

	startedDays, alreadyRunningDays, skippedDays := runner.ForceQueueDays(cookie, dates)
	c.JSON(http.StatusAccepted, backfillRangeResponse{
		Dates:              dates,
		StartedDays:        startedDays,
		AlreadyRunningDays: alreadyRunningDays,
		SkippedDays:        skippedDays,
		Message:            "range backfill queued in background",
		UsedTempRunner:     usedTempRunner,
		AggregatorEnabled:  h.aggregator != nil,
	})
}
