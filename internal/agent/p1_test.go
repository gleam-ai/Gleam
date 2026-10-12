package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gleam/internal/harness/memory"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestExecute_ErrorKindOnPermissionDenied(t *testing.T) {
	r := registry.New()
	r.MustRegister(&funcTool{name: "guard", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return nil, errors.New("permission denied: 403")
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "guard"}}}, "t", "auto", false)

	st := res.Steps[0]
	if st.ErrorKind != types.ErrPermission {
		t.Errorf("权限拒绝应归为 permission，实际 %q（error=%q）", st.ErrorKind, st.Error)
	}
}

func TestExecute_TimeoutClassifiedAndAttemptCounted(t *testing.T) {
	var calls int32
	r := registry.New()
	r.MustRegister(&funcTool{name: "always-slow", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&calls, 1)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		StepTimeout: 50 * time.Millisecond, StepRetries: 1, Notifier: NopNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "always-slow"}}}, "t", "auto", false)

	st := res.Steps[0]
	if st.ErrorKind != types.ErrTimeout {
		t.Errorf("超时应归为 timeout，实际 %q", st.ErrorKind)
	}
	if st.Attempt != 2 || !st.Retried {
		t.Errorf("重试 1 次后耗尽应记 Attempt=2、Retried=true，实际 Attempt=%d Retried=%v", st.Attempt, st.Retried)
	}
}

func TestExecute_FirstTrySuccessNotMarkedRetried(t *testing.T) {
	r := registry.New()
	r.MustRegister(&funcTool{name: "ok", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return "fine", nil
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "ok"}}}, "t", "auto", false)

	st := res.Steps[0]
	if st.Attempt != 1 || st.Retried {
		t.Errorf("一次成功应记 Attempt=1、Retried=false，实际 Attempt=%d Retried=%v", st.Attempt, st.Retried)
	}
}

func TestBuildResult_FailureBreakdownAndExecutedPlan(t *testing.T) {
	exec := &Result{
		Total: 2,
		Steps: []types.StepResult{
			{StepID: "s1", Tool: "shell.exec", Status: types.StepSucceeded, Outcome: types.OutcomeFailed, Error: "命令以退出码 1 结束", ErrorKind: types.ErrBusiness},
			{StepID: "s2", Tool: "web.fetch", Status: types.StepFailed, Error: "permission denied", ErrorKind: types.ErrPermission},
		},
	}
	exec.ExecutedPlan = types.Plan{Goal: "g", Steps: []types.Step{{ID: "s1", Tool: "shell.exec"}, {ID: "s2", Tool: "web.fetch"}}}

	res := (&Agent{}).buildResult("g", exec, types.Reflection{Score: 40}, nil, types.ContextBreakdown{Instruction: 10, State: 5, Total: 15}, "t1")

	if res.FailureBreakdown[types.ErrBusiness] != 1 || res.FailureBreakdown[types.ErrPermission] != 1 {
		t.Errorf("归因分布应含 business=1、permission=1，实际 %v", res.FailureBreakdown)
	}
	if res.ExecutedPlan == nil || len(res.ExecutedPlan.Steps) != 2 {
		t.Fatalf("ExecutedPlan 应带全部步骤，实际 %+v", res.ExecutedPlan)
	}
	if res.PromptBreakdown == nil || res.PromptBreakdown.Total == 0 {
		t.Error("PromptBreakdown 应随结果携带")
	}
}

func TestBuildSystemPromptMeasured_SumsAndLayout(t *testing.T) {
	p := &Planner{Reg: newTestRegistry(), TaskMode: string(types.TaskWork), Role: "general", MaxSteps: 12}
	cwd := t.TempDir()
	recent := []memory.Turn{{Role: "user", Content: "帮我整理下载目录"}, {Role: "assistant", Content: "好的，已列出 12 个文件"}}
	summary := "早期对话摘要：用户偏好按类型分文件夹。"

	prompt, bd := p.buildSystemPromptMeasured("整理下载目录", cwd, recent, nil, summary, "")
	plain := p.buildSystemPrompt("整理下载目录", cwd, recent, nil, summary, "")
	if prompt != plain {
		t.Error("计量版与普通版输出必须逐字节一致——加计量不能悄悄改提示词")
	}

	if bd.Total == 0 {
		t.Fatal("Total 不应为 0")
	}
	if sum := bd.Instruction + bd.Capability + bd.Knowledge + bd.State; sum != bd.Total {
		t.Fatalf("分段之和 %d != 总长 %d，说明有段漏计", sum, bd.Total)
	}
	if bd.State == 0 {
		t.Error("带对话与摘要时状态段不应为 0")
	}
	if bd.Capability == 0 {
		t.Error("能力段（菜单+schema）不应为 0")
	}
}

func TestDeriveTraceID_ClustersSameInput(t *testing.T) {
	a1 := deriveTraceID("同一个目标", "work", "coder", "mock")
	a2 := deriveTraceID("同一个目标", "work", "coder", "mock")
	b := deriveTraceID("另一个目标", "work", "coder", "mock")

	if a1 != a2 {
		t.Errorf("同输入应得同 trace_id：%s vs %s", a1, a2)
	}
	if a1 == b {
		t.Error("不同目标不应碰撞出相同 trace_id")
	}
	if len(a1) != 10 {
		t.Errorf("trace_id 应为 10 位十六进制，实际 %q", a1)
	}
}

func TestRunGoal_CarriesTraceAndBreakdown(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "带 trace 的目标"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("任务未成功: %+v", res)
	}
	if len(res.TraceID) != 10 {
		t.Errorf("TraceID 应为 10 位十六进制，实际 %q", res.TraceID)
	}
	if res.PromptBreakdown == nil || res.PromptBreakdown.Total == 0 {
		t.Error("PromptBreakdown 应随产出携带")
	}
	if res.ExecutedPlan == nil || len(res.ExecutedPlan.Steps) != 1 {
		t.Errorf("ExecutedPlan 应带 1 个步骤，实际 %+v", res.ExecutedPlan)
	}
}

type denyNotifier struct{ NopNotifier }

func (d denyNotifier) OnApproval(req types.ApprovalRequest) types.ApprovalResponse {
	return types.ApprovalResponse{Approved: false, Note: "此地无银"}
}

func TestExecute_ApprovalRejectionClassifiedPermission(t *testing.T) {
	r := registry.New()
	r.MustRegister(&funcTool{name: "wants-approval", perm: types.PermissionUserApproved, fn: func(ctx context.Context, args map[string]any) (any, error) {
		return "done", nil
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: denyNotifier{}, StepTimeout: time.Second}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "wants-approval"}}}, "t", "auto", false)

	st := res.Steps[0]
	if st.Status != types.StepFailed {
		t.Fatalf("审批拒绝应失败，实际 %s", st.Status)
	}
	if st.ErrorKind != types.ErrPermission {
		t.Errorf("审批拒绝应归为 permission，实际 %q（error=%q）", st.ErrorKind, st.Error)
	}
}
