package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gleam/internal/atomicfile"
	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

const (
	snapshotDirName  = "snapshots"
	manifestFileName = "manifest.json"
	snapshotMaxBytes = 1 << 20
	snapshotMaxTotal = 32 << 20
	snapshotMaxTasks = 200
	snapshotSniffLen = 8 * 1024
)

type preImage struct {
	Seq     int    `json:"seq"`
	StepID  string `json:"step_id"`
	Tool    string `json:"tool"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Bytes   int64  `json:"bytes,omitempty"`
	IsDir   bool   `json:"is_dir,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
	File    string `json:"file,omitempty"`
	Reason  string `json:"reason,omitempty"`
	ModTime string `json:"mod_time,omitempty"`
	At      string `json:"at"`
}

func (p preImage) reversible() bool {
	return !p.Exists || p.File != ""
}

func SnapshotRoot(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	return filepath.Join(dataDir, snapshotDirName)
}

func PruneSnapshots(dataDir string, keep int) {
	pruneTaskDirs(SnapshotRoot(dataDir), keep)
}

type snapshotter struct {
	dir          string
	manifestPath string

	mu   sync.Mutex
	recs []preImage
	used int64
}

func newSnapshotter(dataDir, taskID string) *snapshotter {
	root := SnapshotRoot(dataDir)
	if root == "" {
		return &snapshotter{}
	}
	dir := filepath.Join(root, safeSeg(taskID))
	manifest := filepath.Join(dir, manifestFileName)
	if _, err := toolutil.ResolveInRoots(manifest, []string{root}); err != nil {
		return &snapshotter{}
	}
	s := &snapshotter{dir: dir, manifestPath: manifest}
	s.load()
	return s
}

func (s *snapshotter) load() {
	data, err := os.ReadFile(s.manifestPath)
	if err != nil {
		return
	}
	var recs []preImage
	if json.Unmarshal(data, &recs) != nil {
		return
	}
	s.recs = recs
	for _, r := range s.recs {
		if r.File == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(s.dir, r.File)); err == nil {
			s.used += info.Size()
		}
	}
}

func (s *snapshotter) enabled() bool { return s != nil && s.dir != "" }

func (s *snapshotter) capture(ctx context.Context, stepID, toolName string, tool types.Tool, args map[string]any) {
	if !s.enabled() || tool == nil {
		return
	}
	pa, ok := tool.(types.PathAware)
	if !ok {
		return
	}
	for _, p := range pa.Paths(ctx, args) {
		if !filepath.IsAbs(p) {
			continue
		}
		s.one(stepID, toolName, p)
	}
}

func (s *snapshotter) one(stepID, toolName, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := preImage{
		Seq: len(s.recs) + 1, StepID: stepID, Tool: toolName, Path: path,
		At: time.Now().Format(time.RFC3339),
	}
	info, err := os.Stat(path)
	if err != nil {
		s.commit(rec)
		return
	}
	rec.Exists = true
	rec.Bytes = info.Size()
	rec.Mode = uint32(info.Mode().Perm())
	rec.ModTime = info.ModTime().Format(time.RFC3339)
	switch {
	case info.IsDir():
		rec.IsDir = true
		rec.Reason = "当时是个目录，内容未快照（还原只能删掉空目录）"
	case rec.Bytes > snapshotMaxBytes:
		rec.Reason = fmt.Sprintf("当时有 %s，超过单文件快照上限，内容未留存", humanSize(rec.Bytes))
	case s.used+rec.Bytes > snapshotMaxTotal:
		rec.Reason = "本任务快照总量已达上限，内容未留存"
	case isBinaryContent(path, rec.Bytes):
		rec.Reason = "二进制内容，未快照（只对文本做对比与还原）"
	default:
		name, n, werr := s.writeContent(rec.Seq, path)
		if werr != nil {
			rec.Reason = "读旧内容失败：" + types.Shorten(werr.Error(), 80)
		} else {
			rec.File = name
			rec.Bytes = n
			s.used += n
		}
	}
	s.commit(rec)
}

func (s *snapshotter) commit(rec preImage) {
	s.recs = append(s.recs, rec)
	if err := s.flushManifest(); err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 快照清单落盘失败（本次仍可用，重跑后不可还原）：%v\n", err)
	}
}

