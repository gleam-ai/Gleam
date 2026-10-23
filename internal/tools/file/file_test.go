package file

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTools(t *testing.T) (*Tools, string) {
	t.Helper()
	ws := t.TempDir()
	return New(ws), ws
}

func TestFileTools_WriteReadList(t *testing.T) {
	tools, ws := newTools(t)
	ctx := context.Background()

	out, err := tools.requireResolve(ctx, map[string]any{}, "path")
	_ = out
	if err == nil {
		t.Error("空参数应报错")
	}
	wt := &writeTool{tools}
	w, err := wt.Execute(ctx, map[string]any{"path": filepath.Join("sub", "a.txt"), "content": "你好 Gleam"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.(map[string]any)["bytes_written"] != int(12) {
		t.Errorf("bytes_written = %v", w)
	}

	if _, err := wt.Execute(ctx, map[string]any{"path": filepath.Join("sub", "a.txt"), "content": "+", "append": true}); err != nil {
		t.Fatalf("append: %v", err)
	}

	rt := &readTool{tools}
	r, err := rt.Execute(ctx, map[string]any{"path": filepath.Join("sub", "a.txt")})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rm := r.(map[string]any)
	if rm["content"] != "你好 Gleam+" {
		t.Errorf("content = %q", rm["content"])
	}
	if rm["truncated"] != false {
		t.Errorf("truncated = %v", rm["truncated"])
	}

	lt := &listTool{tools}
	l, err := lt.Execute(ctx, map[string]any{"path": "sub"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	lm := l.(map[string]any)
	if lm["count"] != 1 {
		t.Errorf("count = %v", lm["count"])
	}
	_ = ws
}

func TestFileTools_WorkspaceBoundary(t *testing.T) {
	tools, _ := newTools(t)
	ctx := context.Background()
	wt := &writeTool{tools}
	if _, err := wt.Execute(ctx, map[string]any{"path": filepath.Join(os.TempDir(), "outside.txt"), "content": "x"}); err == nil {
		t.Error("越界写入应报错")
	}
	if _, err := wt.Execute(ctx, map[string]any{"path": "../outside.txt", "content": "x"}); err == nil {
		t.Error("目录穿越应报错")
	}
	rt := &readTool{tools}
	if _, err := rt.Execute(ctx, map[string]any{"path": "../etc/passwd"}); err == nil {
		t.Error("越界读取应报错")
	}
}

func TestFileTools_RejectsSymlinkEscape(t *testing.T) {
	tools, ws := newTools(t)
	outside := t.TempDir()
	link := filepath.Join(ws, "link")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		output, junctionErr := exec.Command("cmd.exe", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if junctionErr != nil {
			t.Skipf("junction unavailable: %v: %s", junctionErr, output)
		}
	}
	if _, err := (&readTool{tools}).Execute(context.Background(), map[string]any{"path": filepath.Join("link", "secret.txt")}); err == nil {
		t.Fatal("symlink escape should be rejected")
	}
	for _, path := range []string{"link/payload.txt", "link/not-created/nested/payload.txt"} {
		t.Run(path, func(t *testing.T) {
			if _, err := (&writeTool{tools}).Execute(context.Background(), map[string]any{"path": path, "content": "escaped"}); err == nil {
				t.Error("write through an external symlink should be rejected")
			}
			if _, err := os.Stat(filepath.Join(ws, filepath.FromSlash(path))); !os.IsNotExist(err) {
				t.Errorf("rejected write created an external file: %v", err)
			}
		})
	}
	t.Run("linked missing root", func(t *testing.T) {
		root := filepath.Join(link, "new-root", "nested")
		path := filepath.Join(outside, "new-root", "nested", "payload.txt")
		if _, err := (&writeTool{New(root)}).Execute(context.Background(), map[string]any{"path": path, "content": "allowed"}); err != nil {
			t.Fatalf("root and target must use the same physical boundary: %v", err)
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != "allowed" {
			t.Fatalf("write through an explicitly allowed linked root = %q, %v", b, err)
		}
	})
}

func TestFileTools_WriteMissingParents(t *testing.T) {
	tools, ws := newTools(t)
	path := filepath.Join("new", "nested", "parent", "payload.txt")
	if _, err := (&writeTool{tools}).Execute(context.Background(), map[string]any{"path": path, "content": "inside"}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(ws, path)); err != nil || string(b) != "inside" {
		t.Fatalf("deep workspace write = %q, %v", b, err)
	}
}

func TestFileTools_MkdirMoveDelete(t *testing.T) {
	tools, ws := newTools(t)
	ctx := context.Background()
	mt := &mkdirTool{tools}
	if _, err := mt.Execute(ctx, map[string]any{"path": "d1/d2"}); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if r, _ := mt.Execute(ctx, map[string]any{"path": "d1/d2"}); r.(map[string]any)["created"] != false {
		t.Error("重复 mkdir 应返回 created=false")
	}

	wt := &writeTool{tools}
	if _, err := wt.Execute(ctx, map[string]any{"path": "d1/d2/b.txt", "content": "x"}); err != nil {
		t.Fatal(err)
	}
	mv := &moveTool{tools}
	if _, err := mv.Execute(ctx, map[string]any{"src": "d1/d2/b.txt", "dst": "d1/b-moved.txt"}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "d1", "b-moved.txt")); err != nil {
		t.Error("移动后文件不存在")
	}

	dt := &deleteTool{tools}
	if _, err := dt.Execute(ctx, map[string]any{"path": "d1"}); err == nil {
		t.Error("删目录缺 recursive 应报错")
	}
	if _, err := dt.Execute(ctx, map[string]any{"path": "d1", "recursive": true}); err != nil {
		t.Fatalf("delete recursive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "d1")); !os.IsNotExist(err) {
		t.Error("目录应已删除")
	}
}

func TestFileTools_Search(t *testing.T) {
	tools, _ := newTools(t)
	ctx := context.Background()
	wt := &writeTool{tools}
	for _, name := range []string{"a.pdf", "b.pdf", "c.txt", "d.md"} {
		if _, err := wt.Execute(ctx, map[string]any{"path": name, "content": "x"}); err != nil {
			t.Fatal(err)
		}
	}
	st := &searchTool{tools}
	r, err := st.Execute(ctx, map[string]any{"root": ".", "pattern": "*.pdf"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	rm := r.(map[string]any)
	if rm["count"] != 2 {
		t.Errorf("count = %v, files = %v", rm["count"], rm["files"])
	}
	files := rm["files"].([]string)
	if !strings.HasSuffix(files[0], "a.pdf") {
		t.Errorf("排序错误: %v", files)
	}
}

func TestFileTools_LargeReadTruncates(t *testing.T) {
	tools, ws := newTools(t)
	big := filepath.Join(ws, "big.txt")
	content := strings.Repeat("x", maxReadBytes+1024)
	if err := os.WriteFile(big, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := &readTool{tools}
	r, err := rt.Execute(context.Background(), map[string]any{"path": big})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rm := r.(map[string]any)
	if rm["truncated"] != true {
		t.Error("超限读取应标记 truncated")
	}
	if len(rm["content"].(string)) != maxReadBytes {
		t.Errorf("截断长度 = %d", len(rm["content"].(string)))
	}
}

func TestFileTools_PathAware(t *testing.T) {
	tools, ws := newTools(t)
	paths := tools.Paths(context.Background(), map[string]any{"path": "a.txt", "src": "b.txt"})
	if len(paths) != 2 {
		t.Fatalf("Paths = %v", paths)
	}
	if !strings.HasPrefix(filepath.ToSlash(paths[0]), filepath.ToSlash(ws)) {
		t.Errorf("路径应解析到工作区内: %v", paths[0])
	}
}

func TestFileTools_WriteMissingContentDoesNotTruncate(t *testing.T) {
	tools, ws := newTools(t)
	path := filepath.Join(ws, "keep.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := &writeTool{tools}
	if _, err := wt.Execute(context.Background(), map[string]any{"path": path}); err == nil {
		t.Fatal("缺少 content 应报错")
	}
	if b, _ := os.ReadFile(path); string(b) != "keep" {
		t.Fatalf("缺参调用截断了文件: %q", b)
	}
	if _, err := wt.Execute(context.Background(), map[string]any{"path": path, "content": ""}); err != nil {
		t.Fatalf("显式空字符串应允许: %v", err)
	}
	if b, _ := os.ReadFile(path); len(b) != 0 {
		t.Fatalf("显式空字符串应清空文件，实际 %q", b)
	}
}
