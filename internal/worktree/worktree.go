package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gleam/internal/atomicfile"
	gitcmd "gleam/internal/tools/git"
)

type Meta struct {
	TaskID    string `json:"task_id"`
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	BaseRef   string `json:"base_ref"`
	CreatedAt string `json:"created_at"`
}

type Manager struct {
	DataDir      string
	BranchPrefix func() string
	Fetch        func() bool
	Egress       func(host string, nbytes int)
}

func (m *Manager) Root() string {
	if strings.TrimSpace(m.DataDir) == "" {
		return ""
	}
	return filepath.Join(m.DataDir, "worktrees")
}

func (m *Manager) Dir(taskID string) string { return filepath.Join(m.Root(), taskID) }

func (m *Manager) metaPath(taskID string) string {
	return filepath.Join(m.Root(), taskID+".meta.json")
}

var ErrNoDataDir = fmt.Errorf("没有数据目录，无法管理 worktree")

var ErrExists = fmt.Errorf("该任务的 worktree 已经存在")

var ErrNeedsForce = errors.New("这个副本需要确认才能删除")

func validTaskID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func IsRepo(ctx context.Context, dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	_, err := gitcmd.Run(ctx, dir, "rev-parse", "--git-dir")
	return err == nil
}

func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := gitcmd.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("不在 git 仓库里：%w", err)
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", fmt.Errorf("拿不到仓库根目录")
	}
	return root, nil
}

func (m *Manager) Create(ctx context.Context, taskID, repo, baseRef string) (Meta, error) {
	if m.Root() == "" {
		return Meta{}, ErrNoDataDir
	}
	if !validTaskID(taskID) {
		return Meta{}, fmt.Errorf("任务 ID %q 不能当目录名用", taskID)
	}
	root, err := RepoRoot(ctx, repo)
	if err != nil {
		return Meta{}, err
	}
	path := m.Dir(taskID)
	branch := m.branchFor(taskID)
	if !gitcmd.ValidRefName(branch) {
		return Meta{}, fmt.Errorf("分支名 %q 不合法", branch)
	}
	if _, err := os.Stat(path); err == nil {
		return Meta{}, fmt.Errorf("%w：%s", ErrExists, path)
	}
	if baseRef == "" {
		baseRef = "HEAD"
	}
	if !gitcmd.ValidRefName(baseRef) {
		return Meta{}, fmt.Errorf("起点 %q 不合法", baseRef)
	}
	if err := os.MkdirAll(m.Root(), 0o755); err != nil {
		return Meta{}, err
	}
	if m.Fetch != nil && m.Fetch() {
		if err := m.fetch(ctx, root); err != nil {
			fmt.Fprintf(os.Stderr, "[gleam] worktree 创建前 fetch 失败（继续用本地 HEAD）：%v\n", err)
		}
	}
	if _, err := gitcmd.Run(ctx, root, "worktree", "add", "-b", branch, path, baseRef); err != nil {
		return Meta{}, err
	}
	meta := Meta{
		TaskID: taskID, Repo: root, Path: path, Branch: branch, BaseRef: baseRef,
		CreatedAt: time.Now().Format(time.RFC3339Nano),
	}
	if err := m.save(meta); err != nil {
		_, _ = gitcmd.Run(ctx, root, "worktree", "remove", "--force", path)
		return Meta{}, err
	}
	return meta, nil
}

func (m *Manager) fetch(ctx context.Context, root string) error {
	if m.Egress != nil {
		host := remoteHost(ctx, root)
		defer func() { m.Egress(host, 0) }()
	}
	_, err := gitcmd.Run(ctx, root, "fetch", "--prune")
	return err
}

func remoteHost(ctx context.Context, root string) string {
	out, err := gitcmd.Run(ctx, root, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return hostOfURL(strings.TrimSpace(out))
}

func hostOfURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.IndexByte(s, '/'); j >= 0 {
			s = s[:j]
		}
		if i := strings.LastIndexByte(s, '@'); i >= 0 {
			s = s[i+1:]
		}
		return s
	}
	host, _, found := strings.Cut(s, ":")
	if !found {
		return ""
	}
	if i := strings.LastIndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	return host
}

