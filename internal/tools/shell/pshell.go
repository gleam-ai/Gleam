package shell

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// PSHELL is a persistent shell isolated per owner.
//
// Design notes:
//   - Each call = one more command line appended in the same shell process; the previous cd/env persists.
//   - owner defaults to the task id (injected by the caller); different tasks never interfere.
//   - Every command gets a sentinel echo appended automatically, used to slice stdout down to this call.
//   - No stdin interaction is exposed; interactive commands (vim/top) would hang, with the timeout as the backstop.
//
// Implementation strategy: start a long-lived process with `sh -i` or `cmd /K`, write commands via stdin,
// and mark the end of each call with a unique sentinel. Windows cmd does not support -i but does support /K.
type PShell struct {
	mu      sync.Mutex
	owner   string
	cwd     string
	started time.Time
	// Simplified: each execution = sh -c "cd <cwd> && <cmd> && echo sentinel"
	// State persistence rides on the cwd field, not on cd inside a shell (avoids losing state when the subprocess exits).
	// This is the KISS version: good enough for scenarios like "cd xxx && npm install && npm test".
}

// PShells is the pool of persistent shells, maintained per owner.
type PShells struct {
	mu sync.Mutex
	m  map[string]*PShell
}

func NewPShells() *PShells { return &PShells{m: map[string]*PShell{}} }

// The owner comes from the context, injected by the caller; it defaults to "default".
type pshellOwnerKey struct{}

func WithPShellOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, pshellOwnerKey{}, owner)
}

func pshellOwnerFrom(ctx context.Context) string {
	if ctx == nil {
		return "default"
	}
	if v, ok := ctx.Value(pshellOwnerKey{}).(string); ok && v != "" {
		return v
	}
	return "default"
}

// Exec runs a command in the persistent shell for the given owner; the returned cwd sets the starting point for the next call.
func (p *PShells) Exec(ctx context.Context, owner, command string, timeout time.Duration, roots []string) (map[string]any, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	p.mu.Lock()
	ps, ok := p.m[owner]
	if !ok {
		ps = &PShell{owner: owner, started: time.Now()}
		p.m[owner] = ps
	}
	p.mu.Unlock()

	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Prefix this command with the result of the last cd: the most naive way to get persistence.
	full := command
	if ps.cwd != "" {
		full = "cd " + shellQuote(ps.cwd) + " && " + command
	}
	// After the command, append a pwd to carry the new cwd back.
	sentinel := "__GLEAM_PWD_" + types.NewID() + "__"
	full = full + " && printf '\\n" + sentinel + "%s\\n' \"$PWD\""

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := runShellOnce(cctx, full, ps.cwd, roots)
	if err != nil {
		return nil, err
	}
	stdout, _ := out["stdout"].(string)
	// Extract the pwd following the sentinel and update the persistent cwd.
	if idx := strings.LastIndex(stdout, sentinel); idx >= 0 {
		newCWD := strings.TrimSpace(stdout[idx+len(sentinel):])
		if newCWD != "" {
			ps.cwd = newCWD
		}
		stdout = strings.TrimRight(stdout[:idx], "\n")
		out["stdout"] = stdout
	}
	out["persistent_cwd"] = ps.cwd
	return out, nil
}

func (p *PShells) Reset(owner string) {
	p.mu.Lock()
	delete(p.m, owner)
	p.mu.Unlock()
}

// shellQuote quotes a path so spaces / Chinese-character directory names do not break it.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\"'") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PersistentShellTool exposes shell.persistent.
type PersistentShellTool struct {
	Shells         *PShells
	Roots          []string
	DefaultTimeout time.Duration
}

func NewPersistentShell(shells *PShells, defaultTimeout time.Duration, roots ...string) types.Tool {
	return &PersistentShellTool{Shells: shells, Roots: roots, DefaultTimeout: defaultTimeout}
}

func (t *PersistentShellTool) Name() string { return "shell.persistent" }
func (t *PersistentShellTool) Description() string {
	return "在持久 Shell 中执行命令：同一任务内多次调用会保留 cd 后的工作目录。适合 'cd xxx && npm install && npm test' 这类连续操作。"
}
func (t *PersistentShellTool) Permission() types.Permission { return types.PermissionFullAccess }
func (t *PersistentShellTool) Schema() map[string]any {
	return toolutil.Schema("持久 Shell 执行", []string{"command"}, map[string]any{
		"command":     toolutil.SchemaProp("要执行的命令", "string"),
		"timeout_sec": toolutil.SchemaProp("超时秒数（默认 60）", "integer"),
	})
}
func (t *PersistentShellTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.Shells == nil {
		return nil, fmt.Errorf("持久 shell 未启用")
	}
	cmd, err := toolutil.RequireStr(args, "command")
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(toolutil.Int(args, "timeout_sec", int(t.DefaultTimeout/time.Second))) * time.Second
	owner := pshellOwnerFrom(ctx)
	return t.Shells.Exec(ctx, owner, cmd, timeout, t.Roots)
}
