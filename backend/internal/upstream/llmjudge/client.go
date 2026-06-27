// Package llmjudge 调用 TCC 中配置的 GPT-5.5 Responses 接口。
package llmjudge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	ResponseFormat  string
}

type rawModelConfig struct {
	Temperature     json.RawMessage `json:"temperature"`
	MaxTokens       json.RawMessage `json:"max_tokens"`
	MaxOutputTokens json.RawMessage `json:"max_output_tokens"`
}

type rawResponseFormat struct {
	Type string `json:"type"`
}

type rawConfig struct {
	Enabled         *bool             `json:"enabled"`
	BaseURL         string            `json:"base_url"`
	APIKey          string            `json:"api_key"`
	APIKeyCamel     string            `json:"apiKey"`
	Model           string            `json:"model"`
	Endpoint        string            `json:"endpoint"`
	TimeoutMS       json.RawMessage   `json:"timeout_ms"`
	Temperature     json.RawMessage   `json:"temperature"`
	MaxOutputTokens json.RawMessage   `json:"max_output_tokens"`
	ResponseFormat  rawResponseFormat `json:"response_format"`
	ModelConfig     rawModelConfig    `json:"modelConfig"`
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
	apiKey := strings.TrimSpace(rc.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(rc.APIKeyCamel)
	}
	temperature := floatFromRaw(rc.Temperature, floatFromRaw(rc.ModelConfig.Temperature, 0))
	maxOutputTokens := intFromRaw(rc.MaxOutputTokens, intFromRaw(rc.ModelConfig.MaxOutputTokens, intFromRaw(rc.ModelConfig.MaxTokens, 1200)))
	responseFormat := strings.TrimSpace(rc.ResponseFormat.Type)
	if responseFormat == "" {
		responseFormat = "json_object"
	}
	cfg := Config{
		Enabled:         true,
		BaseURL:         strings.TrimRight(strings.TrimSpace(rc.BaseURL), "/"),
		APIKey:          apiKey,
		Model:           strings.TrimSpace(rc.Model),
		Endpoint:        strings.TrimSpace(rc.Endpoint),
		TimeoutMS:       intFromRaw(rc.TimeoutMS, 60000),
		Temperature:     temperature,
		MaxOutputTokens: maxOutputTokens,
		ResponseFormat:  responseFormat,
	}
	if rc.Enabled != nil {
		cfg.Enabled = *rc.Enabled
	}
	if !cfg.Enabled {
		return Config{}, fmt.Errorf("TCC 配置 %s 已禁用", ConfigKey)
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 api_key", ConfigKey)
	}
	if cfg.Model == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 model", ConfigKey)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" && strings.TrimSpace(cfg.Endpoint) == "" {
		return Config{}, fmt.Errorf("TCC 配置 %s 缺少 base_url / endpoint", ConfigKey)
	}
	return cfg, nil
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesRequest struct {
	Model           string                 `json:"model"`
	Instructions    string                 `json:"instructions,omitempty"`
	Input           string                 `json:"input"`
	Temperature     float64                `json:"temperature,omitempty"`
	MaxOutputTokens int                    `json:"max_output_tokens,omitempty"`
	Text            map[string]interface{} `json:"text,omitempty"`
}

type responsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error json.RawMessage `json:"error"`
}

// errorMessage 从上游 error 字段里尽量提取一句可读信息（兼容对象 / 字符串 / 其它类型）。
func errorMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var obj struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && (obj.Message != "" || obj.Code != "") {
		if obj.Message != "" {
			return obj.Message
		}
		return obj.Code
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return s
	}
	return string(raw)
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

	instructions, input := splitMessages(messages)
	reqURL, err := resolveRequestURL(cfg)
	if err != nil {
		return "", err
	}

	reqBody, err := json.Marshal(responsesRequest{
		Model:           cfg.Model,
		Instructions:    instructions,
		Input:           input,
		Temperature:     cfg.Temperature,
		MaxOutputTokens: cfg.MaxOutputTokens,
		Text: map[string]interface{}{
			"format": map[string]interface{}{
				"type": cfg.ResponseFormat,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var out responsesResponse
	decodeErr := json.Unmarshal(body, &out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg := errorMessage(out.Error); msg != "" {
			return "", fmt.Errorf("GPT-5.5 返回 HTTP %d: %s", resp.StatusCode, msg)
		}
		return "", fmt.Errorf("GPT-5.5 返回 HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	if decodeErr != nil {
		return "", fmt.Errorf("decode response: %w (原始返回: %s)", decodeErr, snippet(body))
	}
	if msg := errorMessage(out.Error); msg != "" {
		return "", fmt.Errorf("GPT-5.5 返回错误: %s", msg)
	}
	text := extractOutputText(out)
	if text == "" {
		return "", fmt.Errorf("GPT-5.5 返回为空 (原始返回: %s)", snippet(body))
	}
	return text, nil
}

// snippet 截断原始响应，避免错误信息过长。
func snippet(b []byte) string {
	const max = 500
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func splitMessages(messages []Message) (instructions string, input string) {
	var systemParts []string
	var inputParts []string
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(msg.Role)) {
		case "system":
			systemParts = append(systemParts, content)
		case "user":
			inputParts = append(inputParts, content)
		default:
			inputParts = append(inputParts, "["+msg.Role+"]\n"+content)
		}
	}
	return strings.Join(systemParts, "\n\n"), strings.Join(inputParts, "\n\n")
}

func resolveRequestURL(cfg Config) (string, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if isAbsoluteURL(endpoint) {
		return endpoint, nil
	}
	if endpoint == "" {
		if strings.HasSuffix(baseURL, "/responses") {
			return baseURL, nil
		}
		if baseURL == "" {
			return "", fmt.Errorf("TCC 配置 %s 缺少可用 endpoint", ConfigKey)
		}
		return baseURL + "/responses", nil
	}
	if baseURL == "" {
		return "", fmt.Errorf("TCC 配置 %s 缺少 base_url", ConfigKey)
	}
	if strings.HasPrefix(endpoint, "/") {
		return baseURL + endpoint, nil
	}
	return baseURL + "/" + endpoint, nil
}

func isAbsoluteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func extractOutputText(out responsesResponse) string {
	if text := strings.TrimSpace(out.OutputText); text != "" {
		return text
	}
	var parts []string
	for _, item := range out.Output {
		for _, content := range item.Content {
			text := strings.TrimSpace(content.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
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
