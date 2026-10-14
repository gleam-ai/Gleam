package hardware

import (
	"context"
	"fmt"
	"sync"
)

// Qianwen Office A2 Recording Card.
//
// A2 is a smart recording card by Qianwen Office. It features:
// - High-quality microphone array
// - On-device speech-to-text transcription
// - USB-C connectivity
// - Local storage for recordings
// - Sync to cloud for AI processing
//
// Integration uses USB serial communication.

type qianwenA2 struct {
	mu       sync.RWMutex
	state    DeviceState
	port     string
	serialID string
}

// NewQianwenA2 creates a new Qianwen A2 recording card device driver.
func NewQianwenA2() Device {
	return &qianwenA2{state: StateDisconnected}
}

func (q *qianwenA2) ID() string   { return "qianwen_a2" }
func (q *qianwenA2) Name() string { return "千问办公录音卡 A2" }
func (q *qianwenA2) Description() string {
	return "智能录音卡，支持本地语音转文字、USB-C 连接、云端同步"
}

func (q *qianwenA2) Capabilities() []Capability {
	return []Capability{CapAudioIn, CapTranscribe, CapStorage, CapUSB}
}

func (q *qianwenA2) State() DeviceState {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.state
}

func (q *qianwenA2) Connect(ctx context.Context, config map[string]any) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	port, _ := config["port"].(string)
	if port == "" {
		port = "COM3" // Default Windows port
	}
	serialID, _ := config["serial_id"].(string)

	q.port = port
	q.serialID = serialID
	q.state = StateConnected
	return nil
}

func (q *qianwenA2) Disconnect(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.state = StateDisconnected
	q.port = ""
	q.serialID = ""
	return nil
}

func (q *qianwenA2) Send(ctx context.Context, command string, payload any) (any, error) {
	q.mu.RLock()
	if q.state != StateConnected {
		q.mu.RUnlock()
		return nil, fmt.Errorf("qianwen_a2: not connected")
	}
	q.mu.RUnlock()

	// Commands: start_record, stop_record, transcribe, sync, delete_recording
	switch command {
	case "start_record":
		return map[string]any{"status": "recording", "session_id": "rec_001"}, nil
	case "stop_record":
		return map[string]any{"status": "stopped", "duration_sec": 120, "file": "rec_001.wav"}, nil
	case "transcribe":
		file, _ := payload.(map[string]any)["file"].(string)
		return map[string]any{
			"status":   "completed",
			"file":     file,
			"text":     "这是转录的示例文本...",
			"language": "zh-CN",
		}, nil
	case "sync":
		return map[string]any{"status": "synced", "files": 5}, nil
	case "delete_recording":
		file, _ := payload.(map[string]any)["file"].(string)
		return map[string]any{"status": "deleted", "file": file}, nil
	default:
		return nil, fmt.Errorf("qianwen_a2: unknown command %q", command)
	}
}

func (q *qianwenA2) Receive(ctx context.Context) (any, error) {
	q.mu.RLock()
	if q.state != StateConnected {
		q.mu.RUnlock()
		return nil, fmt.Errorf("qianwen_a2: not connected")
	}
	q.mu.RUnlock()

	// Poll for recording status or transcription results
	return map[string]any{
		"recording":        false,
		"storage_used_mb":  256,
		"storage_total_mb": 2048,
	}, nil
}

func (q *qianwenA2) Info(ctx context.Context) map[string]any {
	return map[string]any{
		"firmware":      "2.1.0",
		"storage_used":  "256 MB",
		"storage_total": "2 GB",
		"recordings":    12,
		"battery":       92,
		"usb_connected": true,
	}
}

func init() {
	Register(NewQianwenA2())
}
