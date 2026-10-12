package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

type countingSink struct {
	mu  sync.Mutex
	got []types.StepResult
}

func (s *countingSink) RecordStep(taskID string, r types.StepResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, r)
}

func (s *countingSink) ids() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]int{}
	for _, r := range s.got {
		m[r.StepID]++
	}
	return m
}

func TestExecute_CarriedStepsAreNotExecuted(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	reg := newTestRegistry()
	reg.MustRegister(&funcTool{name: "tally", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		mu.Lock()
		calls[args["id"].(string)]++
		mu.Unlock()
		return map[string]any{"ok": args["id"]}, nil
	}})

	carried := map[string]types.StepResult{
		"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK, Output: map[string]any{"ok": "s1"}},
	}
	e := &Executor{
		Reg: reg, Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{},
		Carried: carried,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "tally", Args: map[string]any{"id": "s1"}},
		{ID: "s2", Tool: "tally", DependsOn: []string{"s1"}, Args: map[string]any{"id": "s2"}},
		{ID: "s3", Tool: "tally", DependsOn: []string{"s2"}, Args: map[string]any{"id": "s3"}},
	}}
	res := e.Execute(context.Background(), plan, "t1", "auto", false)

	if calls["s1"] != 0 {
		t.Errorf("s1 被沿用了却仍然执行了 %d 次——恢复变成了重跑", calls["s1"])
	}
	if calls["s2"] != 1 || calls["s3"] != 1 {
		t.Errorf("s2/s3 应各执行一次，实际 %d/%d", calls["s2"], calls["s3"])
	}
	if res.Carried != 1 {
		t.Errorf("沿用步数 = %d，应为 1——不单独计数的话，「这次成功 3 步」会被读成「这次跑了 3 步」", res.Carried)
	}
	if res.Succeeded != 3 {
		t.Errorf("成功数 = %d，沿用下来的成功步骤也算成功（结果确实是成功的）", res.Succeeded)
	}
	if !res.ByID["s1"].Carried {
		t.Error("沿用的步骤必须被标注，否则读的人分不清哪几步是这次的")
	}
	if res.ByID["s2"].Carried || res.ByID["s3"].Carried {
		t.Error("真跑过的步骤不能标成沿用")
	}
	if m, ok := res.ByID["s1"].Output.(map[string]any); !ok || m["ok"] != "s1" {
		t.Errorf("沿用的结果内容丢了: %#v", res.ByID["s1"].Output)
	}
}

func TestExecute_CarriedKeepsOriginalOutcome(t *testing.T) {
	e := &Executor{
		Reg: newTestRegistry(), Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{},
		Carried: map[string]types.StepResult{
			"s1": {StepID: "s1", Status: types.StepFailed, Outcome: types.OutcomeFailed, Error: "当时炸了"},
		},
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "fail"},
		{ID: "s2", Tool: "counter", DependsOn: []string{"s1"}},
	}}
	res := e.Execute(context.Background(), plan, "t1", "auto", false)

	if res.ByID["s1"].Status != types.StepFailed || res.ByID["s1"].Error != "当时炸了" {
		t.Errorf("沿用的失败步骤被改写了: %+v", res.ByID["s1"])
	}
	if res.Failed != 1 {
		t.Errorf("沿用下来的失败仍应计入失败数，实际 %d", res.Failed)
	}
	if res.ByID["s2"].Status != types.StepSkipped {
		t.Errorf("依赖未满足应跳过，实际 %+v", res.ByID["s2"])
	}
	if res.ByID["s2"].Carried {
		t.Error("被跳过的步骤不是沿用——它这次是被判定的，不是沿用的")
	}
}

func TestExecute_CarriedStepsReachSinkExactlyOnce(t *testing.T) {
	sink := &countingSink{}
	e := &Executor{
		Reg: newTestRegistry(), Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{},
		Carried: map[string]types.StepResult{
			"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		},
		Sink: sink,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "counter"},
		{ID: "s2", Tool: "counter"},
	}}
	e.Execute(context.Background(), plan, "t1", "auto", false)

	ids := sink.ids()
	if ids["s1"] != 1 || ids["s2"] != 1 {
		t.Errorf("每步应各落一条日志，实际 %v", ids)
	}
}

func TestExecute_NoCarriedIsUnchanged(t *testing.T) {
	ran := 0
	reg := newTestRegistry()
	reg.MustRegister(&funcTool{name: "tally", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		ran++
		return "ok", nil
	}})
	e := &Executor{Reg: reg, Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{}}
	plan := types.Plan{Steps: []types.Step{{ID: "s1", Tool: "tally"}, {ID: "s2", Tool: "tally"}}}
	res := e.Execute(context.Background(), plan, "t1", "auto", false)

	if ran != 2 {
		t.Errorf("无 Carried 时每步都应执行，实际执行 %d 次", ran)
	}
	if res.Carried != 0 {
		t.Errorf("沿用步数应为 0，实际 %d", res.Carried)
	}
}

func TestExecute_CarriedReleasesDependentsPromptly(t *testing.T) {
	e := &Executor{
		Reg: newTestRegistry(), Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: NopNotifier{},
		Carried: map[string]types.StepResult{
			"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
			"s2": {StepID: "s2", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		},
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "counter"},
		{ID: "s2", Tool: "counter", DependsOn: []string{"s1"}},
		{ID: "s3", Tool: "counter", DependsOn: []string{"s2"}},
	}}
	done := make(chan *Result, 1)
	go func() { done <- e.Execute(context.Background(), plan, "t1", "auto", false) }()
	select {
	case res := <-done:
		if res.Carried != 2 || res.Succeeded != 3 {
			t.Errorf("沿用 2 步、总成功 3 步，实际沿用 %d 成功 %d", res.Carried, res.Succeeded)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("执行没有返回：沿用步骤的完成信号没有放行下游（预置晚于 goroutine 启动）")
	}
}

func TestExecute_CarriedProgressIsDistinguished(t *testing.T) {
	n := &recordingNotifier{}
	e := &Executor{
		Reg: newTestRegistry(), Gate: safety.New("auto", nil, nil, nil, time.Second), Notifier: n,
		Carried: map[string]types.StepResult{
			"s1": {StepID: "s1", Status: types.StepSucceeded, Outcome: types.OutcomeOK},
		},
	}
	plan := types.Plan{Steps: []types.Step{{ID: "s1", Tool: "counter"}, {ID: "s2", Tool: "counter"}}}
	e.Execute(context.Background(), plan, "t1", "auto", false)

	joined := ""
	for _, ev := range n.progress {
		joined += ev.Message + "\n"
	}
	if !strings.Contains(joined, "沿用") {
		t.Errorf("进度里应出现「沿用」字样，实际：\n%s", joined)
	}
}
