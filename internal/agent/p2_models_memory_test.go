package agent

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/config"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestAuxiliaryUsageDoesNotMoveWatermark(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.ContextWindow = 1000

	f.a.addUsage("t1", "plan", llm.Usage{PromptTokens: 970})
	f.a.addUsage("t1", "verify", llm.Usage{PromptTokens: 90})
	if got := f.a.windowReading().Pct; got != 97 {
		t.Fatalf("辅助调用把水位从 97%% 改成了 %d%%", got)
	}

	f.a.addUsage("t1", "chat_mode", llm.Usage{PromptTokens: 400})
	if got := f.a.windowReading().Pct; got != 40 {
		t.Fatalf("主对话应更新水位到 40%%，实际 %d%%", got)
	}
}

func TestRunGoal_ChatAnswerStoredOnce(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "chat_mode", Texts: []string{"唯一回答"}},
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{
		Goal:     "你好",
		TaskMode: types.TaskChat,
	})
	if res.Status != types.GoalSuccess {
		t.Fatalf("chat 模式应成功: %s %s", res.Status, res.Error)
	}
	copies := 0
	for _, turn := range f.a.Mem.Short.Recent(0) {
		if turn.Role == "assistant" && turn.Content == "唯一回答" {
			copies++
		}
	}
	if copies != 1 {
		t.Fatalf("助手回答在短期记忆里出现了 %d 次，应为 1 次", copies)
	}
}

func TestRunGoal_ChatStreamPreservesWhitespace(t *testing.T) {
	answer := strings.Repeat("word ", 16) + "\n\n```go\n\tfmt.Println(\"ok\")\n```\n"
	f := newFixture(t, []llm.Scripted{{Kind: "chat_mode", Texts: []string{answer}}})
	f.a.Cfg.Agent.ContextCompress = false
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "你好", TaskMode: types.TaskChat})
	if res.Status != types.GoalSuccess {
		t.Fatalf("chat failed: %+v", res)
	}
	var streamed strings.Builder
	for _, ev := range f.notify.progress {
		if ev.Phase == "chat" && ev.Kind == "llm" {
			streamed.WriteString(ev.Message)
		}
	}
	if streamed.String() != answer {
		t.Fatalf("stream changed whitespace: got %q want %q", streamed.String(), answer)
	}
}

type partialCancelClient struct {
	cancel context.CancelFunc
}

func (c *partialCancelClient) Name() string { return "partial-cancel" }

func (c *partialCancelClient) Chat(context.Context, llm.ChatRequest) (string, error) {
	return "", context.Canceled
}

func (c *partialCancelClient) ChatStream(context.Context, llm.ChatRequest, func(string)) (string, error) {
	c.cancel()
	return "已经写出的半段", context.Canceled
}

func TestRunGoal_CancelKeepsPartialChatAnswer(t *testing.T) {
	f := newFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.a.LLM = &partialCancelClient{cancel: cancel}

	res := f.a.RunGoal(ctx, types.GoalRequest{
		Goal:     "讲一个长故事",
		TaskMode: types.TaskChat,
	})
	if res.Status != types.GoalCancelled {
		t.Fatalf("status = %s，应为 cancelled", res.Status)
	}
	if res.Summary != "已经写出的半段" {
		t.Fatalf("取消后丢失了已生成内容: summary = %q", res.Summary)
	}
	if got := f.a.Mem.Short.Recent(0); len(got) == 0 || got[len(got)-1].Content != "已经写出的半段" {
		t.Fatalf("半段回答没有进入短期记忆: %+v", got)
	}
}

func TestActivatingModelClearsTiersFromPreviousHost(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.Models = []config.ModelEntry{
		{ID: "m-a", Name: "A", ProviderID: "openai", Plan: "token", Model: "gpt-a", IsDefault: true},
		{ID: "m-b", Name: "B", ProviderID: "zhipu", Plan: "token", Model: "glm-b"},
	}
	f.a.Cfg.LLM.BaseURL = "https://api.openai.com/v1"
	f.a.Cfg.LLM.FastModel = "glm-only-on-a"
	f.a.Cfg.LLM.Tiers = map[string]string{"coding": "glm-only-on-a"}

	if _, err := f.a.SetActiveModel("m-b"); err != nil {
		t.Fatal(err)
	}
	if f.a.Cfg.LLM.FastModel != "" {
		t.Errorf("切换厂商后仍保留辅助模型 %q", f.a.Cfg.LLM.FastModel)
	}
	if len(f.a.Cfg.LLM.Tiers) != 0 {
		t.Errorf("切换厂商后仍保留档位 %v", f.a.Cfg.LLM.Tiers)
	}
}

func TestApplySettingsSyncsDefaultEntryConnection(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.Provider = "openai"
	f.a.Cfg.LLM.ProviderID = "openai"
	f.a.Cfg.LLM.Plan = "token"
	f.a.Cfg.LLM.BaseURL = "https://api.openai.com/v1"
	f.a.Cfg.LLM.Model = "gpt-old"
	f.a.Cfg.LLM.Models = []config.ModelEntry{{
		ID: "m-1", Name: "旧连接", ProviderID: "openai", Plan: "token",
		BaseURL: "https://api.openai.com/v1", Model: "gpt-old", IsDefault: true,
	}}

	if _, err := f.a.ApplySettings(map[string]any{"llm": map[string]any{
		"provider_id": "zhipu",
		"plan":        "token",
		"protocol":    "openai_chat",
		"base_url":    "https://open.bigmodel.cn/api/paas/v4",
		"model":       "glm-4.6",
	}}); err != nil {
		t.Fatal(err)
	}
	entry := f.a.Cfg.LLM.Models[0]
	if entry.ProviderID != "zhipu" || entry.Model != "glm-4.6" ||
		entry.BaseURL != "https://open.bigmodel.cn/api/paas/v4" || entry.Protocol != "openai_chat" {
		t.Fatalf("默认条目没有跟随高级设置更新: %+v", entry)
	}
}

func TestApplySettingsRejectsRedactedProxyURL(t *testing.T) {
	f := newFixture(t, nil)
	original := "http://alice:password@127.0.0.1:19002"
	f.a.Cfg.Network.ProxyMode = "manual"
	f.a.Cfg.Network.ProxyURL = original

	for _, redacted := range []string{
		"http://***@127.0.0.1:19002",
		"http://%2A%2A%2A@127.0.0.1:19002",
	} {
		_, err := f.a.ApplySettings(map[string]any{"network": map[string]any{
			"proxy_mode": "manual",
			"proxy_url":  redacted,
		}})
		if err == nil || !strings.Contains(err.Error(), "占位符") {
			t.Fatalf("脱敏地址 %q 应被拒绝，实际 err = %v", redacted, err)
		}
		if f.a.Cfg.Network.ProxyURL != original {
			t.Fatalf("脱敏地址覆盖了真实配置: %q", f.a.Cfg.Network.ProxyURL)
		}
	}
}
