package agent

import (
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/registry"
	"gleam/internal/llm"
	"gleam/internal/tools/file"
	"gleam/internal/tools/shell"
	"gleam/internal/tools/std"
	"gleam/internal/tools/web"
	"gleam/pkg/types"
)

func realishRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	file.New(t.TempDir()).RegisterAll(reg)
	reg.MustRegister(shell.New(5 * time.Second))
	reg.MustRegister(web.New())
	reg.MustRegister(std.NewReply())
	for _, name := range []string{
		"memory.save", "memory.search",
		"schedule.create", "schedule.list", "schedule.delete",
		"skill.list", "skill.run",
	} {
		reg.MustRegister(&funcTool{name: name, perm: types.PermissionReadOnly})
	}
	return reg
}

func TestRoleTemplate_EveryRoleIsComplete(t *testing.T) {
	knownTiers := map[string]bool{
		TierEconomy: true, TierCoding: true, TierOffice: true, TierReasoning: true,
	}
	for _, r := range BuiltinRoles {
		if strings.TrimSpace(r.SystemPrompt) == "" {
			t.Errorf("角色 %s 缺少系统提示词", r.ID)
		}
		if strings.TrimSpace(r.AcceptanceFocus) == "" {
			t.Errorf("角色 %s 缺少验收重点", r.ID)
		}
		if strings.TrimSpace(r.OutputFormat) == "" {
			t.Errorf("角色 %s 缺少产出格式约束", r.ID)
		}
		if !knownTiers[r.ModelTier] {
			t.Errorf("角色 %s 的模型档位 %q 不在已知档位里", r.ID, r.ModelTier)
		}
		if r.ID != "general" && len(r.Tools) == 0 {
			t.Errorf("角色 %s 未声明核心工具，菜单模式下容易被筛漏", r.ID)
		}
	}
}

func TestRoleTemplate_ToolNamesResolve(t *testing.T) {
	reg := realishRegistry(t)
	for _, r := range BuiltinRoles {
		for _, name := range r.Tools {
			if _, ok := reg.Get(name); !ok {
				t.Errorf("角色 %s 声明的工具 %q 在注册表里不存在", r.ID, name)
			}
		}
	}
}

func TestRoleTemplate_ToolsAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, r := range BuiltinRoles {
		local := map[string]bool{}
		for _, name := range r.Tools {
			if local[name] {
				t.Errorf("角色 %s 的工具列表里 %q 重复", r.ID, name)
			}
			local[name] = true
			if prev, ok := seen[name]; ok {
				t.Logf("工具 %q 同时出现在 %s 与 %s（可接受，但值得留意区分度）", name, prev, r.ID)
			} else {
				seen[name] = r.ID
			}
		}
	}
	if len(seen) < 5 {
		t.Errorf("各角色声明的工具去重后只有 %d 个，模板之间缺少区分度", len(seen))
	}
}

func TestRoleTemplate_OutputFormatReachesPrompt(t *testing.T) {
	p := &Planner{Reg: bigRegistry(25), MaxToolSchemas: 12, Role: "coder"}
	got := p.buildSystemPrompt("改个 bug", "D:/ws", nil, nil, "", "")
	want := FindRole("coder").OutputFormat
	if want == "" {
		t.Fatal("coder 角色应有产出格式约束")
	}
	if !strings.Contains(got, want) {
		t.Error("产出格式没有进入规划器提示词")
	}
	if !strings.Contains(got, "## 本场景产出格式") {
		t.Error("产出格式应有独立小标题，便于模型识别")
	}
}

func TestRoleTemplate_RoleToolsAlwaysGetSchema(t *testing.T) {
	reg := realishRegistry(t)
	role := FindRole("coder")
	p := &Planner{Reg: reg, MaxToolSchemas: 3, Role: role.ID, RoleTools: role.Tools}
	sec := p.toolSchemaSection("随便做点什么")

	if got := strings.Count(sec, " | 参数："); got != 3 {
		t.Errorf("仍应只注入 3 个 schema，实际 %d", got)
	}
	if !strings.Contains(sec, "- reply：") {
		t.Error("reply 是收尾步骤，必须始终附完整 schema")
	}
	present := 0
	for _, name := range role.Tools {
		if strings.Contains(sec, "- "+name+"：") {
			present++
		}
	}
	if want := 3 - 1; present < want {
		t.Errorf("剩余预算应优先给场景核心工具（至少 %d 个），实际只装下 %d 个", want, present)
	}
	if !strings.Contains(sec, "本场景核心工具") {
		t.Error("应说明这些工具为何被保留，用户才知道它从哪来")
	}
}

