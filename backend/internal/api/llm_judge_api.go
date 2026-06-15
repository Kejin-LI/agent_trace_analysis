package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/llmjudge"
)

type llmJudgeRequest struct {
	SystemPrompt string          `json:"system_prompt"`
	Input        json.RawMessage `json:"input"`
}

func (h *Handler) llmJudgeEvaluate(c *gin.Context) {
	var req llmJudgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
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

	cfg, err := llmjudge.LoadConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	userContent := fmt.Sprintf("请评估以下 Agent 会话，并严格返回 JSON：\n\n%s", string(req.Input))
	raw, err := h.llmJudge.ChatJSON(c.Request.Context(), cfg, []llmjudge.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userContent},
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "GPT-5.5 未返回合法 JSON: " + err.Error(), "raw": raw})
		return
	}
	if len(result) == 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "GPT-5.5 返回空 JSON 对象", "raw": raw})
		return
	}
	result["model_label"] = "GPT-5.5"
	result["source"] = "tcc_gpt55"
	c.JSON(http.StatusOK, gin.H{"data": result})
}
