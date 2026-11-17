package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// PowerShell cmdlets are Verb-Noun; cmd.exe has no native command of that
// shape, so the leading verb-noun token is a conservative dialect signal.
var psVerbPattern = regexp.MustCompile(`(?i)^(get|set|new|remove|rename|copy|move|write|read|invoke|foreach|where|select|sort|add|clear|start|stop|test|resolve|convertto|convertfrom|import|export|out|format|measure|compare|find|search|join|split|push|pop|enter|exit|enable|disable|install|uninstall|update|wait|receive|send|connect|disconnect|open|close|show|hide|register|unregister|reset|restart|resume|suspend|save|sync|trace|debug)-[a-z0-9]+`)

// powerShellScript reports whether command is PowerShell and returns the script
// body to run. Cases: an explicit powershell/pwsh -Command wrapper (body
// unwrapped), a leading $variable, or a bare Verb-Noun cmdlet. -File and
// -EncodedCommand wrappers are left to cmd.exe untouched.
func powerShellScript(command string) (string, bool) {
	s := strings.TrimSpace(command)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "powershell") || strings.HasPrefix(lower, "pwsh") {
		i := strings.Index(lower, "-command")
		if i < 0 {
			return "", false
		}
		body := strings.TrimSpace(s[i+len("-command"):])
		if len(body) >= 2 {
			first, last := body[0], body[len(body)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				body = body[1 : len(body)-1]
			}
		}
		if strings.TrimSpace(body) == "" {
			return "", false
		}
		return body, true
	}
	if strings.HasPrefix(s, "$") || psVerbPattern.MatchString(s) {
		return s, true
	}
	return "", false
}

// powerShellExe locates powershell.exe even when the parent PATH was trimmed.
func powerShellExe() string {
	if root := strings.TrimSpace(os.Getenv("SystemRoot")); root != "" {
		p := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		return p
	}
	return "powershell"
}
