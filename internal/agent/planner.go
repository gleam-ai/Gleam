package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/memory"
	"gleam/internal/harness/registry"
	"gleam/internal/llm"
	"gleam/internal/textmatch"
	"gleam/pkg/types"
)

const planningTimeout = 90 * time.Second

const screenTimeout = 15 * time.Second

type Planner struct {
	LLM            llm.Client
	Reg            *registry.Registry
	MaxSteps       int
	Style          string
	Role           string
	TaskMode       string
	CommitHint     string
	TaskID         string
	MaxToolSchemas int
	Helper         llm.Client
	ForceTools     []string
	RoleTools      []string
	Tier           string
	OnLLMDelta     func(text string)
	LastBreakdown  types.ContextBreakdown
	Pinned         []string
}

func (p *Planner) Plan(ctx context.Context, goal, cwd string, recent []memory.Turn, relevant []memory.Hit, summary, feedback string) (types.Plan, error) {
	sys := p.buildSystemPrompt(goal, cwd, recent, relevant, summary, feedback)
	req := llm.ChatRequest{System: sys, Messages: []llm.Message{{Role: llm.RoleUser, Content: goal}}, TaskID: p.TaskID}

	pctx, cancel := context.WithTimeout(ctx, planningTimeout)
	defer cancel()

	var text string
	var err error
	text, err = p.LLM.Chat(pctx, req)
	if err != nil {
		return types.Plan{}, fmt.Errorf("规划调用失败: %w", err)
	}

	raw, err := extractJSON(text)
	if err != nil {
		return types.Plan{}, fmt.Errorf("规划输出不是有效 JSON: %w", err)
	}
	plan, err := p.validate(raw, goal)
	if err != nil {
		return types.Plan{}, fmt.Errorf("计划校验失败: %w", err)
	}
	return plan, nil
}

