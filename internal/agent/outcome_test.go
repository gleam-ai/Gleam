package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

type outcomeTool struct {
	name    string
	out     any
	outcome types.StepOutcome
	note    string
}

func (t *outcomeTool) Name() string                 { return t.name }
func (t *outcomeTool) Description() string          { return "outcome tool " + t.name }
func (t *outcomeTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *outcomeTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}}
}
func (t *outcomeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.out, nil
}
func (t *outcomeTool) Outcome(args map[string]any, out any) (types.StepOutcome, string) {
	return t.outcome, t.note
}

func outcomeExec(t *testing.T, tool types.Tool, steps ...types.Step) *Result {
	t.Helper()
	r := registry.New()
	r.MustRegister(tool)
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{}}
	return e.Execute(context.Background(), types.Plan{Steps: steps}, "t1", "auto", false)
}

func TestExecute_BusinessFailureCountsAsFailed(t *testing.T) {
	tool := &outcomeTool{
		name: "bizfail", out: map[string]any{"exit_code": 1},
		outcome: types.OutcomeFailed, note: "命令以退出码 1 结束",
	}
	res := outcomeExec(t, tool, types.Step{ID: "s1", Tool: "bizfail"})

	if res.Failed != 1 || res.Succeeded != 0 {
		t.Fatalf("业务失败应计入 Failed，实际 Succeeded=%d Failed=%d", res.Succeeded, res.Failed)
	}
	if got := statusOfExec(res); got != types.GoalFailed {
		t.Errorf("状态应为 failed，实际 %s", got)
	}
	st := res.Steps[0]
	if st.Status != types.StepSucceeded || st.Outcome != types.OutcomeFailed {
		t.Errorf("Status/Outcome 应分别为 succeeded/failed，实际 %s/%s", st.Status, st.Outcome)
	}
	if st.Error == "" {
		t.Error("业务失败的原因必须抬到 Error，否则反思器与用户都看不见")
	}
}

func TestExecute_EmptyIsNotFailure(t *testing.T) {
	tool := &outcomeTool{
		name: "empty", out: map[string]any{"count": 0, "files": []string{}},
		outcome: types.OutcomeEmpty,
	}
	res := outcomeExec(t, tool, types.Step{ID: "s1", Tool: "empty"})

	if res.Succeeded != 1 || res.Failed != 0 {
		t.Fatalf("空结果不该算失败，实际 Succeeded=%d Failed=%d", res.Succeeded, res.Failed)
	}
	if res.Empty != 1 {
		t.Errorf("空结果应被单独计数，实际 Empty=%d", res.Empty)
	}
	if got := statusOfExec(res); got != types.GoalSuccess {
		t.Errorf("空结果不是错误，状态应为 success，实际 %s", got)
	}
}

func TestExecute_OutcomeSurvivesDedupe(t *testing.T) {
	r := registry.New()
	r.MustRegister(&outcomeTool{
		name: "listed", out: map[string]any{"count": 0},
		outcome: types.OutcomeEmpty,
	})
	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, Dedupe: true, StepTimeout: 2 * time.Second,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "listed"},
		{ID: "s2", Tool: "listed", DependsOn: []string{"s1"}},
	}}
	res := e.Execute(context.Background(), plan, "t1", "auto", false)

	if !res.Steps[1].Deduped {
		t.Fatalf("第二步应走复用，实际 %+v", res.Steps[1])
	}
	if res.Empty != 2 {
		t.Errorf("复用的空结果仍应计为 Empty，实际 Empty=%d", res.Empty)
	}
	if res.Steps[1].Outcome != types.OutcomeEmpty {
		t.Errorf("复用后 Outcome 应保持 empty，实际 %q", res.Steps[1].Outcome)
	}
}

func TestBuildStepDigest_ReportsOutcomeAndFailure(t *testing.T) {
	exec := &Result{
		Total: 3,
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "shell.exec", Status: types.StepSucceeded, Outcome: types.OutcomeFailed, Error: "命令以退出码 1 结束"},
			{StepID: "s2", Tool: "file.search", Status: types.StepSucceeded, Outcome: types.OutcomeEmpty},
			{StepID: "s3", Tool: "file.write", Status: types.StepFailed, Error: "路径超出允许的工作区范围"},
		},
	}
	got := buildStepDigest(exec)
	for _, want := range []string{"s1", "退出码 1", "s2", "结果为空", "s3", "路径超出"} {
		if !strings.Contains(got, want) {
			t.Errorf("摘要应包含 %q，实际：\n%s", want, got)
		}
	}
	if buildStepDigest(nil) != "" {
		t.Error("nil 执行结果应返回空串")
	}
}

func TestBuildDigest_TellsReflectorAboutOutcome(t *testing.T) {
	exec := &Result{
		Total: 2,
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "shell.exec", Status: types.StepSucceeded, Outcome: types.OutcomeFailed, Error: "命令以退出码 1 结束"},
			{StepID: "s2", Tool: "file.search", Status: types.StepSucceeded, Outcome: types.OutcomeEmpty},
		},
	}
	got := buildDigest("目标", exec, 1, nil, nil)
	if !strings.Contains(got, "执行失败") {
		t.Errorf("业务失败应显式写成执行失败，实际：\n%s", got)
	}
	if !strings.Contains(got, "空结果") {
		t.Errorf("空结果应显式标出，实际：\n%s", got)
	}
}

func TestRunGoal_ReplanFeedbackCarriesStepResults(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"调用会失败的工具","tool":"fail","args":{}}]}`),
		reflectScript(40, "replan", "步骤失败"),
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "跑一次"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("任务未成功: %+v", res)
	}

	var plans []llm.ChatRequest
	for _, c := range f.llm.Calls {
		if llm.KindOf(c.System) == "plan" {
			plans = append(plans, c)
		}
	}
	if len(plans) < 2 {
		t.Fatalf("应有至少两次规划调用，实际 %d", len(plans))
	}
	second := plans[1].System
	if !strings.Contains(second, "重规划反馈") {
		t.Error("第二次规划的提示词里没有重规划反馈段")
	}
	if !strings.Contains(second, "boom") {
		t.Errorf("反馈里应带上上次失败的具体原因（fail 工具返回的 boom）")
	}
	if !strings.Contains(second, "s1") {
		t.Error("反馈里应指明是哪个步骤出的问题")
	}
}
