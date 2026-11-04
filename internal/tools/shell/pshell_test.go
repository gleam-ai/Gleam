package shell

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPShell_PreservesCWDAcrossCalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pwd sentinel; Windows uses different echo/pwd semantics")
	}
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	shells := NewPShells()
	ctx := context.Background()

	// First call: cd into the subdirectory; pwd should reflect it.
	out1, err := shells.Exec(ctx, "task-A", "cd "+sub, 5*time.Second, nil)
	if err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if cwd, _ := out1["persistent_cwd"].(string); cwd != sub {
		t.Errorf("after cd: persistent_cwd = %q, want %q", cwd, sub)
	}

	// Second call: run pwd without any cd; it should still sit in the previous cwd.
	out2, err := shells.Exec(ctx, "task-A", "pwd", 5*time.Second, nil)
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	stdout, _ := out2["stdout"].(string)
	if !strings.Contains(stdout, sub) {
		t.Errorf("second pwd should be in subdir, got %q", stdout)
	}
}

func TestPShell_OwnerIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-specific test")
	}
	shells := NewPShells()
	ctx := context.Background()
	tmp := t.TempDir()

	if _, err := shells.Exec(ctx, "task-A", "cd "+tmp, 5*time.Second, nil); err != nil {
		t.Fatal(err)
	}
	// task-B must not see task-A's cwd.
	outB, err := shells.Exec(ctx, "task-B", "pwd", 5*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	cwdB, _ := outB["persistent_cwd"].(string)
	if cwdB == tmp {
		t.Errorf("task-B inherited task-A cwd: %q", cwdB)
	}
}

func TestPersistentShellTool_RequiresCommand(t *testing.T) {
	tool := NewPersistentShell(NewPShells(), 5*time.Second)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("missing command should error")
	}
}

func TestPersistentShellTool_NilShellsFails(t *testing.T) {
	tool := NewPersistentShell(nil, 5*time.Second)
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo hi"}); err == nil {
		t.Fatal("nil shells should error")
	}
}
