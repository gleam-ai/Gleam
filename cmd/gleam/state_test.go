package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gleam/internal/agent"
	"gleam/internal/config"
	"gleam/internal/harness/growth"
	"gleam/pkg/types"
)

func TestStateCategories_FourArticleCategoriesInOrder(t *testing.T) {
	want := []string{"配置", "会话", "记忆", "任务", "资产"}
	if len(stateCategories) != len(want) {
		t.Fatalf("类别数 = %d，期望 %d（%v）", len(stateCategories), len(want), want)
	}
	for i, w := range want {
		if stateCategories[i].Name != w {
			t.Errorf("第 %d 类 = %q，期望 %q", i+1, stateCategories[i].Name, w)
		}
	}
	for _, c := range stateCategories {
		if strings.TrimSpace(c.Meaning) == "" || strings.TrimSpace(c.Who) == "" {
			t.Errorf("类别 %q 的语义或「谁能改」为空——这一段的价值就在这两列", c.Name)
		}
	}
}

func TestStateEntries_Shape(t *testing.T) {
	known := map[string]bool{}
	for _, c := range stateCategories {
		known[c.Name] = true
	}
	perCategory := map[string]int{}
	seen := map[string]bool{}
	for _, e := range stateEntries {
		if !known[e.Category] {
			t.Errorf("%q 的类别 %q 不在 stateCategories 里", e.Path, e.Category)
		}
		perCategory[e.Category]++
		if e.Path == "" || strings.TrimSpace(e.Role) == "" {
			t.Errorf("条目 %+v 缺路径或说明", e)
			continue
		}
		if strings.Contains(e.Path, "\\") {
			t.Errorf("%q 用了反斜杠：表里统一用正斜杠，拼路径时过 filepath.FromSlash", e.Path)
		}
		if strings.HasPrefix(e.Path, "/") || filepath.IsAbs(e.Path) {
			t.Errorf("%q 是绝对路径：表里的路径一律相对数据目录", e.Path)
		}
		if strings.Contains(e.Path, "..") {
			t.Errorf("%q 含 ..：数据目录之外的条目要标 Outside，而不是写相对逃逸路径", e.Path)
		}
		if seen[e.Path] {
			t.Errorf("%q 在表里出现两次", e.Path)
		}
		seen[e.Path] = true
	}
	for _, c := range stateCategories {
		if perCategory[c.Name] == 0 {
			t.Errorf("类别 %q 一条落盘位置都没有——那就不是一类状态", c.Name)
		}
	}
}

func TestStateReport_RendersCategoriesAndEntries(t *testing.T) {
	out := stateReport(t.TempDir())
	for _, want := range []string{
		"配置", "会话", "记忆", "任务", "资产",
		"只报位置与体量", "不是不合格", "当前目录",
		"settings.yaml", "tasks", "runs", "memory/longterm.json", "tool-output",
		"谁能改",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q，实际：\n%s", want, out)
		}
	}
}

func TestDescribeStatePath_MissingIsQuiet(t *testing.T) {
	dir := t.TempDir()
	e := stateEntry{Category: "记忆", Path: "memory/longterm.json", Role: "长期记忆"}
	if got := describeStatePath(dir, e); got != "[无]" {
		t.Errorf("不存在的路径应报 [无]，实际 %q", got)
	}
}

func TestDescribeStatePath_CountsRecursively(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "tool-output", "t1")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"s1.txt", "s2.txt"} {
		if err := os.WriteFile(filepath.Join(deep, n), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := describeStatePath(dir, stateEntry{Path: "tool-output", Role: "缓存"})
	if !strings.Contains(got, "2 个文件") {
		t.Errorf("应递归数到 2 个文件，实际 %q", got)
	}
	if !strings.Contains(got, "10 B") {
		t.Errorf("体量应为 10 B，实际 %q", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024, "5.0 MB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q，期望 %q", c.n, got, c.want)
		}
	}
}

func TestStateEntries_CoverWhatRuntimeCreates(t *testing.T) {
	dataDir := t.TempDir()
	ws := t.TempDir()
	script := filepath.Join(t.TempDir(), "script.json")
	body, err := json.Marshal([]map[string]any{
		{"kind": "plan", "texts": []string{
			`{"steps":[
				{"id":"s1","description":"写对账文件","tool":"file.write","args":{"path":"state-probe.txt","content":"对账用"}},
				{"id":"s2","description":"回复","tool":"reply","args":{"text":"好"},"depends_on":["s1"]}
			],"estimated_time":"short"}`,
		}},
		{"kind": "reflect", "texts": []string{
			`{"score":90,"verdict":"done","reason":"完成","suggestion":""}`,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, body, 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := buildRuntime("", ws, dataDir, true, script, agent.NopNotifier{})
	if err != nil {
		t.Fatalf("装配 runtime 失败: %v", err)
	}
	defer rt.cleanup()

	rt.agent.RunGoal(context.Background(), types.GoalRequest{Goal: "打个招呼", Mode: "auto"})

	rt.growth.Record(growth.Entry{Type: "task_completed", Goal: "对账用"})
	if rt.sched != nil {
		if _, err := rt.sched.AddJob("state-probe", "", 3600, "对账用", "auto"); err != nil {
			t.Logf("加定时任务失败（不影响对账）: %v", err)
		}
	}
	agent.NewRunLog(dataDir).RecordStep("t-probe", types.StepResult{StepID: "s1"})
	if _, err := agent.SpillOutput(dataDir, "t-probe", "s1", "落盘缓存对账用"); err != nil {
		t.Logf("落盘缓存失败（不影响对账）: %v", err)
	}
	if err := rt.cfg.SaveOverlay(filepath.Join(dataDir, config.OverlayFile)); err != nil {
		t.Logf("写覆盖层失败（不影响对账）: %v", err)
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	for _, must := range []string{"memory", "skills", "tasks", "conversations", "spaces"} {
		if !got[must] {
			t.Fatalf("装配后数据目录里应有 %q，实际只有 %v——这条测试的前提不成立", must, keysOf(got))
		}
	}

	covered := map[string]bool{}
	for _, e := range stateEntries {
		first := strings.SplitN(e.Path, "/", 2)[0]
		covered[first] = true
	}
	for name := range got {
		if !covered[name] {
			t.Errorf("数据目录里出现了 %q，但 stateEntries 里没有它——新加的落盘位置要补进表里（它是路径的唯一 owner）", name)
		}
	}
}

func TestStateEntries_KnownMembersPresent(t *testing.T) {
	have := map[string]bool{}
	for _, e := range stateEntries {
		have[e.Path] = true
	}
	for _, want := range []string{
		"settings.yaml", "credentials.json",
		"conversations", "spaces", "memory/context.json",
		"memory/longterm.json", "growth.json",
		"tasks", "runs", "replays", "schedules.json", "pending_approvals.json",
		"audit.jsonl", "tool-output", "snapshots", "geo_history.json",
		"skills", "snippets.json",
	} {
		if !have[want] {
			t.Errorf("表里缺 %q——要么删错了行，要么真删了这处落盘（那就该同时改代码）", want)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
