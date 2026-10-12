package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshot_CoversBehaviorFields(t *testing.T) {
	cfg := Default()
	cfg.LLM.Provider = "openai"
	cfg.LLM.Model = "gpt-x"
	cfg.LLM.FastModel = "gpt-x-mini"
	cfg.LLM.Tiers = map[string]string{"fast": "m-fast", "deep": "m-deep"}
	cfg.Safety.Mode = "strict"
	cfg.Agent.DoneThreshold = 77
	cfg.Agent.MaxReplans = 3
	cfg.Agent.MaxSteps = 11
	cfg.Agent.StepTimeoutSecs = 45
	cfg.Agent.StepRetries = 2
	cfg.Agent.MaxConcurrency = 4
	cfg.Agent.DedupeCalls = false
	cfg.Agent.MaxOutputRunes = 1234
	cfg.Agent.MaxToolSchemas = 5
	cfg.Agent.ReflectEachStep = true

	s := cfg.Snapshot()
	if s.Provider != "openai" || s.Model != "gpt-x" || s.FastModel != "gpt-x-mini" {
		t.Errorf("模型三项没跟上: %+v", s)
	}
	if s.SafetyMode != "strict" {
		t.Errorf("SafetyMode = %q", s.SafetyMode)
	}
	if s.DoneThreshold != 77 || s.MaxReplans != 3 || s.MaxSteps != 11 {
		t.Errorf("停止条件没跟上: %+v", s)
	}
	if s.StepTimeoutSecs != 45 || s.StepRetries != 2 || s.MaxConcurrency != 4 || s.DedupeCalls || s.MaxOutputRunes != 1234 {
		t.Errorf("单步执行参数没跟上: %+v", s)
	}
	if s.MaxToolSchemas != 5 || !s.ReflectEachStep {
		t.Errorf("上下文参数没跟上: %+v", s)
	}
	if s.Tiers["fast"] != "m-fast" || s.Tiers["deep"] != "m-deep" {
		t.Errorf("档位没跟上: %+v", s.Tiers)
	}
}

func TestSnapshot_BoolFieldsAreCopied(t *testing.T) {
	cfg := Default()
	cfg.Agent.DedupeCalls = true
	cfg.Agent.ReflectEachStep = true

	s := cfg.Snapshot()
	if !s.DedupeCalls {
		t.Error("DedupeCalls 没被拷进快照——而它的默认值就是 true，只能靠显式设 true 验出来")
	}
	if !s.ReflectEachStep {
		t.Error("ReflectEachStep 没被拷进快照")
	}
}

func TestSnapshot_TiersIsDeepCopy(t *testing.T) {
	cfg := Default()
	cfg.LLM.Tiers = map[string]string{"fast": "m-fast"}

	s := cfg.Snapshot()
	cfg.LLM.Tiers["fast"] = "改过的档位"
	cfg.LLM.Tiers["new"] = "新增档位"

	if s.Tiers["fast"] != "m-fast" {
		t.Errorf("改活配置改动了快照：Tiers[fast] = %q，应为 m-fast", s.Tiers["fast"])
	}
	if _, ok := s.Tiers["new"]; ok {
		t.Error("活配置新增档位后快照里也出现了——两者共享了同一个 map")
	}
}

func TestSnapshot_NilConfig(t *testing.T) {
	var cfg *Config
	if s := cfg.Snapshot(); s.Model != "" || s.MaxSteps != 0 {
		t.Errorf("nil 配置应得到零值快照，实际 %+v", s)
	}
}

func TestSnapshot_LeavesOutSecretsAndPaths(t *testing.T) {
	cfg := Default()
	cfg.LLM.APIKey = "sk-绝密-不该落盘"
	cfg.LLM.BaseURL = "https://internal.example/v1"
	cfg.Workspace = "D:/某个工作区"
	cfg.DataDir = "C:/Users/x/.gleam"

	b, err := json.Marshal(cfg.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	blob := string(b)
	for _, leak := range []string{"sk-绝密-不该落盘", "internal.example", "D:/某个工作区", "C:/Users/x/.gleam"} {
		if strings.Contains(blob, leak) {
			t.Errorf("快照里出现了 %q，它既不该进快照也不该明文落盘:\n%s", leak, blob)
		}
	}
}
