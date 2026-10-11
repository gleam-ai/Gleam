package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/harness/scheduler"
	"gleam/pkg/types"
)

type fakeNotifier struct {
	mu     sync.Mutex
	events []types.TaskDoneEvent
}

func (f *fakeNotifier) OnProgress(types.ProgressEvent) {}
func (f *fakeNotifier) OnApproval(types.ApprovalRequest) types.ApprovalResponse {
	return types.ApprovalResponse{}
}
func (f *fakeNotifier) OnSuggestion(string, string)             {}
func (f *fakeNotifier) OnSuggestSkill(string, types.SkillDraft) {}

func (f *fakeNotifier) OnTaskDone(ev types.TaskDoneEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("建管道失败：%v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() { os.Stderr = old }()
		fn()
	}()
	_ = w.Close()
	return <-done
}

func withSysNotify(t *testing.T) *[]string {
	t.Helper()
	var sent []string
	old := sysNotify
	sysNotify = func(title, message string) error {
		sent = append(sent, title+" — "+message)
		return nil
	}
	t.Cleanup(func() { sysNotify = old })
	return &sent
}

func job(name, policy string) scheduler.Job {
	return scheduler.Job{Name: name, Goal: "检查磁盘占用", Notify: policy, Enabled: true}
}

func stubRunGoal(fn func(*agent.Agent, context.Context, types.GoalRequest) *types.GoalResult) func() {
	old := runGoalFor
	runGoalFor = fn
	return func() { runGoalFor = old }
}

func result(status types.GoalStatus) *types.GoalResult {
	return &types.GoalResult{
		TaskID: "t-1",
		Goal:   "检查磁盘占用",
		Status: status,
		Score:  90,
		Error:  "磁盘 /data 已用 96%",
	}
}

func TestNotifyScheduledDone_Wiring(t *testing.T) {
	cases := []struct {
		name        string
		policy      string
		status      types.GoalStatus
		wantSys     bool
		wantSilence bool
	}{
		{"默认策略·失败 → 弹通知", scheduler.NotifyOnFailure, types.GoalFailed, true, false},
		{"默认策略·部分完成 → 弹通知", scheduler.NotifyOnFailure, types.GoalPartial, true, false},
		{"默认策略·已取消 → 弹通知", scheduler.NotifyOnFailure, types.GoalCancelled, true, false},
		{"always·成功 → 弹通知", scheduler.NotifyAlways, types.GoalSuccess, true, false},
		{"always·失败 → 弹通知", scheduler.NotifyAlways, types.GoalFailed, true, false},
		{"空策略等价默认·失败 → 弹通知", "", types.GoalFailed, true, false},
		{"默认策略·成功 → 不弹，但仍送达", scheduler.NotifyOnFailure, types.GoalSuccess, false, true},
		{"never·失败 → 不弹，但仍送达", scheduler.NotifyNever, types.GoalFailed, false, true},
		{"脏策略值·失败 → 按默认走，仍弹", "yelling", types.GoalFailed, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sent := withSysNotify(t)
			sink := &fakeNotifier{}
			var out string
			out = captureStderr(t, func() {
				notifyScheduledDone(job("每日巡检", c.policy), result(c.status), sink)
			})

			if got := len(*sent) > 0; got != c.wantSys {
				t.Errorf("系统通知：发了=%v，期望=%v（stderr=%q）", got, c.wantSys, out)
			}
			if sink.count() != 1 {
				t.Errorf("事件推送：期望每次都推 1 次，实际 %d 次（stderr=%q）", sink.count(), out)
			}
			if got := strings.Contains(out, "不弹系统通知"); got != c.wantSilence {
				t.Errorf("静默痕迹：出现=%v，期望=%v（stderr=%q）", got, c.wantSilence, out)
			}
			if c.wantSilence && !strings.Contains(out, "检查磁盘占用") {
				t.Errorf("静默时也该说清是哪个任务，stderr=%q", out)
			}
		})
	}
}

