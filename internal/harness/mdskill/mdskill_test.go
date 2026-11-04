package mdskill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, frontmatter, body string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestList_TwoFormsAndDedupe(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "code-review",
		"name: code-review\ndescription: 审查当前改动\nwhen_to_use: 用户要求 review 时\ndisable-model-invocation: true\n",
		"# 步骤\n1. 读 diff")
	// Flat layout <root>/<name>.md
	flat := filepath.Join(root, "quick-note.md")
	if err := os.WriteFile(flat, []byte("---\nname: quick-note\ndescription: 快速记录\n---\n正文"), 0o644); err != nil {
		t.Fatal(err)
	}

	list := List([]Root{{Dir: root, Label: "workspace"}})
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	byName := map[string]Skill{}
	for _, s := range list {
		byName[s.Name] = s
	}
	cr := byName["code-review"]
	if cr.Description != "审查当前改动" || cr.WhenToUse != "用户要求 review 时" || !cr.UserOnly {
		t.Errorf("code-review = %+v", cr)
	}
	if cr.Source != "workspace" {
		t.Errorf("source = %q", cr.Source)
	}
	if _, ok := byName["quick-note"]; !ok {
		t.Errorf("平铺形态未被发现")
	}
}

func TestList_NearestRootWins(t *testing.T) {
	ws, home := t.TempDir(), t.TempDir()
	writeSkill(t, ws, "deploy", "name: deploy\ndescription: 工作区版本\n", "ws body")
	writeSkill(t, home, "deploy", "name: deploy\ndescription: 用户版本\n", "home body")

	list := List([]Root{{Dir: ws, Label: "workspace"}, {Dir: home, Label: "user"}})
	if len(list) != 1 || list[0].Description != "工作区版本" {
		t.Fatalf("同名应就近遮蔽: %+v", list)
	}
}

func TestOpen_ReturnsFullContent(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "code-review", "name: code-review\ndescription: 审查\n", "## 第一步\n读取 diff")

	sk, err := Open([]Root{{Dir: root, Label: "workspace"}}, "code-review")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sk.Content, "## 第一步") || !strings.Contains(sk.Content, "读取 diff") {
		t.Errorf("content 不含正文: %q", sk.Content)
	}
	if _, err := Open([]Root{{Dir: root, Label: "workspace"}}, "missing"); err == nil {
		t.Fatal("不存在的技能应报错")
	}
	if _, err := Open([]Root{{Dir: root, Label: "workspace"}}, "Bad_Name"); err == nil {
		t.Fatal("非 kebab-case 应报错")
	}
}

func TestParseFile_InvalidEntriesDropped(t *testing.T) {
	root := t.TempDir()
	// Missing description
	writeSkill(t, root, "no-desc", "name: no-desc\n", "正文")
	// Name is not kebab-case
	writeSkill(t, root, "Bad Name", "name: Bad Name\ndescription: x\n", "正文")
	// Unclosed frontmatter
	d := filepath.Join(root, "unclosed")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := List([]Root{{Dir: root, Label: "w"}}); len(got) != 0 {
		t.Fatalf("全部无效条目应被丢弃，got %+v", got)
	}
}

func TestParseFile_NameFallbackToDir(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "auto-named", "description: 只有描述\n", "正文")
	list := List([]Root{{Dir: root, Label: "w"}})
	if len(list) != 1 || list[0].Name != "auto-named" {
		t.Fatalf("name 应回退到目录名: %+v", list)
	}
}
