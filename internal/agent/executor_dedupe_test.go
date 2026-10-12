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

type countingTool struct {
	calls int64
	err   error
}

func (c *countingTool) Name() string        { return "counter" }
func (c *countingTool) Description() string { return "counting tool" }
func (c *countingTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (c *countingTool) Permission() types.Permission { return types.PermissionReadOnly }
func (c *countingTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	atomic.AddInt64(&c.calls, 1)
	if c.err != nil {
		return nil, c.err
	}
	return map[string]any{"n": 1}, nil
}

func dedupeRegistry(t *countingTool) *registry.Registry {
	r := registry.New()
	r.MustRegister(t)
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"text": args["text"]}, nil
		}})
	return r
}

func dedupePlan() types.Plan {
	return types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "counter", Args: map[string]any{"k": "v"}},
		{ID: "s2", Tool: "counter", Args: map[string]any{"k": "v"}},
		{ID: "s3", Tool: "reply", Args: map[string]any{"text": "ok"}, DependsOn: []string{"s1", "s2"}},
	}}
}

func TestExecutor_DedupesIdenticalReadOnlyCalls(t *testing.T) {
	tool := &countingTool{}
	e := &Executor{
		Reg: dedupeRegistry(tool), Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, Dedupe: true, StepTimeout: 2 * time.Second,
	}
	res := e.Execute(context.Background(), dedupePlan(), "t1", string(types.ModeAuto), false)
	if res.Succeeded != 3 {
		t.Fatalf("三个步骤都应成功: %+v", res.Steps)
	}
	if got := atomic.LoadInt64(&tool.calls); got != 1 {
		t.Fatalf("相同只读调用应只执行 1 次，实际 %d", got)
	}
	if res.ByID["s1"].Deduped == res.ByID["s2"].Deduped {
		t.Fatalf("应恰好一个步骤被标记为去重，实际 s1=%v s2=%v",
			res.ByID["s1"].Deduped, res.ByID["s2"].Deduped)
	}
}

func TestExecutor_DedupeCountsReported(t *testing.T) {
	tool := &countingTool{}
	var deduped, toolCalls int
	e := &Executor{
		Reg: dedupeRegistry(tool), Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, Dedupe: true, StepTimeout: 2 * time.Second,
		OnUsage: func(_ string, tc, _, dd int) { toolCalls += tc; deduped += dd },
	}
	e.Execute(context.Background(), dedupePlan(), "t1", string(types.ModeAuto), false)
	if deduped != 1 {
		t.Fatalf("应上报 1 次去重，实际 %d", deduped)
	}
	if toolCalls != 2 {
		t.Fatalf("工具调用次数应为 2，实际 %d", toolCalls)
	}
}

func TestExecutor_DedupeDisabled(t *testing.T) {
	tool := &countingTool{}
	e := &Executor{
		Reg: dedupeRegistry(tool), Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, Dedupe: false, StepTimeout: 2 * time.Second,
	}
	e.Execute(context.Background(), dedupePlan(), "t1", string(types.ModeAuto), false)
	if got := atomic.LoadInt64(&tool.calls); got != 2 {
		t.Fatalf("关闭去重时应执行 2 次，实际 %d", got)
	}
}

func TestExecutor_SkipsRepeatedFailedCall(t *testing.T) {
	tool := &countingTool{err: errors.New("boom")}
	e := &Executor{
		Reg: dedupeRegistry(tool), Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, Dedupe: true, StepTimeout: 2 * time.Second,
	}
	res := e.Execute(context.Background(), dedupePlan(), "t1", string(types.ModeAuto), false)
	if got := atomic.LoadInt64(&tool.calls); got != 1 {
		t.Fatalf("失败调用不应重复执行，实际执行 %d 次", got)
	}

	executed, deduped := 0, 0
	for _, id := range []string{"s1", "s2"} {
		r := res.ByID[id]
		switch {
		case r.Deduped:
			if r.Status != types.StepFailed {
				t.Errorf("%s 被去重跳过时应记为失败，实际 %s", id, r.Status)
			}
			if !strings.Contains(r.Error, "已跳过") {
				t.Errorf("%s 的跳过原因应说明是重复的失败调用，实际 %q", id, r.Error)
			}
			deduped++
		case r.Status == types.StepFailed:
			executed++
		}
	}
	if executed != 1 || deduped != 1 {
		t.Fatalf("两次相同调用应恰好一次真执行、一次去重跳过，实际 executed=%d deduped=%d", executed, deduped)
	}
}

func TestExecutor_DedupeKeepsDistinctArgs(t *testing.T) {
	tool := &countingTool{}
	e := &Executor{
		Reg: dedupeRegistry(tool), Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, Dedupe: true, StepTimeout: 2 * time.Second,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "counter", Args: map[string]any{"k": "a"}},
		{ID: "s2", Tool: "counter", Args: map[string]any{"k": "b"}},
	}}
	e.Execute(context.Background(), plan, "t1", string(types.ModeAuto), false)
	if got := atomic.LoadInt64(&tool.calls); got != 2 {
		t.Fatalf("参数不同应各执行一次，实际 %d", got)
	}
}
