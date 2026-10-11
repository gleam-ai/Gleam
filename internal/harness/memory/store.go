package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gleam/internal/atomicfile"
	"gleam/pkg/types"
)

type Store struct {
	mu       sync.Mutex
	path     string
	dim      int
	maxItems int
	items    []Item
	index    map[string]int
	dirty    bool
}

func OpenStore(path string, dim, maxItems int) (*Store, error) {
	if dim <= 0 {
		dim = 256
	}
	if maxItems <= 0 {
		maxItems = 5000
	}
	s := &Store{path: path, dim: dim, maxItems: maxItems, index: map[string]int{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(data, &items); err != nil {
		_ = os.Rename(path, path+".corrupt")
		return s, nil
	}
	for _, it := range items {
		s.appendItem(it)
	}
	return s, nil
}

func (s *Store) appendItem(it Item) {
	if _, exists := s.index[it.ID]; exists {
		return
	}
	s.index[it.ID] = len(s.items)
	s.items = append(s.items, it)
}

func (s *Store) Remember(content string, tags []string) (string, error) {
	return s.RememberWithSource(content, "", tags)
}

func (s *Store) RememberWithSource(content, source string, tags []string) (string, error) {
	return s.RememberScoped(content, source, "", tags)
}

func (s *Store) RememberScoped(content, source, scope string, tags []string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("记忆内容为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	newVec := embed(content, s.dim)
	now := time.Now()

	dupIdx := -1
	for i := range s.items {
		if s.items[i].Deleted {
			continue
		}
		if s.items[i].Scope != scope {
			continue
		}
		if cosine(newVec, s.items[i].Vec) >= DupThreshold {
			dupIdx = i
			break
		}
	}
	if dupIdx >= 0 {
		it := &s.items[dupIdx]
		it.Content = content
		it.Tags = append([]string(nil), tags...)
		it.CreatedAt = now
		it.Vec = newVec
		if source != "" {
			it.Source = source
		}
		s.dirty = true
		return it.ID, nil
	}

	id := types.NewID()
	it := Item{
		ID:        id,
		Content:   content,
		Tags:      append([]string(nil), tags...),
		CreatedAt: now,
		Vec:       newVec,
		Source:    source,
		Scope:     scope,
	}
	if len(s.items) >= s.maxItems {
		oldest := 0
		for i := 1; i < len(s.items); i++ {
			if s.items[i].CreatedAt.Before(s.items[oldest].CreatedAt) {
				oldest = i
			}
		}
		delete(s.index, s.items[oldest].ID)
		s.items = append(s.items[:oldest], s.items[oldest+1:]...)
		s.reindex()
	}
	s.appendItem(it)
	s.dirty = true
	return id, nil
}

func (s *Store) Search(query string, k int) []Hit {
	return s.SearchScoped(query, k, "")
}

func (s *Store) SearchScoped(query string, k int, scope string) []Hit {
	s.mu.Lock()
	items := append([]Item(nil), s.items...)
	s.mu.Unlock()
	if scope != "" {
		kept := items[:0]
		for _, it := range items {
			if it.Scope == "" || it.Scope == scope {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	if k <= 0 {
		k = 5
	}
	return SearchHitsTop(items, query, k, s.dim)
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.index[id]
	if !ok {
		return false
	}
	s.items = append(s.items[:idx], s.items[idx+1:]...)
	delete(s.index, id)
	s.reindex()
	s.dirty = true
	return true
}

func (s *Store) SoftDelete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.index[id]
	if !ok || s.items[idx].Deleted {
		return false
	}
	s.items[idx].Deleted = true
	s.dirty = true
	return true
}

func (s *Store) DeletedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := range s.items {
		if s.items[i].Deleted {
			n++
		}
	}
	return n
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.items, "", " ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.path, data, 0o644); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
func (s *Store) reindex() {
	s.index = make(map[string]int, len(s.items))
	for i, it := range s.items {
		s.index[it.ID] = i
	}
}

type Manager struct {
	Short *ShortTerm
	Long  *Store

	mu           sync.Mutex
	overflow     []Turn
	summary      string
	savedTok     int
	pinned       []string
	omittedTurns int
	omittedRunes int
	dir          string
	carry        int
}

func (m *Manager) CarryTurns() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.carry <= 0 {
		return CarryFull
	}
	return m.carry
}

func (m *Manager) SetCarryTurns(n int) bool {
	if n < CarryTight {
		n = CarryTight
	}
	if n > CarryFull {
		n = CarryFull
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.carry
	if old <= 0 {
		old = CarryFull
	}
	if old == n {
		return false
	}
	m.carry = n
	return true
}

const pinnedCap = 20

var pinHints = []string{"记住", "约定", "以后", "规则是", "必须", "始终", "一直用", "不要用"}

func (m *Manager) Pin(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pinned {
		if p == text {
			return
		}
	}
	m.pinned = append(m.pinned, text)
	if len(m.pinned) > pinnedCap {
		m.pinned = m.pinned[len(m.pinned)-pinnedCap:]
	}
	m.saveContextLocked()
}

func (m *Manager) Pinned() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.pinned...)
}

func (m *Manager) Omission() (turns, runes int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.omittedTurns, m.omittedRunes
}

func (m *Manager) SummaryAnnotated() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(m.summary) == "" {
		return ""
	}
	return fmt.Sprintf("%s\n（已自动压缩：累计省略 %d 轮 / 约 %d 字）", m.summary, m.omittedTurns, m.omittedRunes)
}

const overflowCap = 100

const summaryCap = 2000

const (
	CarryFull  = 6
	CarryTight = 2
)

const contextFile = "context.json"

type contextSnapshot struct {
	Overflow     []Turn   `json:"overflow"`
	Summary      string   `json:"summary"`
	SavedToken   int      `json:"saved_tokens"`
	Pinned       []string `json:"pinned,omitempty"`
	OmittedTurns int      `json:"omitted_turns,omitempty"`
	OmittedRunes int      `json:"omitted_runes,omitempty"`
}

func Open(dataDir string, shortCap, dim, maxItems int) (*Manager, error) {
	dir := filepath.Join(dataDir, "memory")
	long, err := OpenStore(filepath.Join(dir, "longterm.json"), dim, maxItems)
	if err != nil {
		return nil, err
	}
	m := &Manager{Short: NewShortTerm(shortCap), Long: long, dir: dir}
	if data, err := os.ReadFile(filepath.Join(dir, contextFile)); err == nil {
		var snap contextSnapshot
		if json.Unmarshal(data, &snap) == nil {
			m.overflow = snap.Overflow
			m.summary = snap.Summary
			m.savedTok = snap.SavedToken
			m.pinned = snap.Pinned
			m.omittedTurns = snap.OmittedTurns
			m.omittedRunes = snap.OmittedRunes
		}
	}
	return m, nil
}

func (m *Manager) AddTurn(role, content string) {
	if role == "user" && hitsAny(content, pinHints) {
		m.Pin(content)
	}
	m.mu.Lock()
	if m.Short.Len() >= m.Short.cap {
		if old, ok := m.Short.Oldest(); ok {
			m.overflow = append(m.overflow, old)
			if len(m.overflow) > overflowCap {
				m.overflow = m.overflow[1:]
			}
		}
	}
	m.mu.Unlock()
	m.Short.Add(role, content)
}

func hitsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func (m *Manager) Compress(summarize func(overflow string) (string, error)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.overflow) == 0 {
		return false
	}
	turns := m.overflow
	text := renderTurns(turns)
	m.overflow = nil
	m.omittedTurns += len(turns)
	m.omittedRunes += len([]rune(text))

	summary := ""
	if summarize != nil {
		if s, err := summarize(text); err == nil && len([]rune(strings.TrimSpace(s))) >= 4 {
			summary = strings.TrimSpace(s)
		}
	}
	if summary == "" {
		summary = extractiveSummary(text)
	}

	raw := 0
	for _, t := range turns {
		raw += types.EstimateTokens(t.Content)
	}
	m.savedTok += maxInt(0, raw-types.EstimateTokens(summary))

	prevLen := len([]rune(m.summary))
	m.summary = strings.TrimSpace(m.summary + "\n" + summary)
	if r := []rune(m.summary); len(r) > summaryCap {
		m.summary = string(r[len(r)-summaryCap:])
		m.omittedRunes += len(r) - summaryCap
	} else {
		m.omittedRunes += maxInt(0, prevLen+len([]rune(summary))-len([]rune(m.summary)))
	}
	m.saveContextLocked()
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Manager) Summary() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.summary
}

