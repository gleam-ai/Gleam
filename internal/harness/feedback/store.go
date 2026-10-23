package feedback

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"gleam/internal/atomicfile"
	"gleam/pkg/types"
)

const maxAttachmentBytes = 5 << 20

var idPattern = regexp.MustCompile(`^fb-[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

var attachmentPattern = regexp.MustCompile(`^fb-[0-9]{8}-[0-9]{6}-[0-9a-f]{4}-[0-9]+$`)

type Store struct {
	dataDir string
}

func NewStore(dataDir string) *Store { return &Store{dataDir: dataDir} }

var fallbackSeq atomic.Uint32

func NewID() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		v := fallbackSeq.Add(1)
		b[0], b[1] = byte(v>>8), byte(v)
	}
	now := time.Now()
	return fmt.Sprintf("fb-%s-%s", now.Format("20060102-150405"), hex.EncodeToString(b[:]))
}

func (s *Store) dir() string { return filepath.Join(s.dataDir, "feedback") }

func (s *Store) path(id string) string {
	if !idPattern.MatchString(id) {
		return ""
	}
	return filepath.Join(s.dir(), id+".json")
}

func (s *Store) Save(f *types.Feedback) error {
	if f == nil {
		return fmt.Errorf("没有可提交的反馈")
	}
	if s.dataDir == "" {
		return fmt.Errorf("未配置数据目录，反馈未提交")
	}
	path := s.path(f.ID)
	if path == "" {
		return fmt.Errorf("反馈 ID %q 不合形状，未提交", f.ID)
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now()
	}
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return fmt.Errorf("序列化反馈失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, b, 0o644)
}

func (s *Store) Read(id string) (*types.Feedback, error) {
	path := s.path(id)
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
	var f types.Feedback
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("解析反馈失败（%s）: %w", path, err)
	}
	if f.ID != id {
		return nil, fmt.Errorf("反馈内容与文件名不符（%s 里写的是 %q）", path, f.ID)
	}
	return &f, nil
}

func (s *Store) Delete(id string) error {
	path := s.path(id)
	if path == "" {
		return nil
	}
	names, _ := filepath.Glob(filepath.Join(s.dir(), id+"-*"))
	for _, n := range names {
		if err := os.Remove(n); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) List(limit int) ([]*types.Feedback, int, error) {
	if limit <= 0 {
		limit = 50
	}
	entries, err := os.ReadDir(s.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	type entry struct {
		id  string
		mod time.Time
	}
	var files []entry
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, entry{strings.TrimSuffix(name, ".json"), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	out := make([]*types.Feedback, 0, limit)
	skipped := 0
	for _, f := range files {
		if len(out) >= limit {
			break
		}
		item, err := s.Read(f.id)
		if err != nil || item == nil {
			skipped++
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, skipped, nil
}

func (s *Store) SaveAttachment(id string, data []byte) (string, string, error) {
	if !idPattern.MatchString(id) {
		return "", "", fmt.Errorf("反馈 ID %q 不合形状", id)
	}
	if len(data) == 0 {
		return "", "", fmt.Errorf("截图是空的")
	}
	if len(data) > maxAttachmentBytes {
		return "", "", fmt.Errorf("截图 %d MB 超过 %d MB 上限",
			len(data)>>20, maxAttachmentBytes>>20)
	}
	ext, ok := imageExt(data)
	if !ok {
		return "", "", fmt.Errorf("不是支持的图片类型（只收 PNG / JPEG / GIF / WebP）")
	}
	n := 1
	for {
		name := fmt.Sprintf("%s-%d%s", id, n, ext)
		if _, err := os.Stat(filepath.Join(s.dir(), name)); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", "", err
		}
		n++
	}
	name := fmt.Sprintf("%s-%d%s", id, n, ext)
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return "", "", err
	}
	if err := atomicfile.Write(filepath.Join(s.dir(), name), data, 0o600); err != nil {
		return "", "", err
	}
	return name, sniffMIME(data), nil
}

func (s *Store) ReadAttachment(name string) ([]byte, string, error) {
	if !safeAttachmentName(name) {
		return nil, "", nil
	}
	b, err := os.ReadFile(filepath.Join(s.dir(), name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	return b, sniffMIME(b), nil
}

func safeAttachmentName(name string) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return attachmentPattern.MatchString(base) && imageExtAllowed(filepath.Ext(name))
}

func imageExtAllowed(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func imageExt(data []byte) (string, bool) {
	switch sniffMIME(data) {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	}
	return "", false
}

func sniffMIME(data []byte) string {
	if len(data) > 512 {
		data = data[:512]
	}
	return http.DetectContentType(data)
}
