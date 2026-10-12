package safety

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gleam/internal/atomicfile"
)

type PendingApproval struct {
	TaskID string    `json:"task_id"`
	StepID string    `json:"step_id"`
	Tool   string    `json:"tool"`
	Risk   string    `json:"risk"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func (g *Gate) SetPendingPath(p string) {
	if g == nil {
		return
	}
	g.pendingMu.Lock()
	g.pendingPath = p
	g.pendingMu.Unlock()
}

func (g *Gate) MarkPending(e PendingApproval) {
	if g == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	for i := range g.pending {
		if g.pending[i].TaskID == e.TaskID && g.pending[i].StepID == e.StepID {
			g.pending[i] = e
			g.savePendingLocked()
			return
		}
	}
	g.pending = append(g.pending, e)
	g.savePendingLocked()
}

func (g *Gate) ClearPending(taskID, stepID string) {
	if g == nil {
		return
	}
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	out := g.pending[:0]
	for _, p := range g.pending {
		if p.TaskID == taskID && p.StepID == stepID {
			continue
		}
		out = append(out, p)
	}
	if len(out) == len(g.pending) {
		return
	}
	g.pending = out
	g.savePendingLocked()
}

func (g *Gate) PendingApprovals() []PendingApproval {
	if g == nil {
		return nil
	}
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	out := append([]PendingApproval(nil), g.pending...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func LoadPendingApprovals(path string) ([]PendingApproval, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []PendingApproval
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, nil
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

func ClearPendingFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (g *Gate) savePendingLocked() {
	if g.pendingPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(g.pendingPath), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(g.pending, "", " ")
	if err != nil {
		return
	}
	_ = atomicfile.Write(g.pendingPath, data, 0o644)
}