func (p *Planner) toolMenuSection() string {
	if p.MaxToolSchemas <= 0 {
		return ""
	}
	tools := p.Reg.List()
	if len(tools) <= p.MaxToolSchemas {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n## 能力菜单（共 %d 个工具）\n", len(tools))
	for _, t := range tools {
		fmt.Fprintf(&b, "- %s\n", t.Name())
	}
	return b.String()
}

func (p *Planner) toolSchemaSection(goal string) string {
	tools := p.Reg.List()
	if p.MaxToolSchemas <= 0 || len(tools) <= p.MaxToolSchemas {
		var b strings.Builder
		b.WriteString("\n## 可用工具\n")
		for _, t := range tools {
			b.WriteString(compactToolSchema(t))
		}
		return b.String()
	}
	picked := p.selectTools(goal, tools, p.MaxToolSchemas)
	var b strings.Builder
	if len(p.RoleTools) > 0 {
		b.WriteString("\n（本场景核心工具：" + strings.Join(p.RoleTools, "、") + "，已附完整 schema）\n")
	}
	if len(p.ForceTools) > 0 {
		b.WriteString("\n（本任务此前已用过：")
		for i, n := range p.ForceTools {
			if i > 0 {
				b.WriteString("、")
			}
			b.WriteString(n)
		}
		b.WriteString("，已附完整 schema）\n")
	}
	fmt.Fprintf(&b, "\n## 可用工具（以下 %d 个与本次目标最相关，附完整参数 schema）\n", len(picked))
	for _, t := range picked {
		b.WriteString(compactToolSchema(t))
	}
	b.WriteString("只能使用上面附了 schema 的工具；菜单里的其他工具本次不可用。\n")
	return b.String()
}

func compactToolSchema(t types.Tool) string {
	schema := t.Schema()
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	reqSet := make(map[string]bool, len(required))
	for _, r := range required {
		reqSet[r] = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- %s：%s | 参数：", t.Name(), firstLine(t.Description()))
	first := true
	for name, prop := range props {
		if !first {
			b.WriteString("，")
		}
		first = false
		propMap, ok := prop.(map[string]any)
		if !ok {
			fmt.Fprintf(&b, "%s", name)
			continue
		}
		propType, _ := propMap["type"].(string)
		propDesc, _ := propMap["description"].(string)
		fmt.Fprintf(&b, "%s(%s", name, propType)
		if propDesc != "" {
			fmt.Fprintf(&b, ", %s", firstLine(propDesc))
		}
		b.WriteString(")")
		if reqSet[name] {
			b.WriteString("[必填]")
		}
	}
	if len(props) == 0 {
		b.WriteString("无")
	}
	b.WriteString("\n")
	return b.String()
}

func (p *Planner) selectTools(goal string, tools []types.Tool, n int) []types.Tool {
	var picked []types.Tool
	pickedSet := map[string]bool{}
	keep := func(t types.Tool) {
		if !pickedSet[t.Name()] && len(picked) < n {
			pickedSet[t.Name()] = true
			picked = append(picked, t)
		}
	}
	for _, t := range tools {
		if t.Name() == "reply" || strings.Contains(strings.ToLower(goal), t.Name()) || p.forced(t.Name()) {
			keep(t)
		}
	}
	for _, name := range p.ForceTools {
		if t, ok := p.Reg.Get(name); ok {
			keep(t)
		}
	}
	for _, name := range p.RoleTools {
		if t, ok := p.Reg.Get(name); ok {
			keep(t)
		}
	}
	if p.TaskMode == "code" {
		for _, name := range []string{"file.write", "file.read", "file.list", "file.search", "shell.exec"} {
			if t, ok := p.Reg.Get(name); ok {
				keep(t)
			}
		}
	}
	names := p.screenToolNames(goal, tools, n)
	if len(names) == 0 {
		names = scoreToolNames(goal, tools, n)
	}
	for _, name := range names {
		if t, ok := p.Reg.Get(name); ok {
			keep(t)
		}
	}
	for _, t := range tools {
		keep(t)
	}
	return picked
}

func (p *Planner) SelectTools(goal string) []string {
	tools := p.Reg.List()
	if p.MaxToolSchemas <= 0 || len(tools) <= p.MaxToolSchemas {
		out := make([]string, 0, len(tools))
		for _, t := range tools {
			out = append(out, t.Name())
		}
		return out
	}
	picked := p.selectTools(goal, tools, p.MaxToolSchemas)
	out := make([]string, 0, len(picked))
	for _, t := range picked {
		out = append(out, t.Name())
	}
	return out
}

func (p *Planner) SystemPromptFor(goal, cwd string) string {
	return p.buildSystemPrompt(goal, cwd, nil, nil, "", "")
}

func (p *Planner) ActiveRules() []string { return ActiveRuleIDs(p.TaskMode, p.Tier, p.Role) }

func (p *Planner) RelevantTools(goal string) []string {
	tools := p.Reg.List()
	if len(tools) == 0 {
		return nil
	}
	n := p.MaxToolSchemas
	if n <= 0 || n > len(tools) {
		n = len(tools)
	}
	return scoreToolNames(goal, tools, n)
}

const routingTextLimit = 100

func routingText(t types.Tool) string {
	return strings.ToLower(t.Name() + " " + types.Shorten(t.Description(), routingTextLimit))
}

func scoreToolNames(goal string, tools []types.Tool, n int) []string {
	terms := textmatch.Terms(goal)
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		name  string
		score int
	}
	var list []scored
	for _, t := range tools {
		name := strings.ToLower(t.Name())
		s := textmatch.Score(terms, routingText(t)) + 2*textmatch.Score(terms, name)
		if s > 0 {
			list = append(list, scored{t.Name(), s})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].name < list[j].name
	})
	if len(list) > n {
		list = list[:n]
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.name)
	}
	return out
}

func (p *Planner) forced(name string) bool {
	for _, n := range p.ForceTools {
		if n == name {
			return true
		}
	}
	return false
}

