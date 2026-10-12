package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/pkg/types"
)

func TestSafeSeg_BlocksTraversal(t *testing.T) {
	exact := []struct{ in, want string }{
		{"s1", "s1"},
		{"s-1_a.txt", "s-1_a.txt"},
		{"..", "_"},
		{".", "_"},
		{"", "_"},
		{"...a...", "a"},
	}
	for _, c := range exact {
		if got := safeSeg(c.in); got != c.want {
			t.Errorf("safeSeg(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}

	hostile := []string{
		"../../etc/passwd", `..\..\windows\system32`, "a/../b", "a/b/c",
		"..", "../", "./..", "C:evil", `\\server\share`, "  空格 与 中文  ",
		"x\x00y", "%%2e%2e%2f", "．.", "　..", strings.Repeat(".", 300),
	}
	for _, in := range hostile {
		got := safeSeg(in)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("safeSeg(%q) = %q 仍含路径分隔符", in, got)
		}
		if got == "." || got == ".." {
			t.Errorf("safeSeg(%q) = %q 仍是相对路径段", in, got)
		}
		if got == "" {
			t.Errorf("safeSeg(%q) 返回空串——空串拼进路径会指向上级目录", in)
		}
		if strings.ContainsRune(got, 0) {
			t.Errorf("safeSeg(%q) 结果含 NUL", in)
		}
		if again := safeSeg(got); again != got {
			t.Errorf("safeSeg 不幂等：safeSeg(%q)=%q，再净化变成 %q", in, got, again)
		}
	}
}

func TestSafeSeg_Truncates(t *testing.T) {
	got := safeSeg(strings.Repeat("a", 500))
	if len(got) > spillMaxSegLen {
		t.Errorf("应截断到 %d，实际 %d", spillMaxSegLen, len(got))
	}
}

func TestSpillOutput_WritesUnderRoot(t *testing.T) {
	dataDir := t.TempDir()
	content := strings.Repeat("原始内容", 500)

	p, err := SpillOutput(dataDir, "t1", "s1", content)
	if err != nil {
		t.Fatalf("落盘失败：%v", err)
	}
	root := SpillRoot(dataDir)
	if !strings.HasPrefix(filepath.Clean(p), filepath.Clean(root)+string(filepath.Separator)) {
		t.Fatalf("落盘路径应在 %s 之内，实际 %s", root, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读回落盘文件失败：%v", err)
	}
	if string(b) != content {
		t.Errorf("落盘内容应一字不差（长度 %d vs %d）", len([]rune(string(b))), len([]rune(content)))
	}
}

func TestSpillOutput_MaliciousIDStaysInsideRoot(t *testing.T) {
	dataDir := t.TempDir()
	root := SpillRoot(dataDir)
	outside := filepath.Join(filepath.Dir(root), "pwned.txt")

	cases := []struct{ taskID, stepID string }{
		{"../../..", "pwned"},
		{"t1", "../../../pwned"},
		{"..", ".."},
		{"a/../../..", "b"},
	}
	for _, c := range cases {
		p, err := SpillOutput(dataDir, c.taskID, c.stepID, "恶意内容")
		if err != nil {
			continue
		}
		if !strings.HasPrefix(filepath.Clean(p), filepath.Clean(root)+string(filepath.Separator)) {
			t.Errorf("taskID=%q stepID=%q 落到了根目录之外：%s", c.taskID, c.stepID, p)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatalf("越界文件被写出来了：%s", outside)
	}
}

func TestSpillOutput_NoDataDirFails(t *testing.T) {
	if _, err := SpillOutput("", "t1", "s1", "x"); err == nil {
		t.Error("未配置数据目录时应报错，而不是退化到相对路径写文件")
	}
}

func TestFitRunes_SpillsAndPointsAtFile(t *testing.T) {
	dataDir := t.TempDir()
	head := strings.Repeat("甲", 400)
	tail := strings.Repeat("乙", 400)
	src := head + strings.Repeat("丙", 5000) + tail

	e := &Executor{MaxOutputRunes: 1000, DataDir: dataDir}
	got := e.fitRunes(src, "task-1", "step-1")

	if !strings.Contains(got, "已截断") {
		t.Fatal("应说明被截断")
	}
	p := extractSpillPath(got)
	if p == "" {
		t.Fatalf("提示语里应给出完整内容的路径，实际：%s", got)
	}
	if !strings.Contains(got, "临时文件") {
		t.Error("应写明是临时文件（这是缓存不是归档）")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("提示语给的路径读不到：%v", err)
	}
	if string(b) != src {
		t.Error("落盘内容应是**完整原文**，而不是截断后的文本")
	}
	if got := len([]rune(got)); got > 1000 {
		t.Errorf("截断后应不超过预算 1000，实际 %d 字", got)
	}
}

func TestFitRunes_SpillFailureIsReported(t *testing.T) {
	src := strings.Repeat("丙", 5000)
	e := &Executor{MaxOutputRunes: 1000}
	got := e.fitRunes(src, "t1", "s1")

	if !strings.Contains(got, "未能落盘") {
		t.Errorf("落盘失败必须说出来，实际：%s", got[len(got)-200:])
	}
	if extractSpillPath(got) != "" {
		t.Error("落盘失败时不该给出一个取不到的路径")
	}
	if !strings.Contains(got, "file.read") {
		t.Error("落盘失败时应给出退路（缩小范围 / file.read）")
	}
}

func TestFitRunes_SmallBudgetSkipsSpill(t *testing.T) {
	dataDir := t.TempDir()
	e := &Executor{MaxOutputRunes: 100, DataDir: dataDir}
	got := e.fitRunes(strings.Repeat("丙", 5000), "t1", "s1")
	if len([]rune(got)) != 100 {
		t.Errorf("小预算下应精确截断到预算，实际 %d 字", len([]rune(got)))
	}
	if _, err := os.Stat(filepath.Join(SpillRoot(dataDir), safeSeg("t1"))); err == nil {
		t.Error("小预算下不该落盘（预算里放不下路径说明，落了也没人知道）")
	}
}

func TestPruneSpill_KeepsNewest(t *testing.T) {
	dataDir := t.TempDir()
	root := SpillRoot(dataDir)

	for i := 0; i < 5; i++ {
		dir := filepath.Join(root, "task"+string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(time.Duration(i-5) * time.Hour)
		if err := os.Chtimes(dir, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	PruneSpill(dataDir, 2)

	left, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 {
		t.Fatalf("应保留 2 个任务目录，实际 %d：%v", len(left), names(left))
	}
	for _, e := range left {
		if e.Name() == "taska" || e.Name() == "taskb" || e.Name() == "taskc" {
			t.Errorf("最旧的 %s 应被淘汰", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "taske")); err != nil {
		t.Error("最新的 taske 必须留下——它是刚跑完的那个任务")
	}
}

func TestPruneSpill_NoopWhenUnderLimit(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(SpillRoot(dataDir), "t1"), 0o755); err != nil {
		t.Fatal(err)
	}
	PruneSpill(dataDir, 200)
	if _, err := os.Stat(filepath.Join(SpillRoot(dataDir), "t1")); err != nil {
		t.Error("未超上限时不该删任何东西")
	}
	PruneSpill(filepath.Join(dataDir, "never-created"), 10)
	PruneSpill("", 10)
}

func TestPruneSpill_KeepZeroIsNoop(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(SpillRoot(dataDir), "t1"), 0o755); err != nil {
		t.Fatal(err)
	}
	PruneSpill(dataDir, 0)
	if _, err := os.Stat(filepath.Join(SpillRoot(dataDir), "t1")); err != nil {
		t.Error("keep=0 不该删光缓存")
	}
}

func TestExecutor_RefSubstitutionSpillsFullContent(t *testing.T) {
	dataDir := t.TempDir()
	full := strings.Repeat("很长的文件内容", 2000)

	r := registry.New()
	r.MustRegister(&funcTool{name: "bigout", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, _ map[string]any) (any, error) { return full, nil }})
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"text": args["text"]}, nil
		}})

	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, StepTimeout: 2 * time.Second,
		MaxOutputRunes: 1000, DataDir: dataDir,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "bigout"},
		{ID: "s2", Tool: "reply", Args: map[string]any{"text": "{ref:s1}"}, DependsOn: []string{"s1"}},
	}}
	res := e.Execute(context.Background(), plan, "t-spill", string(types.ModeAuto), false)
	if res.Succeeded != 2 {
		t.Fatalf("两步都应成功：%+v", res.Steps)
	}

	text, _ := res.ByID["s2"].FinalArgs["text"].(string)
	p := extractSpillPath(text)
	if p == "" {
		t.Fatalf("注入下游的参数里应带落盘路径，实际：%s", text)
	}
	want := filepath.Join(SpillRoot(dataDir), "t-spill", "s1.txt")
	if filepath.Clean(p) != filepath.Clean(want) {
		t.Errorf("落盘路径应为 %s，实际 %s", want, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("落盘文件读不到：%v", err)
	}
	if string(b) != full {
		t.Error("落盘的应是步骤的完整原始输出")
	}
}

