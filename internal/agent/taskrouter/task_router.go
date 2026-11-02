package taskrouter

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gleam/internal/agent/tools/cua"
)

// Router maps task text to appropriate tool execution.
type Router struct {
	cu           *cua.ComputerUse // Singleton, ~2KB memory usage
	urlPattern   *regexp.Regexp
	blockedHosts map[string]bool
}

// New creates a new router with CUA support.
func New() *Router {
	return &Router{
		cu:           cua.NewComputerUse(),
		urlPattern:   regexp.MustCompile(`https?://[^\s]+`),
		blockedHosts: loadBlockedHosts(),
	}
}

// LoadBlockedHosts loads domain blacklist from environment variable.
func loadBlockedHosts() map[string]bool {
	blocked := map[string]bool{
		"127.0.0.1": true,
		"localhost": true,
		"intranet":  true,
	}

	if env := os.Getenv("GLEAM_CUA_BLOCKED_DOMAINS"); env != "" {
		for _, host := range strings.Split(env, ",") {
			blocked[strings.TrimSpace(host)] = true
		}
	}

	return blocked
}

// RouteTask analyzes task text and returns tool + params for browser-accessible URLs.
// Returns nil if no browser URL found - let agent choose standard tools.
func (rt *Router) RouteTask(ctx context.Context, task string) (interface{}, error) {
	urls := rt.urlPattern.FindAllString(task, -1)

	if len(urls) == 0 {
		return nil, fmt.Errorf("no browser-accessible URL found")
	}

	firstURL := urls[0]
	domain := extractDomain(firstURL)

	// Security check: block IPs and dangerous domains
	if rt.blockedHosts[domain] || rt.isIP(domain) {
		return nil, fmt.Errorf("blocked domain or IP: %s", domain)
	}

	// Return CUA action for this URL
	return rt.cuAction(firstURL), nil
}

// CuAction returns prepared CUA parameters for opening URL.
func (rt *Router) cuAction(urlStr string) map[string]interface{} {
	return map[string]interface{}{
		"action": "open_url", // New action type for CUA
		"params": map[string]interface{}{
			"url":              urlStr,
			"use_uiautomation": shouldUseUIAutomation(urlStr),
		},
	}
}

// ExtractDomain pulls hostname from URL string.
func extractDomain(urlStr string) string {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		// Fallback: try substring extraction
		idx := strings.Index(urlStr, "://")
		if idx == -1 {
			idx = 0
		} else {
			idx += 3
		}

		endIdx := strings.Index(urlStr[idx:], "/")
		if endIdx == -1 {
			return urlStr[idx:]
		}
		return urlStr[idx : idx+endIdx]
	}

	return parsed.Hostname()
}

// IsIP checks if string looks like an IP address.
func (rt *Router) isIP(s string) bool {
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

// ShouldUseUIAutomation determines if UI Automation mode should be used.
func shouldUseUIAutomation(urlStr string) bool {
	// Chinese apps/sites prefer UI Automation
	domain := extractDomain(urlStr)
	chinesePatterns := []string{"wechat", "qq", "dingtalk", "weixin", "企业"}

	for _, pattern := range chinesePatterns {
		if strings.Contains(domain, pattern) {
			return true
		}
	}

	// Config override for Windows users who want always-UIA
	if os.Getenv("GLEAM_FORCE_UIAUTOMATION") == "true" {
		return true
	}

	// Default: use direct mode on Windows, MCP elsewhere
	if os.Getenv("OS") == "Windows_NT" {
		return true
	}
	return false
}

// Example usage in executor pre-check:
/*
func (e *Executor) PreExecuteTask(task string) error {
	router := e.agent.TaskRouter

	result, err := router.RouteTask(context.Background(), task)
	if err != nil {
		return err // No URL detected, continue with normal flow
	}

	// URL detected! Pre-wire CUA tool
	if action, ok := result.(map[string]interface{}); ok {
		e.preparedCUAAction = action
		e.useCUA = true
		log.Printf("[taskrouter] URL detected: %s, using CUA mode", action["params"]["url"])
	}

	return nil
}
*/
