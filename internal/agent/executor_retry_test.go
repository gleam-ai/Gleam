package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

func TestExecute_TimeoutRetrySucceeds(t *testing.T) {
	var calls int32
	r := registry.New()
	r.MustRegister(&funcTool{name: "flaky", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return "recovered", nil
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		StepTimeout: 80 * time.Millisecond, StepRetries: 2, Notifier: &recordingNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "flaky"}}}, "t", "auto", false)
	if res.Steps[0].Status != types.StepSucceeded {
		t.Fatalf("重试后应成功: %+v", res.Steps[0])
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("工具调用次数 = %d，应为 2", got)
	}
}

func TestExecute_TimeoutRetriesExhausted(t *testing.T) {
	var calls int32
	r := registry.New()
	r.MustRegister(&funcTool{name: "always-slow", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&calls, 1)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		StepTimeout: 60 * time.Millisecond, StepRetries: 1, Notifier: &recordingNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "always-slow"}}}, "t", "auto", false)
	if res.Steps[0].Status != types.StepFailed {
		t.Fatalf("重试耗尽应失败: %+v", res.Steps[0])
	}
	if !strings.Contains(res.Steps[0].Error, "超时") {
		t.Errorf("错误应标明超时: %q", res.Steps[0].Error)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("工具调用次数 = %d，应为 2（1 次执行 + 1 次重试）", got)
	}
}

func TestExecute_TimeoutNotRetriedForSideEffects(t *testing.T) {
	var calls int32
	r := registry.New()
	r.MustRegister(&funcTool{name: "write-slow", perm: types.PermissionUserApproved, fn: func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&calls, 1)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		StepTimeout: 60 * time.Millisecond, StepRetries: 3, Notifier: &recordingNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "write-slow"}}}, "t", "auto", true)
	if res.Steps[0].Status != types.StepFailed {
		t.Fatalf("超时的写操作应失败: %+v", res.Steps[0])
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("写操作超时不应自动重试，调用次数 = %d", got)
	}
}

func TestExecute_AcceptsLateSuccess(t *testing.T) {
	var calls int32
	r := registry.New()
	r.MustRegister(&funcTool{name: "late", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&calls, 1)
		<-ctx.Done()
		return "done", nil // finished at the deadline, but it did finish
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		StepTimeout: 50 * time.Millisecond, StepRetries: 2, Notifier: &recordingNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "late"}}}, "t", "auto", false)
	if res.Steps[0].Status != types.StepSucceeded {
		t.Fatalf("截止时刻完成的结果应被接受: %+v", res.Steps[0])
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("已成功的步骤不应重试，调用次数 = %d", got)
	}
}

func TestExecute_NonTimeoutErrorNotRetried(t *testing.T) {
	var calls int32
	r := registry.New()
	r.MustRegister(&funcTool{name: "plain-fail", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("boom")
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		StepTimeout: time.Second, StepRetries: 3, Notifier: &recordingNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "plain-fail"}}}, "t", "auto", false)
	if res.Steps[0].Status != types.StepFailed {
		t.Fatalf("应失败: %+v", res.Steps[0])
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("非超时错误不应重试，调用次数 = %d", got)
	}
}

func TestExecute_ZeroRetriesKeepsOldBehavior(t *testing.T) {
	var calls int32
	r := registry.New()
	r.MustRegister(&funcTool{name: "slow-once", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&calls, 1)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	e := &Executor{Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		StepTimeout: 50 * time.Millisecond, StepRetries: 0, Notifier: NopNotifier{}}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "slow-once"}}}, "t", "auto", false)
	if res.Steps[0].Status != types.StepFailed {
		t.Fatalf("应失败: %+v", res.Steps[0])
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("StepRetries=0 时不应重试，调用次数 = %d", got)
	}
}
