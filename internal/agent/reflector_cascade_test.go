package agent

import (
	"context"
	"strings"
	"testing"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

func cascadeExec() *Result {
	return &Result{
		Steps:     []types.StepResult{{StepID: "s1", Tool: "reply", Status: types.StepSucceeded, Output: "好"}},
		Succeeded: 1, Total: 1,
	}
}

func TestReflector_LowConfidenceCascadesOnce(t *testing.T) {
	judge := llm.NewMock()
	judge.Enqueue("reflect", `{"score":90,"verdict":"done","confidence":30,"reason":"初步看完成"}`)
	strong := llm.NewMock()
	strong.Enqueue("reflect", `{"score":40,"verdict":"replan","confidence":85,"reason":"输出与目标不符"}`)
	r := &Reflector{LLM: judge, Escalate: strong}

	refl := r.Evaluate(context.Background(), "回一句", cascadeExec(), 1)

	if !refl.Escalated {
		t.Error("低置信度应触发级联")
	}
	if refl.Verdict != "replan" || refl.Score != 40 {
		t.Errorf("应采用级联模型的判定: %+v", refl)
	}
	if len(strong.Calls) != 1 {
		t.Errorf("级联只复核一次，实际 %d 次", len(strong.Calls))
	}
	if !strings.Contains(refl.Reason, "级联复核") {
		t.Errorf("理由应说明级联: %q", refl.Reason)
	}
}

func TestReflector_HighConfidenceSkipsCascade(t *testing.T) {
	judge := llm.NewMock()
	judge.Enqueue("reflect", `{"score":90,"verdict":"done","confidence":90,"reason":"完成"}`)
	strong := llm.NewMock()
	r := &Reflector{LLM: judge, Escalate: strong}

	refl := r.Evaluate(context.Background(), "回一句", cascadeExec(), 1)

	if refl.Escalated || len(strong.Calls) != 0 {
		t.Errorf("高置信度不应级联: %+v, calls=%d", refl, len(strong.Calls))
	}
	if refl.Verdict != "done" {
		t.Errorf("判定 = %+v", refl)
	}
}

func TestReflector_MissingConfidenceIsNotLow(t *testing.T) {
	judge := llm.NewMock()
	judge.Enqueue("reflect", `{"score":90,"verdict":"done","reason":"完成"}`)
	strong := llm.NewMock()
	r := &Reflector{LLM: judge, Escalate: strong}

	refl := r.Evaluate(context.Background(), "回一句", cascadeExec(), 1)

	if refl.Escalated || len(strong.Calls) != 0 {
		t.Error("未报告置信度是未知，不是低置信度，不应级联")
	}
	if strings.Contains(refl.Reason, "人工复核") {
		t.Error("未报告置信度不应挂人工复核提示")
	}
}

func TestReflector_CascadeFailureKeepsFirstVerdict(t *testing.T) {
	judge := llm.NewMock()
	judge.Enqueue("reflect", `{"score":90,"verdict":"done","confidence":30,"reason":"初步看完成"}`)
	strong := llm.NewMock()
	strong.Enqueue("reflect", `这不是 JSON`)
	r := &Reflector{LLM: judge, Escalate: strong}

	refl := r.Evaluate(context.Background(), "回一句", cascadeExec(), 1)

	if refl.Escalated {
		t.Error("级联失败不应标记已级联")
	}
	if refl.Verdict != "done" {
		t.Errorf("级联失败应保留初判: %+v", refl)
	}
	if !strings.Contains(refl.Reason, "人工复核") {
		t.Errorf("级联失败应转人工复核提示: %q", refl.Reason)
	}
}

func TestReflector_LowConfidenceWithoutCascadeFlagsHuman(t *testing.T) {
	judge := llm.NewMock()
	judge.Enqueue("reflect", `{"score":90,"verdict":"done","confidence":40,"reason":"初步看完成"}`)
	r := &Reflector{LLM: judge}

	refl := r.Evaluate(context.Background(), "回一句", cascadeExec(), 1)

	if !strings.Contains(refl.Reason, "人工复核") || !strings.Contains(refl.Suggestion, "人工核对") {
		t.Errorf("无更强模型时低置信度应提示人工复核: %+v", refl)
	}
}

func TestReflector_PromptAsksConfidence(t *testing.T) {
	for _, hasChecks := range []bool{false, true} {
		sys := reflectorSystemPrompt(hasChecks, "digest")
		if !strings.Contains(sys, `"confidence"`) {
			t.Errorf("hasChecks=%v 的输出 schema 应含 confidence", hasChecks)
		}
	}
}
func TestRunGoal_LowConfidenceCascadesToMainModel(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.LLM.Provider = "openai"
	f.a.Cfg.LLM.Model = "main-model"
	f.a.Cfg.LLM.Tiers = map[string]string{TierCoding: "coder-model"}
	f.a.Cfg.Agent.MaxReplans = 1
	coder := llm.NewMock()
	coder.Model = "coder-model"
	coder.Enqueue("plan",
		`{"steps":[{"id":"s1","tool":"reply","args":{"text":"好"}}]}`,
		`{"steps":[{"id":"s1","tool":"reply","args":{"text":"好的，已完成"}}]}`)
	coder.Enqueue("reflect",
		`{"score":90,"verdict":"done","confidence":30,"reason":"初步看完成"}`,
		`{"score":95,"verdict":"done","confidence":90,"reason":"复核后确认完成"}`)
	f.a.tierClients = map[string]llm.Client{TierCoding: coder}
	f.llm.Enqueue("reflect", `{"score":40,"verdict":"replan","confidence":85,"reason":"输出与目标不符"}`)

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "回一句", Role: "coder"})

	cascaded := 0
	for _, c := range f.llm.Calls {
		if llm.KindOf(c.System) == "reflect" {
			cascaded++
		}
	}
	if cascaded != 1 {
		t.Fatalf("执行者低置信度时主模型应被级联调用一次反思，实际 %d 次", cascaded)
	}
	plans := 0
	for _, c := range coder.Calls {
		if llm.KindOf(c.System) == "plan" {
			plans++
		}
	}
	if plans != 2 {
		t.Errorf("级联判 replan 应触发第二轮规划，实际 %d 轮", plans)
	}
	if res.Status != types.GoalSuccess {
		t.Errorf("第二轮高置信度通过后应成功: status=%s err=%s", res.Status, res.Error)
	}
}
