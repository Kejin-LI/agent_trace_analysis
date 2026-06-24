package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/ark"
)

// assistantKB 是右下角「智能助手」的知识底座（说明文档要点 + 平台指标口径）。
// 启动时随二进制内嵌，作为 system prompt 常驻内存，不随每次请求传输。
//
//go:embed assistant_kb.md
var assistantKB string

// assistant 输入的硬上限：双保险控制 token，避免前端误传超大文本打爆上游。
const (
	maxTraceSummaryRunes = 8000
	maxSelectedTextRunes = 2000
	maxQuestionRunes     = 2000
	maxHistoryMessages   = 8
	maxHistoryRunes      = 1200
)

// assistantSystemPrompt 是智能助手的系统设定：依据知识库回答平台问题，
// 在详情页可结合前端压缩好的 trace 纪要做轨迹分析。
var assistantSystemPrompt = `你是 Agent Synapse 平台的智能助手。Agent Synapse 是一个面向 Agentic RL 的诊断平台。

【你的职责】
1. 依据下方提供的【平台知识库】回答用户关于平台功能、指标口径、概念、排查路径的问题。
2. 当用户在某个 Session 详情页提问、且消息中附带【当前 Session 轨迹纪要】时，结合该轨迹做针对性分析（如某一步为什么失败、是否空转、工具调用是否合理）。
3. 当消息中附带【用户划选的内容】时，优先围绕这段引用作答。

【硬性要求】
1. 平台口径类问题必须严格依据知识库，不得编造知识库里没有的公式、阈值或字段。知识库没有覆盖且你也不确定时，如实说明。
2. 轨迹分析聚焦语义层面（对不对、该不该、有没有跑偏），不要复述用户已能看到的量化数字。
3. 如果消息中附带【用户划选的内容】，回答时必须显式带上这段引用文本，再结合该引用回答用户问题。优先使用简短引用格式，例如“引用：……”或 Markdown 引用块，且引用内容必须来自用户提供的原文，不要改写其原意。
4. 回答简洁，使用规范 Markdown；结论先行，必要时配 1~3 条要点。语气专业、克制、可落地。
5. 直接输出正文，不要"好的""以下是回答"之类的开场白。

【平台知识库】
` + assistantKB

// assistantChatRequest 前端提交的助手对话请求体。
//
// 后端无状态：不读库、不取 session。TraceSummary 是前端从 window.SESSION._real
// 现场压缩好的逐轮纪要文本（可空）；SelectedText 是划词引用（可空）。
type assistantChatRequest struct {
	Question     string        `json:"question"`
	SessionID    string        `json:"session_id"`
	SessionTitle string        `json:"session_title"`
	TraceSummary string        `json:"trace_summary"`
	SelectedText string        `json:"selected_text"`
	History      []ark.Message `json:"history"`
}

// chat 处理 POST /api/chat：
// 组装「知识库 system + 历史 + 当前 user」消息，流式调用豆包并以 SSE 转发给前端。
// 全程不触碰数据库，trace 压缩已在前端完成，后端内存占用恒定。
func (h *Handler) chat(c *gin.Context) {
	var req assistantChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "问题为空"})
		return
	}

	cfg, err := ark.LoadConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	messages := buildAssistantMessages(req, question)

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

// buildAssistantMessages 组装发往上游的消息序列：system(知识库) → 历史若干轮 → 当前 user。
func buildAssistantMessages(req assistantChatRequest, question string) []ark.Message {
	messages := make([]ark.Message, 0, len(req.History)+2)
	messages = append(messages, ark.Message{Role: "system", Content: assistantSystemPrompt})

	// 历史只取最近若干轮，并对单条做长度裁剪，控制上下文体积。
	history := req.History
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}
	for _, m := range history {
		role := m.Role
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		messages = append(messages, ark.Message{Role: role, Content: clipRunes(content, maxHistoryRunes)})
	}

	messages = append(messages, ark.Message{Role: "user", Content: buildAssistantUserContent(req, question)})
	return messages
}

// buildAssistantUserContent 把划词引用、当前 session 轨迹纪要、用户问题拼成 user 消息。
// 仅在对应字段非空时才拼入，未提供轨迹时即纯知识库问答。
func buildAssistantUserContent(req assistantChatRequest, question string) string {
	var b strings.Builder

	if quote := strings.TrimSpace(req.SelectedText); quote != "" {
		b.WriteString("【用户划选的内容】\n")
		b.WriteString(clipRunes(quote, maxSelectedTextRunes))
		b.WriteString("\n\n")
	}

	if summary := strings.TrimSpace(req.TraceSummary); summary != "" {
		if title := strings.TrimSpace(req.SessionTitle); title != "" {
			b.WriteString("【当前 Session】")
			b.WriteString(title)
			b.WriteString("\n")
		}
		b.WriteString("【当前 Session 轨迹纪要】\n")
		b.WriteString(clipRunes(summary, maxTraceSummaryRunes))
		b.WriteString("\n\n")
	}

	b.WriteString("【用户问题】\n")
	b.WriteString(clipRunes(question, maxQuestionRunes))
	return b.String()
}

// clipRunes 按 rune 截断，避免切断多字节字符。
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
