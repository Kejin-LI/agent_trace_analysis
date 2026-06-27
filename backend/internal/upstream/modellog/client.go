// Package modellog 封装上游模型日志 Session 查询接口。
//
// 接口契约见 docs：POST {host}/agentic-api/v1/platform/artifact_model_log/session/list
//
// 鉴权方式当前未最终确定（参见 TODO）。Client 把所有鉴权信息做成 Header 注入器，
// 未来确定后只需改 NewClient 构造函数和 environment 配置，不影响调用方。
package modellog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPath    = "/agentic-api/v1/platform/artifact_model_log/session/list"
	defaultTimeout = 15 * time.Second
)

// Client 上游模型日志接口客户端。
//
// 配置通过环境变量注入：
//   - MODEL_LOG_HOST     上游 host（如 https://agentic-aidp.bytedance.net）
//   - MODEL_LOG_PATH     可选，覆盖默认 path
//   - MODEL_LOG_COOKIE   可选，**兜底** SSO Cookie（仅本地调试用；生产应由调用方逐请求透传）
//   - MODEL_LOG_PPE      可选，PPE 分支名（如 ppe_query_model_log），非空时自动加 X-Use-PPE: 1
//   - MODEL_LOG_TIMEOUT  可选，请求超时秒数（默认 15）
type Client struct {
	host           string
	path           string
	http           *http.Client
	headers        map[string]string
	fallbackCookie string
}

// NewClient 从环境变量构造 Client。
// host 缺失时返回错误，调用方应在启动时检测。
func NewClient() (*Client, error) {
	host := strings.TrimRight(strings.TrimSpace(os.Getenv("MODEL_LOG_HOST")), "/")
	if host == "" {
		return nil, fmt.Errorf("MODEL_LOG_HOST is required for DATA_SOURCE=api")
	}

	path := strings.TrimSpace(os.Getenv("MODEL_LOG_PATH"))
	if path == "" {
		path = defaultPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	timeout := defaultTimeout
	if v := os.Getenv("MODEL_LOG_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil && d > 0 {
			timeout = d
		}
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if ppe := os.Getenv("MODEL_LOG_PPE"); ppe != "" {
		headers["X-Use-PPE"] = "1"
		headers["x-tt-env"] = ppe
	}
	// 注：Cookie 不再放进 headers map，改为 List(...) 逐请求传入；
	// MODEL_LOG_COOKIE 仅作 dev/调试兜底。

	return &Client{
		host:           host,
		path:           path,
		http:           &http.Client{Timeout: timeout},
		headers:        headers,
		fallbackCookie: os.Getenv("MODEL_LOG_COOKIE"),
	}, nil
}

// TimeRange 时间窗，闭区间。
// 格式支持 "YYYY-MM-DD HH:mm:ss" / "YYYY-MM-DD HH:mm" / "YYYY-MM-DD"。
type TimeRange struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// Page 分页参数。page_size <= 0 时上游返回全部（注意响应体大小）。
type Page struct {
	PageNo   int `json:"page_no"`
	PageSize int `json:"page_size"`
}

// ListRequest 上游接口请求体。
//
// OnlyUnpublishedArtifacts 为 true 时只查询未发布过的产物关联的 model_log；
// 缺省（false）时接口只查询已发布的 template 产物。
type ListRequest struct {
	TimeRange                TimeRange `json:"time_range"`
	Page                     Page      `json:"page"`
	OnlyUnpublishedArtifacts bool      `json:"only_unpublished_artifacts,omitempty"`
}

// File 单个 TOS JSONL 文件描述。
//
// 注意：上游实际返回 file_list 为字符串数组（URL 字符串），而非对象数组。
// 这里实现 UnmarshalJSON 兼容两种格式：
//   - "https://..."           → File{URL: "..."}
//   - {"name":"...","url":"...","size":"..."} → 完整 File
type File struct {
	Name     string `json:"name,omitempty"`
	URL      string `json:"url"`
	Size     string `json:"size,omitempty"`
	CreateAt string `json:"create_at,omitempty"`
}

// UnmarshalJSON 兼容字符串与对象两种 file 表示形式。
func (f *File) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		f.URL = s
		return nil
	}
	type alias File
	var a alias
	if err := json.Unmarshal(trimmed, &a); err != nil {
		return err
	}
	*f = File(a)
	return nil
}

// Session 聚合后的一个 session（接口按 user_id+artifact_id+session_id 聚合）。
type Session struct {
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	ArtifactID string `json:"artifact_id"`
	SessionID  string `json:"session_id"`
	Size       string `json:"size"`
	FileList   []File `json:"file_list"`
	CreateAt   string `json:"create_at,omitempty"`
	UpdateAt   string `json:"update_at,omitempty"`
}

// flexInt 兼容 int 与字符串数字的 JSON 解码（上游 total 返回字符串）。
type flexInt int

func (f *flexInt) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("flexInt: parse %q: %w", s, err)
		}
		*f = flexInt(n)
		return nil
	}
	var n int
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

// ListResponse 上游响应。
type ListResponse struct {
	Common struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		TraceID string `json:"trace_id,omitempty"`
	} `json:"common"`
	Total flexInt   `json:"total"`
	Data  []Session `json:"data"`
}

// List 调用上游 session/list 接口。
//
// cookie 为本次请求的 Cookie header 内容；为空时回落到 fallbackCookie（env 配置）。
// 生产环境下调用方应直接透传当前用户的 Cookie，确保鉴权逐用户隔离。
//
// 任意 HTTP 非 2xx 或 common.code != 0 都视为错误，调用方收到 nil response。
func (c *Client) List(ctx context.Context, cookie string, req ListRequest) (*ListResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+c.path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}
	if cookie == "" {
		cookie = c.fallbackCookie
	}
	if cookie != "" {
		httpReq.Header.Set("Cookie", cookie)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream http %d: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var out ListResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w; body=%s", err, truncate(string(respBody), 500))
	}
	if out.Common.Code != 0 {
		return nil, fmt.Errorf("upstream code=%d msg=%s", out.Common.Code, out.Common.Message)
	}
	return &out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
