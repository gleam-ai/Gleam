package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "growth.json")

	if err := Write(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte(`{"b":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"b":2}` {
		t.Errorf("内容 = %q", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("临时文件残留: %s", e.Name())
		}
	}
}

func TestWriteFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "missing.json")
	if err := Write(path, []byte("x"), 0o644); err == nil {
		t.Fatal("写入不存在的目录应报错")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("失败后不该建出目标文件: %v", err)
	}

	good := filepath.Join(dir, "good.json")
	if err := Write(good, []byte("完好"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(good, "非法子路径"), []byte("y"), 0o644); err == nil {
		t.Log("该平台上此路径未报错，跳过残留检查")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("失败路径留下了临时文件: %s", e.Name())
		}
	}
	data, err := os.ReadFile(good)
	if err != nil || string(data) != "完好" {
		t.Errorf("原文件被动了: %q %v", data, err)
	}
}

func TestWritePerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的权限位语义不同")
	}
	path := filepath.Join(t.TempDir(), "secret.json")
	if err := Write(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v，want 0600", st.Mode().Perm())
	}
}
