package webui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fakeGoTree(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if version != "" {
		if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n2026-01-01\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDetectGoCustomPathReadsVersionFile(t *testing.T) {
	root := fakeGoTree(t, "go1.26.0")
	st := DetectGo(root)
	if !st.Found {
		t.Fatal("应探测到假 Go 树")
	}
	if st.Version != "go1.26.0" {
		t.Errorf("Version = %q, 应读 VERSION 文件首行", st.Version)
	}
	if st.Source != "user-specified" {
		t.Errorf("Source = %q", st.Source)
	}
}

func TestDetectGoCustomPathWithoutVersionFile(t *testing.T) {
	root := fakeGoTree(t, "")
	st := DetectGo(root)
	if !st.Found {
		t.Fatal("无 VERSION 文件也应判定存在")
	}
	if st.Version != "" {
		t.Errorf("Version = %q, 应为空而不是执行二进制去猜", st.Version)
	}
}

func TestDetectGoRejectsRelativeAndOddPaths(t *testing.T) {
	for _, p := range []string{"relative/path", "C:bin", "/tmp/$(evil)", "/tmp/a;b"} {
		if st := DetectGo(p); st.Found {
			t.Errorf("路径 %q 不应被接受", p)
		}
	}
}

func TestDetectGoMissingBinary(t *testing.T) {
	root := t.TempDir()
	if st := DetectGo(root); st.Found {
		t.Error("空目录不应判定为 Go")
	}
}
