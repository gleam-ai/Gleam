package webui

import (
	"testing"
	"time"
)

const acceptancePlan = `{"acceptance":["回复中给出 3 条建议","每条建议都写明负责人"],"steps":[
	{"id":"s1","description":"回复用户","tool":"reply","args":{"text":"建议一：把目标拆成可独立交付的切片。建议二：把结论写成文档。建议三：用异步沟通替代同步等待。"}}
]}`

const acceptanceReflect = `{"checks":[
	{"criterion":"回复中给出 3 条建议","passed":true,"note":""},
	{"criterion":"每条建议都写明负责人","passed":true,"note":""}
],"verdict":"done","reason":"两条标准均满足","suggestion":""}`

func TestAcceptance_ReachesPayload(t *testing.T) {
	f, _ := newGEOFixtureFull(t, acceptancePlan, acceptanceReflect)
	res := f.call("POST", "/api/goals", map[string]any{
		"goal":      "给我三条关于远程协作的建议",
		"task_mode": "work",
	})
	id, _ := res["task_id"].(string)
	if id == "" {
		t.Fatalf("未返回 task_id: %v", res)
	}
	task := f.waitTask(id, 10*time.Second)
	if task["status"] != "success" {
		t.Fatalf("任务未成功: %v", task)
	}

	payload, _ := task["result"].(map[string]any)
	if payload == nil {
		t.Fatalf("结果为空: %v", task)
	}

	acc, _ := payload["acceptance"].([]any)
	if len(acc) != 2 {
		t.Fatalf("应带回 2 条验收标准，实际 %v", payload["acceptance"])
	}
	if acc[0] != "回复中给出 3 条建议" || acc[1] != "每条建议都写明负责人" {
		t.Errorf("验收标准内容不符: %v", acc)
	}

	checks, _ := payload["checks"].([]any)
	if len(checks) != 2 {
		t.Fatalf("应对齐出 2 条判定，实际 %v", payload["checks"])
	}
	first, _ := checks[0].(map[string]any)
	if first["passed"] != true {
		t.Errorf("第一条应判通过: %v", first)
	}
	if first["criterion"] != "回复中给出 3 条建议" {
		t.Errorf("criterion 应为标准原文: %v", first["criterion"])
	}

	if got := int(payload["score"].(float64)); got != 100 {
		t.Errorf("两条全过应为 100 分，实际 %d", got)
	}
}

func TestAcceptance_PartialStatusReachesPayload(t *testing.T) {
	refl := `{"checks":[
		{"criterion":"回复中给出 3 条建议","passed":true,"note":""},
		{"criterion":"每条建议都写明负责人","passed":false,"note":"回复里没有写负责人"}
	],"verdict":"done","reason":"建议已给出","suggestion":""}`
	f, _ := newGEOFixtureFull(t, acceptancePlan, refl)
	f.agent.Cfg.Agent.StuckThreshold = 0
	res := f.call("POST", "/api/goals", map[string]any{
		"goal":      "给我三条关于远程协作的建议",
		"task_mode": "work",
	})
	id, _ := res["task_id"].(string)
	task := f.waitTask(id, 15*time.Second)

	if task["status"] != "partial" {
		t.Fatalf("验收未全过应判部分完成，实际 %v（%v）", task["status"], task["result"])
	}
	payload, _ := task["result"].(map[string]any)
	if payload == nil {
		t.Fatalf("结果为空: %v", task)
	}
	if got := int(payload["score"].(float64)); got != 50 {
		t.Errorf("1/2 通过应为 50 分，实际 %d", got)
	}
	checks, _ := payload["checks"].([]any)
	second, _ := checks[1].(map[string]any)
	if second["passed"] != false {
		t.Errorf("第二条应判未通过: %v", second)
	}
	if note, _ := second["note"].(string); note == "" {
		t.Error("未通过的判定要说明差在哪，否则用户无从下手")
	}
}

func TestAcceptance_MissingCheckCountsAsFailedInPayload(t *testing.T) {
	refl := `{"checks":[
		{"criterion":"回复中给出 3 条建议","passed":true,"note":""}
	],"verdict":"done","reason":"ok","suggestion":""}`
	f, _ := newGEOFixtureFull(t, acceptancePlan, refl)
	f.agent.Cfg.Agent.StuckThreshold = 0
	res := f.call("POST", "/api/goals", map[string]any{
		"goal":      "给我三条关于远程协作的建议",
		"task_mode": "work",
	})
	id, _ := res["task_id"].(string)
	task := f.waitTask(id, 15*time.Second)
	payload, _ := task["result"].(map[string]any)
	if payload == nil {
		t.Fatalf("结果为空: %v", task)
	}
	checks, _ := payload["checks"].([]any)
	if len(checks) != 2 {
		t.Fatalf("漏判也要补齐到 2 条，实际 %v", checks)
	}
	second, _ := checks[1].(map[string]any)
	if second["passed"] != false {
		t.Errorf("漏判的一条应按未通过计: %v", second)
	}
	if task["status"] != "partial" {
		t.Errorf("1/2 通过应判部分完成，实际 %v", task["status"])
	}
}

func TestAcceptance_ScoreNotFromSelfReport(t *testing.T) {
	refl := `{"score":100,"checks":[
		{"criterion":"回复中给出 3 条建议","passed":false,"note":"只给了 1 条"},
		{"criterion":"每条建议都写明负责人","passed":false,"note":"完全没有负责人"}
	],"verdict":"replan","reason":"差距明显","suggestion":""}`
	f, _ := newGEOFixtureFull(t, acceptancePlan, refl)
	f.agent.Cfg.Agent.StuckThreshold = 0
	res := f.call("POST", "/api/goals", map[string]any{
		"goal":      "给我三条关于远程协作的建议",
		"task_mode": "work",
	})
	id, _ := res["task_id"].(string)
	task := f.waitTask(id, 15*time.Second)

	payload, _ := task["result"].(map[string]any)
	if payload == nil {
		t.Fatalf("结果为空: %v", task)
	}
	if got := int(payload["score"].(float64)); got == 100 {
		t.Fatal("模型自评 100 分被采信了，逐条判定形同虚设")
	}
	if got := int(payload["score"].(float64)); got != 0 {
		t.Errorf("两条全不过应为 0 分，实际 %d", got)
	}
	if task["status"] != "failed" {
		t.Errorf("验收一条都没过应判失败，实际 %v", task["status"])
	}
}

func TestAcceptance_AbsentKeepsOldBehavior(t *testing.T) {
	f, _ := newGEOFixtureFull(t,
		`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好的"}}]}`,
		`{"score":88,"verdict":"done","reason":"已完成","suggestion":""}`)
	res := f.call("POST", "/api/goals", map[string]any{
		"goal":      "随便回一句",
		"task_mode": "work",
	})
	id, _ := res["task_id"].(string)
	task := f.waitTask(id, 10*time.Second)
	if task["status"] != "success" {
		t.Fatalf("任务未成功: %v", task)
	}
	payload, _ := task["result"].(map[string]any)
	if _, has := payload["acceptance"]; has {
		t.Errorf("没定验收标准时不应凭空出现 acceptance: %v", payload["acceptance"])
	}
	if got := int(payload["score"].(float64)); got != 88 {
		t.Errorf("无验收标准时应沿用模型评分 88，实际 %d", got)
	}
}
