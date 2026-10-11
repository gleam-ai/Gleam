package agent

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/config"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func verifyAgent(provider, mainModel string, tiers map[string]string) (*Agent, *llm.MockClient) {
	cfg := config.Default()
	cfg.LLM.Provider = provider
	cfg.LLM.Model = mainModel
	cfg.LLM.Tiers = tiers
	main := llm.NewMock()
	main.Model = mainModel
	return &Agent{LLM: main, Cfg: cfg}, main
}

func TestVerifyLLM_FallsBackWithoutVerifyTier(t *testing.T) {
	a, main := verifyAgent("openai", "main-model", nil)

	if a.verifyIndependent("") {
		t.Error("没配 reasoning 档时不该声称独立")
	}
	if got := a.VerifyModel(""); got != "main-model" {
		t.Errorf("未换档时应如实返回执行者模型，实际 %q", got)
	}
	if a.VerifyLLM("") != main {
		t.Error("未换档时应回退到执行者客户端")
	}
}

func TestVerifyLLM_SwitchesToReasoningTier(t *testing.T) {
	a, main := verifyAgent("openai", "main-model", map[string]string{TierReasoning: "judge-model"})
	judge := llm.NewMock()
	judge.Model = "judge-model"
	a.tierClients = map[string]llm.Client{TierReasoning: judge}

	if !a.verifyIndependent("") {
		t.Fatal("配了 reasoning 档就应构成独立验收")
	}
	if got := a.VerifyModel(""); got != "judge-model" {
		t.Errorf("验收模型应为 judge-model，实际 %q", got)
	}
	if a.VerifyLLM("") != judge {
		t.Error("验收应换到 reasoning 档的客户端")
	}
	if a.VerifyLLM("") == main {
		t.Error("验收客户端不能与执行者相同")
	}
}

func TestVerifyLLM_SameModelIsNotIndependent(t *testing.T) {
	a, _ := verifyAgent("openai", "main-model", map[string]string{TierReasoning: "main-model"})

	if a.verifyIndependent("") {
		t.Error("档位指向同一个模型不构成独立")
	}
	if got := a.VerifyModel(""); got != "main-model" {
		t.Errorf("应回退到执行者模型，实际 %q", got)
	}
}

func TestVerifyLLM_ExecutorAlreadyOnReasoningFallsBack(t *testing.T) {
	a, _ := verifyAgent("openai", "main-model", map[string]string{
		TierReasoning: "judge-model",
		TierCoding:    "coder-model",
	})

	if a.verifyIndependent("analyst") {
		t.Error("执行者已在 reasoning 档时不该声称换成了另一个模型")
	}
	if got := a.VerifyModel("analyst"); got != "judge-model" {
		t.Errorf("应如实返回执行者实际用的模型，实际 %q", got)
	}
}

func TestVerifyLLM_VerifyTierPointingAtMainModelStillIndependent(t *testing.T) {
	a, main := verifyAgent("openai", "main-model", map[string]string{
		TierReasoning: "main-model",
		TierCoding:    "coder-model",
	})

	if !a.verifyIndependent("coder") {
		t.Fatal("执行者在 coding 档、验收在主模型，模型不同即构成独立")
	}
	if got := a.VerifyModel("coder"); got != "main-model" {
		t.Errorf("验收模型应为 main-model，实际 %q", got)
	}
	if a.VerifyLLM("coder") != main {
		t.Error("验收档指向主模型时应复用主客户端，而不是另建一个")
	}
}

func TestVerifyLLM_MockSessionNeverSwitches(t *testing.T) {
	a, main := verifyAgent("mock", "main-model", map[string]string{TierReasoning: "judge-model"})

	if a.verifyIndependent("") {
		t.Error("mock 会话下不该声称独立")
	}
	if a.VerifyLLM("") != main {
		t.Error("mock 会话应回退到主客户端")
	}
}

func TestRunGoal_ReflectorUsesIndependentVerifyModel(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(10, "replan", "执行者的反思不该被调用"),
	})
	f.a.Cfg.LLM.Provider = "openai"
	f.a.Cfg.LLM.Model = "main-model"
	f.a.Cfg.LLM.Tiers = map[string]string{TierReasoning: "judge-model"}
	judge := llm.NewMock()
	judge.Model = "judge-model"
	judge.Enqueue("reflect", `{"score":95,"verdict":"done","reason":"独立验收通过"}`)
	f.a.tierClients = map[string]llm.Client{TierReasoning: judge}

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "回一句"})

	if res.Status != types.GoalSuccess {
		t.Fatalf("应由验收模型判定为完成，实际 status=%s err=%s", res.Status, res.Error)
	}
	if len(judge.Calls) == 0 {
		t.Fatal("验收模型一次都没被调用——反思器还在用执行者的客户端")
	}
	for _, c := range f.llm.Calls {
		if llm.KindOf(c.System) == "reflect" {
			t.Error("反思调用落到了执行者的客户端上，独立性没生效")
		}
	}
}

func TestReadiness_ReportsVerifyIndependence(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "openai"
	cfg.LLM.Model = "main-model"
	cfg.LLM.Tiers = map[string]string{TierCoding: "coder-model"}
	a := &Agent{LLM: llm.NewMock(), Cfg: cfg}

	joined := strings.Join(readyItem(t, a.Readiness(), "model_lock").Evidence, "\n")
	if !strings.Contains(joined, "验收模型：main-model") || !strings.Contains(joined, "与执行者同源") {
		t.Errorf("没配 reasoning 档时应明说未换成，实际：\n%s", joined)
	}

	cfg.LLM.Tiers[TierReasoning] = "judge-model"
	joined = strings.Join(readyItem(t, a.Readiness(), "model_lock").Evidence, "\n")
	if !strings.Contains(joined, "验收模型：judge-model") || !strings.Contains(joined, "独立验收生效") {
		t.Errorf("配了 reasoning 档时应报独立验收生效，实际：\n%s", joined)
	}
}
