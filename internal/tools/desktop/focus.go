package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// FocusWindow brings the first top-level window whose title contains substr
// (case-insensitive) to the foreground and returns the title read back from
// the foreground window afterwards. Callers ground a screenshot in that
// returned title instead of assuming the right window was captured.
func FocusWindow(substr string) (string, error) {
	substr = strings.TrimSpace(substr)
	if substr == "" {
		return "", fmt.Errorf("窗口标题关键词为空")
	}
	if strings.ContainsAny(substr, "\n\r") {
		return "", fmt.Errorf("窗口标题关键词不能包含换行")
	}
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("窗口聚焦目前仅支持 Windows")
	}
	return focusWindowWindows(substr)
}

func focusWindowWindows(substr string) (string, error) {
	// The keyword travels through an env var, never spliced into the script:
	// it is model-provided text and must not become code. OutputEncoding is
	// pinned to UTF-8 because the console code page would otherwise mangle
	// non-ASCII window titles on their way back to Go.
	script := `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; $t=$env:GLEAM_WINDOW_TITLE; $p=Get-Process | Where-Object { $_.MainWindowTitle -and $_.MainWindowTitle.IndexOf($t,[StringComparison]::OrdinalIgnoreCase) -ge 0 } | Select-Object -First 1; if(-not $p){ exit 2 }; Add-Type -Name Win32 -Namespace Gleam -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd); [DllImport("user32.dll")] public static extern bool ShowWindowAsync(IntPtr hWnd, int nCmdShow); [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow(); [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr hWnd, System.Text.StringBuilder text, int count);'; [Gleam.Win32]::ShowWindowAsync($p.MainWindowHandle,9) | Out-Null; [Gleam.Win32]::SetForegroundWindow($p.MainWindowHandle) | Out-Null; Start-Sleep -Milliseconds 400; $h=[Gleam.Win32]::GetForegroundWindow(); $sb=New-Object System.Text.StringBuilder 512; [Gleam.Win32]::GetWindowText($h,$sb,512) | Out-Null; Write-Output $sb.ToString()`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.Env = append(os.Environ(), "GLEAM_WINDOW_TITLE="+substr)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 2 {
			return "", fmt.Errorf("未找到标题含 %q 的窗口", substr)
		}
		return "", fmt.Errorf("窗口聚焦失败: %w", err)
	}
	title := strings.TrimSpace(string(out))
	// Grounding check: read back what is actually in front. A locked session
	// or another app stealing focus must fail loudly, not hand back a
	// screenshot of the wrong window.
	if !strings.Contains(strings.ToLower(title), strings.ToLower(substr)) {
		return "", fmt.Errorf("窗口聚焦未生效：前台仍是 %q（可能锁屏或被其他窗口抢占）", title)
	}
	return title, nil
}