type ContextStats struct {
	ShortTurns   int    `json:"short_turns"`
	ShortCap     int    `json:"short_cap"`
	FillPct      int    `json:"fill_pct"`
	Overflow     int    `json:"overflow"`
	SummaryRunes int    `json:"summary_chars"`
	Summary      string `json:"summary"`
	SavedTokens  int    `json:"saved_tokens"`
}

func fillPct(turns, capacity int) int {
	if capacity <= 0 {
		return 0
	}
	if p := 100 * turns / capacity; p < 100 {
		return p
	}
	return 100
}

func (m *Manager) Stats() ContextStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ContextStats{
		ShortTurns:   m.Short.Len(),
		ShortCap:     m.Short.cap,
		FillPct:      fillPct(m.Short.Len(), m.Short.cap),
		Overflow:     len(m.overflow),
		SummaryRunes: len([]rune(m.summary)),
		Summary:      m.summary,
		SavedTokens:  m.savedTok,
	}
}

func (m *Manager) ClearSummary() {
	m.mu.Lock()
	m.overflow = nil
	m.summary = ""
	m.savedTok = 0
	m.pinned = nil
	if m.dir != "" {
		_ = os.Remove(filepath.Join(m.dir, contextFile))
	}
	m.mu.Unlock()
}

func (m *Manager) ResetConversation() {
	if m.Short != nil {
		m.Short.Reset()
	}
	m.ClearSummary()
	m.mu.Lock()
	m.carry = CarryFull
	m.mu.Unlock()
}

