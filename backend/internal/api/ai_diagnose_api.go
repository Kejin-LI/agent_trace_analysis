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

	// Span 级分析（action=analyze_span）专用字段。
	// 由前端从 Span 抽屉已有数据直接传入，后端不重复取数。
	SpanName     string `json:"span_name"`
	SpanType     string `json:"span_type"`
	SpanStatus   string `json:"span_status"`
	SpanDuration string `json:"span_duration"`
	SpanInput    string `json:"span_input"`
	SpanOutput   string `json:"span_output"`
	SpanError    string `json:"span_error"`
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

// clusterSummaryPrompt 是「异常聚类 · AI 批量总结」的系统设定。
//
// 场景：在异常对比抽屉里，用户勾选了若干命中同一类问题的 Session，
// 要把这批 Session 在该问题上的表现归纳成一份逻辑清晰、可溯源的总结。
const clusterSummaryPrompt = `你是一名资深的 AI Agent 质量分析师，擅长把"一类异常问题在多个会话上的表现"归纳成一份逻辑清晰、详尽、可落地的诊断报告。

你将拿到：一类异常问题的名称、所属维度，以及一批命中该问题的 Session 列表。每个 Session 含：Session ID、它在该问题上的得分（部分为规则/轨迹类命中，无独立维度评分）、以及它在该问题上的具体表现描述。分数越低代表该问题在这个 Session 上越严重。

【硬性要求】
1. 围绕这一类问题做深度归纳，提炼共性、差异与潜在根因，不要机械复述每条原文，也不要泛泛而谈。
2. 必须落到具体 Session：每一个结论、归类、案例都要点名对应的 Session，并带上它的分数（无分的注明"无独立维度评分"）。严禁脱离具体 Session 空谈。
3. 引用 Session 时，必须原样写出完整的 Session ID 文本（例如 ses_107869272ffeqePw2S24NYAD4h），一字不差、不得改写、缩写或省略中间字符——系统会据此自动转成可点击跳转链接。
4. 内容要尽可能详细、有参考价值，建议必须具体可操作（能指导工程师改 prompt / 改工具 / 加约束），避免"加强监控""优化逻辑"这类空话。
5. 采用规范 Markdown（用 ## 二级标题分区，区内用有序/无序列表分点，可用 **加粗** 强调关键词），结构如下：
   ## 整体结论
   2~4 句定性这批 Session 在该问题上的共性表现、严重程度分布与影响面，点明最值得关注的现象。
   ## 问题归类
   按表现/根因把 Session 分成 2~4 类，每类作为一个列表项：先用一句话点出该类的共同特征与可能成因，再列出属于该类的 Session ID（每个都带分数）。
   ## 典型案例
   挑 2~3 个最严重（分数最低或表现最典型）的 Session，每个作为一个小节展开：开头写明 Session ID 与分数，然后分点说明【现象】（结合其具体描述）、【推测根因】、【该案例的针对性修复方向】。
   ## 改进建议
   给出 3~5 条按优先级排序、可直接执行的改进措施，每条说明【做什么】和【预期解决哪类问题】，并尽量关联到上面的具体 Session 或问题分类。
6. 直接输出正文，不要"好的""以下是总结"之类开场白。语气专业、克制、务实。`

// spanSystemPrompt 是「AI 分析此 Span」的系统设定。
//
// 只针对执行轨迹中的单个节点（一次大模型调用 / 一次工具调用）做语义点评，
// 刻意区别于抽屉里已展示的量化指标（耗时/token/状态等）。
const spanSystemPrompt = `你是一名资深的 AI Agent 行为分析师，擅长对执行轨迹中的单个节点（Span）做"语义级"点评。

你将拿到一个 Span 的关键信息：节点类型、名称、状态，以及它的输入(Input)与输出(Output)。这个 Span 通常是一次大模型调用或一次工具调用。

【硬性要求】
1. 严禁复述抽屉里用户已经能看到的量化指标（耗时、token 数、状态码等），这些不是你的工作。
2. 聚焦"这一个节点"的语义分析：
   - 这一步在整体任务中想达成什么目的
   - Input 是否合理、信息是否充分（有没有缺关键上下文、有没有冗余噪声）
   - Output / 决策是否恰当（不是快慢，而是"对不对、该不该"，有没有跑偏、空转、重复）
   - 如果失败或异常，最可能的语义层原因是什么
3. 输出简洁，采用如下结构（用规范 Markdown）：
   - 一句话**结论**（这一步做得如何）
   - 1~3 条**关键分析**（带证据，引用 Input/Output 里的具体片段）
   - 如有必要，给 1 条**改进建议**
4. 直接输出正文，不要"好的""以下是分析"之类开场白。语气专业、克制、可落地。`

