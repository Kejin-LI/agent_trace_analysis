package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

type manualReviewRequest struct {
	SessionID         string          `json:"session_id"`
	TraceID           string          `json:"trace_id"`
	ArtifactID        string          `json:"artifact_id"`
	SessionTitle      string          `json:"session_title"`
	SessionUser       string          `json:"session_user"`
	SessionUserID     string          `json:"session_user_id"`
	SessionStartedAt  *time.Time      `json:"session_started_at"`
	SessionDurationMs int64           `json:"session_duration_ms"`
	SessionTurns      int             `json:"session_turns"`
	SessionTraceCount int             `json:"session_trace_count"`
	ReasonCode        string          `json:"reason_code"`
	ReasonCodes       []string        `json:"reason_codes"`
	ReasonText        string          `json:"reason_text"`
	SubmitNote        string          `json:"submit_note"`
	Submitter         string          `json:"submitter"`
	RulePassed        int             `json:"rule_passed"`
	RuleTotal         int             `json:"rule_total"`
	LLMJudgeScore     int             `json:"llm_judge_score"`
	LLMJudgeModel     string          `json:"llm_judge_model"`
	LLMJudgeResult    json.RawMessage `json:"llm_judge_result"`
	RuleEvalResult    json.RawMessage `json:"rule_eval_result"`
	EvidenceSnapshot  json.RawMessage `json:"evidence_snapshot"`
}

func (h *Handler) createManualReview(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	var req manualReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	reasonCode := normalizeReasonCodes(req.ReasonCodes, req.ReasonCode)
	if req.SessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 为空"})
		return
	}
	if reasonCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择送审原因"})
		return
	}

	reviewID := "mrev_" + randHex(12)
	row := model.StgSessionManualReviewRequest{
		ReviewID:          reviewID,
		SessionID:         req.SessionID,
		TraceID:           trimLen(req.TraceID, 128),
		ArtifactID:        trimLen(req.ArtifactID, 128),
		SessionTitle:      trimLen(req.SessionTitle, 512),
		SessionUser:       trimLen(req.SessionUser, 128),
		SessionUserID:     trimLen(req.SessionUserID, 128),
		SessionStartedAt:  req.SessionStartedAt,
		SessionDurationMs: req.SessionDurationMs,
		SessionTurns:      req.SessionTurns,
		SessionTraceCount: req.SessionTraceCount,
		ReviewType:        "manual_calibration",
		Status:            "pending",
		ReasonCode:        trimLen(reasonCode, 255),
		ReasonText:        trimLen(req.ReasonText, 512),
		SubmitNote:        trimLen(req.SubmitNote, 512),
		Submitter:         trimLen(firstReviewNonEmpty(req.Submitter, req.SessionUser, req.SessionUserID), 128),
		Reviewer:          "",
		RulePassed:        req.RulePassed,
		RuleTotal:         req.RuleTotal,
		LLMJudgeScore:     req.LLMJudgeScore,
		LLMJudgeModel:     trimLen(firstReviewNonEmpty(req.LLMJudgeModel, "gpt-5.5"), 64),
		LLMJudgeResult:    jsonOrNull(req.LLMJudgeResult),
		RuleEvalResult:    jsonOrNull(req.RuleEvalResult),
		EvidenceSnapshot:  jsonOrNull(req.EvidenceSnapshot),
		HumanResult:       "null",
		HumanScore:        -1,
		IsDeleted:         0,
	}
	if row.RulePassed == 0 && row.RuleTotal == 0 {
		row.RulePassed = -1
		row.RuleTotal = -1
	}
	if err := h.db.Create(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"review_id": reviewID,
		"status":    row.Status,
	})
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}
	return hex.EncodeToString(b)
}

func jsonOrNull(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return "null"
	}
	return string(raw)
}

func trimLen(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstReviewNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// normalizeReasonCodes 合并多选送审原因，去重去空并保持稳定顺序，
// 兼容仅传 reason_code（可能为逗号分隔）的旧请求。
func normalizeReasonCodes(codes []string, fallback string) string {
	raw := append([]string{}, codes...)
	if len(raw) == 0 && strings.TrimSpace(fallback) != "" {
		raw = strings.Split(fallback, ",")
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return strings.Join(out, ",")
}
