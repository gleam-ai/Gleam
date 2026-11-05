package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"gleam/internal/config"
	"gleam/pkg/types"
)

// HookRunner runs user-configured tool lifecycle hooks (the lightweight
// equivalent of the DSH hooks bridge).
//
//   - pre_tool: runs before the tool executes and reads a JSON payload from
//     stdin. Exit code 2 blocks this tool call (reason taken from
//     stdout/stderr), exit code 0 allows it, and any other exit code only
//     logs a warning without blocking -- a buggy hook must not wedge the
//     whole task.
//   - post_tool: runs after the tool finishes, purely for notification
//     (audit trails, desktop messages, ...); any failure is only logged.
//
// Hooks are user config inside the machine's own trust boundary, and the
// command is interpreted by the platform shell (cmd /c on Windows, sh -c
// elsewhere). The payload goes through stdin, never spliced into the
// command line, to avoid injection.
type HookRunner struct {
	Hooks   []config.HookConfig
	Timeout time.Duration
	// Exec is the execution seam tests can replace; nil uses the platform shell.
	Exec func(ctx context.Context, command string, stdin []byte) (code int, output string, err error)
}

const hookDefaultTimeout = 10 * time.Second

// NewHookRunner returns nil when there are no hooks (callers check for nil
// and skip at zero cost).
func NewHookRunner(hooks []config.HookConfig) *HookRunner {
	if len(hooks) == 0 {
		return nil
	}
	return &HookRunner{Hooks: hooks, Timeout: hookDefaultTimeout}
}

// hookPayload is the JSON payload handed to the hook process on stdin.
type hookPayload struct {
	Event  string         `json:"event"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args,omitempty"`
	TaskID string         `json:"task_id,omitempty"`
	OK     bool           `json:"ok,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// PreTool returns a non-empty string to block this tool call; the string is
// the reason the model will see.
func (h *HookRunner) PreTool(ctx context.Context, tool string, args map[string]any) string {
	return h.run(ctx, hookPayload{Event: "pre_tool", Tool: tool, Args: args, TaskID: taskIDFrom(ctx)}, true)
}

// PostTool is the notification hook after a tool finishes; best-effort and
// never affects the task result.
func (h *HookRunner) PostTool(ctx context.Context, tool string, args map[string]any, ok bool, errMsg string) {
	h.run(ctx, hookPayload{Event: "post_tool", Tool: tool, Args: args, TaskID: taskIDFrom(ctx), OK: ok, Error: errMsg}, false)
}

func (h *HookRunner) run(ctx context.Context, payload hookPayload, canBlock bool) string {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = hookDefaultTimeout
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	for _, hc := range h.Hooks {
		if hc.Event != payload.Event || !hookMatches(hc.Tools, payload.Tool) {
			continue
		}
		hctx, cancel := context.WithTimeout(ctx, timeout)
		code, output, execErr := h.exec(hctx, hc.Command, raw)
		cancel()
		switch {
		case execErr != nil:
			// Spawn failure or timeout: infrastructure trouble, log it but
			// keep the tool execution path unchanged.
			fmt.Fprintf(os.Stderr, "[hook] %s hook failed to run (tool %s): %v\n", payload.Event, payload.Tool, execErr)
		case code == 0:
			// Allow / done.
		case code == 2 && canBlock:
			reason := strings.TrimSpace(output)
			if reason == "" {
				reason = "no reason given"
			}
			return fmt.Sprintf("tool %s blocked by pre_tool hook: %s", payload.Tool, types.Shorten(reason, 200))
		default:
			fmt.Fprintf(os.Stderr, "[hook] %s hook exit code %d (tool %s): %s\n",
				payload.Event, code, payload.Tool, types.Shorten(output, 200))
		}
	}
	return ""
}

func (h *HookRunner) exec(ctx context.Context, command string, stdin []byte) (int, string, error) {
	if h.Exec != nil {
		return h.Exec(ctx, command, stdin)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Stdin = bytes.NewReader(stdin)
	var out boundedBuffer
	out.limit = maxHookOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return -1, out.String(), err
		}
	}
	return code, out.String(), nil
}

const maxHookOutput = 64 << 10

// boundedBuffer keeps the head of a hook's output without letting a noisy hook
// grow the process heap for the whole timeout. Write always reports the full
// length so cmd never treats the truncation as a short write.
type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Buffer.Len(); room > 0 {
		if room > len(p) {
			room = len(p)
		}
		_, _ = b.Buffer.Write(p[:room])
	}
	return len(p), nil
}

func hookMatches(patterns []string, tool string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if p == tool {
			return true
		}
	}
	return false
}
