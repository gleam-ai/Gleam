package agent

import (
	"context"
	"testing"

	"gleam/internal/harness/growth"
	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestRunGoal_FirstPassOnFirstAttempt(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "打个招呼"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("任务未成功: %+v", res)
	}
	if !res.FirstPass {
		t.Error("第一轮就通过，FirstPass 应为 true")
	}
	if res.Reworks != 0 {
		t.Errorf("Reworks = %d，一次做对应为 0", res.Reworks)
	}
}

func TestRunGoal_ReworkCountedWhenReplanNeeded(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"调用会失败的工具","tool":"fail","args":{}}]}`),
		reflectScript(40, "replan", "步骤失败"),
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "跑一次"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("任务未成功: %+v", res)
	}
	if res.FirstPass {
		t.Error("返工一次才成，FirstPass 应为 false（成功 ≠ 一次就合格）")
	}
	if res.Reworks != 1 {
		t.Errorf("Reworks = %d，应为 1", res.Reworks)
	}
}

func TestRunGoal_PlanValidationRetryIsNotRework(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"用不存在的工具","tool":"no_such_tool","args":{}}]}`),
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "跑一次"})
	if res.Status != types.GoalSuccess {
		t.Fatalf("任务未成功: %+v", res)
	}
	if res.Reworks != 0 {
		t.Errorf("Reworks = %d，应为 0：计划校验失败时没有任何执行，谈不上返工", res.Reworks)
	}
	if res.FirstPass {
		t.Error("FirstPass 应为 false：第一次尝试连计划都没通过，不是一次就合格")
	}
}

func TestRunGoal_PartialIsNotFirstPass(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[
			{"id":"s1","description":"会失败的一步","tool":"fail","args":{}},
			{"id":"s2","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "自认完成"),
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "跑两步"})
	if res.Status != types.GoalPartial {
		t.Fatalf("有步骤失败时应落成部分完成: %+v", res)
	}
	if res.FirstPass {
		t.Error("部分完成不是一次就合格，FirstPass 应为 false")
	}
}

func TestGoalProgress_StampSkipsChatPath(t *testing.T) {
	res := &types.GoalResult{Status: types.GoalSuccess, FirstPass: true, Reworks: 3}
	(&goalProgress{}).stamp(res)
	if !res.FirstPass || res.Reworks != 3 {
		t.Errorf("没进过循环时不应改写交付口径: FirstPass=%v Reworks=%d", res.FirstPass, res.Reworks)
	}
}

func TestGoalProgress_StampRequiresSuccess(t *testing.T) {
	res := &types.GoalResult{Status: types.GoalPartial}
	(&goalProgress{attempts: 1, firstRound: true}).stamp(res)
	if res.FirstPass {
		t.Error("最终不是成功，FirstPass 应为 false")
	}
	if res.Reworks != 0 {
		t.Errorf("Reworks = %d，应为 0", res.Reworks)
	}
}

func TestRunGoal_ChatModeLeavesDeliveryUnset(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "chat", Texts: []string{"你好呀，有什么可以帮你的？"}},
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "你好", TaskMode: types.TaskChat})
	if res.FirstPass || res.Reworks != 0 {
		t.Errorf("对话模式不该带交付侧口径: FirstPass=%v Reworks=%d", res.FirstPass, res.Reworks)
	}
}

func TestRunGoal_GrowthEntryCarriesDelivery(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"会失败的一步","tool":"fail","args":{}}]}`),
		reflectScript(40, "replan", "步骤失败"),
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	g, err := growth.Open(f.a.Cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.a.Growth = g

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "跑一次"})
	if res.Reworks != 1 {
		t.Fatalf("前置条件不成立：结果里 Reworks = %d，应为 1", res.Reworks)
	}

	var got *growth.Entry
	for _, e := range g.All() {
		if e.Type == "task_completed" {
			c := e
			got = &c
		}
	}
	if got == nil {
		t.Fatal("没有落下 task_completed 记录")
	}
	if !got.Delivery {
		t.Error("工作模式的任务必须标 Delivery，否则交付口径的分母永远是空的")
	}
	if got.Reworks != res.Reworks {
		t.Errorf("记录里的 Reworks = %d，结果里是 %d——两者必须一致", got.Reworks, res.Reworks)
	}
	if got.FirstPass != res.FirstPass {
		t.Errorf("记录里的 FirstPass = %v，结果里是 %v", got.FirstPass, res.FirstPass)
	}
	if got.FirstPass {
		t.Error("返工一次才成的任务不该记成一次通过")
	}
}

func TestRunGoal_GrowthEntryFirstPass(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	g, err := growth.Open(f.a.Cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.a.Growth = g

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "打个招呼"})
	if !res.FirstPass {
		t.Fatalf("前置条件不成立：结果里 FirstPass 应为 true，实际 %+v", res)
	}

	entries := g.All()
	if len(entries) == 0 {
		t.Fatal("没有落下记录")
	}
	last := entries[len(entries)-1]
	if !last.Delivery {
		t.Error("工作模式的任务应标 Delivery")
	}
	if !last.FirstPass {
		t.Error("一次通过的任务，成长记录里也该是 FirstPass=true——否则通过率永远是 0")
	}
	if last.Reworks != 0 {
		t.Errorf("记录里 Reworks = %d，应为 0", last.Reworks)
	}
}

func TestRunGoal_ChatEntryHasNoDeliveryScope(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		{Kind: "chat", Texts: []string{"你好呀"}},
	})
	g, err := growth.Open(f.a.Cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	f.a.Growth = g

	f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "你好", TaskMode: types.TaskChat})

	entries := g.All()
	if len(entries) == 0 {
		t.Fatal("没有落下记录")
	}
	if last := entries[len(entries)-1]; last.Delivery {
		t.Error("对话模式不该标 Delivery——它会把对话任务混进交付口径的分母")
	}
}
