package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gleam/internal/config"
	"gleam/internal/harness/credentials"
	"gleam/internal/llm"
)

const glmHost = "https://open.bigmodel.cn/api/paas/v4"

func scopeOf(baseURL string) string { return llm.KeyScope(baseURL) }

func modelPatch(name, model, key string) map[string]any {
	return map[string]any{
		"id":       "m-" + name,
		"name":     name,
		"protocol": "openai_chat",
		"base_url": "https://models.example.test/v1",
		"model":    model,
		"api_key":  key,
	}
}

func TestWebUI_ModelAddBecomesActive(t *testing.T) {
	f := newFixture(t, nil)
	f.call("POST", "/api/models", modelPatch("primary", "gpt-x", "sk-abc"))

	list := f.call("GET", "/api/models", nil)
	active, _ := list["active"].(map[string]any)
	if active == nil {
		t.Fatalf("/api/models returned no active entry: %v", list)
	}
	if active["id"] != "m-primary" {
		t.Errorf("active = %v, want the entry just added", active["id"])
	}
	if active["api_key_set"] != true {
		t.Errorf("api_key_set = %v", active["api_key_set"])
	}
	cfg := f.call("GET", "/api/settings", nil)["llm"].(map[string]any)
	if cfg["api_key_set"] != true {
		t.Errorf("settings api_key_set = %v, want true after add", cfg["api_key_set"])
	}
}

func TestWebUI_ModelAddRewiresRuntime(t *testing.T) {
	f := newFixture(t, nil)
	f.agent.Cfg.LLM.Provider = "custom"

	f.call("POST", "/api/models", modelPatch("primary", "gpt-latest", "sk-real"))

	if got := f.agent.Cfg.LLM.Model; got != "gpt-latest" {
		t.Errorf("Cfg.LLM.Model = %q", got)
	}
	if got := f.agent.Cfg.LLM.BaseURL; got != "https://models.example.test/v1" {
		t.Errorf("Cfg.LLM.BaseURL = %q", got)
	}
	if got := f.agent.LLMName(); got == "unavailable" {
		t.Error("main client is still unavailable: adding a model did not reach the runtime")
	}

	f.call("POST", "/api/models", modelPatch("backup", "gpt-backup", "sk-backup"))
	f.call("POST", "/api/models/m-backup/activate", nil)
	if got := f.agent.Cfg.LLM.Model; got != "gpt-backup" {
		t.Errorf("Cfg.LLM.Model after activate = %q", got)
	}
	active := f.call("GET", "/api/models", nil)["active"].(map[string]any)
	if active["id"] != "m-backup" {
		t.Errorf("active = %v, want the activated entry", active["id"])
	}
}

func TestWebUI_ModelDeleteFallsBack(t *testing.T) {
	f := newFixture(t, nil)
	f.agent.Cfg.LLM.Provider = "custom"
	f.call("POST", "/api/models", modelPatch("only", "only-model", "sk-1"))
	f.call("DELETE", "/api/models/m-only", nil)

	if list := f.call("GET", "/api/models", nil); list["active"] != nil {
		t.Errorf("active still reported with an empty list: %v", list["active"])
	}
}

func TestWebUI_SettingsGetMasked(t *testing.T) {
	f := newFixture(t, nil)
	s := f.call("GET", "/api/settings", nil)
	if s["persona"] == nil || s["agent"] == nil || s["llm"] == nil {
		t.Fatalf("settings 视图缺 section: %v", s)
	}
	llm := s["llm"].(map[string]any)
	if _, hasKey := llm["api_key"]; hasKey {
		t.Error("不应回传 api_key 明文")
	}
	if llm["api_key_set"] != false {
		t.Errorf("api_key_set = %v", llm["api_key_set"])
	}
}