// diagnose 处理 POST /api/ai-diagnose：
// 从 TCC 取方舟配置，拼装 prompt，流式调用豆包 2.0，并以 SSE 把增量文本转发给前端。
func (h *Handler) diagnose(c *gin.Context) {
	var req diagnoseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	// span 分析只看单节点，不需要会话纪要；其余 action 仍要求 summary。
	if req.Action != "analyze_span" && strings.TrimSpace(req.Summary) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "对话摘要为空，无法诊断"})
		return
	}
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
	case "analyze_span":
		return spanSystemPrompt, buildSpanUserContent(req), nil
	case "cluster_summary":
		if strings.TrimSpace(req.Summary) == "" {
			return "", "", fmt.Errorf("未选择任何 Session，无法总结")
		}
		return clusterSummaryPrompt, buildClusterSummaryUserContent(req), nil
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

// buildClusterSummaryUserContent 把「一类问题 + 选中的一批 Session」拼成 user 消息。
// req.Title=问题名，req.Intent=维度，req.Summary=前端已拼好的 Session 列表纪要
// （每条含 Session ID / 该问题分数 / 具体描述）。后端不重复取数。
func buildClusterSummaryUserContent(req diagnoseRequest) string {
	var b strings.Builder
	b.WriteString("异常问题：")
	if t := strings.TrimSpace(req.Title); t != "" {
		b.WriteString(t)
	} else {
		b.WriteString("（未命名）")
	}
	b.WriteString("\n")
	if dim := strings.TrimSpace(req.Intent); dim != "" {
		b.WriteString("所属维度：")
		b.WriteString(dim)
		b.WriteString("\n")
	}
	b.WriteString("\n以下是命中该问题的 Session 列表（含各自在该问题上的分数与具体表现），请据此撰写归纳总结：\n\n")
	b.WriteString(req.Summary)
	return b.String()
}

// buildSpanUserContent 把单个 span 的关键信息拼成 user 消息。
// 只喂当前节点（类型/名称/状态/输入/输出/错误），不带其他上下文。
func buildSpanUserContent(req diagnoseRequest) string {
	var b strings.Builder
	b.WriteString("请对下面这个执行轨迹节点（Span）做语义级点评：\n\n")
	if t := strings.TrimSpace(req.SpanType); t != "" {
		b.WriteString("节点类型：")
		b.WriteString(t)
		b.WriteString("\n")
	}
	if n := strings.TrimSpace(req.SpanName); n != "" {
		b.WriteString("节点名称：")
		b.WriteString(n)
		b.WriteString("\n")
	}
	if s := strings.TrimSpace(req.SpanStatus); s != "" {
		b.WriteString("状态：")
		b.WriteString(s)
		b.WriteString("\n")
	}
	if e := strings.TrimSpace(req.SpanError); e != "" {
		b.WriteString("\n错误信息：\n")
		b.WriteString(e)
		b.WriteString("\n")
	}
	b.WriteString("\n--- Input ---\n")
	if in := strings.TrimSpace(req.SpanInput); in != "" {
		b.WriteString(in)
	} else {
		b.WriteString("（无）")
	}
	b.WriteString("\n\n--- Output ---\n")
	if out := strings.TrimSpace(req.SpanOutput); out != "" {
		b.WriteString(out)
	} else {
		b.WriteString("（无）")
	}
	b.WriteString("\n")
	return b.String()
}
