package taskrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gleam/internal/agent/tools/cua"
)

// EnhancedRouter extends basic router with configurable modes and auditing.
type EnhancedRouter struct {
	config     *Config
	baseRouter *Router
	cu         *cua.ComputerUse
	wg         sync.WaitGroup
	stopChan   chan struct{}
	rwMutex    sync.RWMutex
	auditLog   *AuditLogger
}

// NewEnhancedRouter creates router with config support.
func NewEnhancedRouter(cfg *Config) *EnhancedRouter {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	base := New() // Create base router for URL pattern matching

	router := &EnhancedRouter{
		config:     cfg,
		baseRouter: base,
		cu:         cua.NewComputerUse(),
		stopChan:   make(chan struct{}),
	}

	if err := cfg.Validate(); err != nil {
		log.Printf("[taskrouter] invalid config, using defaults: %v", err)
		cfg = DefaultConfig()
	}

	return router
}

// IsIP checks if string looks like an IP address.
func (er *EnhancedRouter) isIP(s string) bool {
	hasDigits := false
	hasDot := false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			hasDigits = true
		}
		if c == '.' {
			hasDot = true
		}
	}
	return hasDigits && hasDot
}

// Enable switches routing on/off dynamically.
func (er *EnhancedRouter) Enable(enabled bool) {
	er.rwMutex.Lock()
	defer er.rwMutex.Unlock()
	er.config.Enabled = enabled
}

// RouteTask analyzes input and returns CUA action if URL detected.
func (er *EnhancedRouter) RouteTask(ctx context.Context, task string) (*CUAAction, error) {
	er.rwMutex.RLock()
	enabled := er.config.Enabled
	er.rwMutex.RUnlock()

	if !enabled {
		return nil, nil // Routing disabled
	}

	urls := er.baseRouter.urlPattern.FindAllString(task, -1)
	if len(urls) == 0 {
		return nil, fmt.Errorf("no URL found")
	}

	domain := extractDomain(urls[0])

	// Security check
	if er.isBlocked(domain) || er.isIP(domain) {
		return nil, fmt.Errorf("blocked domain: %s", domain)
	}

	// Whitelist check
	if len(er.config.AllowedHosts) > 0 && !er.isAllowed(domain) {
		return nil, fmt.Errorf("domain not whitelisted: %s", domain)
	}

	action := er.createCUAAction(urls[0])
	er.logRouting(task, action)

	return action, nil
}

// CreateCUAAction builds standardized CUA execution parameters.
func (er *EnhancedRouter) createCUAAction(urlStr string) *CUAAction {
	return &CUAAction{
		URL:             urlStr,
		Domain:          extractDomain(urlStr),
		UseUIAutomation: shouldUseUIAutomation(urlStr),
		Timestamp:       time.Now(),
		AuditID:         generateAuditID(),
	}
}

// ExecuteImmediately runs CUA action without waiting for confirmation.
func (er *EnhancedRouter) ExecuteImmediately(ctx context.Context, action *CUAAction) error {
	if !er.config.AutoConfirmURLs {
		return fmt.Errorf("auto-confirm disabled")
	}

	params := map[string]interface{}{
		"action":           "open_url",
		"use_uiautomation": action.UseUIAutomation,
	}

	result, err := er.cu.Execute(ctx, params)
	if err != nil {
		return fmt.Errorf("CUA execution failed: %w", err)
	}

	er.auditLog.LogExecution(action.AuditID, result)
	return nil
}

// StartBackgroundMonitor launches dedicated monitoring goroutine.
func (er *EnhancedRouter) StartBackgroundMonitor(ctx context.Context) {
	if er.config.Mode != BackgroundMode {
		return
	}

	er.wg.Add(1)
	go er.monitorLoop(ctx)
}

// StopBackgroundMonitor gracefully terminates monitoring.
func (er *EnhancedRouter) StopBackgroundMonitor() {
	if er.config.Mode != BackgroundMode {
		return
	}

	close(er.stopChan)
	er.wg.Wait()
}

// monitorLoop periodically checks for new URLs.
func (er *EnhancedRouter) monitorLoop(ctx context.Context) {
	defer er.wg.Done()

	interval := 5 * time.Second
	if er.config.PollIntervalSecs > 0 {
		interval = time.Duration(er.config.PollIntervalSecs) * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-er.stopChan:
			return
		case <-ticker.C:
			// Polling logic would go here
			// For now, skip polling mode implementation
		}
	}
}

// LogRouting records task → URL mapping.
func (er *EnhancedRouter) logRouting(task string, action *CUAAction) {
	if er.auditLog == nil {
		er.initAuditLog()
	}

	er.auditLog.LogTask(action.AuditID, task)
}

// InitAuditLog initializes audit logger from config path.
func (er *EnhancedRouter) initAuditLog() {
	path := er.config.LogPath
	if path == "" {
		path = filepath.Join(os.TempDir(), "gleam_cua_audit.log")
	}

	logger, err := NewAuditLogger(path)
	if err != nil {
		log.Printf("[taskrouter] failed to init audit logger: %v", err)
		return
	}

	er.auditLog = logger
}

// IsBlocked checks if domain is blocked by config or env var.
func (er *EnhancedRouter) isBlocked(domain string) bool {
	er.rwMutex.RLock()
	defer er.rwMutex.RUnlock()

	// Check explicit block list
	for _, blocked := range er.config.BlockedHosts {
		if blocked == domain {
			return true
		}
	}

	// Check environment variable
	envBlocked := os.Getenv("GLEAM_CUA_BLOCKED_DOMAINS")
	if envBlocked != "" {
		for _, blocked := range splitStrings(envBlocked, ",") {
			if blocked == domain {
				return true
			}
		}
	}

	return false
}

// IsAllowed checks whitelist.
func (er *EnhancedRouter) isAllowed(domain string) bool {
	for _, allowed := range er.config.AllowedHosts {
		if allowed == domain {
			return true
		}
	}
	return false
}

// Helper functions
func splitStrings(s, sep string) []string {
	if s == "" {
		return nil
	}
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i:i+1] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

// CUAAction represents prepared CUA execution parameters.
type CUAAction struct {
	URL             string    `json:"url"`
	Domain          string    `json:"domain"`
	UseUIAutomation bool      `json:"use_uiautomation"`
	Timestamp       time.Time `json:"timestamp"`
	AuditID         string    `json:"audit_id"`
}

// AuditLogger handles audit log writing.
type AuditLogger struct {
	filePath string
	file     *os.File
	mu       sync.Mutex
}

// NewAuditLogger creates audit logger instance.
func NewAuditLogger(path string) (*AuditLogger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &AuditLogger{filePath: path, file: f}, nil
}

// LogTask records a task submission.
func (al *AuditLogger) LogTask(auditID, task string) {
	al.mu.Lock()
	defer al.mu.Unlock()

	line := fmt.Sprintf("[%s] TASK=%s\n", auditID, task)
	al.file.WriteString(line)
}

// LogExecution records execution result.
func (al *AuditLogger) LogExecution(auditID string, result interface{}) {
	al.mu.Lock()
	defer al.mu.Unlock()

	data, _ := json.Marshal(result)
	line := fmt.Sprintf("[%s] RESULT=%s\n", auditID, string(data))
	al.file.WriteString(line)
}

// Utility functions
func generateAuditID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), randInt())
}

func randInt() int {
	return time.Now().Nanosecond()
}