func (s *snapshotter) writeContent(seq int, path string) (string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", 0, err
	}
	name := fmt.Sprintf("%d.pre", seq)
	if err := atomicfile.Write(filepath.Join(s.dir, name), b, 0o600); err != nil {
		return "", 0, err
	}
	return name, int64(len(b)), nil
}

func (s *snapshotter) flushManifest() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.recs, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.manifestPath, b, 0o600)
}

func (s *snapshotter) records() []preImage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]preImage(nil), s.recs...)
}

func isBinaryContent(path string, size int64) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, snapshotSniffLen)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	if strings.IndexByte(string(buf), 0) >= 0 {
		return true
	}
	if utf8.Valid(buf) {
		return false
	}
	if size <= snapshotSniffLen || len(buf) <= 4 {
		return true
	}
	return !utf8.Valid(buf[:len(buf)-4])
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	for _, u := range []string{"KB", "MB", "GB"} {
		v /= unit
		if v < unit {
			return fmt.Sprintf("%.1f %s", v, u)
		}
	}
	return fmt.Sprintf("%.1f GB", v)
}

func taskPreImages(dataDir, taskID string) ([]preImage, error) {
	root := SnapshotRoot(dataDir)
	if SafeTaskName(taskID) == "" || root == "" {
		return nil, fmt.Errorf("任务标识或数据目录不合法")
	}
	dir := filepath.Join(root, safeSeg(taskID))
	if _, err := toolutil.ResolveInRoots(filepath.Join(dir, manifestFileName), []string{root}); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, manifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var recs []preImage
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("快照清单读不懂：%w", err)
	}
	return recs, nil
}

func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return filepath.ToSlash(p)
}

func firstPreImageOf(recs []preImage, path string) (preImage, bool) {
	want := pathKey(path)
	for _, r := range recs {
		if r.Path != "" && pathKey(r.Path) == want {
			return r, true
		}
	}
	return preImage{}, false
}

func preImagePath(dataDir, taskID string, rec preImage) (string, error) {
	if rec.File == "" {
		return "", fmt.Errorf("这条快照没有留住内容")
	}
	root := SnapshotRoot(dataDir)
	if SafeTaskName(taskID) == "" || root == "" {
		return "", fmt.Errorf("任务标识或数据目录不合法")
	}
	full := filepath.Join(root, safeSeg(taskID), filepath.Base(rec.File))
	if _, err := toolutil.ResolveInRoots(full, []string{root}); err != nil {
		return "", err
	}
	return full, nil
}

func preImageContent(dataDir, taskID string, rec preImage) ([]byte, error) {
	full, err := preImagePath(dataDir, taskID, rec)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func preImageGroup(recs []preImage, path string) []preImage {
	first, ok := firstPreImageOf(recs, path)
	if !ok {
		return nil
	}
	if first.Tool != "file.move" {
		return []preImage{first}
	}
	var out []preImage
	seen := map[string]bool{}
	for _, r := range recs {
		if r.StepID != first.StepID || r.Tool != first.Tool || seen[pathKey(r.Path)] {
			continue
		}
		if head, has := firstPreImageOf(recs, r.Path); has {
			seen[pathKey(r.Path)] = true
			out = append(out, head)
		}
	}
	return out
}

func restorePreImage(dataDir, taskID string, rec preImage) (string, error) {
	if !rec.Exists {
		info, err := os.Stat(rec.Path)
		if err != nil {
			return "本任务之前这里没有东西，现在也没有——无需还原", nil
		}
		if err := os.Remove(rec.Path); err != nil {
			if info.IsDir() {
				return "", fmt.Errorf("这个目录不是空的，没有删（里面可能有别的文件）：%w", err)
			}
			return "", err
		}
		if info.IsDir() {
			return "已删掉本任务建出的目录（仅空目录）", nil
		}
		return "已删掉本任务建出的文件", nil
	}
	b, err := preImageContent(dataDir, taskID, rec)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(rec.Path), 0o755); err != nil {
		return "", err
	}
	perm := os.FileMode(0o644)
	if rec.Mode != 0 {
		perm = os.FileMode(rec.Mode)
	}
	if err := atomicfile.Write(rec.Path, b, perm); err != nil {
		return "", err
	}
	return fmt.Sprintf("已退回到任务开始前的内容（%s）", humanSize(int64(len(b)))), nil
}
