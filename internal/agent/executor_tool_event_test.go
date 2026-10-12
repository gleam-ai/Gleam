package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

type toolRecordingNotifier struct {
	recordingNotifier
	tools []types.ToolEvent
}

func (n *toolRecordingNotifier) OnTool(ev types.ToolEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tools = append(n.tools, ev)
}

func (n *toolRecordingNotifier) byStep(t *testing.T) map[string][]types.ToolEvent {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	out := map[string][]types.ToolEvent{}
	for _, ev := range n.tools {
		out[ev.StepID] = append(out[ev.StepID], ev)
	}
	for id, evs := range out {
		var starts, ends int
		for _, ev := range evs {
			switch ev.Phase {
			case types.ToolStart:
				starts++
			case types.ToolEnd:
				ends++
			}
		}
		if starts > 1 || ends > 1 {
			t.Errorf("%s 的工具事件不成对：start %d 次 / end %d 次（%+v）", id, starts, ends, evs)
		}
	}
	return out
}

func (n *toolRecordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.tools)
}

func TestExecute_ToolEventsArePaired(t *testing.T) {
	n := &toolRecordingNotifier{}
	e := &Executor{
		Reg:      newTestRegistry(),
		Gate:     safety.New("auto", nil, nil, nil, time.Second),
		Notifier: n,
	}
	e.Execute(context.Background(), types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "counter"},
		{ID: "s2", Tool: "counter", DependsOn: []string{"s1"}},
	}}, "t1", "auto", false)

	byStep := n.byStep(t)
	if len(byStep) != 2 {
		t.Fatalf("两步都该有工具事件，实得 %d 步：%+v", len(byStep), byStep)
	}
	for id, evs := range byStep {
		if len(evs) != 2 {
			t.Fatalf("%s 应恰好一对（start + end），实得 %d 条：%+v", id, len(evs), evs)
		}
		if evs[0].Phase != types.ToolStart || evs[1].Phase != types.ToolEnd {
			t.Errorf("%s 的顺序应是 start → end，实得 %s → %s", id, evs[0].Phase, evs[1].Phase)
		}
		end := evs[1]
		if end.Status != types.ToolDone {
			t.Errorf("%s 成功收口应是 done，实得 %s", id, end.Status)
		}
		if end.Tool != "counter" {
			t.Errorf("%s 应带工具名，实得 %q", id, end.Tool)
		}
		if end.TaskID != "t1" {
			t.Errorf("%s 应带 task_id，实得 %q", id, end.TaskID)
		}
		if end.Title == "" {
			t.Errorf("%s 缺人读描述", id)
		}
	}
}

func TestExecute_ToolEventCarriesFailure(t *testing.T) {
	n := &toolRecordingNotifier{}
	e := &Executor{
		Reg:      newTestRegistry(),
		Gate:     safety.New("auto", nil, nil, nil, time.Second),
		Notifier: n,
	}
	e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "fail"}}}, "t1", "auto", false)

	evs := n.byStep(t)["s1"]
	if len(evs) != 2 {
		t.Fatalf("失败步骤同样该有一对事件，实得 %+v", evs)
	}
	end := evs[1]
	if end.Status != types.ToolFailed {
		t.Errorf("失败收口应是 failed，实得 %s", end.Status)
	}
	if !strings.Contains(end.Error, "boom") {
		t.Errorf("失败原因该带过去，实得 %q", end.Error)
	}
}

func TestExecute_ToolEventReportsSkipped(t *testing.T) {
	n := &toolRecordingNotifier{}
	e := &Executor{
		Reg:      newTestRegistry(),
		Gate:     safety.New("auto", nil, nil, nil, time.Second),
		Notifier: n,
	}
	e.Execute(context.Background(), types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "fail"},
		{ID: "s2", Tool: "counter", DependsOn: []string{"s1"}},
	}}, "t1", "auto", false)

	evs := n.byStep(t)["s2"]
	if len(evs) != 1 || evs[0].Phase != types.ToolEnd {
		t.Fatalf("被跳过的步骤应只有一条 end，实得 %+v", evs)
	}
	if evs[0].Status != types.ToolSkipped {
		t.Errorf("状态应是 skipped，实得 %s", evs[0].Status)
	}
	if !strings.Contains(evs[0].Error, "s1") {
		t.Errorf("跳过原因该指出是哪一步没过，实得 %q", evs[0].Error)
	}
}

