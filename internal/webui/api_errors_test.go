package webui

import (
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/space"
)

func TestWebUI_ErrorStatusMappingAndNoPathLeak(t *testing.T) {
	f := newFixture(t, nil)
	store, err := space.Open(f.dataDir, f.ws)
	if err != nil {
		t.Fatal(err)
	}
	f.agent.Spaces = store

	checkNoLeak := func(label string, body map[string]any) {
		if s, _ := body["error"].(string); strings.Contains(s, f.dataDir) || strings.Contains(s, `\conversations\`) || strings.Contains(s, `\spaces\`) {
			t.Errorf("%s 泄漏数据目录路径: %v", label, body["error"])
		}
	}

	if code, body := f.raw("GET", "/api/conversations/not-hex-id", nil); code != 400 {
		t.Errorf("非法会话 id 应 400，实际 %d %v", code, body)
	} else {
		checkNoLeak("会话 400", body)
	}
	if code, body := f.raw("GET", "/api/conversations/deadbeefdeadbeef", nil); code != 404 {
		t.Errorf("不存在会话应 404，实际 %d %v", code, body)
	} else {
		checkNoLeak("会话 404", body)
	}

	if code, body := f.raw("DELETE", "/api/spaces/state", nil); code != 400 {
		t.Errorf("保留名 state 删除应 400，实际 %d %v", code, body)
	} else {
		checkNoLeak("空间 400", body)
	}
	if code, _ := f.raw("DELETE", "/api/spaces/sp_000000000000", nil); code != 404 {
		t.Errorf("不存在空间应 404，实际 %d", code)
	}
	if code, _ := f.raw("DELETE", "/api/spaces/default", nil); code != 400 {
		t.Errorf("默认空间删除应 400，实际 %d", code)
	}
	if code, _ := f.raw("POST", "/api/spaces/state/activate", nil); code != 400 {
		t.Errorf("激活保留名应 400，实际 %d", code)
	}
}

func TestWebUI_DuplicateScheduleRejected(t *testing.T) {
	f := newFixture(t, nil)
	if code, _ := f.raw("POST", "/api/schedules", map[string]any{"name": "daily", "goal": "整理桌面", "cron": "0 9 * * *"}); code >= 400 {
		t.Fatalf("首次创建应成功，实际 %d", code)
	}
	code, body := f.raw("POST", "/api/schedules", map[string]any{"name": "daily", "goal": "覆盖用目标", "cron": "*/5 * * * *"})
	if code != 400 {
		t.Fatalf("同名任务应 400，实际 %d %v", code, body)
	}
	if !strings.Contains(sprintErr(body), "已存在") {
		t.Errorf("错误信息应说明重名: %v", body)
	}
	jobs := f.call("GET", "/api/schedules", nil)
	list, _ := jobs["jobs"].([]any)
	if len(list) != 1 {
		t.Fatalf("任务数应为 1，实际 %d", len(list))
	}
	if j, _ := list[0].(map[string]any); j["goal"] != "整理桌面" {
		t.Errorf("原任务目标被改动: %v", j["goal"])
	}
}

func TestSubscribeReplaysQuestionsAndTodos(t *testing.T) {
	f := newFixture(t, nil)
	s := f.srv
	s.mu.Lock()
	s.questions["q-replay"] = &questionWaiter{
		ID:       "q-replay",
		TaskID:   "task-replay",
		Question: "继续吗？",
		Choices:  []string{"继续"},
		ch:       make(chan questionAnswer, 1),
	}
	s.todos["task-replay"] = []todoItemView{{Text: "整理文件", Status: "done"}}
	s.mu.Unlock()

	ch := s.subscribe()
	defer s.unsubscribe(ch)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			seen[ev.Type] = true
		case <-time.After(time.Second):
			t.Fatalf("重放事件不足：%v", seen)
		}
	}
	if !seen["question"] || !seen["todos"] {
		t.Fatalf("重连必须重放待回答问题与待办快照，实际 %v", seen)
	}
}

func sprintErr(body map[string]any) string {
	if s, ok := body["error"].(string); ok {
		return s
	}
	return ""
}
