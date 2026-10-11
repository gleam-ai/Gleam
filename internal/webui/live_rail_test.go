package webui

import (
	"testing"

	"gleam/internal/buildinfo"
)

func TestInfo_CarriesMemoryCount(t *testing.T) {
	f := newFixture(t, nil)

	before := infoMemoryCount(t, f)

	if out := f.call("POST", "/api/memory", map[string]any{
		"content": "用户偏好：现场栏的数字必须来自单一 owner",
		"tags":    []string{"preference"},
	}); out == nil {
		t.Fatal("存记忆失败")
	}
	after := infoMemoryCount(t, f)

	if after != before+1 {
		t.Errorf("存一条记忆后 /api/info 的 memory 应 +1：%d → %d（改门面不改这里就是这条线断的样子）", before, after)
	}
}

func TestInfo_VersionIsBuildinfo(t *testing.T) {
	f := newFixture(t, nil)
	info := f.call("GET", "/api/info", nil)
	got, _ := info["version"].(string)
	if got != buildinfo.Version {
		t.Errorf("/api/info 的 version = %q，应为 owner 的 %q", got, buildinfo.Version)
	}
}

func infoMemoryCount(t *testing.T, f *fixture) int64 {
	t.Helper()
	info := f.call("GET", "/api/info", nil)
	raw, ok := info["memory"]
	if !ok {
		t.Fatal("/api/info 没有 memory 字段")
	}
	n, ok := raw.(float64)
	if !ok {
		t.Fatalf("memory 应为数字，实际 %T", raw)
	}
	return int64(n)
}
