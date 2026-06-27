// Package ark 封装火山方舟（豆包 2.0）大模型 Chat Completions 接口。
//
// 方舟 V3 完全兼容 OpenAI 协议，端点为 {base_url}/chat/completions。
// 鉴权 / 端点配置全部来自 TCC 加密配置，密钥绝不落代码与前端。
//
// TCC 配置项：agentic_trace_server.ark.config（加密 JSON）
//
//	{
//	  "base_url": "https://ark.cn-beijing.volces.com/api/v3",
//	  "api_key":  "方舟 API Key",
//	  "model":    "豆包 2.0 Pro 接入点 ID（ep-xxx）"
//	}
package ark

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/secret"
)

// ConfigKey 是 TCC 上存放方舟配置的配置项名。
const ConfigKey = "agentic_trace_server.ark.config"

const (
	defaultBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	chatPath       = "/chat/completions"
	defaultTimeout = 120 * time.Second
)

// Config 方舟接入配置（从 TCC 读取）。
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

// LoadConfig 从 TCC 读取方舟配置并校验必填项。
func LoadConfig(ctx context.Context) (Config, error) {
	m, err := secret.GetEncryptedJSON(ctx, ConfigKey)
	if err != nil {
		return Config{}, fmt.Errorf("读取 TCC 配置 %s 失败: %w", ConfigKey, err)
	}
	cfg := Config{
		BaseURL: strings.TrimRight(strings.TrimSpace(m["base_url"]), "/"),
		APIKey:  strings.TrimSpace(m["api_key"]),
		Model:   strings.TrimSpace(m["model"]),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 api_key", ConfigKey)
	}
	if cfg.Model == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 model（豆包2.0 接入点 ID）", ConfigKey)
	}
	return cfg, nil
}

// Message OpenAI 兼容消息体。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// streamChunk 是 SSE 每个 data 块的精简解构。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Client 方舟客户端。每次调用时按当前 TCC 配置发起请求，配置变更可平滑生效。
type Client struct {
	http *http.Client
}

// NewClient 构造一个带默认超时的客户端。
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: defaultTimeout}}
}

// StreamChat 以流式方式调用方舟 chat/completions，逐块把模型增量文本通过 onDelta 回调吐出。
//
// onDelta 每收到一段非空增量就被调用一次；调用方负责把它转发给前端 SSE。
// 任意网络/协议错误都会返回 error；正常结束（收到 [DONE]）返回 nil。
func (c *Client) StreamChat(ctx context.Context, cfg Config, messages []Message, onDelta func(string)) error {
	reqBody, err := json.Marshal(chatRequest{
		Model:    cfg.Model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+chatPath, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("方舟返回 HTTP %d: %s", resp.StatusCode, truncate(buf.String(), 500))
	}

	scanner := bufio.NewScanner(resp.Body)
	// SSE 单条 data 可能较大，放宽缓冲上限到 1MB。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // 跳过无法解析的心跳/控制块
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				onDelta(ch.Delta.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
