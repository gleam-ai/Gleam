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

func TestPowerShellScript_Detection(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`powershell -Command "Get-Date"`, `Get-Date`, true},
		{`pwsh -NoProfile -Command "Rename-Item -Path 'a' -NewName 'b'"`, `Rename-Item -Path 'a' -NewName 'b'`, true},
		{`powershell -Command 'Get-Date'`, `Get-Date`, true},
		{`Get-ChildItem -Path .`, `Get-ChildItem -Path .`, true},
		{`Rename-Item old.txt new.txt`, `Rename-Item old.txt new.txt`, true},
		{`$x = 'a'; Rename-Item $x 'b'`, `$x = 'a'; Rename-Item $x 'b'`, true},
		{`echo hi`, "", false},
		{`findstr /C:"x" file.txt`, "", false},
		{`go test ./...`, "", false},
		{`dir`, "", false},
		{`powershell -File script.ps1`, "", false},
		{`powershell -EncodedCommand AAA`, "", false},
	}
	for _, c := range cases {
		got, ok := powerShellScript(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("powerShellScript(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestShellCommand_PowerShellUsesCommandNotBypass(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	cmd := shellCommand(`$x = 'a'; Rename-Item $x 'b'`)
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "-Command") {
		t.Errorf("PowerShell 应经 -Command 直接执行: %v", cmd.Args)
	}
	if strings.Contains(joined, "-ExecutionPolicy") || strings.Contains(joined, "Bypass") {
		t.Errorf("不应关闭脚本执行策略: %v", cmd.Args)
	}
	if strings.Contains(joined, "-File") {
		t.Errorf("不应走 -File 临时脚本: %v", cmd.Args)
	}
}

func TestExecute_PowerShellRenameWithVariable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	root := t.TempDir()
	old := filepath.Join(root, "old.txt")
	if err := os.WriteFile(old, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewWithRoots(30*time.Second, root)
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": `$src = 'old.txt'; Rename-Item -Path $src -NewName 'new.txt'`,
		"cwd":     root,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["exit_code"].(int) != 0 {
		t.Fatalf("$ 变量 PowerShell 改名应成功: %v", m)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("旧文件应已消失，stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Errorf("新文件应存在: %v", err)
	}
}

func TestExecute_PowerShellNonASCIIPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	root := t.TempDir()
	tool := NewWithRoots(30*time.Second, root)
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": `New-Item -Path '微光.txt' -ItemType File -Force | Out-Null`,
		"cwd":     root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m := out.(map[string]any); m["exit_code"].(int) != 0 {
		t.Fatalf("中文名文件创建应成功: %v", m)
	}
	if _, err := os.Stat(filepath.Join(root, "微光.txt")); err != nil {
		t.Errorf("中文名文件应被正确创建（BOM 解码生效）: %v", err)
	}
}

func TestExecute_CmdNativeStillWorks(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewWithRoots(30*time.Second, root)
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": `findstr hello a.txt`,
		"cwd":     root,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["exit_code"].(int) != 0 {
		t.Fatalf("cmd 原生 findstr 不应被方言改写影响: %v", m)
	}
	if !strings.Contains(m["stdout"].(string), "hello") {
		t.Errorf("findstr 输出应含 hello: %v", m)
	}
}
