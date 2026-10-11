package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type GLMClient struct {
	BaseURL       string
	APIKey        string
	Model         string
	Temperature   float64
	MaxTokens     int
	HTTP          *http.Client
	noStreamUsage atomic.Bool
}

func NewGLM(baseURL, apiKey, model string, temperature float64, maxTokens int, timeout time.Duration) *GLMClient {
	return &GLMClient{
		BaseURL:     normalizeBase(baseURL, "/chat/completions"),
		APIKey:      apiKey,
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		HTTP:        &http.Client{Timeout: timeout, Transport: Transport()},
	}
}

func (c *GLMClient) Name() string { return c.Model }

type chatMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type chatRequestPayload struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	Temperature   float64        `json:"temperature"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatChoice struct {
	Message      *chatMessage `json:"message"`
	Delta        *chatMessage `json:"delta"`
	FinishReason string       `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens         int `json:"prompt_tokens"`
	CompletionTokens     int `json:"completion_tokens"`
	TotalTokens          int `json:"total_tokens"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u *chatUsage) cached() int {
	if u == nil {
		return 0
	}
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	if u.PromptTokensDetails != nil {
		return u.PromptTokensDetails.CachedTokens
	}
	return 0
}

func (u *chatUsage) toUsage() Usage {
	if u == nil {
		return Usage{}
	}
	return Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CachedTokens:     u.cached(),
	}
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *GLMClient) buildMessages(req ChatRequest) []chatMessage {
	msgs := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: RoleSystem, Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	return msgs
}

func (c *GLMClient) payload(req ChatRequest, stream bool) chatRequestPayload {
	temp := req.Temperature
	if temp == 0 {
		temp = c.Temperature
	}
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = c.MaxTokens
	}
	p := chatRequestPayload{
		Model:       c.Model,
		Messages:    c.buildMessages(req),
		Temperature: temp,
		MaxTokens:   maxTok,
		Stream:      stream,
	}
	if stream && !c.noStreamUsage.Load() {
		p.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	return p
}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("llm: HTTP %d: %s", e.code, e.msg)
}

func (c *GLMClient) post(ctx context.Context, payload chatRequestPayload) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("llm: 序列化请求失败: %w", err)
	}
	return DoWithRetry(ctx, DefaultRetryConfig, func(err error) bool { return retryable(ctx, err) }, func() (*http.Response, error) {
		return c.do(ctx, body)
	})
}

func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.code == http.StatusTooManyRequests ||
			se.code == http.StatusBadGateway ||
			se.code == http.StatusServiceUnavailable ||
			se.code == http.StatusGatewayTimeout
	}
	return true
}

func (c *GLMClient) do(ctx context.Context, body []byte) (*http.Response, error) {
	noteEgress(c.BaseURL+"/chat/completions", len(body))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: 请求失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, &statusError{code: resp.StatusCode, msg: strings.TrimSpace(string(snippet))}
	}
	return resp, nil
}

func (c *GLMClient) Chat(ctx context.Context, req ChatRequest) (string, error) {
	resp, err := c.post(ctx, c.payload(req, false))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", fmt.Errorf("llm: 解析响应失败: %w", err)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("llm: API 错误: %s", cr.Error.Message)
	}
	var u Usage
	if cr.Usage != nil {
		u = cr.Usage.toUsage()
	}
	for _, ch := range cr.Choices {
		if ch.Message != nil {
			ReportUsage(req, ch.Message.Content, u)
			return ch.Message.Content, nil
		}
	}
	return "", fmt.Errorf("llm: 响应中无内容")
}

func (c *GLMClient) ChatStream(ctx context.Context, req ChatRequest, onDelta func(string)) (string, error) {
	resp, err := c.post(ctx, c.payload(req, true))
	if err != nil {
		if c.wantsStreamUsage() && rejectedStreamOptions(err) {
			c.noStreamUsage.Store(true)
			resp, err = c.post(ctx, c.payload(req, true))
		}
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	var full strings.Builder
	var streamUsage *chatUsage
	err = scanStream(ctx, resp.Body, func(line string) bool {
		if !strings.HasPrefix(line, "data:") {
			return true
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			return true
		}
		if data == "[DONE]" {
			return false
		}
		var cr chatResponse
		if err := json.Unmarshal([]byte(data), &cr); err != nil {
			return true
		}
		if cr.Usage != nil {
			streamUsage = cr.Usage
		}
		for _, ch := range cr.Choices {
			if ch.Delta != nil {
				if ch.Delta.ReasoningContent != "" {
					full.WriteString(ch.Delta.ReasoningContent)
				}
				if ch.Delta.Content != "" {
					full.WriteString(ch.Delta.Content)
					if onDelta != nil {
						onDelta(ch.Delta.Content)
					}
				}
			}
		}
		return true
	})
	if err != nil {
		return full.String(), err
	}
	if full.Len() == 0 {
		return "", fmt.Errorf("llm: 流式响应为空")
	}
	ReportUsage(req, full.String(), streamUsage.toUsage())
	return full.String(), nil
}

func (c *GLMClient) wantsStreamUsage() bool { return !c.noStreamUsage.Load() }

func rejectedStreamOptions(err error) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	return se.code == http.StatusBadRequest || se.code == http.StatusUnprocessableEntity
}
