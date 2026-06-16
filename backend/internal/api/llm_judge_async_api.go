package api

// 异步 GPT-5.5 评估:把原本同步耗时 20-60s 的 /api/llm-judge 改造成 enqueue + poll 模式。
// 客户端发起后端立即返回 202;真正的 GPT 调用脱离客户端 context,在独立 goroutine 里跑,
// 状态/结果落到 stg_session_quality_evaluations 表(复用 llm_eval_status 字段),
// 浏览器刷新后通过状态接口或持久化结果接口恢复"评估中 / 已完成"渲染。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/llmjudge"
)

// llmJudgeJobTTL:running 状态超过该时长视为僵死,允许新的请求覆盖触发(防止 panic 留下永远 running 的脏行)。
const llmJudgeJobTTL = 5 * time.Minute

// inflightLLMJudge:进程内去重,避免同 session 在毫秒级被重复触发(刷新+点击竞态)。
// 仅做最佳努力去重,持久化层另由 status='running' + 时间戳保证。
var inflightLLMJudge sync.Map // key: session_id, value: time.Time(start)

type llmJudgeAsyncRequest struct {
	SessionID    string          `json:"session_id"`
	ArtifactID   string          `json:"artifact_id"`
	TraceID      string          `json:"trace_id"`
	SessionTitle string          `json:"session_title"`
	SessionUser  string          `json:"session_user"`
	UserID       string          `json:"session_user_id"`
	SystemPrompt string          `json:"system_prompt"`
	Input        json.RawMessage `json:"input"`
}

// llmJudgeAsyncStart 接收异步评估请求:落 running 行 → 开 goroutine → 立即返回。
func (h *Handler) llmJudgeAsyncStart(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	var req llmJudgeAsyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}
	sid := strings.TrimSpace(req.SessionID)
	if sid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 为空"})
		return
	}
	system := strings.TrimSpace(req.SystemPrompt)
	if system == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "system_prompt 为空"})
		return
	}
	if len(req.Input) == 0 || string(req.Input) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "judge input 为空"})
		return
	}

	// 去重:同 session 已经在 running 且未超 TTL 时,直接返回 202 让前端轮询既有任务。
	if existing, ok := h.checkRunningLLMJudge(sid); ok {
		c.JSON(http.StatusAccepted, gin.H{
			"status":     "running",
			"session_id": sid,
			"started_at": existing,
			"message":    "评估正在进行中,请稍候",
		})
		return
	}

	if err := h.markLLMJudgeRunning(sid, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "标记 running 状态失败: " + err.Error()})
		return
	}
	inflightLLMJudge.Store(sid, time.Now())

	// 真正的 GPT 调用:用 Background ctx,与客户端连接解耦,刷新页面/关闭标签都不会中断。
	go h.runLLMJudgeAsync(sid, system, req)

	c.JSON(http.StatusAccepted, gin.H{
		"status":     "running",
		"session_id": sid,
		"started_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// llmJudgeAsyncStatus 轮询接口:返回 running / succeeded / failed / not_found。
// succeeded 时附带前端可直接渲染的 result 对象(等价于 quality-evaluations 接口结构)。
func (h *Handler) llmJudgeAsyncStatus(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	sid := strings.TrimSpace(c.Param("session_id"))
	if sid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id 为空"})
		return
	}
	var row model.StgSessionQualityEvaluation
	err := h.db.
		Select(h.filterExistingColumns([]string{
			"session_id", "artifact_id", "llm_eval_status", "llm_eval_version",
			"llm_evaluated_at", "llm_score", "llm_judge_score", "llm_model",
			"llm_eval_result", "llm_raw_result", "llm_error",
			"combined_score", "rule_score", "updated_at",
		})).
		Where("session_id = ? AND is_deleted = 0", sid).
		Order("updated_at DESC, id DESC").
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"status": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := strings.TrimSpace(row.LLMEvalStatus)
	if status == "" {
		status = "succeeded" // 旧数据无 status 字段,但有结果时按已完成处理
	}

	// running 超过 TTL 视为僵死,降级为 failed 让前端能重试。
	if status == "running" && row.LLMEvaluatedAt != nil && time.Since(*row.LLMEvaluatedAt) > llmJudgeJobTTL {
		status = "failed"
	}

	resp := gin.H{
		"status":           status,
		"session_id":       row.SessionID,
		"llm_eval_version": row.LLMEvalVersion,
		"started_at":       timeToString(row.LLMEvaluatedAt),
		"updated_at":       timeToString(&row.UpdatedAt),
	}
	if status == "succeeded" {
		resp["result"] = qualityEvaluationResponse(row)
	} else if status == "failed" {
		resp["error"] = row.LLMError
	}
	c.JSON(http.StatusOK, resp)
}

// checkRunningLLMJudge 查 stg 表,判断当前 session 是否已有未过期的 running 记录。
func (h *Handler) checkRunningLLMJudge(sessionID string) (string, bool) {
	if v, ok := inflightLLMJudge.Load(sessionID); ok {
		if t, ok := v.(time.Time); ok && time.Since(t) < llmJudgeJobTTL {
			return t.UTC().Format(time.RFC3339), true
		}
		inflightLLMJudge.Delete(sessionID)
	}
	var row model.StgSessionQualityEvaluation
	err := h.db.
		Select(h.filterExistingColumns([]string{"session_id", "llm_eval_status", "llm_evaluated_at", "updated_at"})).
		Where("session_id = ? AND is_deleted = 0", sessionID).
		Order("updated_at DESC, id DESC").
		First(&row).Error
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(row.LLMEvalStatus) != "running" {
		return "", false
	}
	if row.LLMEvaluatedAt != nil && time.Since(*row.LLMEvaluatedAt) > llmJudgeJobTTL {
		return "", false
	}
	startedAt := timeToString(row.LLMEvaluatedAt)
	if startedAt == "" {
		startedAt = timeToString(&row.UpdatedAt)
	}
	return startedAt, true
}

// markLLMJudgeRunning 写一行 running 状态;若已有同 session_id 的行就 update。
func (h *Handler) markLLMJudgeRunning(sessionID string, req llmJudgeAsyncRequest) error {
	now := time.Now()
	row := model.StgSessionQualityEvaluation{
		SessionID:      trimLen(sessionID, 128),
		ArtifactID:     trimLen(req.ArtifactID, 128),
		TraceID:        trimLen(req.TraceID, 128),
		SessionTitle:   trimLen(req.SessionTitle, 1024),
		SessionUser:    trimLen(req.SessionUser, 128),
		SessionUserID:  trimLen(req.UserID, 128),
		LLMEvalStatus:  "running",
		LLMEvaluatedAt: &now,
		LLMTriggeredBy: trimLen(firstReviewNonEmpty(req.SessionUser, req.UserID), 128),
		LLMError:       "",
		IsDeleted:      0,
	}
	return h.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"artifact_id", "trace_id", "session_title", "session_user", "session_user_id",
			"llm_eval_status", "llm_evaluated_at", "llm_triggered_by", "llm_error",
			"is_deleted", "updated_at",
		}),
	}).Create(&row).Error
}

