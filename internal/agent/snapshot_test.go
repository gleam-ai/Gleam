package agent

import (
	"context"
	"encoding/json"
	"testing"

	"gleam/internal/llm"
	"gleam/pkg/types"
)

func TestRunGoal_ConfigSnapshotFrozenAtStart(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
	})
	f.a.Cfg.LLM.Tiers = map[string]string{"fast": "启动时的档位"}

	mutated := false
	f.llm.SetHandler(func(req llm.ChatRequest) string {
		if !mutated {
			mutated = true
			f.a.Cfg.LLM.Model = "改过的模型"
			f.a.Cfg.Agent.MaxConcurrency = 99
			f.a.Cfg.LLM.Tiers["fast"] = "改过的档位"
		}
		return `{"score":95,"verdict":"done","reason":"已回复"}`
	})

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "打个招呼"})
	if !mutated {
		t.Fatal("前置条件不成立：中途改配置的钩子没被触发")
	}
	if f.a.Cfg.LLM.Model != "改过的模型" {
		t.Fatal("活配置没被改到，本测试失去意义")
	}

	if res.ConfigSnapshot == nil {
		t.Fatal("结果里必须带配置快照，否则两条历史结果之间不可比")
	}
	if res.ConfigSnapshot.Model == "改过的模型" {
		t.Error("快照取到了收尾时的模型——必须以任务启动时为准")
	}
	if res.ConfigSnapshot.MaxConcurrency == 99 {
		t.Error("中途改的并发改写了这次任务的记录")
	}
	if got := res.ConfigSnapshot.Tiers["fast"]; got != "启动时的档位" {
		t.Errorf("快照的 Tiers 跟着活配置变了（= %q）——必须是深拷贝", got)
	}
}

func TestRunGoal_ConfigSnapshotSurvivesRoundTrip(t *testing.T) {
	f := newFixture(t, []llm.Scripted{
		planScript(`{"steps":[{"id":"s1","description":"回复","tool":"reply","args":{"text":"好"}}]}`),
		reflectScript(95, "done", "已回复"),
	})
	f.a.Cfg.LLM.Model = "落盘模型"
	f.a.Cfg.Agent.MaxSteps = 21

	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "打个招呼"})

	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var back types.GoalResult
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ConfigSnapshot == nil {
		t.Fatalf("落盘再读回后快照丢了（任务记录里不会有这一段）:\n%s", b)
	}
	if back.ConfigSnapshot.Model != "落盘模型" {
		t.Errorf("读回后的模型 = %q，应为 落盘模型", back.ConfigSnapshot.Model)
	}
	if back.ConfigSnapshot.MaxSteps != 21 {
		t.Errorf("读回后的最大步数 = %d，应为 21", back.ConfigSnapshot.MaxSteps)
	}
}
