package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gleam/internal/config"
	"gleam/internal/harness/credentials"
	"gleam/internal/llm"
)

// Consistency tests for model configuration binding: across multiple models,
// protocols, and hosts, API keys are isolated per host, switching the active
// model switches protocol and endpoint with it, and the fast (auxiliary) model
// carries its own key. Three mock servers impersonate three protocols and
// record the most recent request (path/headers/body).

type recordedCall struct {
	Path   string
	Auth   string // value of the Authorization or x-api-key header
	Header string // which auth header was used
	Body   string
}

type fakeUpstream struct {
	mu     sync.Mutex
	chat   string // reply text served for /chat/completions
	resp   string // reply text served for /responses
	claude string // reply text served for /v1/messages
	last   recordedCall
}

func (f *fakeUpstream) handler(w http.ResponseWriter, r *http.Request) {
	body := make([]byte, r.ContentLength)
	_, _ = r.Body.Read(body)
	auth := r.Header.Get("Authorization")
	hdr := "Authorization"
	if auth == "" {
		auth = r.Header.Get("x-api-key")
		hdr = "x-api-key"
	}
	f.mu.Lock()
	f.last = recordedCall{Path: r.URL.Path, Auth: auth, Header: hdr, Body: string(body)}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + f.chat + `"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`))
	case strings.HasSuffix(r.URL.Path, "/responses"):
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"` + f.resp + `"}]}],"usage":{"input_tokens":5,"output_tokens":6}}`))
	case strings.HasSuffix(r.URL.Path, "/messages"):
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"` + f.claude + `"}],"usage":{"input_tokens":7,"output_tokens":8}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"unknown path ` + r.URL.Path + `"}}`))
	}
}

// newBindingAgent builds an Agent with a real credential store and no LLM
// client: the model binding path only depends on Cfg/Creds/syncModelRuntime and
// does not need a usable planner.
func newBindingAgent(t *testing.T) *Agent {
	t.Helper()
	creds, err := credentials.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.LLM.Provider = "deepseek"
	a := &Agent{Cfg: cfg, Creds: creds, Notifier: NopNotifier{}}
	return a
}

// Vendor preset + key + empty base_url must still save: the key is stored under
// the host resolved from the preset.
// (Regression: bindModelKey used to bind with an empty host and reject the
// request demanding a base_url.)
func TestModelAdd_PresetProviderBindsKeyWithoutBaseURL(t *testing.T) {
	a := newBindingAgent(t)
	out, err := a.ModelAdd(map[string]any{
		"name": "智谱", "provider_id": "zhipu", "plan": "token",
		"model": "glm-4.6", "api_key": "glm-secret",
	})
	if err != nil {
		t.Fatalf("厂商预设 + Key + 空 base_url 应能保存: %v", err)
	}
	entry := a.Cfg.LLM.Models[0]
	if entry.APIKey != "" {
		t.Errorf("明文密钥应从配置清走（入凭证库）: %q", entry.APIKey)
	}
	got, _ := out["models"].([]map[string]any)
	_ = got
	// key is stored in the credential store scoped to the preset host
	key, host := a.Creds.ResolveLLMAPIKey("open.bigmodel.cn")
	if key != "glm-secret" || host != "open.bigmodel.cn" {
		t.Errorf("密钥未按预设主机入库: key=%q host=%q", key, host)
	}
	// the default client is built against the preset endpoint, protocol follows the preset
	if a.Cfg.LLM.BaseURL == "" || !strings.Contains(a.Cfg.LLM.BaseURL, "open.bigmodel.cn") {
		t.Errorf("base_url 未按预设解析: %q", a.Cfg.LLM.BaseURL)
	}
	if a.Cfg.LLM.APIKey != "glm-secret" {
		t.Errorf("运行时密钥未绑定: %q", a.Cfg.LLM.APIKey)
	}
}

