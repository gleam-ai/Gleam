package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gleam/internal/config"
	"gleam/internal/harness/registry"
	"gleam/internal/harness/safety"
	"gleam/internal/llm"
	"gleam/internal/tools/file"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

func gitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("环境里没有 git")
	}
	steps := [][]string{{"init"}, {"config", "user.email", "t@example.invalid"}, {"config", "user.name", "Gleam Test"}}
	for _, a := range steps {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-m", "seed"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
}

func noteTo(n *recordingNotifier) func(phase, msg string, pct int, kind string) {
	return func(phase, msg string, pct int, kind string) {
		n.OnProgress(types.ProgressEvent{Phase: phase, Message: msg, Progress: pct, Kind: kind})
	}
}

func progressText(n *recordingNotifier) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var b strings.Builder
	for _, ev := range n.progress {
		b.WriteString(ev.Kind + ":" + ev.Message + "\n")
	}
	return b.String()
}

func TestIsolateForTaskCreatesWorktree(t *testing.T) {
	f := newFixture(t, nil)
	gitRepo(t, f.ws)
	f.a.Cfg.Worktrees.Enabled = true

	iso := f.a.isolateForTask(context.Background(), types.GoalRequest{}, "task1", func(string, string, int, string) {})
	if len(iso.roots) != 1 {
		t.Fatalf("应当拿到一个副本根，实际 %+v", iso)
	}
	want := filepath.Join(f.a.Cfg.DataDir, "worktrees", "task1")
	if filepath.Clean(iso.roots[0]) != filepath.Clean(want) {
		t.Errorf("副本根 = %q，应为 %q", iso.roots[0], want)
	}
	if filepath.Clean(iso.cwd) != filepath.Clean(want) {
		t.Errorf("规划 cwd = %q，应与副本根一致——否则会照着主工作区规划、往副本里写", iso.cwd)
	}
	if _, err := os.Stat(filepath.Join(iso.roots[0], "seed.txt")); err != nil {
		t.Errorf("副本里应当有仓库已提交的文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.ws, "worktrees")); !os.IsNotExist(err) {
		t.Errorf("副本不该建在工作区里面")
	}
}

func TestIsolateForTaskReusesExisting(t *testing.T) {
	f := newFixture(t, nil)
	gitRepo(t, f.ws)
	f.a.Cfg.Worktrees.Enabled = true

	first := f.a.isolateForTask(context.Background(), types.GoalRequest{}, "task2", func(string, string, int, string) {})
	second := f.a.isolateForTask(context.Background(), types.GoalRequest{}, "task2", func(string, string, int, string) {})
	if len(second.roots) != 1 || filepath.Clean(second.roots[0]) != filepath.Clean(first.roots[0]) {
		t.Errorf("第二次应当复用同一份副本：first=%v second=%v", first.roots, second.roots)
	}
	if got := len(f.a.WorktreesView()["rows"].([]map[string]any)); got != 1 {
		t.Errorf("记录应当只有一条，实际 %d", got)
	}
}

func TestIsolateForTaskDegradesOutsideRepo(t *testing.T) {
	f := newFixture(t, nil)
	f.a.Cfg.Worktrees.Enabled = true
	f.ws = f.a.Cfg.Workspace

	iso := f.a.isolateForTask(context.Background(), types.GoalRequest{}, "task3", noteTo(f.notify))
	if len(iso.roots) != 0 {
		t.Errorf("建不了副本时不应给出边界（会变成空边界=拒绝一切），实际 %v", iso.roots)
	}
	if filepath.Clean(iso.cwd) != filepath.Clean(f.a.Cfg.Workspace) {
		t.Errorf("降级后 cwd 应当是工作区，实际 %q", iso.cwd)
	}
	if !strings.Contains(progressText(f.notify), "worktree") {
		t.Errorf("降级必须说出来，实际进度：\n%s", progressText(f.notify))
	}
	if _, err := os.Stat(filepath.Join(f.a.Cfg.DataDir, "worktrees")); !os.IsNotExist(err) {
		t.Errorf("失败时不该留下副本目录")
	}
}

func TestIsolateDisabledCreatesNothing(t *testing.T) {
	f := newFixture(t, nil)
	gitRepo(t, f.ws)

	iso := f.a.isolateForTask(context.Background(), types.GoalRequest{}, "task4", noteTo(f.notify))
	if len(iso.roots) != 0 {
		t.Errorf("开关关着就不该隔离，实际 %v", iso.roots)
	}
	if _, err := os.Stat(filepath.Join(f.a.Cfg.DataDir, "worktrees")); !os.IsNotExist(err) {
		t.Errorf("开关关着时不建任何东西")
	}
	if got := progressText(f.notify); got != "" {
		t.Errorf("没什么可交代的时候不该报进度，实际：\n%s", got)
	}
}

func TestIsolateSkippedWhenCallerPinsCwd(t *testing.T) {
	f := newFixture(t, nil)
	gitRepo(t, f.ws)
	f.a.Cfg.Worktrees.Enabled = true
	pinned := t.TempDir()

	iso := f.a.isolateForTask(context.Background(),
		types.GoalRequest{Context: map[string]any{"cwd": pinned}}, "task5", func(string, string, int, string) {})
	if len(iso.roots) != 0 || filepath.Clean(iso.cwd) != filepath.Clean(pinned) {
		t.Errorf("点名了 cwd 就不该建副本，实际 %+v", iso)
	}
}

func TestExecutorWritesInsideTaskRoots(t *testing.T) {
	ws, dataDir := t.TempDir(), t.TempDir()
	gitRepo(t, ws)
	wt := filepath.Join(dataDir, "worktrees", "task6")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}

	r := registry.New()
	file.New(ws).RegisterAll(r)
	e := &Executor{
		Reg: r, Gate: safety.New("auto", nil, nil, []string{ws}, 2*time.Second),
		Notifier: NopNotifier{}, StepTimeout: 5 * time.Second, DataDir: dataDir,
	}
	ctx := toolutil.WithRoots(context.Background(), []string{wt})
	plan := types.Plan{Steps: []types.Step{{
		ID: "s1", Tool: "file.write",
		Args: map[string]any{"path": "out.txt", "content": "写在副本里\n"},
	}}}
	exec := e.Execute(ctx, plan, "task6", string(types.ModeAuto), false)
	if exec.Steps[0].Status != types.StepSucceeded {
		t.Fatalf("步骤应当成功（副本根算信任边界，不该卡审批），实际 %+v", exec.Steps[0])
	}
	if _, err := os.Stat(filepath.Join(wt, "out.txt")); err != nil {
		t.Errorf("文件应当落在副本里: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "out.txt")); !os.IsNotExist(err) {
		t.Errorf("主工作区里不该出现这个文件")
	}
	recs, err := taskPreImages(dataDir, "task6")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, rec := range recs {
		if filepath.Clean(rec.Path) == filepath.Clean(filepath.Join(wt, "out.txt")) {
			found = true
		}
	}
	if !found {
		t.Errorf("写前快照应当记副本里的绝对路径，实际 %+v", recs)
	}
}

type queuedToolNotifier struct {
	NopNotifier
	queued chan struct{}
}

func (n *queuedToolNotifier) OnTool(ev types.ToolEvent) {
	if ev.Phase == types.ToolStart {
		n.queued <- struct{}{}
	}
}

func TestWorkspaceSwitchMustNotRetargetApprovedStep(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "gate roots", true: "context roots"}[pinned], func(t *testing.T) {
			first, second, data := t.TempDir(), t.TempDir(), t.TempDir()
			reg := registry.New()
			ft := file.New(first)
			ft.RegisterAll(reg)
			gate := safety.New("auto", nil, nil, []string{first}, time.Second)
			cfg := config.Default()
			cfg.Workspace, cfg.DataDir = first, data
			a := &Agent{Cfg: cfg, FileTools: ft, Gate: gate}
			n := &queuedToolNotifier{queued: make(chan struct{}, 1)}
			e := &Executor{Reg: reg, Gate: gate, Notifier: n, DataDir: data}
			state := &execState{snap: newSnapshotter(data, "switch"), grants: &pathGrants{}}
			sem := make(chan struct{}, 1)
			sem <- struct{}{}
			ctx := context.Background()
			wantRoot := first
			if pinned {
				wantRoot = t.TempDir()
				ctx = toolutil.WithRoots(ctx, []string{wantRoot})
			}
			done := make(chan types.StepResult, 1)
			go func() {
				done <- e.runStepGuarded(ctx, state, types.Step{ID: "s1", Tool: "file.write", Args: map[string]any{"path": "target.txt", "content": "original workspace"}}, "switch", "auto", false, time.Second, sem)
			}()
			select {
			case <-n.queued:
			case <-time.After(5 * time.Second):
				<-sem
				t.Fatal("step did not reach dispatch")
			}
			switchErr := a.applyWorkspace(second)
			<-sem
			r := <-done
			if switchErr != nil {
				t.Fatal(switchErr)
			}
			if r.Status != types.StepSucceeded {
				t.Fatalf("step failed: %+v", r)
			}
			if b, err := os.ReadFile(filepath.Join(wantRoot, "target.txt")); err != nil || string(b) != "original workspace" {
				t.Errorf("original workspace write = %q, %v", b, err)
			}
			if _, err := os.Stat(filepath.Join(second, "target.txt")); !os.IsNotExist(err) {
				t.Errorf("queued step was retargeted to the new workspace: %v", err)
			}
			recs := state.snap.records()
			if len(recs) != 1 || recs[0].Path != filepath.Join(wantRoot, "target.txt") {
				t.Errorf("snapshot and execution roots diverged: %+v", recs)
			}
		})
	}
}

