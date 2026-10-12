package main

import (
	"strings"
	"testing"

	"gleam/internal/agent"
)

func TestScopeTextMarksUnlimited(t *testing.T) {
	if got := scopeText(agent.RuleInfo{}); got != "-" {
		t.Fatalf("无条件生效应排成 %q，实际 %q", "-", got)
	}
	got := scopeText(agent.RuleInfo{Modes: []string{"code"}, Tiers: []string{"coding", "office"}})
	for _, want := range []string{"模式 code", "档位 coding/office"} {
		if !strings.Contains(got, want) {
			t.Errorf("适用范围 %q 里缺少 %q", got, want)
		}
	}
	if !strings.Contains(scopeText(agent.RuleInfo{Roles: []string{"coder"}}), "角色 coder") {
		t.Error("适用范围里缺少角色维度")
	}
}

func TestRenderRulesReport_TableViewListsEveryRule(t *testing.T) {
	o := rulesOutput{Version: "deadbeef01", Mode: "work", Rules: agent.RuleTable()}
	got := renderRulesReport(o, false)
	if !strings.Contains(got, "deadbeef01") {
		t.Error("全表视图没有版本指纹——本次回查就无从与历史对照")
	}
	for _, r := range o.Rules {
		if !strings.Contains(got, r.ID) {
			t.Errorf("全表视图漏了规则 %s", r.ID)
		}
	}
	if !strings.Contains(got, "合计") {
		t.Error("全表视图没有合计行")
	}
}

func TestRenderRulesReport_ScenarioViewSeparatesActiveFromInactive(t *testing.T) {
	table := agent.RuleTable()
	rs := agent.DescribeRuleSet("work", "", "")
	o := rulesOutput{Version: "deadbeef01", Mode: "work", Active: rs.Active, Inactive: rs.Inactive, Rules: table}
	got := renderRulesReport(o, true)

	if !strings.Contains(got, "模式=work") {
		t.Error("场景视图没写出场景——读者不知道自己看的是哪一档")
	}
	for _, id := range rs.Active {
		if !strings.Contains(got, id) {
			t.Errorf("场景视图漏了生效规则 %s", id)
		}
	}
	if !strings.Contains(got, "未生效") {
		t.Error("场景视图没有未生效区——「为什么这条没进提示词」无从回答")
	}
	for _, id := range rs.Inactive {
		if !strings.Contains(got, id) {
			t.Errorf("场景视图漏了未生效规则 %s", id)
		}
	}
	if !strings.Contains(got, "code.verify") {
		t.Error("工作模式下 code.verify 应列为未生效")
	}
}

func TestRenderRulesReport_ChatExplainsWhyEmpty(t *testing.T) {
	rs := agent.DescribeRuleSet("chat", "", "")
	o := rulesOutput{Version: "deadbeef01", Mode: "chat", Active: rs.Active, Rules: agent.RuleTable()}
	got := renderRulesReport(o, true)
	if !strings.Contains(got, "生效 0 条") {
		t.Fatalf("对话模式应显示 0 条，实际：%q", got)
	}
	if !strings.Contains(got, "不经规划") {
		t.Error("对话模式的 0 条需要一句解释，否则会被当成漏记")
	}
}

func TestRulesOutputJSONKeepsOneShape(t *testing.T) {
	o := rulesOutput{Version: "v", Mode: "work", Rules: agent.RuleTable()}
	if len(o.Rules) == 0 {
		t.Fatal("规则表为空，这个断言就没意义了")
	}
	withScenario := o
	withScenario.Active = []string{"output.format"}
	if withScenario.Rules == nil || withScenario.Active == nil {
		t.Error("加场景维度后全表字段不该消失")
	}
}
