package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gleam/internal/atomicfile"
)

type CueSuppression struct {
	Fingerprint string `json:"fingerprint"`
	Signal      string `json:"signal"`
	At          string `json:"at"`
	Reason      string `json:"reason"`
}

const (
	cueDismissed = "dismissed"
	cueAdopted   = "adopted"
)

type cueStore struct {
	mu   sync.Mutex
	path string
	Sup  []CueSuppression

	lastErr error
}

func cueStorePath(dataDir string) string {
	return filepath.Join(dataDir, "cues.json")
}

func loadCueStore(dataDir string) *cueStore {
	st := &cueStore{path: cueStorePath(dataDir)}
	b, err := os.ReadFile(st.path)
	if err != nil {
		if !os.IsNotExist(err) {
			st.lastErr = err
		}
		return st
	}
	var rows []CueSuppression
	if err := json.Unmarshal(b, &rows); err != nil {
		st.lastErr = err
		return st
	}
	st.Sup = rows
	return st
}

func (s *cueStore) muted(fp string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.Sup {
		if x.Fingerprint == fp {
			return true
		}
	}
	return false
}

func (s *cueStore) list(en bool, cards []CueRow) []CueSuppressedRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	byFp := make(map[string]CueRow, len(cards))
	for _, c := range cards {
		byFp[c.ID] = c
	}
	out := make([]CueSuppressedRow, 0, len(s.Sup))
	for _, x := range s.Sup {
		row := CueSuppressedRow{
			CueSuppression: x,
			SignalText:     cueSignalText(x.Signal, en),
			ReasonText:     cueReasonText(x.Reason, en),
		}
		if c, ok := byFp[x.Fingerprint]; ok {
			row.Title = c.Title
			row.Goal = c.Goal
		} else {
			row.Title = bi{
				"这条候补目标已不再成立（历史变了，或归档已被裁剪）",
				"This cue no longer applies (history changed, or the archive was trimmed)",
			}.pick(en)
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

func cueReasonText(reason string, en bool) string {
	switch reason {
	case cueDismissed:
		return bi{"别再提", "Dismissed"}.pick(en)
	case cueAdopted:
		return bi{"已采纳过", "Already adopted"}.pick(en)
	}
	return reason
}

func (s *cueStore) note(en bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastErr == nil {
		return ""
	}
	return fmt.Sprintf(bi{
		"处置记录上一次没写下去（眼下的判断仍然生效，重启后可能重来）：%v",
		"The last suppression write failed (decisions made so far still apply; a restart may bring some of them back): %v",
	}.pick(en), s.lastErr)
}

func (s *cueStore) record(row CueRow, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]CueSuppression, 0, len(s.Sup)+1)
	for _, x := range s.Sup {
		if x.Fingerprint != row.ID {
			kept = append(kept, x)
		}
	}
	s.Sup = append(kept, CueSuppression{
		Fingerprint: row.ID,
		Signal:      row.Signal,
		At:          time.Now().Format(time.RFC3339),
		Reason:      reason,
	})
	return s.flushLocked()
}

func (s *cueStore) clear(fp string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]CueSuppression, 0, len(s.Sup))
	found := false
	for _, x := range s.Sup {
		if x.Fingerprint == fp {
			found = true
			continue
		}
		kept = append(kept, x)
	}
	s.Sup = kept
	if !found {
		return false, nil
	}
	return true, s.flushLocked()
}

func (s *cueStore) flushLocked() error {
	b, err := json.MarshalIndent(s.Sup, "", "  ")
	if err != nil {
		s.lastErr = err
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		s.lastErr = err
		return err
	}
	if err := atomicfile.Write(s.path, b, 0o600); err != nil {
		s.lastErr = err
		return err
	}
	s.lastErr = nil
	return nil
}

func (a *Agent) CueDismiss(id string) error { return a.cueRecord(id, cueDismissed) }

func (a *Agent) CueAdopt(id string) error { return a.cueRecord(id, cueAdopted) }

func (a *Agent) cueRecord(id, reason string) error {
	if id == "" {
		return fmt.Errorf("缺少候补目标编号")
	}
	for _, row := range a.cueRowsAll() {
		if row.ID == id {
			return loadCueStore(a.Cfg.DataDir).record(row, reason)
		}
	}
	return fmt.Errorf("这条候补目标已经不成立了（历史变了，或它已被裁剪）")
}

func (a *Agent) CueUnsuppress(fp string) error {
	if fp == "" {
		return fmt.Errorf("缺少处置记录编号")
	}
	st := loadCueStore(a.Cfg.DataDir)
	ok, err := st.clear(fp)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("没有这条处置记录")
	}
	return nil
}

func (a *Agent) cueRowsAll() []CueRow {
	results, _, err := ListTaskResults(a.Cfg.DataDir, cueTaskScan)
	if err != nil {
		return nil
	}
	rows := deriveCues(results, false)
	return rows
}
