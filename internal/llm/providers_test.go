package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKeyScope(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://open.bigmodel.cn/api/paas/v4", "open.bigmodel.cn"},
		{"https://open.bigmodel.cn/api/coding/paas/v4", "open.bigmodel.cn"},
		{"  http://127.0.0.1:8000/v1/  ", "127.0.0.1:8000"},
		{"https://API.OpenAI.com/v1", "api.openai.com"},
		{"https://a.com", "a.com"},
		{"", ""},
		{"not-a-url", "not-a-url"},
	}
	for _, c := range cases {
		if got := KeyScope(c.in); got != c.want {
			t.Errorf("KeyScope(%q) = %q，want %q", c.in, got, c.want)
		}
	}
	if KeyScope("https://api.deepseek.com/v1") == KeyScope("https://open.bigmodel.cn/v1") {
		t.Error("两家厂商算出了同一个 scope")
	}
}

func TestResolveTarget(t *testing.T) {
	base, model, protocol := ResolveTarget("zhipu", PlanCoding, "", "", "")
	if protocol != ProtocolOpenAIChat || base == "" {
		t.Fatalf("preset = %s %s %s", base, model, protocol)
	}
	if model != "" {
		t.Errorf("没填模型时应留空（预设不再带默认模型），得到 %q", model)
	}
	if _, _, p := ResolveTarget("", "", "https://x/v1", "m", "胡来的协议"); p != ProtocolOpenAIChat {
		t.Errorf("非法协议应回落，得到 %s", p)
	}
	if _, _, p := ResolveTarget("", "", "https://x/v1", "m", ProtocolAnthropic); p != ProtocolAnthropic {
		t.Errorf("无预设时不该覆盖用户显式选的协议，得到 %s", p)
	}
}

func TestResolvePreset_ZhipuCoding(t *testing.T) {
	base, model, protocol := ResolvePreset("zhipu", PlanCoding, "", "")
	if base != "https://open.bigmodel.cn/api/coding/paas/v4" {
		t.Errorf("coding 入口 = %s", base)
	}
	if protocol != ProtocolOpenAIChat {
		t.Errorf("protocol=%s", protocol)
	}
	if model != "" {
		t.Errorf("没填模型时应留空（预设不再带默认模型），得到 %q", model)
	}
	base2, model2, _ := ResolvePreset("zhipu", PlanToken, "https://custom.example/v4", "my-model")
	if base2 != "https://custom.example/v4" || model2 != "my-model" {
		t.Errorf("显式值应优先: %s %s", base2, model2)
	}
	base3, _, _ := ResolvePreset("nope", "token", "https://x/v1", "m")
	if base3 != "https://x/v1" {
		t.Errorf("未知厂商 = %s", base3)
	}
	for _, p := range Providers {
		if FindProvider(p.ID) == nil {
			t.Errorf("FindProvider(%s) 失败", p.ID)
		}
		found := false
		for _, pl := range p.Plans {
			if !ValidProtocol(pl.Protocol) {
				t.Errorf("%s/%s 协议非法: %s", p.ID, pl.Kind, pl.Protocol)
			}
			if pl.Kind == PlanToken {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 缺少 token 套餐", p.ID)
		}
	}
}

func TestResponsesClient_Chat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["model"] != "test-model" || req["instructions"] == "" {
			t.Errorf("payload = %v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []map[string]any{{
				"type": "message",
				"content": []map[string]any{
					{"type": "output_text", "text": "你好"},
					{"type": "output_text", "text": "，世界"},
				},
			}},
		})
	}))
	defer srv.Close()

	c := NewResponses(srv.URL, "key", "test-model", 0.2, 128, 5*time.Second)
	out, err := c.Chat(context.Background(), ChatRequest{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "你好，世界" {
		t.Errorf("out = %q", out)
	}
}

func TestResponsesClient_ChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"流\"}\n\n" +
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"式输出\"}\n\n" +
			"data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer srv.Close()

	c := NewResponses(srv.URL, "key", "test-model", 0.2, 128, 5*time.Second)
	var sb strings.Builder
	out, err := c.ChatStream(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, func(d string) { sb.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if out != "流式输出" || sb.String() != "流式输出" {
		t.Errorf("out=%q delta=%q", out, sb.String())
	}
}

func TestAnthropicClient_Chat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "key" || r.Header.Get("anthropic-version") == "" {
			t.Error("缺少鉴权头")
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["system"] != "sys" || req["max_tokens"] == float64(0) {
			t.Errorf("payload = %v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Claude 回复"}},
		})
	}))
	defer srv.Close()

	c := NewAnthropic(srv.URL, "key", "claude-test", 0.2, 128, 5*time.Second)
	out, err := c.Chat(context.Background(), ChatRequest{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Claude 回复" {
		t.Errorf("out = %q", out)
	}
}