func TestExecutor_SpillNameFollowsContentSource(t *testing.T) {
	dataDir := t.TempDir()
	bodyA := strings.Repeat("甲甲", 4000)
	bodyB := strings.Repeat("乙乙", 4000)

	r := registry.New()
	r.MustRegister(&funcTool{name: "outA", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, _ map[string]any) (any, error) { return bodyA, nil }})
	r.MustRegister(&funcTool{name: "outB", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, _ map[string]any) (any, error) { return bodyB, nil }})
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"a": args["a"], "b": args["b"]}, nil
		}})

	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, StepTimeout: 2 * time.Second,
		MaxOutputRunes: 800, DataDir: dataDir,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "outA"},
		{ID: "s2", Tool: "outB"},
		{ID: "s3", Tool: "reply",
			Args:      map[string]any{"a": "{ref:s1}", "b": "{ref:s2}"},
			DependsOn: []string{"s1", "s2"}},
	}}
	res := e.Execute(context.Background(), plan, "t-two", string(types.ModeAuto), false)
	if res.Succeeded != 3 {
		t.Fatalf("三步都应成功：%+v", res.Steps)
	}

	args := res.ByID["s3"].FinalArgs
	pa := extractSpillPath(args["a"].(string))
	pb := extractSpillPath(args["b"].(string))
	if pa == "" || pb == "" {
		t.Fatalf("两个引用都应带落盘路径：a=%q b=%q", pa, pb)
	}
	if filepath.Clean(pa) == filepath.Clean(pb) {
		t.Fatalf("两个来源不能落进同一个文件（会互相覆盖）：%s", pa)
	}
	ba, err := os.ReadFile(pa)
	if err != nil {
		t.Fatalf("读 a 的落盘文件失败：%v", err)
	}
	bb, err := os.ReadFile(pb)
	if err != nil {
		t.Fatalf("读 b 的落盘文件失败：%v", err)
	}
	if string(ba) != bodyA {
		t.Error("s1 的落盘文件里应是 s1 的完整输出")
	}
	if string(bb) != bodyB {
		t.Error("s2 的落盘文件里应是 s2 的完整输出")
	}
}

func TestExecutor_SpillCappedEvenWithLongPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), strings.Repeat("deepdir/", 12))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := registry.New()
	r.MustRegister(&funcTool{name: "bigout", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, _ map[string]any) (any, error) {
			return strings.Repeat("很长的文件内容", 2000), nil
		}})
	r.MustRegister(&funcTool{name: "reply", perm: types.PermissionReadOnly,
		fn: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"text": args["text"]}, nil
		}})

	const budget = 500
	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, nil, time.Second),
		Notifier: NopNotifier{}, StepTimeout: 2 * time.Second,
		MaxOutputRunes: budget, DataDir: dataDir,
	}
	plan := types.Plan{Steps: []types.Step{
		{ID: "s1", Tool: "bigout"},
		{ID: "s2", Tool: "reply", Args: map[string]any{"text": "{ref:s1}"}, DependsOn: []string{"s1"}},
	}}
	res := e.Execute(context.Background(), plan, "t-longpath", string(types.ModeAuto), false)
	text, _ := res.ByID["s2"].FinalArgs["text"].(string)
	if got := len([]rune(text)); got > budget {
		t.Errorf("结果应不超过预算 %d，实际 %d 字（提示语不该把预算撑破）", budget, got)
	}
}

func extractSpillPath(s string) string {
	const marker = "完整内容见 "
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i+len(marker):]
	if j := strings.Index(rest, "（"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