func TestWebUI_EngineRangeRejection(t *testing.T) {
	f := newFixture(t, nil)
	post := func(body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		resp, err := http.Post(f.ts.URL+"/api/settings", "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, out := post(map[string]any{"agent": map[string]any{"done_threshold": 150}}); code < 400 {
		t.Errorf("done_threshold=150 应被拒绝，得到 %d %v", code, out)
	}
	if f.agent.Cfg.Agent.DoneThreshold == 150 {
		t.Error("越界值被写入配置")
	}
	if code, out := post(map[string]any{"agent": map[string]any{"max_replans": 999}}); code < 400 {
		t.Errorf("max_replans=999 应被拒绝，得到 %d %v", code, out)
	}
	if code, _ := post(map[string]any{"agent": map[string]any{"max_replans": 5, "done_threshold": 200}}); code < 400 {
		t.Errorf("混合越界应整次拒绝，得到 %d", code)
	}
	if f.agent.Cfg.Agent.MaxReplans == 5 {
		t.Error("被拒绝的请求里合法字段不应生效")
	}
	if code, out := post(map[string]any{"agent": map[string]any{"max_steps": 0, "done_threshold": 90}}); code >= 400 {
		t.Errorf("合法值被拒，得到 %d %v", code, out)
	}
	if f.agent.Cfg.Agent.DoneThreshold != 90 {
		t.Errorf("done_threshold = %d，期望 90", f.agent.Cfg.Agent.DoneThreshold)
	}
}

func TestWebUI_SettingsApplyAndPersist(t *testing.T) {
	f := newFixture(t, nil)
	f.agent.Cfg.LLM.Provider = "glm"
	updated := f.call("POST", "/api/settings", map[string]any{
		"persona": map[string]any{"style": "gentle", "name": "小光"},
		"agent":   map[string]any{"step_retries": 3, "context_compress": false},
		"safety":  map[string]any{"mode": "plan_first", "approval_timeout_seconds": 60},
	})
	persona := updated["persona"].(map[string]any)
	if persona["style"] != "gentle" || persona["name"] != "小光" {
		t.Errorf("persona = %v", persona)
	}
	agentSec := updated["agent"].(map[string]any)
	if agentSec["step_retries"] != float64(3) || agentSec["context_compress"] != false {
		t.Errorf("agent = %v", agentSec)
	}
	if updated["safety"].(map[string]any)["mode"] != "plan_first" {
		t.Error("safety.mode 未更新")
	}
	if f.agent.Cfg.Safety.Mode != "plan_first" {
		t.Error("cfg.Safety.Mode 未生效")
	}
	fresh := config.Default()
	if err := config.LoadOverlay(fresh, filepath.Join(f.dataDir, config.OverlayFile)); err != nil {
		t.Fatal(err)
	}
	if fresh.Persona.Style != "gentle" || fresh.Agent.StepRetries != 3 {
		t.Errorf("覆盖层未持久化: %+v %+v", fresh.Persona, fresh.Agent)
	}
}

func TestWebUI_CostGovernanceSettingsRoundTrip(t *testing.T) {
	f := newFixture(t, nil)
	updated := f.call("POST", "/api/settings", map[string]any{
		"agent": map[string]any{
			"dedupe_calls":              false,
			"max_llm_calls_per_task":    7,
			"max_tokens_per_task":       12345,
			"max_task_duration_seconds": 42,
		},
		"llm": map[string]any{"fast_model": "glm-cheap"},
	})
	agentSec := updated["agent"].(map[string]any)
	if agentSec["dedupe_calls"] != false {
		t.Errorf("dedupe_calls = %v", agentSec["dedupe_calls"])
	}
	for k, want := range map[string]float64{
		"max_llm_calls_per_task": 7, "max_tokens_per_task": 12345, "max_task_duration_seconds": 42,
	} {
		if got := agentSec[k].(float64); got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
	if got := updated["llm"].(map[string]any)["fast_model"]; got != "glm-cheap" {
		t.Errorf("fast_model = %v", got)
	}
	if f.agent.Cfg.Agent.DedupeCalls || f.agent.Cfg.Agent.MaxLLMCallsPerTask != 7 ||
		f.agent.Cfg.Agent.MaxTokensPerTask != 12345 || f.agent.Cfg.Agent.MaxTaskDurationSecs != 42 ||
		f.agent.Cfg.LLM.FastModel != "glm-cheap" {
		t.Errorf("运行时配置未生效: agent=%+v fast=%q", f.agent.Cfg.Agent, f.agent.Cfg.LLM.FastModel)
	}
	fresh := config.Default()
	if err := config.LoadOverlay(fresh, filepath.Join(f.dataDir, config.OverlayFile)); err != nil {
		t.Fatal(err)
	}
	if fresh.Agent.MaxLLMCallsPerTask != 7 || fresh.Agent.MaxTokensPerTask != 12345 ||
		fresh.Agent.MaxTaskDurationSecs != 42 || fresh.LLM.FastModel != "glm-cheap" {
		t.Errorf("覆盖层未持久化: agent=%+v fast=%q", fresh.Agent, fresh.LLM.FastModel)
	}
}

func TestWebUI_BudgetZeroMeansUnlimited(t *testing.T) {
	f := newFixture(t, nil)
	updated := f.call("POST", "/api/settings", map[string]any{
		"agent": map[string]any{
			"max_llm_calls_per_task":    0,
			"max_tokens_per_task":       0,
			"max_task_duration_seconds": 0,
		},
	})
	agentSec := updated["agent"].(map[string]any)
	for _, k := range []string{"max_llm_calls_per_task", "max_tokens_per_task", "max_task_duration_seconds"} {
		if got := agentSec[k].(float64); got != 0 {
			t.Errorf("%s = %v, want 0", k, got)
		}
	}
	if f.agent.Cfg.Agent.MaxLLMCallsPerTask != 0 || f.agent.Cfg.Agent.MaxTokensPerTask != 0 ||
		f.agent.Cfg.Agent.MaxTaskDurationSecs != 0 {
		t.Errorf("0 预算未生效: %+v", f.agent.Cfg.Agent)
	}
}

func TestWebUI_SettingsInvalidPatch(t *testing.T) {
	f := newFixture(t, nil)
	req := map[string]any{
		"persona": map[string]any{"style": "狂野"},
		"safety":  map[string]any{"mode": "yolo"},
		"agent":   map[string]any{"step_retries": "abc"},
	}
	resp, err := http.Post(f.ts.URL+"/api/settings", "application/json", strings.NewReader(`{"persona":{"style":"狂野"},"safety":{"mode":"yolo"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("非法补丁应 400, got %d", resp.StatusCode)
	}
	_ = req
	if f.agent.Cfg.Persona.Style == "狂野" || f.agent.Cfg.Safety.Mode == "yolo" {
		t.Error("非法值被应用")
	}
}

func TestWebUI_ContextCompressAndClear(t *testing.T) {
	f := newFixture(t, nil)
	long := strings.Repeat("需要保留的关键上下文细节。", 12)
	for i := 0; i < 22; i++ {
		f.agent.Mem.AddTurn("user", long)
	}
	ctx := f.call("GET", "/api/context", nil)
	if ctx["enabled"] != true || ctx["overflow"].(float64) < 2 {
		t.Fatalf("context = %v", ctx)
	}
	out := f.call("POST", "/api/context/compress", nil)
	if out["compressed"] != true {
		t.Errorf("compress = %v", out)
	}
	ctxAfter := out["context"].(map[string]any)
	if ctxAfter["overflow"].(float64) != 0 || ctxAfter["summary"].(string) == "" {
		t.Errorf("压缩后 = %v", ctxAfter)
	}
	if ctxAfter["est_tokens_saved"].(float64) < 0 {
		t.Errorf("est_tokens_saved = %v", ctxAfter["est_tokens_saved"])
	}
	cleared := f.call("POST", "/api/context/clear", nil)
	if cleared["cleared"] != true {
		t.Errorf("clear = %v", cleared)
	}
	if cleared["context"].(map[string]any)["summary"].(string) != "" {
		t.Error("清空后摘要应为空")
	}
}

func TestWebUI_APIKeySaveAndClear(t *testing.T) {
	f := newFixture(t, nil)
	cred, err := credentials.Open(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.agent.Creds = cred

	up := f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": glmHost, "api_key": "sk-web-abcdef"},
	})
	llmSec := up["llm"].(map[string]any)
	if llmSec["api_key_set"] != true {
		t.Errorf("保存后 api_key_set = %v", llmSec["api_key_set"])
	}
	if got, _ := cred.ResolveLLMAPIKey(scopeOf(glmHost)); got != "sk-web-abcdef" {
		t.Errorf("凭证文件未落盘: %q", got)
	}
	if _, hasPlain := llmSec["api_key"]; hasPlain {
		t.Error("响应不应回传 api_key")
	}
	raw, err := os.ReadFile(filepath.Join(f.dataDir, config.OverlayFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-web-abcdef") {
		t.Error("settings.yaml 泄漏了 api_key")
	}

	f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": glmHost, "api_key": "", "model": "m-next"},
	})
	if got, _ := cred.ResolveLLMAPIKey(scopeOf(glmHost)); got != "sk-web-abcdef" {
		t.Errorf("空串不应清除，得到 %q", got)
	}

	up2 := f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": glmHost, "clear_api_key": true},
	})
	if up2["llm"].(map[string]any)["api_key_set"] != false {
		t.Errorf("清除后 api_key_set = %v", up2["llm"].(map[string]any)["api_key_set"])
	}
	if got, host := cred.ResolveLLMAPIKey(scopeOf(glmHost)); got != "" || host != "" {
		t.Errorf("磁盘未清除: %q / %q", got, host)
	}
	if f.agent.Cfg.LLM.APIKey != "" {
		t.Errorf("内存未清除: %q", f.agent.Cfg.LLM.APIKey)
	}
}

type authCapture struct {
	mu    sync.Mutex
	hits  int
	auths []string
}

func (c *authCapture) record(auth string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits++
	c.auths = append(c.auths, auth)
}

func (c *authCapture) summary() (hits int, nonEmptyAuth string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.auths {
		if a != "" {
			return c.hits, a
		}
	}
	return c.hits, ""
}

func recordingKeyServer(t *testing.T, cap *authCapture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = r.Header.Get("x-api-key")
		}
		cap.record(auth)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"data":[{"id":"m-b"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWebUI_APIKeyNotForwardedToOtherHost(t *testing.T) {
	var capA, capB authCapture
	srvA := recordingKeyServer(t, &capA)
	srvB := recordingKeyServer(t, &capB)
	urlA, urlB := srvA.URL, srvB.URL

	f := newFixture(t, nil)
	cred, err := credentials.Open(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.agent.Creds = cred
	f.agent.Cfg.LLM.Provider = "glm"

	v := f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": urlA, "model": "m-a", "api_key": "sk-A-只属于A"},
	})
	if v["llm"].(map[string]any)["api_key_set"] != true {
		t.Fatalf("A 家应显示已设置: %v", v["llm"])
	}

	v2 := f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": urlB, "model": "m-b"},
	})
	l2 := v2["llm"].(map[string]any)
	if l2["api_key_set"] != false {
		t.Errorf("换到 B 家后不该显示已设置: %v", l2["api_key_set"])
	}
	if l2["api_key_host"] != scopeOf(urlA) {
		t.Errorf("应回传密钥归属主机以便界面说明，得到 %v", l2["api_key_host"])
	}
	if f.agent.Cfg.LLM.APIKey != "" {
		t.Errorf("内存里的生效密钥应已让位，得到 %q", f.agent.Cfg.LLM.APIKey)
	}

	res := f.call("POST", "/api/llm/test", map[string]any{"base_url": urlB, "model": "m-b"})
	if res["ok"] != true {
		t.Fatalf("探测应打到本地假服务，得到 %v", res)
	}
	if hits, auths := capB.summary(); hits == 0 {
		t.Fatal("B 家一次都没收到请求，这条断言就是空的")
	} else if strings.Contains(auths, "sk-A") {
		t.Errorf("B 家收到了 A 家的密钥：%q", auths)
	} else if auths != "" {
		t.Errorf("B 家不该收到任何鉴权头：%q", auths)
	}

	v3 := f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": urlA, "model": "m-a"},
	})
	if v3["llm"].(map[string]any)["api_key_set"] != true {
		t.Fatalf("换回 A 家应恢复已设置: %v", v3["llm"])
	}
	f.call("POST", "/api/llm/test", map[string]any{"base_url": urlA, "model": "m-a"})
	if hits, auths := capA.summary(); hits == 0 || !strings.Contains(auths, "sk-A-只属于A") {
		t.Errorf("A 家应收到原密钥（切回来免重填），得到 %d 次 / %q", hits, auths)
	}
}

func TestWebUI_APIKeyNeedsHost(t *testing.T) {
	f := newFixture(t, nil)
	cred, err := credentials.Open(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.agent.Creds = cred
	f.agent.Cfg.LLM.BaseURL = ""

	code, msg := f.status("POST", "/api/settings", `{"llm":{"api_key":"sk-无处可绑"}}`)
	if code != 400 {
		t.Fatalf("状态码 = %d (%s)，应为 400", code, msg)
	}
	if !strings.Contains(msg, "base_url") {
		t.Errorf("应说明缺 base_url，得到 %q", msg)
	}
	if got, _ := cred.ResolveLLMAPIKey(""); got != "" {
		t.Errorf("不该落盘，得到 %q", got)
	}
}

func TestWebUI_BaseURLClear(t *testing.T) {
	f := newFixture(t, nil)
	f.agent.Cfg.LLM.Provider = "glm"
	f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": "https://custom.example/v4"},
	})
	if f.agent.Cfg.LLM.BaseURL == "" {
		t.Fatal("base_url 未写入")
	}
	f.call("POST", "/api/settings", map[string]any{
		"llm": map[string]any{"base_url": ""},
	})
	if f.agent.Cfg.LLM.BaseURL != "" {
		t.Errorf("清空未生效: %q", f.agent.Cfg.LLM.BaseURL)
	}
}

func TestWebUI_LLMTestProbe(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") == "" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	})
	gwOK := httptest.NewServer(ok)
	defer gwOK.Close()
	gw401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer gw401.Close()

	f := newFixture(t, nil)
	f.agent.Cfg.LLM.Provider = "glm"
	res := f.call("POST", "/api/llm/test", map[string]any{
		"base_url": gwOK.URL, "model": "probe-model", "api_key": "k", "protocol": "openai_chat",
	})
	if res["ok"] != true || res["model"] != "probe-model" {
		t.Errorf("探测成功路径 = %v", res)
	}
	res2 := f.call("POST", "/api/llm/test", map[string]any{
		"base_url": gw401.URL, "model": "m", "api_key": "bad", "protocol": "openai_chat",
	})
	if res2["ok"] != false || res2["kind"] != "auth" || res2["http_status"] != float64(401) {
		t.Errorf("401 应分类为 auth，得到 %v", res2)
	}
	f.agent.Cfg.LLM.Provider = "mock"
	res3 := f.call("POST", "/api/llm/test", map[string]any{})
	if res3["kind"] != "mock" || res3["ok"] != true {
		t.Errorf("mock 短路 = %v", res3)
	}
	res4 := f.call("POST", "/api/llm/test", map[string]any{
		"base_url": gwOK.URL, "model": "m", "api_key": "k", "protocol": "openai_chat",
	})
	if res4["kind"] != "ok" || res4["ok"] != true {
		t.Errorf("显式 base_url 应跳过 mock 短路，得到 %v", res4)
	}
}

func TestWebUI_LLMModelsFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m-a","owned_by":"x"},{"id":"m-b"}]}`))
	}))
	defer srv.Close()

	f := newFixture(t, nil)
	f.agent.Cfg.LLM.Provider = "glm"
	res := f.call("POST", "/api/llm/models", map[string]any{
		"base_url": srv.URL + "/v1", "api_key": "k", "protocol": "openai_chat",
	})
	if res["ok"] != true || res["count"] != float64(2) {
		t.Fatalf("拉取结果 = %v", res)
	}
	list, _ := res["models"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["id"] != "m-a" {
		t.Fatalf("models = %v", res["models"])
	}
	res2 := f.call("POST", "/api/llm/models", map[string]any{
		"base_url": srv.URL + "/nope", "api_key": "bad", "protocol": "openai_chat",
	})
	if res2["ok"] != false || res2["kind"] != "auth" {
		t.Errorf("401 应分类为 auth，得到 %v", res2)
	}
	f.agent.Cfg.LLM.Provider = "mock"
	res3 := f.call("POST", "/api/llm/models", map[string]any{})
	if res3["kind"] != "mock" || res3["ok"] != true {
		t.Errorf("mock 短路 = %v", res3)
	}
	res4 := f.call("POST", "/api/llm/models", map[string]any{
		"base_url": srv.URL + "/v1", "api_key": "k", "protocol": "openai_chat",
	})
	if res4["kind"] != "ok" || res4["count"] != float64(2) {
		t.Errorf("显式 base_url 应跳过 mock 短路，得到 %v", res4)
	}
}

func TestWebUI_SettingsPageRendered(t *testing.T) {
	f := newFixture(t, nil)
	resp, err := http.Get(f.ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	_, _ = io.Copy(&b, resp.Body)
	body := b.String()
	if !strings.Contains(body, "view-settings") || !strings.Contains(body, "记忆与上下文") {
		t.Error("设置视图未内嵌到首页")
	}
}