func TestAnthropicClient_ChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"安\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"心\"}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	c := NewAnthropic(srv.URL, "key", "claude-test", 0.2, 128, 5*time.Second)
	out, err := c.ChatStream(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "安心" {
		t.Errorf("out = %q", out)
	}
}

func TestFactory_ProtocolSelection(t *testing.T) {
	if _, ok := New(ProtocolOpenAIChat, "https://x", "k", "m", 0, 0, 0).(*GLMClient); !ok {
		t.Error("openai_chat 应构造 GLMClient")
	}
	if _, ok := New(ProtocolOpenAIResponses, "https://x", "k", "m", 0, 0, 0).(*ResponsesClient); !ok {
		t.Error("openai_responses 应构造 ResponsesClient")
	}
	if _, ok := New(ProtocolAnthropic, "https://x", "k", "m", 0, 0, 0).(*AnthropicClient); !ok {
		t.Error("anthropic 应构造 AnthropicClient")
	}
	if _, ok := New("", "https://x", "k", "m", 0, 0, 0).(*GLMClient); !ok {
		t.Error("空协议默认 openai_chat")
	}
}

func TestProvidersCatalogInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Providers {
		if strings.TrimSpace(p.ID) == "" {
			t.Fatal("有厂商没有 id")
		}
		if seen[p.ID] {
			t.Errorf("厂商 id 重复：%s", p.ID)
		}
		seen[p.ID] = true

		if len(p.Plans) == 0 {
			t.Errorf("%s 没有任何套餐入口", p.ID)
		}
		seenKinds := map[string]bool{}
		for _, pl := range p.Plans {
			if !ValidProtocol(pl.Protocol) {
				t.Errorf("%s/%s 协议非法：%q", p.ID, pl.Kind, pl.Protocol)
			}
			if strings.TrimSpace(pl.BaseURL) == "" {
				t.Errorf("%s/%s 的入口为空", p.ID, pl.Kind)
			}
			if seenKinds[pl.Kind] {
				t.Errorf("%s 有两条 %s 套餐（ResolvePreset 只认第一个，第二条既选不到也解析不到）", p.ID, pl.Kind)
			}
			seenKinds[pl.Kind] = true
		}

		if len(p.Models) == 0 {
			t.Errorf("%s 没有内置模型清单（界面要能离线选模型）", p.ID)
		}
		for _, m := range p.Models {
			if strings.TrimSpace(m) == "" {
				t.Errorf("%s 的模型清单里有空条目", p.ID)
			}
		}
	}
}

func TestProvidersOverseasLast(t *testing.T) {
	overseas := map[string]bool{
		"openai":     true,
		"anthropic":  true,
		"openrouter": true,
		"gemini":     true,
		"xai":        true,
		"mulerouter": true,
	}
	lastDomestic := -1
	firstOverseas := len(Providers)
	for i, p := range Providers {
		if overseas[p.ID] {
			if i < firstOverseas {
				firstOverseas = i
			}
			continue
		}
		lastDomestic = i
	}
	if firstOverseas == len(Providers) {
		t.Fatal("目录里没有任何海外厂商，overseas 清单可能没跟上目录")
	}
	if lastDomestic > firstOverseas {
		t.Errorf("海外平台必须垫底：第一个海外厂商在下标 %d，但它后面还有国产厂商（最后一条在下标 %d）", firstOverseas, lastDomestic)
	}
}

func TestProvidersCatalogExpected(t *testing.T) {
	for _, id := range []string{
		"zhipu-intl", "moonshot-intl", "qianwen", "byteplus", "stepfun-intl",
		"qianfan-intl", "tokenhub", "tokenhub-intl", "mulerouter",
	} {
		if FindProvider(id) == nil {
			t.Errorf("目录里缺 %s", id)
		}
	}
	if FindProvider("hunyuan") != nil {
		t.Error("腾讯混元旧平台已停服（api.hunyuan.cloud.tencent.com），不该再出现在目录里")
	}
}