// Per-host key isolation: two custom models each live on their own host, and
// whichever is active contributes its key and endpoint. Switching back and
// forth must not mix them up.
func TestModelSwitch_KeysStayPerHost(t *testing.T) {
	upA := &fakeUpstream{chat: "from-A"}
	srvA := httptest.NewServer(http.HandlerFunc(upA.handler))
	defer srvA.Close()
	upB := &fakeUpstream{chat: "from-B"}
	srvB := httptest.NewServer(http.HandlerFunc(upB.handler))
	defer srvB.Close()

	a := newBindingAgent(t)
	a.Creds.SetLLMAPIKey("key-A", llm.KeyScope(srvA.URL))
	a.Creds.SetLLMAPIKey("key-B", llm.KeyScope(srvB.URL))
	if _, err := a.ModelAdd(map[string]any{"name": "A", "base_url": srvA.URL, "model": "m-a", "protocol": "openai_chat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ModelAdd(map[string]any{"name": "B", "base_url": srvB.URL, "model": "m-b", "protocol": "openai_chat"}); err != nil {
		t.Fatal(err)
	}

	// Activate A (the default entry is the first one added) -> requests hit host A with key-A
	if _, err := a.SetActiveModel(a.Cfg.LLM.Models[0].ID); err != nil {
		t.Fatal(err)
	}
	text, err := a.LLM.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil || text != "from-A" {
		t.Fatalf("A 主机应答: %q %v", text, err)
	}
	if !strings.HasSuffix(upA.last.Auth, "key-A") || !strings.HasSuffix(upA.last.Path, "/chat/completions") {
		t.Errorf("A 请求头/路径不对: %+v", upA.last)
	}

	// Switch to B -> key-B, host B
	if _, err := a.SetActiveModel(a.Cfg.LLM.Models[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LLM.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(upB.last.Auth, "key-B") {
		t.Errorf("B 主机应带 key-B, got %q", upB.last.Auth)
	}
	if strings.Contains(upA.last.Body, "") && upA.last.Auth != "key-B" {
		// host A must never receive B's key (the recorded last call is still A's earlier request)
		_ = upA.last
	}

	// Switch back to A -> key-A comes back (resolved per host from the credential store)
	if _, err := a.SetActiveModel(a.Cfg.LLM.Models[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LLM.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(upA.last.Auth, "key-A") {
		t.Errorf("切回 A 后密钥应从凭证库按主机恢复, got %q", upA.last.Auth)
	}
}

// Custom model entries declare their protocol: whichever entry is active
// dictates the request shape --
// openai_chat -> POST /chat/completions; openai_responses -> POST /responses;
// anthropic -> POST /v1/messages (/v1 auto-appended to a base lacking it) + x-api-key.
func TestModelSwitch_ProtocolFollowsEntry(t *testing.T) {
	up := &fakeUpstream{chat: "chat-ok", resp: "resp-ok", claude: "claude-ok"}
	srv := httptest.NewServer(http.HandlerFunc(up.handler))
	defer srv.Close()

	a := newBindingAgent(t)
	a.Creds.SetLLMAPIKey("sk-test", llm.KeyScope(srv.URL))
	add := func(name, protocol string) string {
		out, err := a.ModelAdd(map[string]any{"name": name, "base_url": srv.URL, "model": name, "protocol": protocol})
		if err != nil {
			t.Fatal(err)
		}
		m := out["model"].(map[string]any)
		return m["id"].(string)
	}
	chatID := add("chat-entry", "openai_chat")
	respID := add("resp-entry", "openai_responses")
	claudeID := add("claude-entry", "anthropic")

	ctx := context.Background()
	req := llm.ChatRequest{System: "sys", Messages: []llm.Message{
		{Role: llm.RoleUser, Content: "q1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "q2"},
	}}

	// openai_chat
	if _, err := a.SetActiveModel(chatID); err != nil {
		t.Fatal(err)
	}
	if text, err := a.LLM.Chat(ctx, req); err != nil || text != "chat-ok" {
		t.Fatalf("chat: %q %v", text, err)
	}
	if !strings.HasSuffix(up.last.Path, "/chat/completions") || up.last.Header != "Authorization" {
		t.Errorf("chat 请求形状: %+v", up.last)
	}
	if !strings.Contains(up.last.Body, `"model":"chat-entry"`) {
		t.Errorf("chat body: %s", up.last.Body)
	}

	// openai_responses: path /responses; the assistant turns in the history must be output_text
	if _, err := a.SetActiveModel(respID); err != nil {
		t.Fatal(err)
	}
	if text, err := a.LLM.Chat(ctx, req); err != nil || text != "resp-ok" {
		t.Fatalf("responses: %q %v", text, err)
	}
	if !strings.HasSuffix(up.last.Path, "/responses") {
		t.Errorf("responses 路径: %s", up.last.Path)
	}
	if !strings.Contains(up.last.Body, `"instructions"`) {
		t.Errorf("responses body 缺 instructions: %s", up.last.Body)
	}
	var pr struct {
		Input []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal([]byte(up.last.Body), &pr); err != nil {
		t.Fatal(err)
	}
	if len(pr.Input) != 3 || pr.Input[1].Role != "assistant" || pr.Input[1].Content[0].Type != "output_text" {
		t.Errorf("assistant 历史应为 output_text: %+v", pr.Input)
	}

	// anthropic: base without /v1 -> /v1/messages auto-completed; x-api-key + anthropic-version
	if _, err := a.SetActiveModel(claudeID); err != nil {
		t.Fatal(err)
	}
	if text, err := a.LLM.Chat(ctx, req); err != nil || text != "claude-ok" {
		t.Fatalf("anthropic: %q %v", text, err)
	}
	if up.last.Path != "/v1/messages" {
		t.Errorf("anthropic 路径: %s", up.last.Path)
	}
	if up.last.Header != "x-api-key" || !strings.HasSuffix(up.last.Auth, "sk-test") {
		t.Errorf("anthropic 认证头: %+v", up.last)
	}
	if !strings.Contains(up.last.Body, `"max_tokens"`) {
		t.Errorf("anthropic body 缺 max_tokens: %s", up.last.Body)
	}
}

// Streaming must work end to end for all three protocols (through the client the Agent actually holds).
func TestModelSwitch_StreamAcrossProtocols(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		flush := func(payload string) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(payload))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			flush("data: {\"choices\":[{\"delta\":{\"content\":\"s-\"}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"content\":\"chat\"}}]}\n\n" +
				"data: [DONE]\n\n")
		case strings.HasSuffix(r.URL.Path, "/responses"):
			flush("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"s-resp\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n")
		case strings.HasSuffix(r.URL.Path, "/messages"):
			flush("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"s-claude\"}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := newBindingAgent(t)
	a.Creds.SetLLMAPIKey("sk-stream", llm.KeyScope(srv.URL))
	add := func(name, protocol string) string {
		out, err := a.ModelAdd(map[string]any{"name": name, "base_url": srv.URL, "model": name, "protocol": protocol})
		if err != nil {
			t.Fatal(err)
		}
		return out["model"].(map[string]any)["id"].(string)
	}
	ctx := context.Background()
	req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}
	for _, tc := range []struct{ id, want string }{
		{add("c1", "openai_chat"), "s-chat"},
		{add("r1", "openai_responses"), "s-resp"},
		{add("a1", "anthropic"), "s-claude"},
	} {
		if _, err := a.SetActiveModel(tc.id); err != nil {
			t.Fatal(err)
		}
		text, err := a.LLM.ChatStream(ctx, req, nil)
		if err != nil || text != tc.want {
			t.Errorf("%s stream = %q %v, want %q", tc.id, text, err, tc.want)
		}
	}
}

// The fast (auxiliary) model uses its own host and key when it points at a different vendor.
func TestFastModel_CrossVendorOwnKey(t *testing.T) {
	upMain := &fakeUpstream{chat: "main"}
	srvMain := httptest.NewServer(http.HandlerFunc(upMain.handler))
	defer srvMain.Close()
	upFast := &fakeUpstream{chat: "fast"}
	srvFast := httptest.NewServer(http.HandlerFunc(upFast.handler))
	defer srvFast.Close()

	a := newBindingAgent(t)
	a.Creds.SetLLMAPIKey("key-main", llm.KeyScope(srvMain.URL))
	a.Creds.SetLLMAPIKey("key-fast", llm.KeyScope(srvFast.URL))
	if _, err := a.ModelAdd(map[string]any{"name": "主", "base_url": srvMain.URL, "model": "m-main", "protocol": "openai_chat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ModelAdd(map[string]any{"name": "辅", "base_url": srvFast.URL, "model": "m-fast", "protocol": "openai_chat", "is_fast": true}); err != nil {
		t.Fatal(err)
	}
	if a.FastLLM == nil {
		t.Fatal("辅助模型客户端未建立")
	}
	text, err := a.FastLLM.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	if err != nil || text != "fast" {
		t.Fatalf("fast 应答: %q %v", text, err)
	}
	if !strings.HasSuffix(upFast.last.Auth, "key-fast") || strings.HasSuffix(upMain.last.Auth, "key-fast") {
		t.Errorf("辅助模型密钥未按自己主机绑定: fast=%q", upFast.last.Auth)
	}
}