func (p *Planner) screenToolNames(goal string, tools []types.Tool, n int) []string {
	if p.Helper == nil {
		return nil
	}
	var menu strings.Builder
	for _, t := range tools {
		fmt.Fprintf(&menu, "%s：%s\n", t.Name(), firstLine(t.Description()))
	}
	sys := llm.MarkerToolPick + "\n你是工具筛选器。根据用户目标，从工具菜单里挑出最可能用到的工具，" +
		"只输出 JSON 数组（工具名字符串），最多 " + fmt.Sprint(n) + " 个，不要解释。"
	req := llm.ChatRequest{
		System: sys,
		Messages: []llm.Message{{Role: llm.RoleUser,
			Content: "目标：" + types.Shorten(goal, 300) + "\n\n工具菜单：\n" + menu.String()}},
		Temperature: 0, MaxTokens: 200, TaskID: p.TaskID,
	}
	sctx, cancel := context.WithTimeout(context.Background(), screenTimeout)
	defer cancel()
	text, err := p.Helper.Chat(sctx, req)
	if err != nil {
		return nil
	}
	raw, err := extractJSONArray(text)
	if err != nil {
		return nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil
	}
	return names
}

func firstLine(s string) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	return types.Shorten(s, 60)
}

func styleInstruction(style string) string {
	switch style {
	case "rigorous":
		return "严谨克制：描述精确，不夸大，不遗漏限制条件。"
	case "gentle":
		return "温和自然：像一位可靠的同事，平实友好。"
	default:
		return "高效简洁：直击要点，不做冗余解释。"
	}
}

const promptIdentity = "你是 Gleam（微光），一个本地优先的桌面智能体的规划器。" +
	"你的职责是把用户目标拆解为可执行的工具调用步骤。"

const (
	recentTurnsInjected = 6
	turnContentCap      = 160
	summaryInjectCap    = 600
	memoryHitsInjected  = 3
	memoryHitContentCap = 120
	pinnedInjected      = 8
	pinnedInjectCap     = 200
)

func (p *Planner) buildSystemPrompt(goal, cwd string, recent []memory.Turn, relevant []memory.Hit, summary, feedback string) string {
	s, _ := p.buildSystemPromptMeasured(goal, cwd, recent, relevant, summary, feedback)
	return s
}