func TestExecute_ToolEventOnPanic(t *testing.T) {
	reg := newTestRegistry()
	reg.MustRegister(&funcTool{name: "boom", perm: types.PermissionReadOnly, fn: func(ctx context.Context, args map[string]any) (any, error) {
		panic("工具炸了")
	}})
	n := &toolRecordingNotifier{}
	e := &Executor{
		Reg:      reg,
		Gate:     safety.New("auto", nil, nil, nil, time.Second),
		Notifier: n,
	}
	e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "boom"}}}, "t1", "auto", false)

	evs := n.byStep(t)["s1"]
	if len(evs) != 2 || evs[1].Phase != types.ToolEnd {
		t.Fatalf("panic 步骤也要有 start + end，实得 %+v", evs)
	}
	if evs[1].Status != types.ToolFailed {
		t.Errorf("panic 收口应是 failed，实得 %s", evs[1].Status)
	}
	if evs[1].Error == "" {
		t.Error("panic 收口必须带原因，否则界面上只剩一个没有解释的红叉")
	}
}

func TestExecute_NoToolNotifierIsFine(t *testing.T) {
	e := &Executor{
		Reg:      newTestRegistry(),
		Gate:     safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{},
	}
	res := e.Execute(context.Background(), types.Plan{Steps: []types.Step{{ID: "s1", Tool: "counter"}}}, "t1", "auto", false)
	if res.Succeeded != 1 {
		t.Errorf("没有 ToolNotifier 时任务照跑，实得 %+v", res)
	}
}

func TestToolDetail(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"路径优先", map[string]any{"content": "很长的一段正文", "path": `C:\ws\a.go`}, `C:\ws\a.go`},
		{"命令", map[string]any{"command": "go test ./..."}, "go test ./..."},
		{"多行压成一行", map[string]any{"command": "go build\n  ./..."}, "go build ./..."},
		{"凭据字段被跳过", map[string]any{"api_key": "sk-真值", "path": `C:\ws\a.go`}, `C:\ws\a.go`},
		{"只有凭据就留空", map[string]any{"api_key": "sk-真值"}, ""},
		{"取不到就留空", map[string]any{"content": "只有正文"}, ""},
		{"结构化参数不当详情", map[string]any{"items": []any{"a", "b"}, "query": "关键词"}, "关键词"},
		{"空参数", nil, ""},
	}
	for _, c := range cases {
		if got := toolDetail(c.args); got != c.want {
			t.Errorf("%s：toolDetail 实得 %q，应为 %q", c.name, got, c.want)
		}
	}
	for _, k := range []string{"token", "password", "secret", "credential", "api_key"} {
		if got := toolDetail(map[string]any{k: "值", "command": "x"}); got != "x" {
			t.Errorf("含 %s 时不该把它当详情，实得 %q", k, got)
		}
	}
	if got := toolDetail(map[string]any{"command": strings.Repeat("x", 500)}); len([]rune(got)) > 125 {
		t.Errorf("详情未截断，实得 %d 字", len([]rune(got)))
	}
}

func TestToolEndEventBoundsError(t *testing.T) {
	r := types.StepResult{Status: types.StepFailed, Error: strings.Repeat("错", 500)}
	ev := toolEndEvent("t1", types.Step{ID: "s1", Tool: "counter"}, r)
	if len([]rune(ev.Error)) > 165 {
		t.Errorf("错误文本未截断，实得 %d 字", len([]rune(ev.Error)))
	}
	if ev.Phase != types.ToolEnd || ev.Status != types.ToolFailed {
		t.Errorf("终态映射不对：%+v", ev)
	}
}

func TestToolEndEventStatusMapping(t *testing.T) {
	cases := map[types.StepStatus]string{
		types.StepSucceeded: types.ToolDone,
		types.StepFailed:    types.ToolFailed,
		types.StepSkipped:   types.ToolSkipped,
	}
	for in, want := range cases {
		ev := toolEndEvent("t1", types.Step{ID: "s1"}, types.StepResult{Status: in})
		if ev.Status != want {
			t.Errorf("%s → %s，应为 %s", in, ev.Status, want)
		}
	}
}

func TestFanoutForwardsToolEvents(t *testing.T) {
	n := &toolRecordingNotifier{}
	f := fanout{NopNotifier{}, n}
	tn, ok := toolSink(f)
	if !ok {
		t.Fatal("fanout 应实现 ToolNotifier")
	}
	tn.OnTool(types.ToolEvent{TaskID: "t1", StepID: "s1", Phase: types.ToolEnd})
	if n.count() != 1 {
		t.Errorf("实现了 OnTool 的子实现该收到事件，实得 %d 条", n.count())
	}
	lone := fanout{NopNotifier{}}
	if tn, ok := toolSink(lone); ok {
		tn.OnTool(types.ToolEvent{TaskID: "t1"})
	}
}