func (m *Manager) branchFor(taskID string) string {
	prefix := ""
	if m.BranchPrefix != nil {
		prefix = strings.TrimSpace(m.BranchPrefix())
	}
	if prefix == "" {
		return taskID
	}
	return prefix + taskID
}

func (m *Manager) save(meta Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(m.metaPath(meta.TaskID), data, 0o644)
}

func (m *Manager) Get(taskID string) (Meta, bool) {
	if m.Root() == "" || !validTaskID(taskID) {
		return Meta{}, false
	}
	data, err := os.ReadFile(m.metaPath(taskID))
	if err != nil {
		return Meta{}, false
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, false
	}
	return meta, true
}

func (m *Manager) List() []Meta {
	if m.Root() == "" {
		return nil
	}
	entries, err := filepath.Glob(filepath.Join(m.Root(), "*.meta.json"))
	if err != nil {
		return nil
	}
	out := make([]Meta, 0, len(entries))
	for _, p := range entries {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var meta Meta
		if err := json.Unmarshal(data, &meta); err != nil || meta.TaskID == "" {
			continue
		}
		out = append(out, meta)
	}
	sort.SliceStable(out, func(i, j int) bool { return metaTime(out[i]).After(metaTime(out[j])) })
	return out
}

func metaTime(m Meta) time.Time {
	t, err := time.Parse(time.RFC3339Nano, m.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

func Dirty(ctx context.Context, path string) (bool, string, error) {
	out, err := gitcmd.Run(ctx, path, "status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	var changed, untracked int
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			untracked++
		} else {
			changed++
		}
	}
	if changed == 0 && untracked == 0 {
		return false, "", nil
	}
	parts := make([]string, 0, 2)
	if changed > 0 {
		parts = append(parts, fmt.Sprintf("%d 个文件有未提交的改动", changed))
	}
	if untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d 个新文件还没纳入版本控制", untracked))
	}
	return true, strings.Join(parts, "，"), nil
}

func (m *Manager) Remove(ctx context.Context, taskID string, force bool) error {
	meta, ok := m.Get(taskID)
	if !ok {
		return fmt.Errorf("没有 %s 的 worktree 记录", taskID)
	}
	if _, err := os.Stat(meta.Path); os.IsNotExist(err) {
		m.forget(ctx, meta)
		return nil
	}
	repoGone := false
	if _, err := os.Stat(meta.Repo); os.IsNotExist(err) {
		repoGone = true
	}
	if !force {
		if repoGone {
			return fmt.Errorf("%w：主仓库 %s 已不在，没法判断副本里有没有未提交的改动", ErrNeedsForce, meta.Repo)
		}
		dirty, detail, err := Dirty(ctx, meta.Path)
		if err != nil {
			return fmt.Errorf("%w：读不出副本的状态（%v）", ErrNeedsForce, err)
		}
		if dirty {
			return fmt.Errorf("%w：%s", ErrNeedsForce, detail)
		}
	}
	if repoGone {
		if err := os.RemoveAll(meta.Path); err != nil {
			return err
		}
		_ = os.Remove(m.metaPath(taskID))
		return nil
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, meta.Path)
	if _, err := gitcmd.Run(ctx, meta.Repo, args...); err != nil {
		return err
	}
	_, _ = gitcmd.Run(ctx, meta.Repo, "worktree", "prune")
	_ = os.Remove(m.metaPath(taskID))
	return nil
}

func (m *Manager) forget(ctx context.Context, meta Meta) {
	if _, err := os.Stat(meta.Repo); err == nil {
		_, _ = gitcmd.Run(ctx, meta.Repo, "worktree", "prune")
	}
	_ = os.Remove(m.metaPath(meta.TaskID))
}

func (m *Manager) PruneClean(ctx context.Context, keep int) (removed []Meta, keptDirty []Meta) {
	if keep <= 0 {
		return nil, nil
	}
	all := m.List()
	if len(all) <= keep {
		return nil, nil
	}
	for _, meta := range all[keep:] {
		if err := m.Remove(ctx, meta.TaskID, false); err != nil {
			keptDirty = append(keptDirty, meta)
			continue
		}
		removed = append(removed, meta)
	}
	return removed, keptDirty
}
