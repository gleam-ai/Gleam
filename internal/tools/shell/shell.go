package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	})
}
func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	command, err := toolutil.RequireStr(args, "command")
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(toolutil.Int(args, "timeout_sec", int(t.DefaultTimeout/time.Second))) * time.Second
	if timeout <= 0 {
		timeout = t.DefaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cwd, err := t.resolveCWD(ctx, toolutil.Str(args, "cwd"))
	if err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	setProcessGroup(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = sanitizedEnv(os.Environ())
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
