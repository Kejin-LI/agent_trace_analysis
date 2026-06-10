package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/ark"
)

// diagnoseRequest 前端提交的诊断请求体。
//
// summary 是前端从完整对话流（window.SESSION._real）裁剪拼装好的「逐轮纪要」文本，
// 含：用户诉求、模型思考(reasoning)、工具动作、产出。后端不重复取数，只负责喂模型。
type diagnoseRequest struct {
	SessionID      string `json:"session_id"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	ReportMarkdown string `json:"report_markdown"`
	Question       string `json:"question"`
	Intent         string `json:"intent"`
	Action         string `json:"action"`
}

// systemPrompt 是 AI 一键诊断的系统设定。
//
// 核心约束：做「语义级」复盘，刻意区别于页面上已有的量化指标（耗时/token/工具失败率等），
// 聚焦语义分析；按金字塔结构输出 Markdown。
const systemPrompt = `你是一名资深的 AI Agent 行为分析师，擅长对大模型的完整执行轨迹做"语义级"复盘。

你将拿到一段 Agent 会话的逐轮纪要（用户诉求、模型思考、工具动作、产出）。请基于它撰写一份分析报告。

【硬性要求】
1. 严禁复述页面上用户已经能看到的量化指标（如总耗时、token 数、工具调用次数、失败率、轮次数等），这些不是你的工作。
2. 你的价值在于"语义分析"，聚焦人/指标看不出的东西：
   - 用户的真实诉求是什么（含潜台词、目标随对话的演变）
   - 模型如何理解与拆解任务（思考链路是否抓住了要点、有没有跑偏）
   - 关键决策与工具动作在语义层面是否合理（不是快慢，而是"对不对、该不该"）
   - 整体完成度：是否真正解决了用户的问题，偏差或遗漏在哪里
3. 严格采用"金字塔结构"输出：
   - 先给【顶层结论】：一句话定性 + 完成度评级（优秀 / 良好 / 合格 / 待改进）
   - 再给【三大支撑论点】：每个论点配证据，引用对话中的具体片段佐证
   - 最后给【关键改进建议】：可执行、有针对性
4. 输出使用规范 Markdown：用 ## / ### 分级标题，适当使用有序/无序列表与 **加粗**。语气专业、克制、可落地。
5. 直接输出报告正文，不要写"好的""以下是报告"之类的开场白。`

const consultPrompt = `你是一名资深的 AI Agent 行为分析师助手。

你将拿到：
1. 一段 Agent 会话的逐轮纪要
2. 一份已有诊断报告
3. 用户的一个深入追问

你的任务不是重写整篇报告，而是仅针对这个追问，给出一段高信息密度的"轻咨询"回答。

【要求】
1. 只回答用户当前追问，不要复述整篇报告。
2. 允许补充更细的因果链、关键决策、轮次差异、风险点，但不要输出量化指标。
3. 输出用 Markdown，建议采用：一句结论 + 2~4 条关键说明。
4. 不要说"以下是回答"。直接输出正文。`

const reportUpdatePrompt = `你是一名资深的 AI Agent 行为分析师。

你将拿到：
1. 一段 Agent 会话的逐轮纪要
2. 一份已有诊断报告
3. 用户希望"补充进报告"的追问

你的任务是：输出一段可直接并入原报告的"补充分析正文"。

【要求】
1. 不要重写整篇报告，只输出新增正文，不要带总开场白。
2. 不要输出顶层标题（前端会自动补一个"补充分析 · xxx"标题）。
3. 内容要和已有报告风格一致，聚焦追问本身。
4. 严禁复述页面已有量化指标。输出 Markdown。`

// diagnose 处理 POST /api/ai-diagnose：
// 从 TCC 取方舟配置，拼装 prompt，流式调用豆包 2.0，并以 SSE 把增量文本转发给前端。
func (h *Handler) diagnose(c *gin.Context) {
	var req diagnoseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Summary) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "对话摘要为空，无法诊断"})
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	req.Intent = strings.TrimSpace(req.Intent)

	cfg, err := ark.LoadConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	system, userContent, err := buildPrompt(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	messages := []ark.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userContent},
	}

	// SSE 头：禁用缓冲，逐块下发。
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	writeSSE := func(event string, payload interface{}) {
		buf, _ := json.Marshal(payload)
		if event != "" {
			fmt.Fprintf(c.Writer, "event: %s\n", event)
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", buf)
		flusher.Flush()
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	err = h.ark.StreamChat(ctx, cfg, messages, func(delta string) {
		writeSSE("", gin.H{"delta": delta})
	})
	if err != nil {
		writeSSE("error", gin.H{"error": err.Error()})
		return
	}
	writeSSE("done", gin.H{"done": true})
}

func buildPrompt(req diagnoseRequest) (system string, user string, err error) {
	switch req.Action {
	case "", "initial_report":
		return systemPrompt, buildInitialUserContent(req), nil
	case "consult_only":
		if strings.TrimSpace(req.Question) == "" {
			return "", "", fmt.Errorf("咨询问题为空")
		}
		return consultPrompt, buildFollowupUserContent(req, true), nil
	case "followup_report":
		if strings.TrimSpace(req.Question) == "" {
			return "", "", fmt.Errorf("补充问题为空")
		}
		return reportUpdatePrompt, buildFollowupUserContent(req, false), nil
	default:
		return "", "", fmt.Errorf("未知 action: %s", req.Action)
	}
}

// buildInitialUserContent 把会话元信息与逐轮纪要拼成首次诊断的 user 消息。
func buildInitialUserContent(req diagnoseRequest) string {
	var b strings.Builder
	if t := strings.TrimSpace(req.Title); t != "" {
		b.WriteString("会话主题：")
		b.WriteString(t)
		b.WriteString("\n")
	}
	if id := strings.TrimSpace(req.SessionID); id != "" {
		b.WriteString("Session ID：")
		b.WriteString(id)
		b.WriteString("\n")
	}
	b.WriteString("\n以下是这次会话的逐轮纪要，请据此撰写语义级分析报告：\n\n")
	b.WriteString(req.Summary)
	return b.String()
}

// buildFollowupUserContent 把原报告 + 追问 + 会话纪要拼成追问场景的 user 消息。
func buildFollowupUserContent(req diagnoseRequest, consultOnly bool) string {
	var b strings.Builder
	if t := strings.TrimSpace(req.Title); t != "" {
		b.WriteString("会话主题：")
		b.WriteString(t)
		b.WriteString("\n")
	}
	if id := strings.TrimSpace(req.SessionID); id != "" {
		b.WriteString("Session ID：")
		b.WriteString(id)
		b.WriteString("\n")
	}
	if report := strings.TrimSpace(req.ReportMarkdown); report != "" {
		b.WriteString("\n已有诊断报告：\n")
		b.WriteString(report)
		b.WriteString("\n")
	}
	b.WriteString("\n用户追问：\n")
	b.WriteString(strings.TrimSpace(req.Question))
	b.WriteString("\n")
	if consultOnly {
		b.WriteString("\n请给出一段轻咨询回答，不要改写整篇报告。\n")
	} else {
		b.WriteString("\n请输出一段可并入原报告的补充分析正文，不要重写整篇报告，也不要自行补总标题。\n")
	}
	b.WriteString("\n会话逐轮纪要：\n")
	b.WriteString(req.Summary)
	return b.String()
}
