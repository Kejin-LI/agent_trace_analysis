// Package llmjudge 调用 TCC 中配置的 GPT-5.5 OpenAI 兼容接口。
package llmjudge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/secret"
)

const ConfigKey = "agenttrace_llm_judge_gpt55"

type Config struct {
	Enabled         bool
	BaseURL         string
	APIKey          string
	Model           string
	Endpoint        string
	TimeoutMS       int
	Temperature     float64
	MaxOutputTokens int
}

type rawConfig struct {
	Enabled         *bool           `json:"enabled"`
	BaseURL         string          `json:"base_url"`
	APIKey          string          `json:"api_key"`
	Model           string          `json:"model"`
	Endpoint        string          `json:"endpoint"`
	TimeoutMS       json.RawMessage `json:"timeout_ms"`
	Temperature     json.RawMessage `json:"temperature"`
	MaxOutputTokens json.RawMessage `json:"max_output_tokens"`
}

func LoadConfig(ctx context.Context) (Config, error) {
	raw, err := secret.GetEncrypted(ctx, ConfigKey)
	if err != nil {
		return Config{}, fmt.Errorf("读取 TCC 配置 %s 失败: %w", ConfigKey, err)
	}
	var rc rawConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		return Config{}, fmt.Errorf("TCC 配置 %s 不是合法 JSON: %w", ConfigKey, err)
	}
	cfg := Config{
		Enabled:         true,
		BaseURL:         strings.TrimRight(strings.TrimSpace(rc.BaseURL), "/"),
		APIKey:          strings.TrimSpace(rc.APIKey),
		Model:           strings.TrimSpace(rc.Model),
		Endpoint:        strings.TrimSpace(rc.Endpoint),
		TimeoutMS:       intFromRaw(rc.TimeoutMS, 60000),
		Temperature:     floatFromRaw(rc.Temperature, 0),
		MaxOutputTokens: intFromRaw(rc.MaxOutputTokens, 1200),
	}
	if rc.Enabled != nil {
		cfg.Enabled = *rc.Enabled
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "/chat/completions"
	}
	if !strings.HasPrefix(cfg.Endpoint, "/") {
		cfg.Endpoint = "/" + cfg.Endpoint
	}
	if !cfg.Enabled {
		return Config{}, fmt.Errorf("TCC 配置 %s 已禁用", ConfigKey)
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 base_url", ConfigKey)
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 api_key", ConfigKey)
	}
	if cfg.Model == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 model", ConfigKey)
	}
	return cfg, nil
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string                 `json:"model"`
	Messages       []Message              `json:"messages"`
	Stream         bool                   `json:"stream"`
	Temperature    float64                `json:"temperature"`
	MaxTokens      int                    `json:"max_tokens,omitempty"`
	ResponseFormat map[string]interface{} `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) ChatJSON(ctx context.Context, cfg Config, messages []Message) (string, error) {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := *c.http
	client.Timeout = timeout

	reqBody, err := json.Marshal(chatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Stream:      false,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		ResponseFormat: map[string]interface{}{
			"type": "json_object",
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+cfg.Endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if out.Error.Message != "" {
			return "", fmt.Errorf("GPT-5.5 返回 HTTP %d: %s", resp.StatusCode, out.Error.Message)
		}
		return "", fmt.Errorf("GPT-5.5 返回 HTTP %d", resp.StatusCode)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("GPT-5.5 返回为空")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func intFromRaw(raw json.RawMessage, fallback int) int {
	if len(raw) == 0 {
		return fallback
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		var parsed int
		if _, err := fmt.Sscanf(s, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

func floatFromRaw(raw json.RawMessage, fallback float64) float64 {
	if len(raw) == 0 {
		return fallback
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		var parsed float64
		if _, err := fmt.Sscanf(s, "%f", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}
