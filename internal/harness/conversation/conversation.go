package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/atomicfile"
	"gleam/pkg/types"
)

var (
	ErrInvalidID = errors.New("非法会话 ID")
	ErrNotFound  = errors.New("会话不存在")
)

type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	TaskID    string    `json:"task_id,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	SpaceID   string    `json:"space_id,omitempty"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	SpaceID   string    `json:"space_id,omitempty"`
	Preview   string    `json:"preview"`
	Count     int       `json:"count"`
	UpdatedAt time.Time `json:"updated_at"`
}

const maxTitleRunes = 24

const DefaultSpaceID = "default"

// searchIndex is a lightweight in-memory index for full-text search.
// It's optional and used to avoid rescanning the entire conversation list on every search.
type searchIndex struct {
	mu       sync.RWMutex
	convoMap map[string]*Conversation  // id -> latest conversation snapshot
	wordConv map[string]map[string]int // lowercase word -> {convoID: hitCount}
}

func newSearchIndex() *searchIndex {
	return &searchIndex{
		convoMap: make(map[string]*Conversation),
		wordConv: make(map[string]map[string]int),
	}
}

// addOrUpdate inserts or updates a conversation in the index.
func (idx *searchIndex) addOrUpdate(c *Conversation) {
	if idx == nil {
		return // nil-safe guard for tests that don't use buildIndex
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Index all text (title + content)
	text := strings.ToLower(c.Title)
	for _, m := range c.Messages {
		text += "\n" + strings.ToLower(m.Content)
	}

	// Remove old word mappings for this convo
	for word := range idx.wordConv {
		if _, exists := idx.wordConv[word][c.ID]; exists {
			delete(idx.wordConv[word], c.ID)
			if len(idx.wordConv[word]) == 0 {
				delete(idx.wordConv, word)
			}
		}
	}

	// Build new mappings
	words := strings.Fields(text)
	for _, word := range words {
		if len(word) < 2 {
			continue
		}
		if idx.wordConv[word] == nil {
			idx.wordConv[word] = make(map[string]int)
		}
		idx.wordConv[word][c.ID]++
	}

	idx.convoMap[c.ID] = c
}

// getConvo returns a copy of the conversation with given id.
func (idx *searchIndex) getConvo(id string) (*Conversation, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	c, ok := idx.convoMap[id]
	if !ok {
		return nil, false
	}
	// Return a shallow copy (messages slice is immutable after creation)
	return c, true
}

type Store struct {
	dir   string
	mu    sync.Mutex
	index *searchIndex

	// Batch write optimization
	batchBuf   []Message   // 批量写入缓冲区
	batchID    string      // 当前批次所属的会话 ID
	batchTimer *time.Timer // 定时刷盘定时器
	batchFlush chan string // 触发刷盘的通道 (buf size = 1)
}

func Open(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建会话目录失败: %w", err)
	}
	s := &Store{dir: dir}
	if err := s.migrateLegacy(); err != nil {
		return nil, fmt.Errorf("迁移历史会话失败: %w", err)
	}
	return s, nil
}

// buildIndex rebuilds the in-memory index from all saved conversations.
func (s *Store) buildIndex() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		c, err := s.readLocked(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		s.index.addOrUpdate(c)
	}
	return nil
}

// refreshIndex updates the in-memory index for a specific conversation after read/write.
func (s *Store) refreshIndex(id string) error {
	c, err := s.Get(id)
	if err != nil {
		return err
	}
	s.index.addOrUpdate(c)
	return nil
}

func (s *Store) migrateLegacy() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var c Conversation
		if err := json.Unmarshal(data, &c); err != nil || strings.TrimSpace(c.SpaceID) != "" {
			continue
		}
		c.SpaceID = DefaultSpaceID
		out, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			continue
		}
		_ = atomicfile.Write(path, out, 0o644)
	}
	return nil
}

func (s *Store) path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || id != strings.Map(safeRune, id) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.dir, id+".json"), nil
}

func safeRune(r rune) rune {
	if (r >= 'a' && r <= 'f') || (r >= '0' && r <= '9') {
		return r
	}
	return -1
}

// NewImportID generates a unique conversation ID for imported conversations.
// Uses a timestamp-based approach to avoid collisions with existing IDs.
func NewImportID(title string) string {
	base := strings.TrimSpace(strings.ReplaceAll(title, " ", "-"))
	if base == "" {
		base = "imported"
	}
	// Sanitize and ensure valid format
	id := strings.Map(safeRune, base)
	if id == "" {
		id = fmt.Sprintf("import-%d", time.Now().UnixNano()%100000)
	}
	return types.NewID()
}

// writeConversationLocked writes a conversation without acquiring the lock.
// It assumes the caller already holds s.mu.
func (s *Store) writeConversationLocked(id string, c *Conversation) error {
	p := filepath.Join(s.dir, id+".json")
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("编码会话失败：%w", err)
	}
	return atomicfile.Write(p, b, 0o644)
}

