package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gleam/internal/atomicfile"
)

func SaveBaseline(path string, rep Report) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建基线目录失败: %w", err)
		}
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化基线失败: %w", err)
	}
	if err := atomicfile.Write(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("写入基线失败: %w", err)
	}
	return nil
}

func LoadBaseline(path string) (Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var rep Report
	if err := json.Unmarshal(b, &rep); err != nil {
		return Report{}, fmt.Errorf("解析基线失败（%s）: %w", path, err)
	}
	return rep, nil
}

func Compare(now, base Report) (*Diff, error) {
	if now.Depth != base.Depth {
		return nil, fmt.Errorf("深度不一致：基线是 %s，本次是 %s（换深度请另存一份基线）", base.Depth, now.Depth)
	}
	if now.Tier != base.Tier {
		return nil, fmt.Errorf("模拟档位不一致：基线是 %q，本次是 %q（换档位请另存一份基线）", base.Tier, now.Tier)
	}
	d := &Diff{
		BaselineAt:  base.GeneratedAt,
		PromptDelta: now.PromptChars - base.PromptChars,
	}
	if now.RuleSet != base.RuleSet {
		d.RuleSetFrom, d.RuleSetTo = base.RuleSet, now.RuleSet
	}

	nowPass := map[string]bool{}
	for _, c := range now.Cases {
		nowPass[c.ID] = c.Passed
	}
	basePass := map[string]bool{}
	for _, c := range base.Cases {
		basePass[c.ID] = c.Passed
	}

	for _, c := range now.Cases {
		if c.Known {
			continue
		}
		was, existed := basePass[c.ID]
		switch {
		case !existed:
			d.Added = append(d.Added, c.ID)
		case was && !c.Passed:
			d.Broke = append(d.Broke, c.ID)
		case !was && c.Passed:
			d.Fixed = append(d.Fixed, c.ID)
		}
	}
	for _, c := range base.Cases {
		if _, ok := nowPass[c.ID]; !ok {
			d.Removed = append(d.Removed, c.ID)
		}
	}
	return d, nil
}

func (d *Diff) Regressed() bool {
	return d != nil && len(d.Broke) > 0
}

func (d *Diff) Describe() []string {
	if d == nil {
		return nil
	}
	var out []string
	if len(d.Broke) > 0 {
		out = append(out, fmt.Sprintf("⚠ 回归 %d 项（基线通过、本次未过）：%s", len(d.Broke), join(d.Broke)))
	}
	if len(d.Fixed) > 0 {
		out = append(out, fmt.Sprintf("✓ 修好 %d 项（基线未过、本次通过）：%s", len(d.Fixed), join(d.Fixed)))
	}
	if len(d.Added) > 0 {
		out = append(out, fmt.Sprintf("+ 新增用例 %d 条：%s", len(d.Added), join(d.Added)))
	}
	if len(d.Removed) > 0 {
		out = append(out, fmt.Sprintf("− 基线里有、本次没跑 %d 条：%s（用例被删会让通过率虚高，确认是有意为之）",
			len(d.Removed), join(d.Removed)))
	}
	if d.PromptDelta != 0 {
		out = append(out, fmt.Sprintf("提示词合计 %+d 字符", d.PromptDelta))
	}
	if d.RuleSetFrom != d.RuleSetTo {
		out = append(out, fmt.Sprintf("规则集 %s → %s（规则表内容变了；有回归时先怀疑规则改动，不是代码）",
			orUnknown(d.RuleSetFrom), orUnknown(d.RuleSetTo)))
	}
	if len(out) == 0 {
		out = append(out, "与基线一致，无回归")
	}
	return out
}

func orUnknown(s string) string {
	if s == "" {
		return "未知"
	}
	return s
}

func join(list []string) string {
	s := ""
	for i, v := range list {
		if i > 0 {
			s += "、"
		}
		s += v
	}
	return s
}