// runLLMJudgeAsync goroutine 主体:跑 GPT,把结果或错误回写到 stg 表。
// 关键:用 context.Background(),与原始 HTTP 连接彻底解耦。
func (h *Handler) runLLMJudgeAsync(sessionID, system string, req llmJudgeAsyncRequest) {
	defer inflightLLMJudge.Delete(sessionID)
	defer func() {
		if r := recover(); r != nil {
			h.markLLMJudgeFailed(sessionID, fmt.Sprintf("panic: %v", r))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), llmJudgeJobTTL)
	defer cancel()

	cfg, err := llmjudge.LoadConfig(ctx)
	if err != nil {
		h.markLLMJudgeFailed(sessionID, "load tcc config: "+err.Error())
		return
	}
	userContent := fmt.Sprintf("请评估以下 Agent 会话,并严格返回 JSON:\n\n%s", string(req.Input))
	raw, err := h.llmJudge.ChatJSON(ctx, cfg, []llmjudge.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userContent},
	})
	if err != nil {
		h.markLLMJudgeFailed(sessionID, "gpt5.5 chat: "+err.Error())
		return
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		h.markLLMJudgeFailed(sessionID, "parse json: "+err.Error())
		return
	}
	if len(result) == 0 {
		h.markLLMJudgeFailed(sessionID, "GPT-5.5 返回空 JSON 对象")
		return
	}
	result["model_label"] = "GPT-5.5"
	result["source"] = "tcc_gpt55"
	if err := h.markLLMJudgeSucceeded(sessionID, req, result, raw); err != nil {
		h.markLLMJudgeFailed(sessionID, "persist result: "+err.Error())
		return
	}
}

