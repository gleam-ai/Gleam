package toolutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveInRoots(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if got, err := ResolveInRoots(sub, []string{root}); err != nil || got != sub {
		t.Errorf("根内路径应放行: %q %v", got, err)
	}
	if got, err := ResolveInRoots("sub", []string{root}); err != nil || got != sub {
		t.Errorf("相对路径应基于第一个根: %q %v", got, err)
	}
	if _, err := ResolveInRoots(root, []string{root}); err != nil {
		t.Errorf("根自身应放行: %v", err)
	}
	if _, err := ResolveInRoots(other, []string{root}); err == nil {
		t.Error("根外路径应拒绝")
	}
	sibling := root + "-sibling"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sibling)
	if _, err := ResolveInRoots(sibling, []string{root}); err == nil {
		t.Error("前缀相似的兄弟目录应拒绝（Rel 判定，不是字符串前缀）")
	}
	if _, err := ResolveInRoots("  ", []string{root}); err == nil {
		t.Error("空路径应拒绝")
	}
	if _, err := ResolveInRoots(sub, nil); err == nil {
		t.Error("未配置根目录时应拒绝任何路径")
	}
}

func TestResolveInRootsFailsClosedOnResolutionErrors(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "invalid\x00", "nested", "file.txt")
	for _, tc := range []struct {
		name string
		path string
		root string
	}{
		{"target", bad, root},
		{"root", bad, bad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ResolveInRoots(tc.path, []string{tc.root}); err == nil {
				t.Fatal("an unresolvable path must not fall back to lexical containment")
			}
		})
	}
}

func TestResolveInRootsRejectsSymlinkedAncestorForMissingChild(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "escape")
	if err := linkDir(link, outside); err != nil {
		t.Skipf("无法创建目录链接: %v", err)
	}
	target := filepath.Join(link, "not-created-yet", "file.txt")
	if _, err := ResolveInRoots(target, []string{root}); err == nil {
		t.Fatal("不存在的子路径不能绕过已有链接祖先")
	}
	if _, err := ResolveInRoots(filepath.Join(link, "existing.txt"), []string{root}); err == nil {
		t.Fatal("已有文件经链接后出界应拒绝")
	}
}

func TestResolveInRootsAllowsMissingChildInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "new-dir", "file.txt")
	got, err := ResolveInRoots(target, []string{root})
	if err != nil || got != target {
		t.Fatalf("根内尚不存在的路径应放行: %q %v", got, err)
	}
}

func linkDir(link, target string) error {
	symlinkErr := os.Symlink(target, link)
	if symlinkErr == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return symlinkErr
	}
	// Junctions do not need the symlink privilege granted to the process.
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
}