func (m *Manager) saveContextLocked() {
	if m.dir == "" {
		return
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(contextSnapshot{
		Overflow: m.overflow, Summary: m.summary, SavedToken: m.savedTok,
		Pinned: m.pinned, OmittedTurns: m.omittedTurns, OmittedRunes: m.omittedRunes,
	})
	if err != nil {
		return
	}
	path := filepath.Join(m.dir, contextFile)
	_ = atomicfile.Write(path, data, 0o644)
}

func renderTurns(turns []Turn) string {
	var b strings.Builder
	for _, t := range turns {
		fmt.Fprintf(&b, "%s: %s\n", t.Role, t.Content)
	}
	return b.String()
}

func extractiveSummary(text string) string {
	var parts []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := []rune(line)
		if len(r) > 80 {
			r = r[:80]
		}
		parts = append(parts, string(r))
		if len(parts) >= 8 {
			break
		}
	}
	return "历史要点：" + strings.Join(parts, " / ")
}

func (m *Manager) Remember(content string, tags []string) (string, error) {
	return m.RememberWithSource(content, "", tags)
}

func (m *Manager) RememberScoped(content, source, scope string, tags []string) (string, error) {
	id, err := m.Long.RememberScoped(content, source, scope, tags)
	if err != nil {
		return "", err
	}
	if err := m.Long.Flush(); err != nil {
		return id, err
	}
	return id, nil
}

func (m *Manager) SearchScoped(query string, k int, scope string) []Hit {
	return m.Long.SearchScoped(query, k, scope)
}

func (m *Manager) RememberWithSource(content, source string, tags []string) (string, error) {
	id, err := m.Long.RememberWithSource(content, source, tags)
	if err != nil {
		return "", err
	}
	if err := m.Long.Flush(); err != nil {
		return id, err
	}
	return id, nil
}

func (m *Manager) Relevant(query string, k int) []Hit {
	return m.Long.Search(query, k)
}

func (m *Manager) RelevantScoped(query string, k int, scope string) []Hit {
	return m.Long.SearchScoped(query, k, scope)
}

func (m *Manager) Close() error {
	return m.Long.Flush()
}
