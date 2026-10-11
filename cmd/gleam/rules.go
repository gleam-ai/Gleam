package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"gleam/internal/agent"
)

func cmdRules(args []string) error {
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	mode := fs.String("task-mode", "", "任务模式 work|code|chat（默认 work）")
	tier := fs.String("tier", "", "生效模型档位 economy|coding|office|reasoning")
	role := fs.String("role", "", "角色 ID，如 coder/writer/analyst")
	asJSON := fs.Bool("json", false, "输出结构化 JSON")
	flagArgs, positionals := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positionals) > 0 {
		return fmt.Errorf("rules 不接受位置参数 %q", positionals[0])
	}

	m := strings.TrimSpace(*mode)
	if m == "" {
		m = "work"
	}
	switch m {
	case "work", "code", "chat":
	default:
		return fmt.Errorf("未知任务模式 %q（可选 work|code|chat）", *mode)
	}
	t := strings.TrimSpace(*tier)
	r := strings.TrimSpace(*role)

	scenario := *mode != "" || *tier != "" || *role != ""

	if t != "" && !knownTier(t) {
		fmt.Fprintf(os.Stderr, "[gleam] 注意：档位 %q 不是已知档位名（economy|coding|office|reasoning），它会匹配不到任何按档位条件化的规则\n", t)
	}

	out := rulesOutput{
		Version: agent.RuleSetVersion(),
		Mode:    m,
		Tier:    t,
		Role:    r,
		Rules:   agent.RuleTable(),
	}
	if scenario {
		rs := agent.DescribeRuleSet(m, t, r)
		out.Active, out.Inactive = rs.Active, rs.Inactive
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	fmt.Print(renderRulesReport(out, scenario))
	return nil
}

func knownTier(t string) bool {
	switch t {
	case agent.TierEconomy, agent.TierCoding, agent.TierOffice, agent.TierReasoning:
		return true
	}
	return false
}

type rulesOutput struct {
	Version  string           `json:"version"`
	Mode     string           `json:"mode"`
	Tier     string           `json:"tier,omitempty"`
	Role     string           `json:"role,omitempty"`
	Active   []string         `json:"active,omitempty"`
	Inactive []string         `json:"inactive,omitempty"`
	Rules    []agent.RuleInfo `json:"rules"`
}

func renderRulesReport(o rulesOutput, scenario bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "规则集指纹 %s\n", o.Version)

	if scenario {
		fmt.Fprintf(&b, "场景  模式=%s  档位=%s  角色=%s\n\n", orDash(o.Mode), orDash(o.Tier), orDash(o.Role))
		if o.Mode == "chat" {
			fmt.Fprintf(&b, "生效 %d 条\n", len(o.Active))
			fmt.Fprint(&b, "对话模式直连模型、不经规划，规则表一条都没用上——产出里 rules 为空是正常现象，不是漏记。\n")
			return b.String()
		}
		fmt.Fprintf(&b, "生效 %d 条（按提示词中的出现顺序）：\n", len(o.Active))
		for _, id := range o.Active {
			fmt.Fprintf(&b, "  %-10s %s\n", slotOf(o.Rules, id), id)
		}
		if len(o.Inactive) > 0 {
			fmt.Fprintf(&b, "\n未生效 %d 条（被适用范围挡在门外）：\n", len(o.Inactive))
			for _, id := range o.Inactive {
				fmt.Fprintln(&b, "  "+id)
			}
		}
		fmt.Fprint(&b, "\n这段 ID 列表会写进每次产出的 result.rules 与评测报告；\n回归时先比它——规则改了和代码改坏了，现象是一样的。\n")
		return b.String()
	}

	items, chars := 0, 0
	fmt.Fprintf(&b, "\n%-10s %-18s %4s %6s  %s\n", "槽位", "规则 ID", "条目", "字符", "适用范围（空=不限）")
	for _, r := range o.Rules {
		items += r.Items
		chars += r.Chars
		fmt.Fprintf(&b, "%-10s %-18s %4d %6d  %s\n", r.Slot, r.ID, r.Items, r.Chars, scopeText(r))
	}
	fmt.Fprintf(&b, "\n合计 %d 条规则 / %d 个条目 / %d 字符", len(o.Rules), items, chars)
	fmt.Fprint(&b, "（含各档位变体，单次只会取其中一份）\n")
	fmt.Fprint(&b, "\n加 --task-mode/--tier/--role 看某个场景实际生效的几条。\n")
	return b.String()
}

func slotOf(rules []agent.RuleInfo, id string) string {
	for _, r := range rules {
		if r.ID == id {
			return r.Slot
		}
	}
	return "-"
}

func scopeText(r agent.RuleInfo) string {
	var parts []string
	if len(r.Modes) > 0 {
		parts = append(parts, "模式 "+strings.Join(r.Modes, "/"))
	}
	if len(r.Tiers) > 0 {
		parts = append(parts, "档位 "+strings.Join(r.Tiers, "/"))
	}
	if len(r.Roles) > 0 {
		parts = append(parts, "角色 "+strings.Join(r.Roles, "/"))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "  ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