func (s *Store) Create(spaceIDs ...string) (*Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spaceID := DefaultSpaceID
	if len(spaceIDs) > 0 && strings.TrimSpace(spaceIDs[0]) != "" {
		spaceID = strings.TrimSpace(spaceIDs[0])
	}
	now := time.Now()
	c := &Conversation{ID: types.NewID(), Title: "新对话", SpaceID: spaceID, CreatedAt: now, UpdatedAt: now}
	if err := s.writeLocked(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) Get(id string) (*Conversation, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(p)
}

func (s *Store) List(spaceIDs ...string) ([]Summary, error) {
	filter := ""
	if len(spaceIDs) > 0 {
		filter = strings.TrimSpace(spaceIDs[0])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("读取会话目录失败: %w", err)
	}
	var out []Summary
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		c, err := s.readLocked(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		sid := c.SpaceID
		if sid == "" {
			sid = DefaultSpaceID
		}
		if filter != "" && sid != filter {
			continue
		}
		summary := summarize(c)
		summary.SpaceID = sid
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *Store) CountBySpace() (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		c, err := s.readLocked(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		sid := c.SpaceID
		if sid == "" {
			sid = DefaultSpaceID
		}
		counts[sid]++
	}
	return counts, nil
}

func (s *Store) ReassignSpace(fromID, toID string) (int, error) {
	if strings.TrimSpace(toID) == "" {
		toID = DefaultSpaceID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p := filepath.Join(s.dir, e.Name())
		c, err := s.readLocked(p)
		if err != nil {
			continue
		}
		sid := c.SpaceID
		if sid == "" {
			sid = DefaultSpaceID
		}
		if sid != fromID {
			continue
		}
		c.SpaceID = toID
		c.UpdatedAt = time.Now()
		if err := s.writeLocked(c); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

func (s *Store) Append(id string, msg Message) (*Conversation, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readLocked(p)
	if err != nil {
		return nil, err
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	c.Messages = append(c.Messages, msg)
	c.UpdatedAt = msg.CreatedAt
	if (c.Title == "" || c.Title == "新对话") && msg.Role == "user" {
		c.Title = titleFrom(msg.Content)
	}
	if err := s.writeLocked(c); err != nil {
		return nil, err
	}
	// Update in-memory index for faster searches
	s.index.addOrUpdate(c)
	return c, nil
}

func (s *Store) Rename(id, title string) (*Conversation, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	title = strings.TrimSpace(strings.ReplaceAll(title, "\n", " "))
	if title == "" {
		title = "未命名对话"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.readLocked(p)
	if err != nil {
		return nil, err
	}
	c.Title = truncate(title, maxTitleRunes)
	c.UpdatedAt = time.Now()
	if err := s.writeLocked(c); err != nil {
		return nil, err
	}
	// Update in-memory index for faster searches
	s.index.addOrUpdate(c)
	return c, nil
}

func (s *Store) Delete(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除会话失败: %w", err)
	}
	return nil
}

func (s *Store) readLocked(p string) (*Conversation, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("读取会话失败: %w", err)
	}
	var c Conversation
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("会话文件损坏: %w", err)
	}
	return &c, nil
}

func (s *Store) writeLocked(c *Conversation) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("编码会话失败: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(s.dir, c.ID+".json"), b, 0o644); err != nil {
		return fmt.Errorf("写入会话失败: %w", err)
	}
	return nil
}

func summarize(c *Conversation) Summary {
	sum := Summary{ID: c.ID, Title: c.Title, Count: len(c.Messages), UpdatedAt: c.UpdatedAt}
	if len(c.Messages) > 0 {
		last := c.Messages[len(c.Messages)-1]
		sum.Preview = truncate(strings.TrimSpace(stripNewlines(last.Content)), 40)
	}
	return sum
}

func titleFrom(content string) string {
	return truncate(strings.TrimSpace(stripNewlines(content)), maxTitleRunes)
}

func stripNewlines(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, s)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	// 避免在代理对 (Emoji) 或中日文字符中间切断
	// 末尾留一个位置给省略号
	cut := n - 1
	if cut < 0 {
		return ""
	}
	if cut >= len(r) {
		return string(r) + "…"
	}
	// 检查最后字符是否为代理对的高位，如果是则向前调整
	result := r[:cut]
	if len(result) > 0 {
		last := result[len(result)-1]
		// UTF-16 代理对范围：高 0xD800-0xDBFF，低 0xDC00-0xDFFF
		if last >= 0xD800 && last <= 0xDBFF && cut < len(r) {
			next := r[cut]
			if next >= 0xDC00 && next <= 0xDFFF {
				// 避免切断代理对，往前找完整字符
				for i := len(result) - 2; i >= 0; i-- {
					if result[i] < 0xD800 || result[i] > 0xDBFF {
						result = result[:i+1]
						break
					}
				}
				if len(result) == 0 {
					return "…"
				}
			}
		}
	}
	return string(result) + "…"
}
