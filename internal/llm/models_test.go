package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModelEndpoint(t *testing.T) {
	cases := []struct{ name, protocol, base, want string }{
		{"chat-智谱v4", ProtocolOpenAIChat, "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/models"},
		{"chat-粘了完整端点", ProtocolOpenAIChat, "https://x.example/v1/chat/completions", "https://x.example/v1/models"},
		{"chat-尾斜杠", ProtocolOpenAIChat, "https://x.example/v1/", "https://x.example/v1/models"},
		{"responses", ProtocolOpenAIResponses, "https://api.openai.com/v1", "https://api.openai.com/v1/models"},
		{"anthropic-v1结尾", ProtocolAnthropic, "https://api.anthropic.com/v1", "https://api.anthropic.com/v1/models"},
		{"anthropic-无v1", ProtocolAnthropic, "https://open.bigmodel.cn/api/anthropic", "https://open.bigmodel.cn/api/anthropic/v1/models"},
		{"anthropic-粘了messages", ProtocolAnthropic, "https://api.anthropic.com/v1/messages", "https://api.anthropic.com/v1/models"},
		{"anthropic-粘了models", ProtocolAnthropic, "https://api.anthropic.com/v1/models", "https://api.anthropic.com/v1/models"},
	}
	for _, c := range cases {
		if got := modelEndpoint(c.protocol, c.base); got != c.want {
			t.Errorf("%s: %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestParseModelsResponse(t *testing.T) {
	openai := []byte(`{"data":[{"id":"gpt-a","owned_by":"openai"},{"id":"gpt-b","owned_by":"openai"},{"id":"gpt-a","owned_by":"dup"}]}`)
	ms, err := parseModelsResponse(openai)
	if err != nil || len(ms) != 2 || ms[0].ID != "gpt-a" || ms[0].OwnedBy != "openai" {
		t.Fatalf("data 形态 = %v, %v", ms, err)
	}
	zhipu := []byte(`{"models":[{"id":"glm-5.3-flash"},{"id":"glm-5.3"}]}`)
	ms, err = parseModelsResponse(zhipu)
	if err != nil || len(ms) != 2 || ms[1].ID != "glm-5.3" {
		t.Fatalf("models 形态 = %v, %v", ms, err)
	}
	anth := []byte(`{"data":[{"type":"model","id":"claude-x","display_name":"Claude X"},"string-junk",{"no_id":1}]}`)
	ms, err = parseModelsResponse(anth)
	if err != nil || len(ms) != 1 || ms[0].DisplayName != "Claude X" {
		t.Fatalf("anthropic 形态 = %v, %v", ms, err)
	}
	if _, err := parseModelsResponse([]byte(`{"error":{"message":"quota exceeded"}}`)); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("error 应冒泡，得到 %v", err)
	}
}

func TestListModels_HTTP(t *testing.T) {
	var gotAuth, gotXKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`no route`))
			return
		}
		gotAuth = r.Header.Get("Authorization")
		gotXKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		if gotAuth == "Bearer k1" || gotXKey == "k1" {
			_, _ = w.Write([]byte(`{"data":[{"id":"m-one"},{"id":"m-two"}]}`))
			return
		}
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	ms, err := ListModels(context.Background(), ProtocolOpenAIChat, srv.URL+"/v1", "k1", 5*time.Second)
	if err != nil || len(ms) != 2 {
		t.Fatalf("拉取失败: %v %v", ms, err)
	}
	if gotAuth != "Bearer k1" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if _, err := ListModels(context.Background(), ProtocolOpenAIChat, srv.URL+"/v1", "", 5*time.Second); StatusCode(err) != 401 {
		t.Errorf("空 key 应得 401，得到 %v", err)
	}
	if gotAuth != "" {
		t.Errorf("空 key 不应再发 Authorization，得到 %q", gotAuth)
	}
	if _, err := ListModels(context.Background(), ProtocolAnthropic, srv.URL+"/v1", "k1", 5*time.Second); err != nil {
		t.Errorf("anthropic 拉取失败: %v", err)
	}
	if gotXKey != "k1" || gotVersion == "" {
		t.Errorf("anthropic 头 = %q / %q", gotXKey, gotVersion)
	}
}
