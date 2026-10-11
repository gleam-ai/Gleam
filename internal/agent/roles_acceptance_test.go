package agent

import (
	"strings"
	"testing"
)

func TestRoleAcceptanceFocus_AllRolesCovered(t *testing.T) {
	for _, r := range BuiltinRoles {
		if strings.TrimSpace(r.AcceptanceFocus) == "" {
			t.Errorf("角色 %s（%s）缺少 AcceptanceFocus", r.ID, r.Name)
		}
	}
}

func TestRoleAcceptanceFocus_DistinctPerRole(t *testing.T) {
	seen := map[string]string{}
	for _, r := range BuiltinRoles {
		if prev, dup := seen[r.AcceptanceFocus]; dup {
			t.Errorf("角色 %s 与 %s 的验收重点完全相同：%s", r.ID, prev, r.AcceptanceFocus)
		}
		seen[r.AcceptanceFocus] = r.ID
	}
}

func TestRoleAcceptanceFocus_ReachesPlannerPrompt(t *testing.T) {
	p := &Planner{Reg: menuRegistry(2), MaxSteps: 5, Role: "coder"}
	sys := p.buildSystemPrompt("重构一个函数", "/tmp", nil, nil, "", "")

	focus := FindRole("coder").AcceptanceFocus
	if focus == "" {
		t.Fatal("coder 角色应有验收重点")
	}
	if !strings.Contains(sys, focus) {
		t.Fatalf("规划器提示词应包含 coder 的验收重点：\n%s", focus)
	}
	if !strings.Contains(sys, "本角色（开发工程师）的验收重点") {
		t.Error("应标明这是哪个角色的验收重点")
	}
	general := strings.Index(sys, "## 验收标准（acceptance）")
	roleFocus := strings.Index(sys, "本角色（开发工程师）的验收重点")
	if general < 0 || roleFocus < 0 || roleFocus < general {
		t.Errorf("领域重点应排在通用验收指南之后（general=%d, focus=%d）", general, roleFocus)
	}
	style := strings.Index(sys, "## 协作风格")
	if style >= 0 && roleFocus > style {
		t.Error("领域重点不该出现在协作风格之后，否则离验收指南太远")
	}
}

func TestRoleAcceptanceFocus_DefaultRoleStillGetsFocus(t *testing.T) {
	p := &Planner{Reg: menuRegistry(2), MaxSteps: 5}
	sys := p.buildSystemPrompt("整理一下下载目录", "/tmp", nil, nil, "", "")
	focus := FindRole("").AcceptanceFocus
	if focus == "" {
		t.Fatal("通用角色应有验收重点")
	}
	if !strings.Contains(sys, focus) {
		t.Errorf("未指定角色时也应注入通用角色的验收重点：%s", focus)
	}
}

func TestRoleAcceptanceFocus_NotConfusedWithRolePrompt(t *testing.T) {
	p := &Planner{Reg: menuRegistry(2), MaxSteps: 5, Role: "analyst"}
	sys := p.buildSystemPrompt("统计销售数据", "/tmp", nil, nil, "", "")
	role := FindRole("analyst")
	if !strings.Contains(sys, "## 专家角色："+role.Name) {
		t.Error("专家角色知识段应仍然存在")
	}
	if !strings.Contains(sys, role.SystemPrompt) {
		t.Error("角色知识段正文应仍然存在")
	}
}
