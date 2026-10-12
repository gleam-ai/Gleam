package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ResponsesClient struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	MaxTokens   int
	HTTP        *http.Client
}

func NewResponses(baseURL, apiKey, model string, temperature float64, maxTokens int, timeout time.Duration) *ResponsesClient {
	return &ResponsesClient{
		BaseURL:     normalizeBase(baseURL, "/responses"),
		APIKey:      apiKey,
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		HTTP:        &http.Client{Timeout: timeout, Transport: Transport()},
	}
}

func (c *ResponsesClient) Name() string { return c.Model }

func (c *ResponsesClient) headers() map[string]string {
	h := map[string]string{}
	if c.APIKey != "" {
		h["Authorization"] = "Bearer " + c.APIKey
	}
	return h
}

type responsesPayload struct {
	Model           string             `json:"model"`
	Instructions    string             `json:"instructions,omitempty"`
	Input           []responsesMessage `json:"input"`
	Temperature     float64            `json:"temperature,omitempty"`
	MaxOutputTokens int                `json:"max_output_tokens,omitempty"`
	Stream          bool               `json:"stream,omitempty"`
}

type responsesMessage struct {
	Role    string           `json:"role"`
	Content []map[string]any `json:"content"`
}

func (c *ResponsesClient) payload(req ChatRequest, stream bool) responsesPayload {
	temp := req.Temperature
	if temp == 0 {
		temp = c.Temperature
	}
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = c.MaxTokens
	}
	in := make([]responsesMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		in = append(in, responsesMessage{Role: m.Role, Content: []map[string]any{
			{"type": "input_text", "text": m.Content},
		}})
	}
	return responsesPayload{
		Model: c.Model, Instructions: req.System, Input: in,
		Temperature: temp, MaxOutputTokens: maxTok, Stream: stream,
	}
}

type responsesOutput struct {
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	OutputText string `json:"output_text"`
	Usage      *struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		TotalTokens        int `json:"total_tokens"`
		InputTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func textFromOutput(raw []byte) (string, Usage, error) {
	var out responsesOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", Usage{}, fmt.Errorf("llm: 解析响应失败: %w", err)
	}
	if out.Error != nil {
		return "", Usage{}, fmt.Errorf("llm: API 错误: %s", out.Error.Message)
	}
	var u Usage
	if out.Usage != nil {
		u = Usage{PromptTokens: out.Usage.InputTokens, CompletionTokens: out.Usage.OutputTokens}
		if out.Usage.InputTokensDetails != nil {
			u.CachedTokens = out.Usage.InputTokensDetails.CachedTokens
		}
	}
	var b strings.Builder
	for _, item := range out.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" {
				b.WriteString(part.Text)
			}
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		return s, u, nil
	}
	if s := strings.TrimSpace(out.OutputText); s != "" {
		return s, u, nil
	}
	return "", u, fmt.Errorf("llm: 响应中无内容")
}

func (c *ResponsesClient) Chat(ctx context.Context, req ChatRequest) (string, error) {
	body, err := json.Marshal(c.payload(req, false))
	if err != nil {
		return "", err
	}
	raw, err := postJSON(ctx, c.HTTP, c.BaseURL+"/responses", c.headers(), body)
	if err != nil {
		return "", err
	}
	text, u, err := textFromOutput(raw)
	if err != nil {
		return "", err
	}
	ReportUsage(req, text, u)
	return text, nil
}

func (c *ResponsesClient) ChatStream(ctx context.Context, req ChatRequest, onDelta func(string)) (string, error) {
	body, err := json.Marshal(c.payload(req, true))
	if err != nil {
		return "", err
	}
	rc, err := openStream(ctx, c.HTTP, c.BaseURL+"/responses", c.headers(), body)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	var full strings.Builder
	var u Usage
	err = scanStream(ctx, rc, func(line string) bool {
		if !strings.HasPrefix(line, "data:") {
			return true
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			return true
		}
		var ev struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response *struct {
				Usage *struct {
					InputTokens        int `json:"input_tokens"`
					OutputTokens       int `json:"output_tokens"`
					InputTokensDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return true
		}
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				full.WriteString(ev.Delta)
				if onDelta != nil {
					onDelta(ev.Delta)
				}
			}
		case "response.completed", "response.failed", "response.incomplete":
			if ev.Response != nil && ev.Response.Usage != nil {
				ru := ev.Response.Usage
				u = Usage{PromptTokens: ru.InputTokens, CompletionTokens: ru.OutputTokens}
				if ru.InputTokensDetails != nil {
					u.CachedTokens = ru.InputTokensDetails.CachedTokens
				}
			}
			return false
		}
		return true
	})
	if err != nil {
		return full.String(), err
	}
	if full.Len() == 0 {
		return "", fmt.Errorf("llm: 流式响应为空")
	}
	ReportUsage(req, full.String(), u)
	return full.String(), nil
}

var _ Client = (*ResponsesClient)(nil)
