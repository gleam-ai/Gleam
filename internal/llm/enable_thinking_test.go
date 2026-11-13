package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// captureThinking spins up a local endpoint that records the enable_thinking
// field of each request body, then reports whether the key was present and its
// value. A local httptest server keeps the probe hermetic.
func captureThinking(t *testing.T, build func(url string) Client) (present bool, value bool) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		v, ok := m["enable_thinking"]
		present = ok
		if ok {
			value, _ = v.(bool)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"role": "assistant", "content": "ok"},
			}},
		})
	}))
	defer srv.Close()

	c := build(srv.URL)
	if _, err := c.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	return present, value
}

func TestEnableThinkingOmittedWhenUnset(t *testing.T) {
	present, _ := captureThinking(t, func(url string) Client {
		return NewGLM(url, "key", "m", 0.3, 128, 5*time.Second)
	})
	if present {
		t.Error("未配置时不应发送 enable_thinking（让模型保留自身默认）")
	}
}

func TestEnableThinkingSentVerbatim(t *testing.T) {
	for _, want := range []bool{false, true} {
		v := want
		present, got := captureThinking(t, func(url string) Client {
			c := NewGLM(url, "key", "m", 0.3, 128, 5*time.Second)
			c.EnableThinking = &v
			return c
		})
		if !present || got != want {
			t.Errorf("enable_thinking 应原样发送 %v，实际 present=%v value=%v", want, present, got)
		}
	}
}

func TestWithEnableThinkingOption(t *testing.T) {
	v := false
	present, got := captureThinking(t, func(url string) Client {
		return New(ProtocolOpenAIChat, url, "key", "m", 0.3, 128, 5, WithEnableThinking(&v))
	})
	if !present || got != false {
		t.Errorf("WithEnableThinking(false) 应发送 enable_thinking=false，实际 present=%v value=%v", present, got)
	}
}

func TestWithEnableThinkingNoopOnOtherProtocols(t *testing.T) {
	v := false
	// The option targets OpenAI-chat clients only; it must not panic or alter
	// the Anthropic client, which has no enable_thinking field.
	c := New(ProtocolAnthropic, "http://x", "key", "m", 0.3, 128, 5, WithEnableThinking(&v))
	if _, ok := c.(*AnthropicClient); !ok {
		t.Fatalf("期望 Anthropic 客户端，实际 %T", c)
	}
}
