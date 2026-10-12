package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gleam/pkg/types"
)

type RunLog struct {
	mu  sync.Mutex
	dir string
}

func NewRunLog(dataDir string) *RunLog {
	return &RunLog{dir: filepath.Join(dataDir, "runs")}
}

func (l *RunLog) RecordStep(taskID string, r types.StepResult) {
	if l == nil {
		return
	}
	name := SafeTaskName(taskID)
	if name == "" {
		return
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(l.dir, name+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func SafeTaskName(taskID string) string {
	if taskID == "" || len(taskID) > 128 {
		return ""
	}
	for _, r := range taskID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return ""
		}
	}
	return taskID
}

func RunLogPath(dataDir, taskID string) string {
	name := SafeTaskName(taskID)
	if name == "" {
		return ""
	}
	return filepath.Join(dataDir, "runs", name+".jsonl")
}

func ReadRunLog(dataDir, taskID string) ([]types.StepResult, error) {
	path := RunLogPath(dataDir, taskID)
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []types.StepResult
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r types.StepResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func DiscardRunLog(dataDir, taskID string) {
	path := RunLogPath(dataDir, taskID)
	if path == "" {
		return
	}
	_ = os.Remove(path)
}