func TestNotifyScheduledDone_NilResultIsNoop(t *testing.T) {
	sent := withSysNotify(t)
	sink := &fakeNotifier{}
	out := captureStderr(t, func() {
		notifyScheduledDone(job("每日巡检", scheduler.NotifyAlways), nil, sink)
	})
	if len(*sent) != 0 || sink.count() != 0 {
		t.Errorf("nil 结果不该产生任何通知：系统=%d 事件=%d", len(*sent), sink.count())
	}
	if out != "" {
		t.Errorf("nil 结果不该有输出，实际 %q", out)
	}
}

func TestNotifyScheduledDone_NilSinkStillNotifies(t *testing.T) {
	sent := withSysNotify(t)
	out := captureStderr(t, func() {
		notifyScheduledDone(job("每日巡检", scheduler.NotifyOnFailure), result(types.GoalFailed), nil)
	})
	if len(*sent) != 1 {
		t.Fatalf("sink 为 nil 时系统通知仍应发一次，实际 %d 次（stderr=%q）", len(*sent), out)
	}
}

func TestNotifyScheduledDone_SysNotifyFailureKeepsEvent(t *testing.T) {
	old := sysNotify
	sysNotify = func(string, string) error { return io.ErrClosedPipe }
	t.Cleanup(func() { sysNotify = old })

	sink := &fakeNotifier{}
	out := captureStderr(t, func() {
		notifyScheduledDone(job("每日巡检", scheduler.NotifyOnFailure), result(types.GoalFailed), sink)
	})
	if sink.count() != 1 {
		t.Errorf("弹窗失败时事件仍应推一次，实际 %d 次", sink.count())
	}
	if !strings.Contains(out, "系统通知未送达") {
		t.Errorf("弹窗失败必须在 stderr 留痕，实际 %q", out)
	}
}

func TestNotifyScheduledDone_EventCarriesOriginAndState(t *testing.T) {
	withSysNotify(t)
	sink := &fakeNotifier{}
	captureStderr(t, func() {
		notifyScheduledDone(job("磁盘巡检", scheduler.NotifyAlways), result(types.GoalPartial), sink)
	})
	if sink.count() != 1 {
		t.Fatalf("应推一次事件，实际 %d", sink.count())
	}
	ev := sink.events[0]
	if !strings.Contains(ev.Origin, "磁盘巡检") {
		t.Errorf("事件应带任务名，Origin=%q", ev.Origin)
	}
	if ev.TaskID != "t-1" {
		t.Errorf("事件应带 TaskID，实际 %q", ev.TaskID)
	}
	if ev.StateText() != "部分完成" {
		t.Errorf("状态词应为「部分完成」，实际 %q", ev.StateText())
	}
	if ev.Succeeded() {
		t.Error("部分完成不算完整成功——它正是最该通知的那一类")
	}
	if !strings.Contains(ev.Title(), "磁盘巡检") || !strings.Contains(ev.Title(), "部分完成") {
		t.Errorf("标题应含任务名与状态，实际 %q", ev.Title())
	}
	if !strings.Contains(ev.Line(), "磁盘巡检") && !strings.Contains(ev.Line(), "检查磁盘占用") {
		t.Errorf("正文应点出是哪个目标，实际 %q", ev.Line())
	}
}

func TestSysNotify_DisabledStillPrints(t *testing.T) {
	t.Setenv("GLEAM_NO_SYS_NOTIFY", "1")
	out := captureStderr(t, func() {
		if err := sysNotify("定时任务「每日巡检」失败", "失败：检查磁盘占用"); err != nil {
			t.Errorf("禁用状态下不该报错，实际 %v", err)
		}
	})
	for _, want := range []string{"每日巡检", "检查磁盘占用"} {
		if !strings.Contains(out, want) {
			t.Errorf("禁用时也应打印待发内容（含 %q），实际 %q", want, out)
		}
	}
}

