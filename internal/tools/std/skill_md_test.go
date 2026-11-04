package std

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gleam/internal/harness/mdskill"
)

type fakeStepStore struct{}

func (fakeStepStore) ListSummaries() []SkillSummary {
	return []SkillSummary{{Name: "alpha", Description: "步骤技能", Version: 2, Runs: 3, Successes: 3}}
}
func (fakeStepStore) GetParams(string) ([]string, error) { return nil, nil }

type fakeMD struct{ skills []mdskill.Skill }

func (f *fakeMD) List() []mdskill.Skill { return f.skills }
func (f *fakeMD) Open(name string) (mdskill.Skill, error) {
	for _, s := range f.skills {
		if s.Name == name {
			return s, nil
		}
	}
	return mdskill.Skill{}, mdskillOpenErr(name)
}

func mdskillOpenErr(name string) error {
	return &mdErr{name}
}

type mdErr struct{ name string }

func (e *mdErr) Error() string { return "技能 " + e.name + " 不存在" }

func TestSkillList_MergesMarkdownSkills(t *testing.T) {
	md := &fakeMD{skills: []mdskill.Skill{
		{Name: "code-review", Description: "审查改动", WhenToUse: "要求 review"},
	}}
	tool := NewSkillList(fakeStepStore{}, md)
	out, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	skills := out.(map[string]any)["skills"].([]SkillSummary)
	if len(skills) != 2 {
		t.Fatalf("应合并两类技能，got %d", len(skills))
	}
	sources := map[string]string{}
	for _, s := range skills {
		sources[s.Name] = s.Source
	}
	if sources["alpha"] != "step" || sources["code-review"] != "markdown" {
		t.Errorf("sources = %v", sources)
	}
}

func TestSkillOpen_ReturnsContent(t *testing.T) {
	md := &fakeMD{skills: []mdskill.Skill{
		{Name: "code-review", Description: "审查", Path: "/x/SKILL.md", Content: "## 步骤"},
	}}
	tool := NewSkillOpen(md)
	out, err := tool.Execute(context.Background(), map[string]any{"name": "code-review"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["content"] != "## 步骤" || m["name"] != "code-review" {
		t.Errorf("out = %v", m)
	}
}

func TestSkillOpen_UserOnlyRejected(t *testing.T) {
	md := &fakeMD{skills: []mdskill.Skill{
		{Name: "secret-flow", Description: "仅用户", UserOnly: true, Content: "..."},
	}}
	tool := NewSkillOpen(md)
	if _, err := tool.Execute(context.Background(), map[string]any{"name": "secret-flow"}); err == nil {
		t.Fatal("disable-model-invocation 技能应拒绝模型打开")
	}
}

// End to end: the list -> open progressive disclosure chain on a real disk directory.
func TestSkillMD_EndToEndDiscovery(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "commit-style")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: commit-style\ndescription: 提交信息规范\n---\n# 规范\n1. 用祈使句"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := &mdRootAdapter{roots: []mdskill.Root{{Dir: root, Label: "workspace"}}}

	listTool := NewSkillList(fakeStepStore{}, adapter)
	out, _ := listTool.Execute(context.Background(), nil)
	skills := out.(map[string]any)["skills"].([]SkillSummary)
	var found *SkillSummary
	for i := range skills {
		if skills[i].Name == "commit-style" {
			found = &skills[i]
		}
	}
	if found == nil || found.Source != "markdown" {
		t.Fatalf("commit-style 未出现在合并清单: %+v", skills)
	}

	openTool := NewSkillOpen(adapter)
	got, err := openTool.Execute(context.Background(), map[string]any{"name": "commit-style"})
	if err != nil {
		t.Fatal(err)
	}
	if got.(map[string]any)["content"] != content {
		t.Errorf("open 应返回全文")
	}
}

// mdRootAdapter mimics the minimal shape of mdSkillAdapter in main.go.
type mdRootAdapter struct{ roots []mdskill.Root }

func (a *mdRootAdapter) List() []mdskill.Skill { return mdskill.List(a.roots) }
func (a *mdRootAdapter) Open(name string) (mdskill.Skill, error) {
	return mdskill.Open(a.roots, name)
}
