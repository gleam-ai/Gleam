package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"gleam/pkg/types"
)

func echoCmd(text string) string {
	if runtime.GOOS == "windows" {
		return "echo " + text
	}
	return "printf '" + text + "'"
}

func failingCmd() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit 3"
	}
	return "exit 3"
}

const execTimeout = 60 * time.Second

func TestLimitedBufferKeepsPrefixAndMarksTruncation(t *testing.T) {
	var b limitedBuffer
	b.limit = 8
	if n, err := b.Write([]byte("0123456789")); n != 10 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	s := b.String()
	if !strings.HasPrefix(s, "01234567") || !strings.Contains(s, "输出已截断") {
		t.Errorf("截断结果 = %q", s)
	}
	if b.buf.Len() > b.limit {
		t.Errorf("缓冲区超过上限：%d", b.buf.Len())
	}
}

func TestShell_ExecEcho(t *testing.T) {
	tool := New(execTimeout)
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": echoCmd("hello-gleam"),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := out.(map[string]any)
	if !strings.Contains(m["stdout"].(string), "hello-gleam") {
		t.Errorf("stdout = %q", m["stdout"])
	}
	if m["exit_code"] != 0 {
		t.Errorf("exit_code = %v", m["exit_code"])
	}
}

func TestShell_ExecExitCode(t *testing.T) {
	tool := New(execTimeout)
	out, err := tool.Execute(context.Background(), map[string]any{"command": failingCmd()})
	if err != nil {
		t.Fatalf("非零退出码不应返回 error: %v", err)
	}
	m := out.(map[string]any)
	if m["exit_code"] == 0 {
		t.Errorf("exit_code = %v", m["exit_code"])
	}
	if m["error"] == "" {
		t.Error("error 字段应有描述")
	}
}

func TestShell_OutcomeReportsNonZeroExit(t *testing.T) {
	tool := New(execTimeout)

	okOut, err := tool.Execute(context.Background(), map[string]any{"command": "echo hi"})
	if err != nil {
		t.Fatalf("echo 应成功: %v", err)
	}
	if oc, note := tool.Outcome(nil, okOut); oc != types.OutcomeOK || note != "" {
		t.Errorf("exit 0 应报 ok，实际 %q/%q", oc, note)
	}

	badOut, err := tool.Execute(context.Background(), map[string]any{"command": failingCmd()})
	if err != nil {
		t.Fatalf("非零退出码不应返回 error: %v", err)
	}
	oc, note := tool.Outcome(nil, badOut)
	if oc != types.OutcomeFailed {
		t.Errorf("非零退出应报 failed，实际 %q", oc)
	}
	if !strings.Contains(note, "退出码") {
		t.Errorf("应说明退出码，实际 %q", note)
	}
}

func TestShell_ExecTimeout(t *testing.T) {
	tool := New(execTimeout)
	sleepCmd := "ping -n 6 127.0.0.1 >nul"
	if runtime.GOOS != "windows" {
		sleepCmd = "sleep 5"
	}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command":     sleepCmd,
		"timeout_sec": 1,
	})
	if err == nil {
		t.Fatalf("超时应报错，实际返回: %v", out)
	}
	if !strings.Contains(err.Error(), "超时") && !strings.Contains(err.Error(), "取消") {
		t.Errorf("错误信息应提示超时: %v", err)
	}
}

func TestShell_MissingCommand(t *testing.T) {
	tool := New(time.Second)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("缺少 command 应报错")
	}
}

func TestShell_PermissionAndSchema(t *testing.T) {
	tool := New(time.Second)
	if tool.Name() != "shell.exec" {
		t.Errorf("name = %s", tool.Name())
	}
	schema := tool.Schema()
	if _, ok := schema["properties"].(map[string]any)["command"]; !ok {
		t.Error("schema 缺少 command 属性")
	}
}
