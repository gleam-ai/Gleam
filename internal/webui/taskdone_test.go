package webui

import (
	"testing"
	"time"

	"gleam/pkg/types"
)

func doneEvent(taskID string, status types.GoalStatus) types.TaskDoneEvent {
	return types.TaskDoneEvent{
		TaskID: taskID,
		Origin: "定时任务「每日巡检」",
		Result: types.GoalResult{
			TaskID:     taskID,
			Goal:       "检查磁盘占用",
			Status:     status,
			Score:      0,
			Error:      "磁盘 /data 已用 96%",
			StartedAt:  time.Now().Add(-2 * time.Minute),
			FinishedAt: time.Now(),
		},
	}
}

func TestWebUI_OnTaskDone_RegistersUnknownTask(t *testing.T) {
	f := newFixture(t, nil)

	f.srv.OnTaskDone(doneEvent("sched-t1", types.GoalFailed))

	detail := f.call("GET", "/api/goals/sched-t1", nil)
	if detail["task_id"] != "sched-t1" {
		t.Fatalf("任务详情里 task_id = %v", detail["task_id"])
	}
	if detail["goal"] != "检查磁盘占用" {
		t.Errorf("详情应带上目标，实际 %v", detail["goal"])
	}
	if detail["status"] != string(types.GoalFailed) {
		t.Errorf("详情状态应为 failed，实际 %v", detail["status"])
	}
	events, _ := detail["events"].([]any)
	found := false
	for _, e := range events {
		ev := e.(map[string]any)
		if ev["type"] != "task_done" {
			continue
		}
		found = true
		body, _ := ev["data"].(map[string]any)
		if body == nil {
			t.Fatalf("task_done 事件缺 data 体：%v", ev)
		}
		if body["origin"] != "定时任务「每日巡检」" {
			t.Errorf("事件应带来源，实际 %v", body["origin"])
		}
		if s, _ := body["line"].(string); s == "" {
			t.Error("事件应带一行摘要")
		}
		if s, _ := body["title"].(string); s == "" {
			t.Error("事件应带标题")
		}
	}
	if !found {
		t.Errorf("详情里应能看到 task_done 事件，实际 %v", events)
	}

	list := f.call("GET", "/api/goals", nil)
	inList := false
	for _, it := range list["goals"].([]any) {
		if it.(map[string]any)["task_id"] == "sched-t1" {
			inList = true
		}
	}
	if !inList {
		t.Error("任务列表里应能看到定时任务的结果")
	}
}

func TestWebUI_OnTaskDone_KeepsExistingTaskState(t *testing.T) {
	f := newFixture(t, nil)

	f.srv.mu.Lock()
	f.srv.tasks["live-1"] = &taskInfo{
		ID: "live-1", Goal: "用户提交的目标", Status: types.GoalRunning, Started: time.Now(),
	}
	f.srv.mu.Unlock()

	f.srv.OnTaskDone(doneEvent("live-1", types.GoalFailed))

	detail := f.call("GET", "/api/goals/live-1", nil)
	if detail["goal"] != "用户提交的目标" {
		t.Errorf("不该被后台事件改写目标，实际 %v", detail["goal"])
	}
	if detail["status"] != string(types.GoalRunning) {
		t.Errorf("不该被后台事件改写状态，实际 %v", detail["status"])
	}
	found := false
	for _, e := range detail["events"].([]any) {
		if e.(map[string]any)["type"] == "task_done" {
			found = true
		}
	}
	if !found {
		t.Error("已有任务也应收到 task_done 事件")
	}
}

func TestWebUI_OnTaskDone_MissingStartTimeStillListed(t *testing.T) {
	f := newFixture(t, nil)

	ev := doneEvent("sched-zero", types.GoalFailed)
	ev.Result.StartedAt = time.Time{}
	f.srv.OnTaskDone(ev)

	detail := f.call("GET", "/api/goals/sched-zero", nil)
	if s, _ := detail["started_at"].(string); s == "" {
		t.Error("开始时间为零时应补一个可读时间，而不是留空")
	}
}
