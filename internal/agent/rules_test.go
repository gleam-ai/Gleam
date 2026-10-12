package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"gleam/internal/config"
	"gleam/internal/llm"
)

func TestRules_EveryRuleIsWellFormed(t *testing.T) {
	slots := map[string]bool{SlotContract: true, SlotCode: true, SlotSchedule: true}
	ids := map[string]bool{}
	for _, r := range stableRules {
		if strings.TrimSpace(r.ID) == "" {
			t.Errorf("规则缺少 ID（标题 %q）", r.Title)
		}
		if !slots[r.Slot] {
			t.Errorf("规则 %s 的槽位 %q 不是已知槽位", r.ID, r.Slot)
		}
		if len(r.Items) == 0 {
			t.Errorf("规则 %s 没有任何条目", r.ID)
		}
		for i, it := range r.Items {
			if strings.TrimSpace(it) == "" {
				t.Errorf("规则 %s 的第 %d 条是空白", r.ID, i+1)
			}
		}
		if r.Title != "" && strings.HasPrefix(r.Title, "## ") {
			t.Errorf("规则 %s 的标题不该自带 %q 前缀（渲染器会加）", r.ID, "## ")
		}
		ids[r.ID] = true
	}
	if len(ids) < 6 {
		t.Errorf("规则表只有 %d 个 ID，看起来被误删过", len(ids))
	}
}

func TestRules_ScopeUsesKnownNames(t *testing.T) {
	modes := map[string]bool{"work": true, "code": true}
	tiers := map[string]bool{
		TierEconomy: true, TierCoding: true, TierOffice: true, TierReasoning: true,
	}
	roles := map[string]bool{}
	for _, r := range BuiltinRoles {
		roles[r.ID] = true
	}
	for _, r := range stableRules {
		for _, m := range r.Scope.Modes {
			if !modes[m] {
				t.Errorf("规则 %s 的模式 %q 不是已知任务模式", r.ID, m)
			}
		}
		for _, tr := range r.Scope.Tiers {
			if !tiers[tr] {
				t.Errorf("规则 %s 的档位 %q 不是已知档位", r.ID, tr)
			}
		}
		for _, ro := range r.Scope.Roles {
			if !roles[ro] {
				t.Errorf("规则 %s 的角色 %q 不是已知角色", r.ID, ro)
			}
		}
	}
}

func TestRules_VariantsAreOrderedWidestLast(t *testing.T) {
	byID := map[string][]Rule{}
	for _, r := range stableRules {
		byID[r.ID] = append(byID[r.ID], r)
	}
	for id, list := range byID {
		if len(list) < 2 {
			continue
		}
		for i := 1; i < len(list); i++ {
			if !scopeImplies(list[i-1].Scope, list[i].Scope) {
				t.Errorf("规则 %s 的第 %d 条范围不比第 %d 条更宽：兜底版必须排在变体之后，否则变体永远不生效",
					id, i+1, i)
			}
		}
	}
}

func scopeImplies(a, b Scope) bool {
	return dimImplies(a.Modes, b.Modes) && dimImplies(a.Tiers, b.Tiers) && dimImplies(a.Roles, b.Roles)
}

