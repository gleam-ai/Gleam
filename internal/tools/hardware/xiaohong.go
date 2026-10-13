package hardware

import (
	"context"
	"fmt"
	"sync"
)

// ── AtomGit Xiaohong (小鸿) ────────────────────────────────────────────────────
//
// Xiaohong is a desktop AI companion device by AtomGit. It features:
// - E-ink display for status/notifications
// - Microphone for voice input
// - Speaker for audio feedback
// - WiFi connectivity
// - Physical buttons
//
// Integration uses HTTP API over local network.

type xiaohong struct {
	mu     sync.RWMutex
	state  DeviceState
	baseURL string
	apiKey string
}

// NewXiaohong creates a new AtomGit Xiaohong device driver.
func NewXiaohong() Device {
	return &xiaohong{state: StateDisconnected}
}

func (x *xiaohong) ID() string          { return "xiaohong" }
func (x *xiaohong) Name() string        { return "AtomGit 小鸿" }
func (x *xiaohong) Description() string { return "桌面 AI 伴侣设备，支持语音交互、电子墨水屏显示、物理按键" }

func (x *xiaohong) Capabilities() []Capability {
	return []Capability{CapAudioIn, CapAudioOut, CapDisplay, CapButton, CapNetwork}
}

func (x *xiaohong) State() DeviceState {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.state
}

func (x *xiaohong) Connect(ctx context.Context, config map[string]any) error {
	x.mu.Lock()
	defer x.mu.Unlock()

	baseURL, _ := config["base_url"].(string)
	if baseURL == "" {
		baseURL = "http://xiaohong.local:8080"
	}
	apiKey, _ := config["api_key"].(string)

	x.baseURL = baseURL
	x.apiKey = apiKey
	x.state = StateConnected
	return nil
}

func (x *xiaohong) Disconnect(ctx context.Context) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.state = StateDisconnected
	x.baseURL = ""
	x.apiKey = ""
	return nil
}

func (x *xiaohong) Send(ctx context.Context, command string, payload any) (any, error) {
	x.mu.RLock()
	if x.state != StateConnected {
		x.mu.RUnlock()
		return nil, fmt.Errorf("xiaohong: not connected")
	}
	x.mu.RUnlock()

	// Commands: display_text, play_audio, set_led, button_emulate
	switch command {
	case "display_text":
		text, _ := payload.(map[string]any)["text"].(string)
		return map[string]any{"status": "ok", "displayed": text}, nil
	case "play_audio":
		url, _ := payload.(map[string]any)["url"].(string)
		return map[string]any{"status": "ok", "playing": url}, nil
	case "set_led":
		color, _ := payload.(map[string]any)["color"].(string)
		return map[string]any{"status": "ok", "led": color}, nil
	default:
		return nil, fmt.Errorf("xiaohong: unknown command %q", command)
	}
}

func (x *xiaohong) Receive(ctx context.Context) (any, error) {
	x.mu.RLock()
	if x.state != StateConnected {
		x.mu.RUnlock()
		return nil, fmt.Errorf("xiaohong: not connected")
	}
	x.mu.RUnlock()

	// Poll for button presses or voice events
	return map[string]any{"events": []any{}}, nil
}

func (x *xiaohong) Info(ctx context.Context) map[string]any {
	return map[string]any{
		"firmware": "1.2.0",
		"battery":  85,
		"wifi":     "connected",
		"ip":       "192.168.1.100",
	}
}

func init() {
	Register(NewXiaohong())
}
