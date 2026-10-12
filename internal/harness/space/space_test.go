package space

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	dataDir := t.TempDir()
	s, err := Open(dataDir, dataDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, dataDir
}

func TestSpaceIDTraversalCannotEscapeStore(t *testing.T) {
	s, dataDir := openStore(t)
	mark := filepath.Join(dataDir, "mark.json")
	if err := os.WriteFile(mark, []byte(`{"id":"default","name":"外部文件"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stateJSON := s.statePath()

	for _, bad := range []string{
		".." + string(filepath.Separator) + "mark",
		"../mark",
		"..\\..\\etc\\passwd",
		"state",
		"state.json",
		"default ",
		"sp_zzzzzzzz",
	} {
		if err := s.Delete(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Delete(%q) = %v，应为 ErrInvalidID", bad, err)
		}
		if _, err := s.Get(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Get(%q) = %v，应为 ErrInvalidID", bad, err)
		}
		if _, err := s.Rename(bad, "x"); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Rename(%q) = %v，应为 ErrInvalidID", bad, err)
		}
		if _, err := s.SetPath(bad, ""); !errors.Is(err, ErrInvalidID) {
			t.Errorf("SetPath(%q) = %v，应为 ErrInvalidID", bad, err)
		}
		if err := s.SetActive(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("SetActive(%q) = %v，应为 ErrInvalidID", bad, err)
		}
	}
	if _, err := os.Stat(mark); err != nil {
		t.Errorf("目录外文件被删除/移动了：%v", err)
	}
	if _, err := os.Stat(stateJSON); err != nil {
		t.Errorf("保留文件 state.json 被删除了：%v", err)
	}
}

func TestSpaceNotFoundVsInvalid(t *testing.T) {
	s, _ := openStore(t)
	if _, err := s.Get("sp_000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(不存在) = %v，应为 ErrNotFound", err)
	}
	if err := s.SetActive("sp_000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetActive(不存在) = %v，应为 ErrNotFound", err)
	}
}

func TestSpaceNormalRoundTrip(t *testing.T) {
	s, dataDir := openStore(t)
	sp, err := s.Create("测试空间", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sp.ID, "sp_") {
		t.Fatalf("新空间 id 形态异常: %q", sp.ID)
	}
	got, err := s.Get(sp.ID)
	if err != nil || got.Name != "测试空间" {
		t.Fatalf("Get = %v, %v", got, err)
	}
	entries, _ := os.ReadDir(filepath.Join(dataDir, "spaces"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}