func TestRoleTemplate_RoleToolsFitInDefaultBudget(t *testing.T) {
	reg := realishRegistry(t)
	for _, r := range BuiltinRoles {
		if len(r.Tools) == 0 {
			continue
		}
		p := &Planner{Reg: reg, MaxToolSchemas: 12, Role: r.ID, RoleTools: r.Tools}
		sec := p.toolSchemaSection("随便做点什么")
		for _, name := range r.Tools {
			if !strings.Contains(sec, "- "+name+"：") {
				t.Errorf("角色 %s 的核心工具 %s 在默认预算下未附 schema", r.ID, name)
			}
		}
	}
}

func TestRoleTemplate_GeneralRoleAddsNothing(t *testing.T) {
	reg := realishRegistry(t)
	role := FindRole("")
	if len(role.Tools) != 0 {
		t.Fatalf("通用助手不该声明核心工具，实际 %v", role.Tools)
	}
	p := &Planner{Reg: reg, MaxToolSchemas: 3, Role: role.ID, RoleTools: role.Tools}
	if sec := p.toolSchemaSection("随便做点什么"); strings.Contains(sec, "本场景核心工具") {
		t.Error("没有核心工具时不该出现该说明")
	}
}

func TestTierLLM_FallsBackToMainModel(t *testing.T) {
	main := llm.NewMock()
	mk := func(tiers map[string]string) *Agent {
		cfg := config.Default()
		cfg.LLM.Tiers = tiers
		cfg.LLM.Model = "main-model"
		cfg.LLM.Provider = "glm"
		return &Agent{LLM: main, Cfg: cfg}
	}

	if got := mk(nil).TierLLM("coder"); got != main {
		t.Error("未配档位表时应回退主模型")
	}
	if got := mk(map[string]string{"coding": "coder-model"}).TierLLM("general"); got != main {
		t.Error("角色未声明该档位时应回退主模型")
	}
	if got := mk(map[string]string{"coding": "main-model"}).TierLLM("coder"); got != main {
		t.Error("档位指向的就是主模型时，应复用主客户端而不是另建一个")
	}
	if got := mk(map[string]string{"coding": "   "}).TierLLM("coder"); got != main {
		t.Error("档位值为空白时应回退主模型")
	}
}

func TestTierLLM_BuildsClientPerTier(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "glm"
	cfg.LLM.BaseURL = "http://127.0.0.1:1"
	cfg.LLM.Model = "main-model"
	cfg.LLM.Tiers = map[string]string{"coding": "coder-model", "office": "writer-model"}
	a := &Agent{LLM: llm.NewMock(), Cfg: cfg}

	coder := a.TierLLM("coder")
	if coder == a.LLM {
		t.Fatal("配了 coding 档位就应换模型")
	}
	if coder.Name() != "coder-model" {
		t.Errorf("应使用 coding 档位指定的模型，实际 %s", coder.Name())
	}
	if again := a.TierLLM("coder"); again != coder {
		t.Error("同一档位应复用同一个客户端")
	}
	if writer := a.TierLLM("writer"); writer == coder || writer.Name() != "writer-model" {
		t.Errorf("不同档位应各自建客户端，实际 %v", writer.Name())
	}

	a.Cfg.LLM.Tiers["coding"] = "coder-model-v2"
	a.RebuildTierClients()
	if got := a.TierLLM("coder").Name(); got != "coder-model-v2" {
		t.Errorf("重建后应使用新模型，实际 %s", got)
	}
}

func TestTierLLM_MockProviderNeverBuildsClient(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Provider = "mock"
	cfg.LLM.Model = "main-model"
	cfg.LLM.Tiers = map[string]string{"coding": "coder-model"}
	main := llm.NewMock()
	a := &Agent{LLM: main, Cfg: cfg}
	if got := a.TierLLM("coder"); got != main {
		t.Error("mock 会话下档位应失效，避免离线自测打网络")
	}
}
