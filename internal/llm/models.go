package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	OwnedBy     string `json:"owned_by,omitempty"`
}

func modelEndpoint(protocol, baseURL string) string {
	switch protocol {
	case ProtocolAnthropic:
		base := normalizeBase(baseURL, "/v1/messages", "/messages", "/v1/models", "/models")
		if strings.HasSuffix(base, "/v1") {
			return base + "/models"
		}
		return base + "/v1/models"
	default:
		base := normalizeBase(baseURL, "/chat/completions", "/responses", "/models")
		return base + "/models"
	}
}

func modelHeaders(protocol, apiKey string) map[string]string {
	h := map[string]string{}
	switch protocol {
	case ProtocolAnthropic:
		h["anthropic-version"] = "2023-06-01"
		if apiKey != "" {
			h["x-api-key"] = apiKey
		}
	default:
		if apiKey != "" {
			h["Authorization"] = "Bearer " + apiKey
		}
	}
	return h
}

func getList(ctx context.Context, hc *http.Client, url string, headers map[string]string) ([]byte, error) {
	noteEgress(url, 0)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, &statusError{code: resp.StatusCode, msg: string(snippet)}
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}

func parseModelsResponse(raw []byte) ([]ModelInfo, error) {
	var entry struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		OwnedBy     string `json:"owned_by"`
	}
	var payload struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("llm: 解析模型列表失败: %w", err)
	}
	if payload.Error != nil {
		return nil, fmt.Errorf("llm: API 错误: %s", payload.Error.Message)
	}
	items := payload.Data
	if len(items) == 0 {
		items = payload.Models
	}
	out := make([]ModelInfo, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		if err := json.Unmarshal(it, &entry); err != nil {
			continue
		}
		id := entry.ID
		if id == "" {
			id = entry.Name
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, ModelInfo{ID: id, DisplayName: entry.DisplayName, OwnedBy: entry.OwnedBy})
	}
	return out, nil
}

func ListModels(ctx context.Context, protocol, baseURL, apiKey string, timeout time.Duration) ([]ModelInfo, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	hc := &http.Client{Timeout: timeout, Transport: Transport()}
	raw, err := getList(ctx, hc, modelEndpoint(protocol, baseURL), modelHeaders(protocol, apiKey))
	if err != nil {
		return nil, err
	}
	return parseModelsResponse(raw)
}