func TestFireScheduledJob_Notifies(t *testing.T) {
	withSysNotify(t)
	restore := stubRunGoal(func(*agent.Agent, context.Context, types.GoalRequest) *types.GoalResult {
		return result(types.GoalFailed)
	})
	defer restore()

	sink := &fakeNotifier{}
	dataDir := t.TempDir()
	rt := &runtime{agent: &agent.Agent{Notifier: sink}, cfg: &config.Config{DataDir: dataDir}}
	captureStderr(t, func() { fireScheduledJob(rt, job("每日巡检", scheduler.NotifyOnFailure)) })

	if sink.count() != 1 {
		t.Fatalf("fire 路径应送达一次事件，实际 %d", sink.count())
	}
	if !strings.Contains(sink.events[0].Origin, "每日巡检") {
		t.Errorf("事件应带任务名，实际 %q", sink.events[0].Origin)
	}
	archived, err := agent.ReadTaskResult(dataDir, "t-1")
	if err != nil || archived == nil {
		t.Fatalf("fire 路径应把终态归档到 tasks/：读到=%v err=%v", archived, err)
	}
	if archived.Status != types.GoalFailed {
		t.Errorf("归档应保留失败状态（静默的不该被写成成功），实际 %q", archived.Status)
	}
}

func TestFireScheduledJob_ArchiveIgnoresNotifyPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy string
		status types.GoalStatus
	}{
		{"never·成功", scheduler.NotifyNever, types.GoalSuccess},
		{"never·失败", scheduler.NotifyNever, types.GoalFailed},
		{"on_failure·成功（按策略不弹）", scheduler.NotifyOnFailure, types.GoalSuccess},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sent := withSysNotify(t)
			restore := stubRunGoal(func(*agent.Agent, context.Context, types.GoalRequest) *types.GoalResult {
				return result(c.status)
			})
			defer restore()

			sink := &fakeNotifier{}
			dataDir := t.TempDir()
			rt := &runtime{agent: &agent.Agent{Notifier: sink}, cfg: &config.Config{DataDir: dataDir}}
			out := captureStderr(t, func() { fireScheduledJob(rt, job("每日巡检", c.policy)) })

			if len(*sent) != 0 {
				t.Errorf("按策略不该弹窗，实际弹了 %d 次（stderr=%q）", len(*sent), out)
			}
			archived, err := agent.ReadTaskResult(dataDir, "t-1")
			if err != nil || archived == nil {
				t.Fatalf("策略 %q 配 %q 仍要归档到 tasks/：读到=%v err=%v", c.policy, c.status, archived, err)
			}
			if archived.Status != c.status {
				t.Errorf("归档要保留真实终态，期望 %q 实际 %q", c.status, archived.Status)
			}
			if sink.count() != 1 {
				t.Errorf("事件仍应推一次（送达与策略无关），实际 %d 次", sink.count())
			}
		})
	}
}

func TestBindSchedulerFire_ClosureIsConnected(t *testing.T) {
	withSysNotify(t)
	restore := stubRunGoal(func(*agent.Agent, context.Context, types.GoalRequest) *types.GoalResult {
		return result(types.GoalFailed)
	})
	defer restore()

	sched, err := scheduler.Open(filepath.Join(t.TempDir(), "sched.json"), nil)
	if err != nil {
		t.Fatalf("建调度器失败：%v", err)
	}
	if _, err := sched.AddJob("每日巡检", "", 3600, "检查磁盘占用", "auto"); err != nil {
		t.Fatalf("加任务失败：%v", err)
	}

	sink := &fakeNotifier{}
	rt := &runtime{agent: &agent.Agent{Notifier: sink}, sched: sched, cfg: &config.Config{DataDir: t.TempDir()}}
	bindSchedulerFire(rt)

	sink2 := &fakeNotifier{}
	rt.agent.Notifier = sink2

	captureStderr(t, func() {
		ok, err := sched.TriggerNow("每日巡检")
		if err != nil || !ok {
			t.Errorf("触发失败：ok=%v err=%v", ok, err)
			return
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && sink2.count() == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	})

	if sink2.count() != 1 {
		t.Fatalf("绑定时未捕获 notifier 才算对：绑定后换上的宿主应收到 1 次，实际 %d", sink2.count())
	}
	if sink.count() != 0 {
		t.Errorf("绑定期那个旧宿主不该收到事件（说明 notifier 被提前捕获了），实际 %d", sink.count())
	}
}

func TestBindSchedulerFire_NilSchedulerIsNoop(t *testing.T) {
	rt := &runtime{agent: &agent.Agent{Notifier: agent.NopNotifier{}}}
	bindSchedulerFire(rt)
}
