package hardware_test

import (
	"context"
	"testing"

	"gleam/internal/tools/hardware"
)

func TestRegisterAndList(t *testing.T) {
	devices := hardware.List()
	if len(devices) < 2 {
		t.Fatalf("expected at least 2 devices, got %d", len(devices))
	}

	// Check xiaohong is registered
	var foundXiaohong, foundA2 bool
	for _, d := range devices {
		if d.ID() == "xiaohong" {
			foundXiaohong = true
			if d.Name() != "AtomGit 小鸿" {
				t.Errorf("xiaohong name = %q, want %q", d.Name(), "AtomGit 小鸿")
			}
		}
		if d.ID() == "qianwen_a2" {
			foundA2 = true
			if d.Name() != "千问办公录音卡 A2" {
				t.Errorf("qianwen_a2 name = %q, want %q", d.Name(), "千问办公录音卡 A2")
			}
		}
	}
	if !foundXiaohong {
		t.Error("xiaohong not registered")
	}
	if !foundA2 {
		t.Error("qianwen_a2 not registered")
	}
}

func TestConnectAndDisconnect(t *testing.T) {
	ctx := context.Background()

	// Connect xiaohong
	if err := hardware.Connect(ctx, "xiaohong", map[string]any{
		"base_url": "http://localhost:8080",
	}); err != nil {
		t.Fatalf("connect xiaohong: %v", err)
	}

	// Verify instance exists
	d, ok := hardware.GetInstance("xiaohong")
	if !ok {
		t.Fatal("xiaohong instance not found")
	}
	if d.State() != hardware.StateConnected {
		t.Errorf("state = %v, want %v", d.State(), hardware.StateConnected)
	}

	// Disconnect
	if err := hardware.Disconnect(ctx, "xiaohong"); err != nil {
		t.Fatalf("disconnect xiaohong: %v", err)
	}

	// Verify instance removed
	_, ok = hardware.GetInstance("xiaohong")
	if ok {
		t.Error("xiaohong instance still exists after disconnect")
	}
}

func TestSendReceive(t *testing.T) {
	ctx := context.Background()

	// Connect
	if err := hardware.Connect(ctx, "xiaohong", nil); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer hardware.Disconnect(ctx, "xiaohong")

	// Send command
	d, _ := hardware.GetInstance("xiaohong")
	result, err := d.Send(ctx, "display_text", map[string]any{"text": "Hello"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if result == nil {
		t.Error("send returned nil")
	}

	// Receive
	data, err := d.Receive(ctx)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if data == nil {
		t.Error("receive returned nil")
	}
}

func TestInfo(t *testing.T) {
	ctx := context.Background()

	if err := hardware.Connect(ctx, "qianwen_a2", nil); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer hardware.Disconnect(ctx, "qianwen_a2")

	d, _ := hardware.GetInstance("qianwen_a2")
	info := d.Info(ctx)
	if info["firmware"] == nil {
		t.Error("info missing firmware")
	}
}
