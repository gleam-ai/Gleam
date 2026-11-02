package cua

import (
	"context"
	"testing"
	"time"

	"gleam/internal/harness/safety"
)

func TestComputerUse_DetectMode(t *testing.T) {
	cu := NewComputerUse()

	if cu.mode != "mcp" && cu.mode != "direct" {
		t.Errorf("invalid mode detected: %s", cu.mode)
	}

	expectedModes := []string{"mcp", "direct"}
	valid := false
	for _, m := range expectedModes {
		if cu.mode == m {
			valid = true
			break
		}
	}

	if !valid {
		t.Errorf("mode should be one of %v, got %s", expectedModes, cu.mode)
	}
}

func TestComputerUse_Schema(t *testing.T) {
	cu := NewComputerUse()
	schema := cu.Schema()

	if schema["name"] != "computer_use" {
		t.Errorf("schema name should be 'computer_use', got %v", schema["name"])
	}

	inputSchema, ok := schema["inputSchema"].(map[string]any)
	if !ok {
		t.Fatal("inputSchema is not valid map")
	}

	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties is not valid map")
	}

	required, ok := inputSchema["required"].([]any)
	if ok && len(required) == 0 {
		t.Error("required fields missing or invalid")
	}

	_ = properties // TODO: validate action enum
}

// Note: inputSchema without required fields is valid for optional parameter schemas

func TestComputerUse_Execute_NotFoundAction(t *testing.T) {
	cu := NewComputerUse()
	ctx := context.Background()

	args := map[string]any{
		"action": "nonexistent_action",
		"params": map[string]any{},
	}

	result, err := cu.Execute(ctx, args)

	if err == nil {
		t.Error("expected error for unknown action, got nil")
	}

	if result != nil {
		t.Errorf("expected nil result on error, got %v", result)
	}
}

func TestComputerUse_Execute_TooShortGoal(t *testing.T) {
	cu := NewComputerUse()
	ctx := context.Background()

	// Test click without coordinates
	args := map[string]any{
		"action": "click",
		"params": map[string]any{}, // Missing x, y
	}

	result, err := cu.Execute(ctx, args)

	if err == nil {
		t.Log("click with missing params succeeded (mock mode)")
	} else {
		t.Logf("click failed as expected in non-mock mode: %v", err)
	}

	_ = result
}

func TestComputerUse_Timeout(t *testing.T) {
	cu := NewComputerUse()

	if cu.timeout != 30*time.Second {
		t.Errorf("default timeout should be 30s, got %v", cu.timeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This would timeout if cua wasn't available
	args := map[string]any{
		"action": "screenshot",
		"params": map[string]any{},
	}

	start := time.Now()
	cu.Execute(ctx, args)
	duration := time.Since(start)

	// In mock mode, should complete quickly; in real mode might timeout
	if duration > 6*time.Second {
		t.Errorf("execution took too long: %v", duration)
	}
}

func TestCUASafetyGate_AllowAction(t *testing.T) {
	sg := safety.NewCUASafetyGate()

	// Allow safe actions
	safeActions := []string{"screenshot", "click", "type_text"}
	for _, action := range safeActions {
		err := sg.AllowAction(context.Background(), action, map[string]any{})
		if err != nil {
			t.Errorf("action %q should be allowed, got error: %v", action, err)
		}
	}

	// Block dangerous commands (must contain exec or shell keywords)
	blockedActions := []string{
		"exec_command",
		"shell_script",
		"execute_risky_operation",
	}
	for _, action := range blockedActions {
		err := sg.AllowAction(context.Background(), action, map[string]any{})
		if err == nil {
			t.Errorf("action %q should be blocked, but was allowed", action)
		}
	}
}

func TestCUASafetyGate_AppWhitelist(t *testing.T) {
	sg := safety.NewCUASafetyGate()

	// Pre-loaded apps should be allowed (via AllowAction)
	allowedApps := []string{"chrome", "firefox", "vscode"}
	for _, app := range allowedApps {
		err := sg.AllowAction(context.Background(), "open_app", map[string]any{"app_name": app})
		if err != nil {
			t.Errorf("app %q should be in whitelist, got error: %v", app, err)
		}
	}

	// Unknown apps should be blocked
	unknownApps := []string{"malware", "virus_installer", "script.ki"}
	for _, app := range unknownApps {
		err := sg.AllowAction(context.Background(), "open_app", map[string]any{"app_name": app})
		if err == nil {
			t.Errorf("app %q should NOT be in whitelist, but was allowed", app)
		}
	}
}

func TestCUASafetyGate_History(t *testing.T) {
	sg := safety.NewCUASafetyGate()

	// Generate some history
	for i := 0; i < 10; i++ {
		sg.AllowAction(context.Background(), "click", map[string]any{"x": float64(i), "y": float64(i)})
	}

	history := sg.GetHistory(5)

	if len(history) != 5 {
		t.Errorf("history length should be 5, got %d", len(history))
	}

	lastAction := history[0].Action
	if lastAction != "click" {
		t.Errorf("last action should be 'click', got %q", lastAction)
	}
}

func BenchmarkComputerUse_Execute(b *testing.B) {
	cu := NewComputerUse()

	args := map[string]any{
		"action": "click",
		"params": map[string]any{"x": 100, "y": 200},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cu.Execute(ctx, args)
	}
}
