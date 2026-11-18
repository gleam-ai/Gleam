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

func TestCompletionStatus_AcceptanceDriven(t *testing.T) {
	// Acceptance met (completionReached already verified it) means success even
	// if some intermediate steps failed: result-oriented, not activity-oriented.
	exec := &Result{Total: 3, Succeeded: 2, Failed: 1}
	if got := completionStatus(exec, []string{"文件已写入"}); got != types.GoalSuccess {
		t.Errorf("验收满足应判 success（即便有步骤失败），实际 %s", got)
	}
	if got := completionStatus(exec, nil); got != types.GoalPartial {
		t.Errorf("无验收标准应回退活动导向 partial，实际 %s", got)
	}
	allOK := &Result{Total: 2, Succeeded: 2}
	if got := completionStatus(allOK, nil); got != types.GoalSuccess {
		t.Errorf("无验收标准但全成功应回退 success，实际 %s", got)
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

// The reported case: PDF extraction failed and the report was never written,
// yet the model's reply said "done". No score, verdict or acceptance
// checklist may turn that draft into a completed goal.
func TestCompletionReached_ReplyIncompleteBlocks(t *testing.T) {
	exec := &Result{
		Total: 4, Succeeded: 2, Failed: 1, Skipped: 1,
		ReplyText:       "已完成《markdown.pdf》的阅读与简报整理。",
		ReplyIncomplete: true,
	}
	refl := types.Reflection{Score: 100, Verdict: "done", Reason: "报告已完成"}
	if completionReached(refl, nil, exec, 80) {
		t.Error("降级答复 + 无验收标准时不得判完成")
	}
	if completionReached(refl, []string{"简报已生成"}, exec, 80) {
		t.Error("降级答复 + 有验收标准时同样不得判完成")
	}
	healthy := &Result{Total: 1, Succeeded: 1, ReplyText: "答案"}
	if !completionReached(refl, nil, healthy, 80) {
		t.Error("正常答复不该被新闸门误伤")
	}
	if got := completionStatus(exec, []string{"简报已生成"}); got == types.GoalSuccess {
		t.Errorf("降级答复不得被验收导向判成 success，实际 %s", got)
	}
	if got := statusOfExec(&Result{Total: 1, Succeeded: 1, ReplyIncomplete: true}); got != types.GoalPartial {
		t.Errorf("只有降级答复成功时也必须是 partial，实际 %s", got)
	}
}

func TestBuildResult_ReplyIncompleteDoesNotClaimDone(t *testing.T) {
	exec := &Result{
		Total: 4, Succeeded: 2, Failed: 1, Skipped: 1,
		ReplyText:       "已完成《markdown.pdf》的阅读与简报整理。报告内容：（步骤 s4 未成功：依赖步骤未成功: s3）",
		ReplyIncomplete: true,
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "file.write", Status: types.StepSucceeded},
			{StepID: "s2", Tool: "shell.exec", Status: types.StepSucceeded, Outcome: types.OutcomeFailed, Error: "命令以退出码 1 结束"},
			{StepID: "s3", Tool: "file.read", Status: types.StepFailed, Error: "cannot find the file specified"},
			{StepID: "s4", Tool: "reply", Status: types.StepSucceeded},
		},
	}
	refl := types.Reflection{Score: 100, Verdict: "done", Reason: "报告已完成"}
	res := (&Agent{}).buildResult("阅读附件文档，提炼关键结论并整理成简报", exec, refl, nil, types.ContextBreakdown{}, "t1")
	if strings.Contains(res.Summary, "已完成《markdown.pdf》") || strings.Contains(res.Summary, "报告已完成") {
		t.Errorf("摘要不得复述模型或裁判的完成声明，实际 %q", res.Summary)
	}
	if !strings.Contains(res.Summary, "任务未完成") || !strings.Contains(res.Summary, "草稿未予采信") {
		t.Errorf("摘要必须明说未完成，实际 %q", res.Summary)
	}
	if res.Error == "" {
		t.Error("结果应带上无法判定为完成的原因")
	}
	if completionReached(refl, nil, exec, 80) {
		t.Error("裁判给 100/done 也必须被闸门拦住")
	}
}

func TestHeuristicReflection_ReplyIncompleteIsNotDone(t *testing.T) {
	exec := &Result{Total: 1, Succeeded: 1, ReplyIncomplete: true}
	got := heuristicReflection(exec, "无反思器", false)
	if got.Verdict == "done" || got.Score >= 80 {
		t.Errorf("反思器不可用时，降级答复也不能兜底成 done，实际 %+v", got)
	}
	if !strings.Contains(got.Reason, "不完整") {
		t.Errorf("原因应说明答复不完整，实际 %q", got.Reason)
	}
}

func TestBuildStepDigest_ReplyIncompleteTellsNextRound(t *testing.T) {
	got := buildStepDigest(&Result{Total: 2, Succeeded: 1, Failed: 1, ReplyIncomplete: true,
		Steps: []types.StepResult{{StepID: "s1", Tool: "shell.exec", Status: types.StepFailed, Error: "退出码 1"}}})
	if !strings.Contains(got, "不要在答复里声称完成") {
		t.Errorf("重规划反馈应明确禁止声称完成，实际 %q", got)
	}
}

func TestRunGoal_DegradedReplyNeverClaimsDone(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[
			{"id":"s1","description":"extract PDF text","tool":"fail","args":{}},
			{"id":"s2","description":"final brief","tool":"reply","depends_on":["s1"],"args":{"text":"已完成《markdown.pdf》的阅读与简报整理。报告内容：{ref:s1.text}"}}
		]}`),
		reflectScript(100, "done", "报告已完成"),
	})
	f.a.Cfg.Agent.MaxReplans = 0
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "阅读附件文档，提炼关键结论并整理成简报", Mode: "auto"})

	if res.Status == types.GoalSuccess {
		t.Errorf("依赖失败却自称完成的任务不该成功，实际 %+v", res)
	}
	if strings.Contains(res.Summary, "已完成《markdown.pdf》") || strings.Contains(res.Summary, "报告已完成") {
		t.Errorf("最终摘要不得复述模型或裁判的完成声明，实际 %q", res.Summary)
	}
	if !strings.Contains(res.Summary, "任务未完成") {
		t.Errorf("最终摘要必须明说未完成，实际 %q", res.Summary)
	}
	if res.Error == "" {
		t.Error("结果应带上无法判定为完成的原因")
	}
	var draftKept bool
	for _, st := range res.Steps {
		if st.Tool == "reply" && st.Status == types.StepSucceeded {
			draftKept = true
		}
	}
	if !draftKept {
		t.Error("best-effort 草稿应仍保留在执行结果里，供执行详情查看")
	}
}
