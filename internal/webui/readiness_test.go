package webui

import (
	"reflect"
	"testing"
)

func TestReadinessRoute_ReturnsNineItems(t *testing.T) {
	f, _ := newGEOFixture(t)
	got := f.call("GET", "/api/readiness", nil)

	if s, _ := got["summary"].(string); s == "" {
		t.Fatal("应返回一行摘要")
	}
	rep, ok := got["report"].(map[string]any)
	if !ok {
		t.Fatalf("应返回 report 对象，实际 %v", got)
	}
	items, ok := rep["items"].([]any)
	if !ok || len(items) != 9 {
		t.Fatalf("应有 9 项，实际 %v", rep["items"])
	}
	if rep["total"].(float64) != 9 {
		t.Errorf("total 应为 9，实际 %v", rep["total"])
	}
	for i, raw := range items {
		it := raw.(map[string]any)
		if int(it["index"].(float64)) != i+1 {
			t.Errorf("第 %d 项编号不对：%v", i+1, it["index"])
		}
		for _, field := range []string{"key", "title", "status", "summary"} {
			if s, _ := it[field].(string); s == "" {
				t.Errorf("第 %d 项缺少 %s", i+1, field)
			}
		}
		if _, ok := it["evidence"].([]any); !ok {
			t.Errorf("第 %d 项的 evidence 应是数组", i+1)
		}
	}
}

func TestReadinessRoute_ReflectsRealState(t *testing.T) {
	f, _ := newGEOFixture(t)
	rep := f.call("GET", "/api/readiness", nil)["report"].(map[string]any)

	status := map[string]string{}
	for _, raw := range rep["items"].([]any) {
		it := raw.(map[string]any)
		status[it["key"].(string)] = it["status"].(string)
	}

	if status["delivery"] != "pass" {
		t.Errorf("注册了写工具，delivery 应 pass，实际 %s", status["delivery"])
	}
	if status["business_gap"] != "pass" {
		t.Errorf("工作区已设置，business_gap 应 pass，实际 %s", status["business_gap"])
	}
	if status["model_lock"] != "warn" {
		t.Errorf("未配模型档位应 warn，实际 %s", status["model_lock"])
	}
	if status["iteration_arch"] != "fail" {
		t.Errorf("无成长日志应 fail，实际 %s", status["iteration_arch"])
	}
	if rep["verdict"] != "not_ready" {
		t.Errorf("有 fail 项时应判定 not_ready，实际 %v", rep["verdict"])
	}
}

func TestReadinessRoute_IsReadOnly(t *testing.T) {
	f, _ := newGEOFixture(t)
	before := f.call("GET", "/api/settings", nil)

	first := f.call("GET", "/api/readiness", nil)["report"].(map[string]any)
	second := f.call("GET", "/api/readiness", nil)["report"].(map[string]any)

	after := f.call("GET", "/api/settings", nil)
	if !reflect.DeepEqual(before, after) {
		t.Error("体检修改了设置")
	}
	if first["verdict"] != second["verdict"] ||
		first["passed"].(float64) != second["passed"].(float64) {
		t.Errorf("两次体检结果不一致：%v / %v", first["verdict"], second["verdict"])
	}
}