func (p *Planner) buildSystemPromptMeasured(goal, cwd string, recent []memory.Turn, relevant []memory.Hit, summary, feedback string) (string, types.ContextBreakdown) {
	var b strings.Builder
	var bd types.ContextBreakdown
	seg := func(fn func()) int {
		start := b.Len()
		fn()
		return b.Len() - start
	}

	bd.Instruction += seg(func() {
		b.WriteString(promptIdentity)
		b.WriteString(llm.MarkerPlan + "\n")
		b.WriteString("\n" + renderRules(SlotContract, p.TaskMode, p.Tier, p.Role))
	})

	bd.Knowledge += seg(func() {
		role := FindRole(p.Role)
		if p.Role != "" && p.Role != "general" && role.ID != "general" {
			fmt.Fprintf(&b, "\n## 专家角色：%s\n%s\n", role.Name, role.SystemPrompt)
		}
		if role.AcceptanceFocus != "" {
			fmt.Fprintf(&b, "\n本角色（%s）的验收重点：%s\n", role.Name, role.AcceptanceFocus)
		}
		if role.OutputFormat != "" {
			fmt.Fprintf(&b, "\n## 本场景产出格式\n%s\n", role.OutputFormat)
		}
	})
	bd.Instruction += seg(func() {
		b.WriteString("\n## 协作风格\n" + styleInstruction(p.Style))
	})
	bd.Instruction += seg(func() {
		if blk := renderRules(SlotCode, p.TaskMode, p.Tier, p.Role); blk != "" {
			b.WriteString("\n\n" + blk)
		}
	})
	if p.TaskMode == "code" && strings.TrimSpace(p.CommitHint) != "" {
		bd.Instruction += seg(func() {
			b.WriteString("\n\n## 提交说明写法\n" + strings.TrimSpace(p.CommitHint) + "\n")
		})
	}
	bd.Capability += seg(func() {
		b.WriteString(p.toolMenuSection())
	})
	bd.Instruction += seg(func() {
		if _, ok := p.Reg.Get("schedule.create"); ok {
			b.WriteString("\n" + renderRules(SlotSchedule, p.TaskMode, p.Tier, p.Role) + "\n")
		}
	})

	bd.Capability += seg(func() {
		b.WriteString(p.toolSchemaSection(goal))
	})
	bd.State += seg(func() {
		if n := min(len(p.Pinned), pinnedInjected); n > 0 {
			b.WriteString("\n## 用户约束（钉住，永久有效）\n")
			for _, pin := range p.Pinned[:n] {
				fmt.Fprintf(&b, "- %s\n", types.Shorten(pin, pinnedInjectCap))
			}
		}
	})
	bd.State += seg(func() {
		fmt.Fprintf(&b, "\n## 上下文\n- 工作目录: %s\n- 操作系统: %s\n", cwd, runtime.GOOS)
	})
	bd.State += seg(func() {
		if strings.TrimSpace(summary) != "" {
			fmt.Fprintf(&b, "\n## 早期上下文摘要（已自动压缩）\n%s\n", types.Shorten(summary, summaryInjectCap))
		}
	})
	bd.State += seg(func() {
		if len(relevant) > 0 {
			b.WriteString("\n## 相关长期记忆\n")
			for _, h := range relevant {
				fmt.Fprintf(&b, "- %s\n", types.Shorten(h.Content, memoryHitContentCap))
			}
		}
	})
	bd.State += seg(func() {
		if len(recent) > 0 {
			b.WriteString("\n## 最近对话\n")
			for _, t := range recent {
				fmt.Fprintf(&b, "- %s: %s\n", t.Role, types.Shorten(t.Content, turnContentCap))
			}
		}
	})
	bd.State += seg(func() {
		if feedback != "" {
			fmt.Fprintf(&b, "\n## 重规划反馈（务必修正上次的问题）\n%s\n", feedback)
		}
	})
	bd.Instruction += seg(func() {
		b.WriteString("\n## 输出提醒\n只输出一个 JSON 对象，形如 {\"steps\":[...],\"estimated_time\":\"...\",\"acceptance\":[...]}，不要任何其他文字、不要代码块。\n")
	})
	bd.Total = b.Len()
	p.LastBreakdown = bd
	return b.String(), bd
}

type rawPlan struct {
	Steps []struct {
		ID          string         `json:"id"`
		Description string         `json:"description"`
		Tool        string         `json:"tool"`
		Args        map[string]any `json:"args"`
		DependsOn   []string       `json:"depends_on"`
	} `json:"steps"`
	EstimatedTime string   `json:"estimated_time"`
	Acceptance    []string `json:"acceptance"`
}

const maxAcceptance = 4

func normalizeAcceptance(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, types.Shorten(s, 120))
		if len(out) >= maxAcceptance {
			break
		}
	}
	return out
}

func (p *Planner) validate(raw []byte, goal string) (types.Plan, error) {
	var rp rawPlan
	if err := json.Unmarshal(raw, &rp); err != nil {
		return types.Plan{}, fmt.Errorf("JSON 解析失败: %w", err)
	}
	if len(rp.Steps) == 0 {
		return types.Plan{}, fmt.Errorf("计划没有步骤")
	}
	maxSteps := p.MaxSteps
	if maxSteps <= 0 {
		maxSteps = config.DefaultMaxSteps
	}
	if len(rp.Steps) > maxSteps {
		return types.Plan{}, fmt.Errorf("步骤数 %d 超过上限 %d", len(rp.Steps), maxSteps)
	}

	plan := types.Plan{Goal: goal, EstimatedTime: rp.EstimatedTime, Acceptance: normalizeAcceptance(rp.Acceptance)}
	ids := map[string]bool{}
	for i, rs := range rp.Steps {
		id := strings.TrimSpace(rs.ID)
		if id == "" {
			id = fmt.Sprintf("s%d", i+1)
		}
		if ids[id] {
			return types.Plan{}, fmt.Errorf("步骤 ID 重复: %s", id)
		}
		ids[id] = true
		tool, ok := p.Reg.Get(strings.TrimSpace(rs.Tool))
		if !ok {
			return types.Plan{}, fmt.Errorf("步骤 %s 引用了不存在的工具 %q", id, rs.Tool)
		}
		args := rs.Args
		if args == nil {
			args = map[string]any{}
		}
		if err := validateArgs(tool, args); err != nil {
			return types.Plan{}, fmt.Errorf("步骤 %s（%s）参数不合法: %w", id, tool.Name(), err)
		}
		plan.Steps = append(plan.Steps, types.Step{
			ID: id, Description: rs.Description, Tool: tool.Name(), Args: args, DependsOn: rs.DependsOn,
		})
	}
	for _, st := range plan.Steps {
		for _, d := range st.DependsOn {
			if d == st.ID {
				return types.Plan{}, fmt.Errorf("步骤 %s 依赖自身", st.ID)
			}
			if !ids[d] {
				return types.Plan{}, fmt.Errorf("步骤 %s 依赖了不存在的步骤 %q", st.ID, d)
			}
		}
	}
	if err := checkAcyclic(plan.Steps); err != nil {
		return types.Plan{}, err
	}
	return plan, nil
}

