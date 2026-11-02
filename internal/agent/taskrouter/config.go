package taskrouter

import (
	"encoding/json"
	"os"
)

// ModeType defines how URL detection triggers CUA actions.
type ModeType string

const (
	// EventTriggerMode - Only check URLs when user submits tasks (recommended, zero overhead)
	EventTriggerMode ModeType = "event-triggered"

	// PollingMode - Periodically scan task history for new URLs (higher latency but real-time)
	PollingMode ModeType = "polling"

	// BackgroundMode - Dedicated goroutine monitors all incoming messages (highest overhead)
	BackgroundMode ModeType = "background"
)

// Config defines task router configuration options.
type Config struct {
	// Enabled determines if URL → CUA routing is active
	Enabled bool `json:"enabled"`

	// Mode selects routing strategy (default: event-triggered)
	Mode ModeType `json:"mode"`

	// PollIntervalSecs only used when Mode == polling
	PollIntervalSecs int `json:"poll_interval_secs"`

	// BlockedHosts overrides environment variable list
	BlockedHosts []string `json:"blocked_hosts,omitempty"`

	// AllowedHosts explicit whitelist (if non-empty, blocks everything else)
	AllowedHosts []string `json:"allowed_hosts,omitempty"`

	// ForceUIAutomation forces UI Automation mode even on non-Windows
	ForceUIAutomation bool `json:"force_uiautomation,omitempty"`

	// AutoConfirmURLs automatically executes CUA without user confirmation
	AutoConfirmURLs bool `json:"auto_confirm_urls,omitempty"`

	// LogPath path to store routing audit log
	LogPath string `json:"log_path,omitempty"`
}

// DefaultConfig returns standard settings.
func DefaultConfig() *Config {
	return &Config{
		Enabled: true,
		Mode:    EventTriggerMode, // Most efficient by default
	}
}

// LoadFromFile reads config from JSON file.
func (c *Config) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, c)
}

// SaveToFile writes config to JSON file.
func (c *Config) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Validate checks config sanity.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	switch c.Mode {
	case EventTriggerMode, PollingMode, BackgroundMode:
		// Valid modes
	case "":
		c.Mode = EventTriggerMode
	default:
		return invalidModeError(c.Mode)
	}

	if c.PollIntervalSecs > 0 && c.Mode == PollingMode && c.PollIntervalSecs < 5 {
		return tooShortPollIntervalError(c.PollIntervalSecs)
	}

	return nil
}

// ToEnvironmentVariables converts config to env vars.
func (c *Config) ToEnvironmentVariables() map[string]string {
	env := map[string]string{
		"GLEAM_CUA_ENABLED":            boolToString(c.Enabled),
		"GLEAM_CUA_MODE":               string(c.Mode),
		"GLEAM_CUA_POLL_INTERVAL_SECS": intToStr(c.PollIntervalSecs),
		"GLEAM_CUA_FORCE_UIAUTOMATION": boolToString(c.ForceUIAutomation),
		"GLEAM_CUA_AUTO_CONFIRM_URLS":  boolToString(c.AutoConfirmURLs),
	}

	if len(c.BlockedHosts) > 0 {
		env["GLEAM_CUA_BLOCKED_DOMAINS"] = joinStrings(c.BlockedHosts, ",")
	}

	if len(c.AllowedHosts) > 0 {
		env["GLEAM_CUA_ALLOWED_DOMAINS"] = joinStrings(c.AllowedHosts, ",")
	}

	return env
}

// Helper functions for type conversions
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func intToStr(i int) string {
	if i == 0 {
		return ""
	}
	return string(rune(i + '0'))
}

func joinStrings(s []string, sep string) string {
	result := ""
	for i, s := range s {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// Error helpers
func invalidModeError(mode ModeType) error {
	return os.ErrInvalid
}

func tooShortPollIntervalError(seconds int) error {
	return os.ErrPermission
}
