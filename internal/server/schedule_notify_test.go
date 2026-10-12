package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/scheduler"
	"gleam/pkg/types"
)

func TestRPC_ScheduleNotify_CreateAndSet(t *testing.T) {
	c, _, _ := startServer(t, nil, true)

	res, rpcErr, err := c.call("schedule/create", map[string]any{
		"name": "rpc-job", "goal": "检查磁盘占用", "cron": "0 9 * * *", "notify": "always",
	})
	if err != nil || rpcErr != nil {
		t.Fatalf("创建失败：err=%v rpcErr=%v", err, rpcErr)
	}
	if got := fieldOf(t, res, "notify"); got != "always" {
		t.Errorf("创建响应里策略应为 always，实际 %v", got)
	}

	res, rpcErr, err = c.call("schedule/notify", map[string]any{"name": "rpc-job", "notify": "never"})
	if err != nil || rpcErr != nil {
		t.Fatalf("改策略失败：err=%v rpcErr=%v", err, rpcErr)
	}
	if got := fieldOf(t, res, "notify"); got != "never" {
		t.Errorf("改后应为 never，实际 %v", got)
	}

	res, rpcErr, err = c.call("schedule/notify", map[string]any{"name": "rpc-job", "notify": ""})
	if err != nil || rpcErr != nil {
		t.Fatalf("恢复默认失败：err=%v rpcErr=%v", err, rpcErr)
	}
	if got := fieldOf(t, res, "notify"); got != scheduler.NotifyOnFailure {
		t.Errorf("空串应恢复默认 on_failure，实际 %v", got)
	}
}

func TestRPC_ScheduleNotify_InvalidRejected(t *testing.T) {
	c, a, _ := startServer(t, nil, true)

	_, rpcErr, err := c.call("schedule/create", map[string]any{
		"name": "rpc-bad", "goal": "检查磁盘占用", "cron": "0 9 * * *", "notify": "yelling",
	})
	if err != nil {
		t.Fatalf("调用出错：%v", err)
	}
	if rpcErr == nil {
		t.Fatal("非法策略应返回错误")
	}
	for _, want := range []string{"always", "on_failure", "never"} {
		if !strings.Contains(rpcErr.Message, want) {
			t.Errorf("报错应列出可用值（缺 %q）：%s", want, rpcErr.Message)
		}
	}

	for _, j := range a.Sched.ListJobs() {
		if j.Name == "rpc-bad" {
			t.Error("非法策略被拒后，不该留下同名任务")
		}
	}
}

func TestRPC_ScheduleNotify_MissingJobRejected(t *testing.T) {
	c, _, _ := startServer(t, nil, true)
	_, rpcErr, err := c.call("schedule/notify", map[string]any{"name": "ghost", "notify": "never"})
	if err != nil {
		t.Fatalf("调用出错：%v", err)
	}
	if rpcErr == nil {
		t.Fatal("不存在的任务应返回错误，不能静默成功")
	}
	if !strings.Contains(rpcErr.Message, "ghost") {
		t.Errorf("报错应点出是哪个任务：%s", rpcErr.Message)
	}
}

func TestRPC_OnTaskDonePushesNotification(t *testing.T) {
	c, a, _ := startServer(t, nil, true)

	ev := types.TaskDoneEvent{
		TaskID: "t-done",
		Origin: "定时任务「每日巡检」",
		Result: types.GoalResult{
			TaskID: "t-done", Goal: "检查磁盘占用",
			Status: types.GoalFailed, Error: "磁盘 /data 已用 96%",
		},
	}
	svc, ok := a.Notifier.(*Service)
	if !ok {
		t.Fatalf("Bind 之后 a.Notifier 应是 *Service，实际 %T——说明宿主没接管事件出口", a.Notifier)
	}
	svc.OnTaskDone(ev)

	n, err := c.waitNotification("goal/done", 5*time.Second, nil)
	if err != nil {
		t.Fatalf("没收到 goal/done 通知：%v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(n.Params, &got); err != nil {
		t.Fatalf("通知参数解析失败：%v", err)
	}
	if got["task_id"] != "t-done" {
		t.Errorf("通知应带 task_id，实际 %v", got["task_id"])
	}
	if got["origin"] != "定时任务「每日巡检」" {
		t.Errorf("通知应带来源，实际 %v", got["origin"])
	}
	res, _ := got["result"].(map[string]any)
	if res == nil || res["status"] != "failed" {
		t.Errorf("通知应带结果状态，实际 %v", got["result"])
	}
}

func fieldOf(t *testing.T, raw json.RawMessage, key string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("解析响应失败：%v（原文 %s）", err, raw)
	}
	return m[key]
}
