package space

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/atomicfile"
)

const DefaultID = "default"

var ErrDefaultSpace = errors.New("默认空间不能删除")

var (
	ErrInvalidID = errors.New("非法空间 ID")
	ErrNotFound  = errors.New("空间不存在")
)

var spaceIDPattern = regexp.MustCompile(`^(default|sp_[0-9a-f]{12})$`)

type Space struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ConversationCount int `json:"conversation_count,omitempty"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

type stateFile struct {
	ActiveID string `json:"active_id"`
}

func Open(dataDir, initialWorkspace string) (*Store, error) {
	dir := filepath.Join(dataDir, "spaces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}

	list, err := s.listLocked()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		now := time.Now().UTC()
		def := Space{
			ID:        DefaultID,
			Name:      "默认空间",
			Path:      strings.TrimSpace(initialWorkspace),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.writeLocked(def); err != nil {
			return nil, err
		}
		if err := s.writeStateLocked(DefaultID); err != nil {
			return nil, err
		}
	} else if _, err := s.ActiveID(); err != nil {
		fallback := DefaultID
		if _, err := os.Stat(s.pathLocked(DefaultID)); err != nil {
			fallback = ""
			for _, sp := range list {
				if spaceIDPattern.MatchString(sp.ID) {
					fallback = sp.ID
					break
				}
			}
			if fallback == "" {
				return nil, fmt.Errorf("spaces: 无合法空间可回落")
			}
		}
		_ = s.writeStateLocked(fallback)
	}
	return s, nil
}

func (s *Store) pathLocked(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) path(id string) (string, error) {
	if !spaceIDPattern.MatchString(id) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.dir, id+".json"), nil
}

func (s *Store) statePath() string {
	return filepath.Join(s.dir, "state.json")
}

func (s *Store) writeLocked(sp Space) error {
	sp.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(sp, "", "  ")
	if err != nil {
		return err
	}
	path, err := s.path(sp.ID)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o644)
}

func (s *Store) writeStateLocked(id string) error {
	data, err := json.MarshalIndent(stateFile{ActiveID: id}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.statePath(), data, 0o644)
}

func (s *Store) readOne(path string) (Space, error) {
	var sp Space
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sp, ErrNotFound
		}
		return sp, err
	}
	if err := json.Unmarshal(data, &sp); err != nil {
		return sp, err
	}
	if !spaceIDPattern.MatchString(sp.ID) {
		return sp, ErrInvalidID
	}
	return sp, nil
}

func (s *Store) listLocked() ([]Space, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]Space, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || name == "state.json" {
			continue
		}
		sp, err := s.readOne(filepath.Join(s.dir, name))
		if err != nil || sp.ID == "" {
			continue
		}
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ID == DefaultID) != (out[j].ID == DefaultID) {
			return out[i].ID == DefaultID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Store) List() ([]Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store) Get(id string) (Space, error) {
	if strings.TrimSpace(id) == "" {
		id = DefaultID
	}
	path, err := s.path(id)
	if err != nil {
		return Space{}, err
	}
	return s.readOne(path)
}

func (s *Store) ActiveID() (string, error) {
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		return "", err
	}
	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil || strings.TrimSpace(st.ActiveID) == "" {
		return "", fmt.Errorf("active space 未设置")
	}
	if _, err := s.path(st.ActiveID); err != nil {
		return "", fmt.Errorf("active space 不存在")
	}
	if _, err := os.Stat(s.pathLocked(st.ActiveID)); err != nil {
		return "", fmt.Errorf("active space 不存在")
	}
	return st.ActiveID, nil
}

func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("空间不存在: %w", ErrNotFound)
	}
	return s.writeStateLocked(id)
}

func (s *Store) Create(name, path string) (Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := newID()
	if err != nil {
		return Space{}, err
	}
	now := time.Now().UTC()
	sp := Space{
		ID:        id,
		Name:      strings.TrimSpace(name),
		Path:      strings.TrimSpace(path),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if sp.Name == "" {
		if sp.Path != "" {
			sp.Name = filepath.Base(sp.Path)
		} else {
			sp.Name = "未命名空间"
		}
	}
	if err := s.writeLocked(sp); err != nil {
		return Space{}, err
	}
	return sp, nil
}

func (s *Store) Rename(id, name string) (Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(id)
	if err != nil {
		return Space{}, err
	}
	sp, err := s.readOne(path)
	if err != nil {
		return Space{}, err
	}
	if strings.TrimSpace(name) == "" {
		return Space{}, fmt.Errorf("名称不能为空")
	}
	sp.Name = strings.TrimSpace(name)
	if err := s.writeLocked(sp); err != nil {
		return Space{}, err
	}
	return sp, nil
}

func (s *Store) SetPath(id, path string) (Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(id)
	if err != nil {
		return Space{}, err
	}
	sp, err := s.readOne(p)
	if err != nil {
		return Space{}, err
	}
	sp.Path = strings.TrimSpace(path)
	if err := s.writeLocked(sp); err != nil {
		return Space{}, err
	}
	return sp, nil
}

func (s *Store) Delete(id string) error {
	if id == DefaultID {
		return ErrDefaultSpace
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if _, err := s.readOne(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if active, err := s.ActiveID(); err == nil && active == id {
		if _, err := os.Stat(s.pathLocked(DefaultID)); err == nil {
			_ = s.writeStateLocked(DefaultID)
		}
	}
	return nil
}

func newID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sp_" + hex.EncodeToString(b[:]), nil
}
