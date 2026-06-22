package api

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

type backfillIssueFlagsResponse struct {
	ColumnReady    bool     `json:"column_ready"`
	Scanned        int      `json:"scanned"`
	SetTrue        int      `json:"set_true"`
	SetFalse       int      `json:"set_false"`
	Changed        int      `json:"changed"`
	DatesRefreshed []string `json:"dates_refreshed"`
	CostMs         int64    `json:"cost_ms"`
	Message        string   `json:"message"`
}

// backfillIssueFlags 一次性回算 api_session_aggregates.has_issue（统一标签口径：
// 命中失败规则 OR GPT 问题标签），并按受影响自然日重算 api_daily_summary.abnormal_session_count，
// 让大盘 / 异常 / 明细三个菜单口径对齐。
//
// 复用运行时同一套 aggregateIssueFlagForSession 口径，杜绝 SQL 与代码口径漂移。
// 仅做 DB 内 UPDATE，不打上游、不下载 TOS，可在实例 webshell 通过 localhost curl 直接触发。
// 同步执行：经 localhost 调用不过网关，无 504 风险；前端不暴露此入口。
//
// 可选 query：start_date / end_date（YYYY-MM-DD，本地时区），缺省回算全表。
func (h *Handler) backfillIssueFlags(c *gin.Context) {
	if h == nil || h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	if !apiSessionAggregateHasIssueColumn(h.db) {
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error":   "has_issue column not found on api_session_aggregates; run the DDL first",
			"hint":    "ALTER TABLE api_session_aggregates ADD COLUMN has_issue ...",
			"column":  "has_issue",
			"applied": false,
		})
		return
	}

	start := time.Now()

	type aggRow struct {
		ID            uint64
		SessionID     string
		ArtifactID    string
		RulesJSON     string
		HasIssue      bool
		AggregateDate time.Time
	}

	q := h.db.Model(&model.APISessionAggregate{}).
		Select("id", "session_id", "artifact_id", "rules_json", "has_issue", "aggregate_date")
	if sd := strings.TrimSpace(c.Query("start_date")); sd != "" {
		if t, err := time.ParseInLocation("2006-01-02", sd, time.Local); err == nil {
			q = q.Where("aggregate_date >= ?", t)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date, expected YYYY-MM-DD"})
			return
		}
	}
	if ed := strings.TrimSpace(c.Query("end_date")); ed != "" {
		if t, err := time.ParseInLocation("2006-01-02", ed, time.Local); err == nil {
			q = q.Where("aggregate_date <= ?", t)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date, expected YYYY-MM-DD"})
			return
		}
	}

	var rows []aggRow
	if err := q.Order("id ASC").Find(&rows).Error; err != nil {
		fail(c, err)
		return
	}

	trueIDs := make([]uint64, 0, len(rows))
	falseIDs := make([]uint64, 0, len(rows))
	// 所有出现过的自然日都要重算日汇总：旧 api_daily_summary.abnormal_session_count
	// 是按旧口径（abnormal_level）算的，即使本次 has_issue 没翻转，也要按新口径刷新，
	// 才能保证大盘（读日汇总）与异常页（读 has_issue）对齐。
	allDates := make(map[string]time.Time)
	for _, row := range rows {
		allDates[row.AggregateDate.Format("2006-01-02")] = startOfLocalDay(row.AggregateDate)
		want := aggregateIssueFlagForSession(h.db, row.SessionID, row.ArtifactID, row.RulesJSON)
		if want == row.HasIssue {
			continue
		}
		if want {
			trueIDs = append(trueIDs, row.ID)
		} else {
			falseIDs = append(falseIDs, row.ID)
		}
	}
	changed := len(trueIDs) + len(falseIDs)

	now := time.Now()
	if err := bulkUpdateHasIssue(h, trueIDs, true, now); err != nil {
		fail(c, err)
		return
	}
	if err := bulkUpdateHasIssue(h, falseIDs, false, now); err != nil {
		fail(c, err)
		return
	}

	runner := h.aggregator
	if runner == nil {
		runner = &Aggregator{db: h.db}
	}
	refreshed := make([]string, 0, len(allDates))
	for label, day := range allDates {
		if err := runner.refreshDailySummary(day); err != nil {
			log.Printf("backfill issue flags: refresh summary date=%s failed: %v", label, err)
			continue
		}
		refreshed = append(refreshed, label)
	}
	sort.Strings(refreshed)

	c.JSON(http.StatusOK, backfillIssueFlagsResponse{
		ColumnReady:    true,
		Scanned:        len(rows),
		SetTrue:        len(trueIDs),
		SetFalse:       len(falseIDs),
		Changed:        changed,
		DatesRefreshed: refreshed,
		CostMs:         time.Since(start).Milliseconds(),
		Message:        "has_issue recomputed and daily summaries refreshed",
	})
}

func bulkUpdateHasIssue(h *Handler, ids []uint64, value bool, ts time.Time) error {
	const chunk = 500
	for i := 0; i < len(ids); i += chunk {
		end := i + chunk
		if end > len(ids) {
			end = len(ids)
		}
		if err := h.db.Model(&model.APISessionAggregate{}).
			Where("id IN ?", ids[i:end]).
			Updates(map[string]interface{}{"has_issue": value, "updated_at": ts}).Error; err != nil {
			return err
		}
	}
	return nil
}
