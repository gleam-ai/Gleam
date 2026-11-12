package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

const maxOutput = 64 * 1024

type Tool struct {
	DefaultTimeout time.Duration
	Roots          []string
	// When Jobs is non-nil, background=true is supported: the command goes into the background job table and a job id is returned immediately.
	Jobs *Jobs
}

func New(defaultTimeout time.Duration) *Tool { return &Tool{DefaultTimeout: defaultTimeout} }

func NewWithRoots(defaultTimeout time.Duration, roots ...string) *Tool {
	return &Tool{DefaultTimeout: defaultTimeout, Roots: roots}
}

func (t *Tool) Name() string { return "shell.exec" }
func (t *Tool) Description() string {
	return "在系统 Shell 中执行命令并返回输出（高风险操作，需要用户批准）"
}
func (t *Tool) Permission() types.Permission {
	return types.PermissionFullAccess
}
func (t *Tool) Schema() map[string]any {
	return toolutil.Schema("执行 Shell 命令", []string{"command"}, map[string]any{
		"command":     toolutil.SchemaProp("要执行的命令", "string"),
		"cwd":         toolutil.SchemaProp("工作目录（可选，须在工作区内）", "string"),
		"timeout_sec": toolutil.SchemaProp("超时秒数（默认 30）", "integer"),
		"background":  toolutil.SchemaProp("为 true 时放入后台作业并立即返回 job id，配 jobs.list/output/kill 使用", "boolean"),
	})
}
func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	command, err := toolutil.RequireStr(args, "command")
	if err != nil {
		return nil, err
	}
	cwd, err := t.resolveCWD(ctx, toolutil.Str(args, "cwd"))
	if err != nil {
		return nil, err
	}

	if toolutil.Bool(args, "background") {
		if t.Jobs == nil {
			return nil, fmt.Errorf("后台作业未启用")
		}
		job, err := t.Jobs.Start(ctx, command, cwd, sanitizedEnv(os.Environ()))
		if err != nil {
			return nil, err
		}
		snap := job.Snapshot()
		return map[string]any{
			"background": true,
			"job_id":     snap.ID,
			"command":    command,
			"started_at": snap.StartedAt,
		}, nil
	}

	timeout := time.Duration(toolutil.Int(args, "timeout_sec", int(t.DefaultTimeout/time.Second))) * time.Second
	if timeout <= 0 {
		timeout = t.DefaultTimeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shellCommand(command)
	setProcessGroup(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = ensureWindowsSystemPath(sanitizedEnv(os.Environ()))
	var stdout, stderr limitedBuffer
	stdout.limit = maxOutput
	stderr.limit = maxOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("命令启动失败: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-cctx.Done():
		_ = killProcessTree(cmd)
		<-done
	case err = <-done:
	}

	if ctx.Err() != nil {
		return nil, fmt.Errorf("命令被取消: %w", ctx.Err())
	}
	if cctx.Err() != nil {
		return nil, fmt.Errorf("命令超时（%v）", timeout)
	}

	exitCode := 0
	pipeStuck := false
	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case asExit(err, &exitErr):
			exitCode = exitErr.ExitCode()
		case errors.Is(err, exec.ErrWaitDelay):
			pipeStuck = true
		default:
			return nil, fmt.Errorf("命令执行失败: %w", err)
		}
	}
	out := map[string]any{
		"command":   command,
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"error":     errString(err, exitCode),
	}
	if pipeStuck {
		out["error"] = "命令已结束，但有子进程仍持有输出管道：输出可能不完整"
		out["output_incomplete"] = true
	}
	return out, nil
}