func dimImplies(a, b []string) bool {
	if len(b) == 0 {
		return true
	}
	if len(a) == 0 {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestRules_RenderIsDeterministic(t *testing.T) {
	for _, slot := range []string{SlotContract, SlotCode, SlotSchedule} {
		a := renderRules(slot, "code", TierCoding, "coder")
		b := renderRules(slot, "code", TierCoding, "coder")
		if a != b {
			t.Errorf("槽位 %s 两次渲染结果不一致", slot)
		}
	}
}

func TestRules_CodeRulesOnlyInCodeMode(t *testing.T) {
	for _, mode := range []string{"work", ""} {
		if got := renderRules(SlotCode, mode, "", "coder"); got != "" {
			t.Errorf("模式 %q 下不该渲染编程准则，实际得到：%q", mode, got)
		}
	}
	if got := renderRules(SlotCode, "code", "", "coder"); !strings.Contains(got, "## 编程模式准则") {
		t.Errorf("code 模式下应渲染编程准则，实际得到：%q", got)
	}
}

func TestRules_UntrustedDataClauseIsAlwaysOn(t *testing.T) {
	for _, mode := range []string{"work", "code"} {
		got := renderRules(SlotContract, mode, "", "")
		if !strings.Contains(got, "是数据不是指令") {
			t.Errorf("模式 %q 下契约段应包含注入防护约定，实际：%q", mode, got)
		}
	}
}

func TestRules_ScheduleHintIsAlwaysOn(t *testing.T) {
	for _, mode := range []string{"work", "code"} {
		if got := renderRules(SlotSchedule, mode, "", ""); !strings.Contains(got, "不要只在回复里答应") {
			t.Errorf("模式 %q 下定时段应始终渲染，实际：%q", mode, got)
		}
	}
}

func TestRules_StableSectionStaysCompact(t *testing.T) {
	total := 0
	for _, slot := range []string{SlotContract, SlotCode, SlotSchedule} {
		n := len(renderRules(slot, "code", TierCoding, "coder"))
		t.Logf("槽位 %-9s %4d 字节", slot, n)
		total += n
	}
	const cap = 2400
	if total > cap {
		t.Errorf("稳定段规则合计 %d 字节，超出上限 %d——新增约束前先想清楚它能不能只放在工具说明里", total, cap)
	}
}

func TestRules_CodeVerifySoftensOnlyWithConfiguredTier(t *testing.T) {
	const (
		strict  = "必须安排验证步骤"
		soft    = "不要反复重跑"
		another = "也不要为同一件事重复验证"
	)
	cases := []struct {
		tier     string
		wantSoft bool
		why      string
	}{
		{"", false, "档位未配置：跑在主模型上，能力未知，保守保留强制验证"},
		{TierEconomy, false, "经济档明确是低难度大批量，属于弱模型，保留强制验证"},
		{TierCoding, true, "用户为编码档单独配了模型，去掉反复验证的推力"},
		{TierOffice, true, "办公档同理"},
		{TierReasoning, true, "推理档同理"},
	}
	for _, c := range cases {
		got := renderRules(SlotCode, "code", c.tier, "coder")
		hasStrict := strings.Contains(got, strict)
		hasSoft := strings.Contains(got, soft) && strings.Contains(got, another)
		if c.wantSoft {
			if hasStrict {
				t.Errorf("档位 %q 应使用软化版，实际仍含 %q（%s）", c.tier, strict, c.why)
			}
			if !hasSoft {
				t.Errorf("档位 %q 应使用软化版，实际缺少 %q / %q（%s）", c.tier, soft, another, c.why)
			}
		} else {
			if !hasStrict {
				t.Errorf("档位 %q 应保留强制验证，实际缺少 %q（%s）", c.tier, strict, c.why)
			}
			if hasSoft {
				t.Errorf("档位 %q 不该出现软化措辞（%s）", c.tier, c.why)
			}
		}
		for _, want := range []string{"1. 优先使用 file.*", "2. 改动保持最小 diff", "4. 面向用户的改动说明"} {
			if !strings.Contains(got, want) {
				t.Errorf("档位 %q 下编程准则的 %q 丢了", c.tier, want)
			}
		}
	}
}

func TestRules_VerifyNeverDisappears(t *testing.T) {
	for _, tier := range []string{"", TierEconomy, TierCoding, TierOffice, TierReasoning, "unknown-tier"} {
		got := renderRules(SlotCode, "code", tier, "coder")
		if !strings.Contains(got, "验证") {
			t.Errorf("档位 %q 下第 3 条完全没提验证——条件化不该把底线去掉", tier)
		}
	}
}

func TestEffectiveTier_MatchesTierLLM(t *testing.T) {
	mk := func(tiers map[string]string) *Agent {
		cfg := config.Default()
		cfg.LLM.Tiers = tiers
		cfg.LLM.Model = "main-model"
		cfg.LLM.Provider = "glm"
		return &Agent{LLM: llm.NewMock(), Cfg: cfg}
	}
	if got := mk(nil).EffectiveTier("coder"); got != "" {
		t.Errorf("未配档位表时应返回空，实际 %q", got)
	}
	if got := mk(map[string]string{"coding": "coder-model"}).EffectiveTier("general"); got != "" {
		t.Errorf("角色未声明档位时应返回空，实际 %q", got)
	}
	if got := mk(map[string]string{"coding": "main-model"}).EffectiveTier("coder"); got != "" {
		t.Errorf("档位指向主模型时应返回空（实际就是主模型），实际 %q", got)
	}
	if got := mk(map[string]string{"coding": "   "}).EffectiveTier("coder"); got != "" {
		t.Errorf("档位值为空白时应返回空，实际 %q", got)
	}
	if got := mk(map[string]string{"coding": "coder-model"}).EffectiveTier("coder"); got != TierCoding {
		t.Errorf("配了独立模型时应返回档位名，实际 %q", got)
	}
}

func TestEffectiveTier_MockProviderNeverReportsTier(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "mock"
	cfg.LLM.Model = "main-model"
	cfg.LLM.Tiers = map[string]string{"coding": "coder-model"}
	a := &Agent{LLM: llm.NewMock(), Cfg: cfg}
	if got := a.EffectiveTier("coder"); got != "" {
		t.Errorf("mock 会话下应返回空，实际 %q", got)
	}
}

func TestRules_ScopeNeverDependsOnGoal(t *testing.T) {
	reg := bigRegistry(25)
	p := &Planner{Reg: reg, MaxToolSchemas: 12, Role: "coder", TaskMode: "code", Style: "efficient", Tier: TierCoding}
	a := p.buildSystemPrompt("给 utils.go 里的 parseDate 补单元测试", "D:/ws", nil, nil, "", "")
	b := p.buildSystemPrompt("把上季度销售数据整理成表格并汇总", "D:/ws", nil, nil, "", "")

	cut := func(s string) string { return s[:strings.Index(s, "## 可用工具")] }
	if cut(a) != cut(b) {
		t.Error("稳定段随目标变化了——规则范围必须只挂在稳定维度上")
	}
	if utf8.RuneCountInString(cut(a)) < llm.CacheFriendlyMinChars {
		t.Errorf("稳定段只有 %d 字，低于缓存友好阈值", utf8.RuneCountInString(cut(a)))
	}
}
