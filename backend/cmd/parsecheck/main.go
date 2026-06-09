// Command parsecheck 离线验证日志解析链路：喂一份样本日志文件，
// 打印 Parse + buildBundle 的关键指标，无需 DB / 网络 / Cookie / 发版。
//
// 用法：
//
//	go run ./cmd/parsecheck <样本文件路径>
//	go run ./cmd/parsecheck ../testurl
//
// 输出：trace_count / turns / tool_calls / tokens / duration，以及每一轮的摘要。
// 用于在本地复现"详情页空白 / 指标全 0"问题，定位是解析层还是上层。
package main

import (
	"fmt"
	"os"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/tracelog"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: go run ./cmd/parsecheck <样本文件路径>")
		os.Exit(2)
	}
	path := os.Args[1]

	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("读取文件失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("== 文件: %s (%d 字节) ==\n", path, len(raw))
	if len(raw) == 0 {
		fmt.Println("文件为空。请把一份真实日志样本粘贴进该文件后重试。")
		os.Exit(1)
	}

	pr, err := tracelog.Parse(raw)
	if err != nil {
		fmt.Printf("解析失败(Parse error): %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("session_id = %q\n", pr.SessionID)
	fmt.Printf("rounds(轮次) = %d\n", len(pr.Rounds))

	if len(pr.Rounds) == 0 {
		fmt.Println("\n[诊断] 解析成功但 rounds=0 —— 这就是详情页空白/指标全 0 的根因。")
		fmt.Println("说明日志格式与 parser 期望的事件结构不匹配（logId/promptId/RESPONSE_BODY_FINAL 等）。")
		fmt.Println("下一步：检查样本里事件的 type 字段和层级，对照 parser.go 的解析分支。")
		return
	}

	var totalIn, totalOut, totalReason, totalDur int64
	var totalCalls int
	for i, r := range pr.Rounds {
		dur := r.EndedMs - r.StartedMs
		if dur < 0 {
			dur = 0
		}
		totalIn += r.InputTokens
		totalOut += r.OutputTokens
		totalReason += r.ReasoningTokens
		totalDur += dur
		totalCalls += len(r.Calls)

		prompt := r.UserPrompt
		if rs := []rune(prompt); len(rs) > 40 {
			prompt = string(rs[:40]) + "…"
		}
		fmt.Printf("  round[%d] prompt=%q calls(LLM调用)=%d dur=%dms in=%d out=%d reason=%d\n",
			i, prompt, len(r.Calls), dur, r.InputTokens, r.OutputTokens, r.ReasoningTokens)
	}

	fmt.Println("\n== 汇总 ==")
	fmt.Printf("trace_count = %d\n", len(pr.Rounds))
	fmt.Printf("llm_calls   = %d\n", totalCalls)
	fmt.Printf("duration_ms = %d\n", totalDur)
	fmt.Printf("tokens      = in:%d out:%d reason:%d\n", totalIn, totalOut, totalReason)

	if totalDur == 0 && totalIn == 0 && totalOut == 0 && totalCalls == 0 {
		fmt.Println("\n[诊断] 有 rounds 但指标全 0 —— 说明轮次被识别了，但内部 LLM 调用/usage 没被提取。")
		fmt.Println("重点检查：RESPONSE_BODY_FINAL.data.final.usage 与 logId 关联逻辑。")
	} else {
		fmt.Println("\n[诊断] 解析链路正常，指标非 0。若线上仍空白，问题在 DB 写入或前端渲染，不在解析层。")
	}
}