// runShellOnce runs the command in a one-shot subprocess and returns a result shaped like Tool.Execute.
// Shared by the persistent shell and background jobs, so the timeout/process-tree cleanup logic is written once.
func runShellOnce(ctx context.Context, command, cwd string, roots []string) (map[string]any, error) {
	cmd := shellCommand(command)
	setProcessGroup(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = ensureWindowsSystemPath(sanitizedEnv(os.Environ()))
	var stdout, stderr limitedBuffer
	stdout.limit = maxOutput
	stderr.limit = maxOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("命令启动失败: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case <-ctx.Done():
		_ = killProcessTree(cmd)
		<-done
	case err = <-done:
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("命令被取消或超时: %w", ctx.Err())
	}
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			return nil, fmt.Errorf("命令执行失败: %w", err)
		}
	}
	return map[string]any{
		"command":   command,
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
	}, nil
}

func (t *Tool) resolveCWD(ctx context.Context, cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", nil
	}
	abs, err := toolutil.ResolveInRoots(cwd, toolutil.RootsFrom(ctx, t.Roots))
	if err != nil {
		return "", fmt.Errorf("工作目录不可用：%w", err)
	}
	return abs, nil
}

var credentialMarkers = []string{
	"_API_KEY", "_APIKEY", "API_KEY", "_TOKEN", "ACCESS_TOKEN", "_SECRET",
	"PASSWORD", "PASSWD", "CREDENTIAL", "_PRIVATE_KEY",
}

// windowsCmd returns an absolute path to the command interpreter. Desktop
// builds inherit a PATH that installers and security software can trim, and
// exec.Command("cmd") then fails with "executable file not found" even though
// the shell is installed in the usual place.
func windowsCmd() string {
	if v := strings.TrimSpace(os.Getenv("COMSPEC")); v != "" {
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return v
		}
	}
	if root := strings.TrimSpace(os.Getenv("SystemRoot")); root != "" {
		p := filepath.Join(root, "System32", "cmd.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("cmd.exe"); err == nil {
		return p
	}
	return "cmd.exe"
}

// shellCommand builds the interpreter invocation shared by every shell tool.
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command(windowsCmd(), "/C", command)
	}
	return exec.Command("sh", "-c", command)
}

// ensureWindowsSystemPath keeps System32 on the child PATH so built-ins such as
// curl, where and powershell resolve even when the parent PATH was trimmed.
func ensureWindowsSystemPath(env []string) []string {
	if runtime.GOOS != "windows" {
		return env
	}
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		return env
	}
	want := []string{filepath.Join(root, "System32"), root}
	for i, kv := range env {
		name, val, ok := strings.Cut(kv, "=")
		if !ok || !strings.EqualFold(name, "PATH") {
			continue
		}
		parts := strings.Split(val, ";")
		for _, w := range want {
			if !hasFold(parts, w) {
				parts = append([]string{w}, parts...)
			}
		}
		env[i] = "PATH=" + strings.Join(parts, ";")
		return env
	}
	return append(env, "PATH="+strings.Join(want, ";"))
}

func hasFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

func sanitizedEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if isCredentialVar(name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isCredentialVar(name string) bool {
	up := strings.ToUpper(name)
	for _, m := range credentialMarkers {
		if strings.Contains(up, m) {
			return true
		}
	}
	return false
}

func asExit(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

func (t *Tool) Outcome(args map[string]any, out any) (types.StepOutcome, string) {
	m, ok := out.(map[string]any)
	if !ok {
		return types.OutcomeOK, ""
	}
	code := toolutil.IntOf(m["exit_code"])
	if code == 0 {
		return types.OutcomeOK, ""
	}
	return types.OutcomeFailed, fmt.Sprintf("命令以退出码 %d 结束", code)
}

func errString(err error, code int) string {
	if err == nil || code == 0 {
		return ""
	}
	return err.Error()
}

// limitedBuffer drops data as it is written: a command such as `dir /s C:\`
// must not buffer gigabytes in memory before truncating.
type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	remain := l.limit - l.buf.Len()
	if remain > 0 {
		if len(p) > remain {
			l.buf.Write(p[:remain])
			l.truncated = true
		} else {
			l.buf.Write(p)
		}
	} else if len(p) > 0 {
		l.truncated = true
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string {
	if l.truncated {
		return l.buf.String() + "\n…（输出已截断）"
	}
	return l.buf.String()
}
