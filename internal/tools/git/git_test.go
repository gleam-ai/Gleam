package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsValidRefName(t *testing.T) {
	ok := []string{"feature-x", "修复登录", "a/b/c", "v1.0"}
	bad := []string{"", "-x", ".hidden", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a..b", "a/", "a\\b"}
	for _, s := range ok {
		if !ValidRefName(s) {
			t.Errorf("%q 应合法", s)
		}
	}
	for _, s := range bad {
		if ValidRefName(s) {
			t.Errorf("%q 应被拒绝", s)
		}
	}
}

func TestWithPrefix(t *testing.T) {
	c := Config{BranchPrefix: "gleam/"}
	if got := c.withPrefix("fix"); got != "gleam/fix" {
		t.Errorf("got %q", got)
	}
	if got := c.withPrefix("gleam/fix"); got != "gleam/fix" {
		t.Errorf("已有前缀不该重复加，got %q", got)
	}
	if got := (Config{}).withPrefix("fix"); got != "fix" {
		t.Errorf("没配前缀应原样，got %q", got)
	}
}

func TestBranchAndCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("环境里没有 git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Gleam Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{BranchPrefix: "gleam/", Roots: []string{dir}}
	prov := func() Config { return cfg }
	ctx := context.Background()

	out, err := NewBranch(prov).Execute(ctx, map[string]any{"name": "fix-thing"})
	if err != nil {
		t.Fatalf("建分支失败: %v", err)
	}
	if got := out.(map[string]any)["branch"]; got != "gleam/fix-thing" {
		t.Errorf("分支名 = %v，应带前缀", got)
	}

	out2, err := NewCommit(prov).Execute(ctx, map[string]any{"message": "feat: 初始化"})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if committed, _ := out2.(map[string]any)["committed"].(bool); !committed {
		t.Errorf("应提交成功: %v", out2)
	}

	out3, err := NewCommit(prov).Execute(ctx, map[string]any{"message": "空提交"})
	if err != nil {
		t.Fatalf("空提交不该报错: %v", err)
	}
	if committed, _ := out3.(map[string]any)["committed"].(bool); committed {
		t.Errorf("没有改动时不该产生提交: %v", out3)
	}

	if _, err := NewBranch(prov).Execute(ctx, map[string]any{"name": "x", "dir": filepath.Dir(dir)}); err == nil {
		t.Error("工作区外的目录应被拒绝")
	}
}

func TestPushArgvForceWithLease(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("环境里没有 git")
	}
	work := t.TempDir()
	bare := t.TempDir()
	mk := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mk(bare, "init", "--bare")
	mk(work, "init")
	mk(work, "config", "user.email", "test@example.invalid")
	mk(work, "config", "user.name", "Gleam Test")
	mk(work, "remote", "add", "origin", bare)
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cfg := Config{Roots: []string{work}, ForcePush: true}
	prov := func() Config { return cfg }
	if _, err := NewCommit(prov).Execute(ctx, map[string]any{"message": "init"}); err != nil {
		t.Fatal(err)
	}
	out, err := NewPush(prov).Execute(ctx, map[string]any{"set_upstream": true})
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if f, _ := out.(map[string]any)["force_with_lease"].(bool); !f {
		t.Error("开了强制推送，结果里应标出用了 --force-with-lease")
	}

	cfg.ForcePush = false
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCommit(prov).Execute(ctx, map[string]any{"message": "second"}); err != nil {
		t.Fatal(err)
	}
	out2, err := NewPush(prov).Execute(ctx, nil)
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if f, _ := out2.(map[string]any)["force_with_lease"].(bool); f {
		t.Error("关了强制推送还带 --force-with-lease")
	}
}
