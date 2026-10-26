package webui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gleam/internal/llm"
)

func TestCancel_PlanFirstGate(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[{"id":"s1","description":"写","tool":"file.write","args":{"path":"pf.txt","content":"x"}}]}`}},
	})
	out := f.call("POST", "/api/goals", map[string]any{"goal": "写 pf.txt", "mode": "plan_first"})
	id, _ := out["task_id"].(string)
	ap := f.waitApproval(id)
	events := f.srv.subscribe()
	defer f.srv.unsubscribe(events)
	if ap["whole_plan"] != true {
		t.Fatalf("plan_first 应在整计划上停一次: %v", ap)
	}

	f.call("POST", "/api/goals/"+id+"/cancel", nil)

	done := f.waitTask(id, 2*time.Second)
	if done["status"] != "cancelled" {
		t.Errorf("取消后状态 = %v", done["status"])
	}
	res, _ := done["result"].(map[string]any)
	if got, _ := res["error"].(string); got != "任务已取消" {
		t.Errorf("取消的原因 = %q，应为「任务已取消」（写成拒绝或超时都是假原因）", got)
	}
	if n := f.pendingApprovals(); n != 0 {
		t.Errorf("取消后仍有 %d 条未决审批", n)
	}
	if _, err := os.Stat(filepath.Join(f.ws, "pf.txt")); !os.IsNotExist(err) {
		t.Error("取消后不应写出产物")
	}
	timeout := time.After(time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type != "approval_resolved" {
				continue
			}
			var resolved map[string]any
			if err := json.Unmarshal(ev.Data, &resolved); err != nil {
				t.Fatal(err)
			}
			if resolved["id"] != ap["id"] {
				t.Fatalf("wrong resolved approval: %v", resolved)
			}
			return
		case <-timeout:
			t.Fatal("cancel did not retire approval cards in other tabs")
		}
	}
}

func TestCancel_StepGate(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "plan", Texts: []string{`{"steps":[{"id":"s1","description":"删除","tool":"file.delete","args":{"path":"victim.txt","recursive":false}}]}`}},
	})
	if err := os.WriteFile(filepath.Join(f.ws, "victim.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := f.call("POST", "/api/goals", map[string]any{"goal": "删除 victim.txt", "mode": "auto"})
	id, _ := out["task_id"].(string)
	ap := f.waitApproval(id)
	if ap["whole_plan"] == true {
		t.Fatalf("auto 模式应停在单步上: %v", ap)
	}

	f.call("POST", "/api/goals/"+id+"/cancel", nil)

	done := f.waitTask(id, 2*time.Second)
	if done["status"] != "cancelled" {
		t.Errorf("取消后状态 = %v", done["status"])
	}
	if n := f.pendingApprovals(); n != 0 {
		t.Errorf("取消后仍有 %d 条未决审批", n)
	}
	if _, err := os.Stat(filepath.Join(f.ws, "victim.txt")); err != nil {
		t.Errorf("未批准就取消，产物不该被动过: %v", err)
	}
}

func TestWebUI_ContinuousReadOnlyConversationNoApproval(t *testing.T) {
	for _, mode := range []string{"chat", "work"} {
		t.Run(mode, func(t *testing.T) {
			var scripts []llm.Scripted
			for i := 0; i < 6; i++ {
				if mode == "chat" {
					scripts = append(scripts, llm.Scripted{Kind: "chat_mode", Texts: []string{"连续回答"}})
				} else {
					scripts = append(scripts,
						llm.Scripted{Kind: "plan", Texts: []string{`{"steps":[{"id":"r","tool":"reply","args":{"text":"连续回答"}}]}`}},
						llm.Scripted{Kind: "reflect", Texts: []string{`{"score":95,"verdict":"done","reason":"已回答"}`}})
				}
			}
			f := newFixture(t, scripts)
			f.agent.Cfg.Agent.ContextCompress = false
			id := f.call("POST", "/api/conversations", nil)["id"].(string)
			events := f.srv.subscribe()
			defer f.srv.unsubscribe(events)
			for i := 0; i < 6; i++ {
				out := f.call("POST", "/api/goals", map[string]any{
					"goal": fmt.Sprintf("请回答第 %d 个问题", i), "task_mode": mode,
					"mode": "plan_first", "conversation_id": id,
				})
				done := f.waitTask(out["task_id"].(string), 2*time.Second)
				if done["status"] != "success" {
					t.Fatalf("turn %d failed: %v", i, done)
				}
			}
			msgs := f.call("GET", "/api/conversations/"+id, nil)["messages"].([]any)
			if len(msgs) != 12 {
				t.Fatalf("lost or duplicated turns: %d", len(msgs))
			}
			for len(events) > 0 {
				if ev := <-events; ev.Type == "approval" {
					t.Fatalf("read-only conversation requested approval: %s", ev.Data)
				}
			}
		})
	}
}
