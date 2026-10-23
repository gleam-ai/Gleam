package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AnthropicClient struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	MaxTokens   int
	HTTP        *http.Client
	timeout     time.Duration
}

func NewAnthropic(baseURL, apiKey, model string, temperature float64, maxTokens int, timeout time.Duration) *AnthropicClient {
	return &AnthropicClient{
		BaseURL:     normalizeBase(baseURL, "/v1/messages", "/messages"),
		APIKey:      apiKey,
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		HTTP:        &http.Client{Transport: Transport()},
		timeout:     timeout,
	}
}

func (c *AnthropicClient) Name() string { return c.Model }

func (c *AnthropicClient) endpoint() string {
	if strings.HasSuffix(c.BaseURL, "/v1") {
		return c.BaseURL + "/messages"
	}
	return c.BaseURL + "/v1/messages"
}

func (c *AnthropicClient) headers() map[string]string {
	h := map[string]string{
		"anthropic-version": "2023-06-01",
	}
	if c.APIKey != "" {
		h["x-api-key"] = c.APIKey
	}
	return h
}

type anthropicPayload struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      any                `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicSystemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
}

const anthropicCacheMinTokens = 1024

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *AnthropicClient) payload(req ChatRequest, stream bool) anthropicPayload {
	temp := req.Temperature
	if temp == 0 && !req.TemperatureSet {
		temp = c.Temperature
	}
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = c.MaxTokens
	}
	if maxTok <= 0 {
		maxTok = 4096
	}
	msgs := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, anthropicMessage{Role: m.Role, Content: m.Content})
	}
	var system any
	if req.System != "" {
		if EstimateTokens(req.System) >= anthropicCacheMinTokens {
			system = []anthropicSystemBlock{{
				Type: "text", Text: req.System,
				CacheControl: &cacheControl{Type: "ephemeral"},
			}}
		} else {
			system = req.System
		}
	}
	return anthropicPayload{
		Model: c.Model, MaxTokens: maxTok, System: system, Messages: msgs,
		Temperature: temp, Stream: stream,
	}
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *AnthropicClient) Chat(ctx context.Context, req ChatRequest) (string, error) {
	ctx, cancel := withRequestTimeout(ctx, c.timeout)
	defer cancel()
	body, err := json.Marshal(c.payload(req, false))
	if err != nil {
		return "", err
	}
	raw, err := postJSON(ctx, c.HTTP, c.endpoint(), c.headers(), body)
	if err != nil {
		return "", err
	}
	var out anthropicResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm: 解析响应失败: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("llm: API 错误: %s", out.Error.Message)
	}
	var b strings.Builder
	for _, part := range out.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("llm: 响应中无内容")
	}
	var u Usage
	if out.Usage != nil {
		u = Usage{
			PromptTokens:     out.Usage.InputTokens + out.Usage.CacheReadInputTokens + out.Usage.CacheCreationInputTokens,
			CompletionTokens: out.Usage.OutputTokens,
			CachedTokens:     out.Usage.CacheReadInputTokens,
		}
	}
	ReportUsage(req, b.String(), u)
	return b.String(), nil
}

func (c *AnthropicClient) ChatStream(ctx context.Context, req ChatRequest, onDelta func(string)) (string, error) {
	body, err := json.Marshal(c.payload(req, true))
	if err != nil {
		return "", err
	}
	rc, err := openStream(ctx, c.HTTP, c.endpoint(), c.headers(), body)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	var full strings.Builder
	var u Usage
	var streamErr error
	err = scanStream(ctx, rc, func(line string) bool {
		if !strings.HasPrefix(line, "data:") {
			return true
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			return true
		}
		var ev struct {
			Type  string `json:"type"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
			Message *struct {
				Usage *struct {
					InputTokens              int `json:"input_tokens"`
					OutputTokens             int `json:"output_tokens"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return true
		}
		if ev.Type == "error" || ev.Type == "message_error" {
			if ev.Error != nil && ev.Error.Message != "" {
				streamErr = fmt.Errorf("llm: 流式响应错误(%s): %s", ev.Error.Type, ev.Error.Message)
			} else {
				streamErr = fmt.Errorf("llm: 流式响应错误")
			}
			return false
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				mu := ev.Message.Usage
				u.PromptTokens = mu.InputTokens + mu.CacheReadInputTokens + mu.CacheCreationInputTokens
				u.CachedTokens = mu.CacheReadInputTokens
				u.CompletionTokens = mu.OutputTokens
			}
		case "message_delta":
			if ev.Usage != nil {
				if ev.Usage.OutputTokens > 0 {
					u.CompletionTokens = ev.Usage.OutputTokens
				}
				if ev.Usage.InputTokens > 0 {
					u.PromptTokens = ev.Usage.InputTokens + ev.Usage.CacheReadInputTokens + ev.Usage.CacheCreationInputTokens
				}
				if ev.Usage.CacheReadInputTokens > 0 {
					u.CachedTokens = ev.Usage.CacheReadInputTokens
				}
			}
			if ev.Delta.Text != "" {
				full.WriteString(ev.Delta.Text)
				if onDelta != nil {
					onDelta(ev.Delta.Text)
				}
			}
		case "content_block_delta":
			if ev.Delta.Text != "" {
				full.WriteString(ev.Delta.Text)
				if onDelta != nil {
					onDelta(ev.Delta.Text)
				}
			}
		case "message_stop":
			return false
		}
		return true
	})
	if err != nil {
		return full.String(), err
	}
	if streamErr != nil {
		return full.String(), streamErr
	}
	if full.Len() == 0 {
		return "", fmt.Errorf("llm: 流式响应为空")
	}
	ReportUsage(req, full.String(), u)
	return full.String(), nil
}

var _ Client = (*AnthropicClient)(nil)
