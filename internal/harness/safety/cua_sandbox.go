// Package cua_sandbox provides safety gates for computer use operations.
package safety

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// CUASafetyGate enforces security policies on computer use actions.
type CUASafetyGate struct {
	mu          sync.Mutex
	allowedApps map[string]bool // whitelisted applications
	history     []ActionRecord  // action audit trail
	maxHistory  int
}

// ActionRecord logs a single computer use operation.
type ActionRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Safe      bool      `json:"safe"`
	Error     string    `json:"error,omitempty"`
}

// NewCUASafetyGate creates a new safety gate with default policies.
func NewCUASafetyGate() *CUASafetyGate {
	return &CUASafetyGate{
		allowedApps: map[string]bool{
			"chrome":    true,
			"safari":    true,
			"firefox":   true,
			"vscode":    true,
			"terminal":  true,
			"finder":    runtime.GOOS == "darwin",
			"explorer":  runtime.GOOS == "windows",
			"nautilus":  runtime.GOOS == "linux",
			
			// Chinese applications whitelist (Task #2)
			"wechat":           true,
			"weixin":           true,
			"qq":               true,
			"dingtalk":         true,
			"dingxiang":        true, // DingTalk variant
			"qidian":           true, // Qidian IM
			"tencent会议":       true, // Tencent Meeting
			"microsoftteams":   true,
			"企业微信":          true, // Enterprise WeChat
		},
		history:    make([]ActionRecord, 0),
		maxHistory: 1000,
	}
}

// AllowAction checks if the given action is permitted.
func (sg *CUASafetyGate) AllowAction(ctx context.Context, action string, params map[string]any) error {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	if strings.Contains(action, "exec") || strings.Contains(action, "shell") {
		return fmt.Errorf("action %s blocked: unsafe command execution not allowed", action)
	}

	if action == "open_app" {
		appName, _ := params["app_name"].(string)
		if !sg.isAppAllowed(appName) {
			return fmt.Errorf("application %s not in whitelist", appName)
		}
	}

	record := ActionRecord{
		Timestamp: time.Now(),
		Action:    action,
		Safe:      true,
	}
	sg.history = append(sg.history, record)
	if len(sg.history) > sg.maxHistory {
		sg.history = sg.history[1:]
	}

	return nil
}

// isAppAllowed checks if an application is whitelisted.
func (sg *CUASafetyGate) isAppAllowed(appName string) bool {
	lower := strings.ToLower(appName)
	
	// Check explicit whitelist
	if sg.allowedApps[lower] {
		return true
	}
	
	// Check Chinese app patterns
	chinesePatterns := []struct{
		pattern string
		name    string
	}{
		{"微信", "WeChat"},
		{"企业微信", "Enterprise WeChat"},
		{"钉钉", "DingTalk"},
		{"腾讯会议", "Tencent Meeting"},
		{"qq", "QQ"},
	}
	
	for _, p := range chinesePatterns {
		if strings.Contains(lower, p.pattern) || strings.Contains(lower, p.name) {
			sg.allowedApps[lower] = true // Auto-whitelist for future calls
			return true
		}
	}
	
	// Check common browser patterns
	browsers := []string{"chrome", "browser", "edge"}
	for _, b := range browsers {
		if strings.Contains(lower, b) {
			return true
		}
	}
	
	return false
}

// ExecuteWithSafety wraps an action with safety checks.
func (sg *CUASafetyGate) ExecuteWithSafety(ctx context.Context, action string, params map[string]any, fn func() error) error {
	if err := sg.AllowAction(ctx, action, params); err != nil {
		sg.recordFailure(action, err.Error())
		return err
	}

	err := fn()
	if err != nil {
		sg.recordFailure(action, err.Error())
	} else {
		sg.mu.Lock()
		sg.history[len(sg.history)-1].Safe = true
		sg.mu.Unlock()
	}

	return err
}

// recordFailure logs a failed action.
func (sg *CUASafetyGate) recordFailure(action, errMsg string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	
	record := ActionRecord{
		Timestamp: time.Now(),
		Action:    action,
		Safe:      false,
		Error:     errMsg,
	}
	sg.history = append(sg.history, record)
	if len(sg.history) > sg.maxHistory {
		sg.history = sg.history[1:]
	}
}

// GetHistory returns recent action audit log.
func (sg *CUASafetyGate) GetHistory(limit int) []ActionRecord {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	
	start := len(sg.history) - limit
	if start < 0 {
		start = 0
	}
	
	return append([]ActionRecord{}, sg.history[start:]...)
}

// SetAppWhitelist updates the application whitelist.
func (sg *CUASafetyGate) SetAppWhitelist(apps []string) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	
	for _, app := range apps {
		sg.allowedApps[strings.ToLower(app)] = true
	}
}

// VerifyProcess runs a process and verifies it doesn't violate policies.
func (sg *CUASafetyGate) VerifyProcess(cmd *exec.Cmd) error {
	cmdStr := strings.Join(cmd.Args, " ")
	
	// Block dangerous commands
	blockedPrefixes := []string{
		"rm -rf", "sudo", "chmod 777", "dd if=", "mkfs",
	}
	
	for _, prefix := range blockedPrefixes {
		if strings.Contains(cmdStr, prefix) {
			return fmt.Errorf("command blocked: contains dangerous pattern %s", prefix)
		}
	}
	
	return nil
}