func (h *Handler) markLLMJudgeFailed(sessionID, msg string) {
	if h == nil || h.db == nil {
		return
	}
	now := time.Now()
	updates := map[string]interface{}{
		"llm_eval_status":  "failed",
		"llm_error":        trimLen(msg, 1024),
		"llm_evaluated_at": &now,
	}
	if err := h.db.Model(&model.StgSessionQualityEvaluation{}).
		Where("session_id = ?", sessionID).
		Updates(updates).Error; err != nil {
		fmt.Printf("markLLMJudgeFailed: %v\n", err)
	}
}

// markLLMJudgeSucceeded 写最终结果,等价于 upsertQualityEvaluation 的核心逻辑,
// 但这里是 goroutine 内执行,无 c *gin.Context 可用,所有字段从 result map 抽取。
func (h *Handler) markLLMJudgeSucceeded(sessionID string, req llmJudgeAsyncRequest, result map[string]interface{}, raw string) error {
	now := time.Now()
	finiteScore := func(v interface{}) *int {
		f, ok := toFloat(v)
		if !ok {
			return nil
		}
		n := int(f)
		if n < 0 {
			n = 0
		}
		if n > 100 {
			n = 100
		}
		return &n
	}
	stringOf := func(v interface{}) string {
		if v == nil {
			return ""
		}
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}
	jsonRawOf := func(v interface{}) string {
		if v == nil {
			return ""
		}
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}

	llmScore := finiteScore(result["score"])
	resolved := stringOf(result["resolved"])
	intent := stringOf(result["intent_match"])
	efficiency := stringOf(result["efficiency_feel"])
	sentiment := stringOf(result["sentiment"])
	actionability := stringOf(result["actionability"])
	hallucination := stringOf(result["hallucination_risk"])

	// 保留原 raw 输入供前端渲染 evidence/score_basis。
	bResult, _ := json.Marshal(result)

	var version int
	var old model.StgSessionQualityEvaluation
	if err := h.db.Where("session_id = ? AND is_deleted = 0", sessionID).
		Order("updated_at DESC, id DESC").
		First(&old).Error; err == nil {
		version = old.LLMEvalVersion + 1
	} else {
		version = 1
	}

	updates := map[string]interface{}{
		"llm_eval_status":              "succeeded",
		"llm_eval_version":             version,
		"llm_evaluated_at":             &now,
		"llm_model":                    firstReviewNonEmpty(stringOf(result["model_label"]), "gpt-5.5"),
		"llm_score":                    llmScore,
		"llm_grade":                    stringOf(result["overall"]),
		"llm_resolved":                 resolved,
		"llm_resolved_score":           finiteScore(result["resolved_score"]),
		"llm_intent_match":             intent,
		"llm_intent_match_score":       finiteScore(result["intent_match_score"]),
		"llm_efficiency_feel":          efficiency,
		"llm_efficiency_feel_score":    finiteScore(result["efficiency_feel_score"]),
		"llm_sentiment":                sentiment,
		"llm_sentiment_score":          finiteScore(result["sentiment_score"]),
		"llm_actionability":            actionability,
		"llm_actionability_score":      finiteScore(result["actionability_score"]),
		"llm_hallucination_risk":       hallucination,
		"llm_hallucination_risk_score": finiteScore(result["hallucination_risk_score"]),
		"llm_summary":                  trimLen(stringOf(result["reason"]), 1024),
		"llm_score_basis":              trimLen(stringOf(result["score_basis"]), 2048),
		"llm_tags":                     jsonRawOf(result["tags"]),
		"llm_evidence":                 jsonRawOf(result["evidence"]),
		"llm_eval_result":              string(bResult),
		"llm_raw_result":               raw,
		"llm_error":                    "",
	}
	// 过滤掉线上不存在的列,防止 schema 漂移导致整条 update 1054。
	available := h.qualityEvaluationColumns()
	if len(available) > 0 {
		for k := range updates {
			if _, ok := available[k]; !ok {
				delete(updates, k)
			}
		}
	}
	return h.db.Model(&model.StgSessionQualityEvaluation{}).
		Where("session_id = ?", sessionID).
		Updates(updates).Error
}

// toFloat 统一处理 json.Number / float64 / int / string 的转换。
func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