func TestRunGoalWorkspaceRootsFrozenBeforePlanning(t *testing.T) {
	f := newFixture(t, nil)
	second := t.TempDir()
	f.a.FileTools = file.New(f.ws)
	f.a.Reg = newTestRegistry()
	f.a.FileTools.RegisterAll(f.a.Reg)
	switched := false
	f.llm.SetHandler(func(req llm.ChatRequest) string {
		if !switched {
			switched = true
			if err := f.a.applyWorkspace(second); err != nil {
				t.Error(err)
			}
			return `{"steps":[{"id":"s1","tool":"file.write","args":{"path":"target.txt","content":"original workspace"}}]}`
		}
		return `{"score":95,"verdict":"done","reason":"written"}`
	})
	res := f.a.RunGoal(context.Background(), types.GoalRequest{Goal: "write target.txt", Mode: "auto"})
	if !switched || len(res.Steps) != 1 || res.Steps[0].Status != types.StepSucceeded {
		t.Fatalf("workspace switch test did not execute the write: %+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(f.ws, "target.txt")); err != nil || string(b) != "original workspace" {
		t.Errorf("task lost its original roots: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(second, "target.txt")); !os.IsNotExist(err) {
		t.Errorf("task wrote in the new workspace: %v", err)
	}
}

func TestDirectToolCallWorkspaceRootsFrozenBeforeApproval(t *testing.T) {
	first, second, data := t.TempDir(), t.TempDir(), t.TempDir()
	reg := registry.New()
	ft := file.New(first)
	ft.RegisterAll(reg)
	cfg := config.Default()
	cfg.Workspace, cfg.DataDir = first, data
	a := &Agent{Cfg: cfg, FileTools: ft, Reg: reg, Gate: safety.New("interactive", nil, nil, []string{first}, time.Second)}
	approved := false
	_, err := a.DirectToolCall(context.Background(), "file.write", map[string]any{"path": "target.txt", "content": "original workspace"}, func(types.ApprovalRequest) types.ApprovalResponse {
		approved = true
		if err := a.applyWorkspace(second); err != nil {
			t.Error(err)
		}
		return types.ApprovalResponse{Approved: true}
	})
	if err != nil || !approved {
		t.Fatalf("direct call: approved=%v, error=%v", approved, err)
	}
	if b, err := os.ReadFile(filepath.Join(first, "target.txt")); err != nil || string(b) != "original workspace" {
		t.Errorf("direct call lost its original roots: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(second, "target.txt")); !os.IsNotExist(err) {
		t.Errorf("direct call wrote in the new workspace: %v", err)
	}
}

func TestSettleWorktreeRemovesCleanKeepsDirty(t *testing.T) {
	f := newFixture(t, nil)
	gitRepo(t, f.ws)
	f.a.Cfg.Worktrees.Enabled = true
	f.a.Cfg.Worktrees.AutoDelete = true
	ctx := context.Background()

	f.a.isolateForTask(ctx, types.GoalRequest{}, "clean1", noteTo(f.notify))
	f.a.settleWorktree("clean1")
	if _, err := os.Stat(filepath.Join(f.a.Cfg.DataDir, "worktrees", "clean1")); !os.IsNotExist(err) {
		t.Errorf("干净的副本应当被自动删掉，stat err = %v", err)
	}

	iso := f.a.isolateForTask(ctx, types.GoalRequest{}, "dirty1", noteTo(f.notify))
	if err := os.WriteFile(filepath.Join(iso.roots[0], "wip.txt"), []byte("还没提交\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.a.settleWorktree("dirty1")
	if _, err := os.Stat(iso.roots[0]); err != nil {
		t.Errorf("有未提交改动的副本不能自动删: %v", err)
	}
	if !strings.Contains(progressText(f.notify), "未提交") {
		t.Errorf("留下它的原因必须说出来，实际进度：\n%s", progressText(f.notify))
	}
}

func TestSettleWorktreePrunesToLimit(t *testing.T) {
	f := newFixture(t, nil)
	gitRepo(t, f.ws)
	f.a.Cfg.Worktrees.Enabled = true
	f.a.Cfg.Worktrees.AutoDelete = false
	f.a.Cfg.Worktrees.MaxCount = 1
	ctx := context.Background()

	old := f.a.isolateForTask(ctx, types.GoalRequest{}, "old1", noteTo(f.notify))
	if err := os.WriteFile(filepath.Join(old.roots[0], "wip.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	f.a.isolateForTask(ctx, types.GoalRequest{}, "new1", noteTo(f.notify))
	f.a.settleWorktree("new1")

	if len(f.a.WorktreesView()["rows"].([]map[string]any)) != 2 {
		t.Errorf("最旧的那份是脏的，应当两份都留着")
	}
	if !strings.Contains(progressText(f.notify), "未提交") {
		t.Errorf("数量还超着的原因必须说出来：\n%s", progressText(f.notify))
	}
}