func validateArgs(t types.Tool, args map[string]any) error {
	schema := t.Schema()
	req, _ := schema["required"].([]any)
	props, _ := schema["properties"].(map[string]any)
	for _, r := range req {
		key, ok := r.(string)
		if !ok {
			continue
		}
		v, present := args[key]
		if s, isStr := v.(string); isStr {
			if !present || strings.TrimSpace(s) == "" {
				return fmt.Errorf("缺少必填参数 %q", key)
			}
			continue
		}
		if !present || v == nil {
			return fmt.Errorf("缺少必填参数 %q", key)
		}
	}
	for k, v := range args {
		prop, ok := props[k].(map[string]any)
		if !ok {
			continue
		}
		typ, _ := prop["type"].(string)
		if err := checkType(k, v, typ); err != nil {
			return err
		}
	}
	return nil
}

func checkType(key string, v any, typ string) error {
	switch typ {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("参数 %q 应为字符串", key)
		}
	case "number", "integer":
		switch v.(type) {
		case float64, int64, int:
		default:
			return fmt.Errorf("参数 %q 应为数字", key)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("参数 %q 应为布尔值", key)
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("参数 %q 应为数组", key)
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("参数 %q 应为对象", key)
		}
	}
	return nil
}

func checkAcyclic(steps []types.Step) error {
	deps := map[string][]string{}
	for _, st := range steps {
		deps[st.ID] = st.DependsOn
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(id string, stack []string) error
	visit = func(id string, stack []string) error {
		color[id] = gray
		stack = append(stack, id)
		for _, d := range deps[id] {
			switch color[d] {
			case gray:
				return fmt.Errorf("计划存在循环依赖: %s", strings.Join(append(stack, d), " → "))
			case white:
				if err := visit(d, stack); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}
	for id := range deps {
		if color[id] == white {
			if err := visit(id, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractJSON(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	start := strings.Index(s, "{")
	if start < 0 {
		return nil, fmt.Errorf("未找到 JSON 对象")
	}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 && i >= start {
				return []byte(s[start : i+1]), nil
			}
		}
	}
	return nil, fmt.Errorf("JSON 对象未闭合")
}

func extractJSONArray(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	start := strings.Index(s, "[")
	if start < 0 {
		return nil, fmt.Errorf("未找到 JSON 数组")
	}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 && i >= start {
				return []byte(s[start : i+1]), nil
			}
		}
	}
	return nil, fmt.Errorf("JSON 数组未闭合")
}

func DeltaThrottle(n int, push func(text string)) (func(delta string), func()) {
	var mu sync.Mutex
	var buf strings.Builder
	emit := func() {
		mu.Lock()
		if buf.Len() > 0 {
			push(buf.String())
			buf.Reset()
		}
		mu.Unlock()
	}
	onDelta := func(delta string) {
		mu.Lock()
		buf.WriteString(delta)
		flush := buf.Len() >= n
		mu.Unlock()
		if flush {
			emit()
		}
	}
	return onDelta, emit
}
