package agent

import (
	"context"
	"os"
	"strconv"
	"strings"

	"gleam/internal/worktree"
	"gleam/pkg/types"
)

type isolation struct {
	roots []string
	cwd   string
}

func (a *Agent) WorktreesView() map[string]any {
	ctx := context.Background()
	rows := []map[string]any{}
	if m := a.worktreeManager(); m != nil {
		for _, meta := range m.List() {
			row := map[string]any{
				"task_id": meta.TaskID, "path": meta.Path, "branch": meta.Branch,
				"repo": meta.Repo, "created_at": meta.CreatedAt, "exists": true,
			}
			switch _, err := os.Stat(meta.Path); {
			case os.IsNotExist(err):
				row["exists"] = false
				row["dirty_detail"] = "目录已经不在了（被外部删掉）；删掉这条记录会顺带收敛 git 自己的登记"
			default:
				dirty, detail, derr := worktree.Dirty(ctx, meta.Path)
				switch {
				case derr != nil:
					row["dirty"] = true
					row["dirty_detail"] = "读不出状态：" + derr.Error()
				default:
					row["dirty"] = dirty
					row["dirty_detail"] = detail
				}
			}
			rows = append(rows, row)
		}
	}
	var enabled, fetch, autoDelete bool
	var maxCount int
	ws := ""
	if a.Cfg != nil {
		ws = a.Cfg.Workspace
		enabled = a.Cfg.Worktrees.Enabled
		fetch = a.Cfg.Worktrees.FetchBeforeCreate
		autoDelete = a.Cfg.Worktrees.AutoDelete
		maxCount = a.Cfg.Worktrees.MaxCount
	}
	return map[string]any{
		"enabled": enabled, "fetch_before_create": fetch,
		"auto_delete": autoDelete, "max_count": maxCount,
		"workspace":  ws,
		"repository": ws != "" && worktree.IsRepo(ctx, ws),
		"rows":       rows,
	}
}

func (a *Agent) RemoveWorktree(taskID string, force bool) error {
	m := a.worktreeManager()
	if m == nil {
		return worktree.ErrNoDataDir
	}
	return m.Remove(context.Background(), taskID, force)
}

func (a *Agent) worktreeManager() *worktree.Manager {
	if a.Cfg == nil || strings.TrimSpace(a.Cfg.DataDir) == "" {
		return nil
	}
	return &worktree.Manager{
		DataDir:      a.Cfg.DataDir,
		BranchPrefix: func() string { return a.Cfg.Git.BranchPrefix },
		Fetch:        func() bool { return a.Cfg.Worktrees.FetchBeforeCreate },
		Egress: func(host string, n int) {
			if a.Gate != nil {
				a.Gate.RecordEgress("git.remote", host, n)
			}
		},
	}
}

func (a *Agent) isolateForTask(ctx context.Context, req types.GoalRequest, taskID string, notify func(phase, msg string, pct int, kind string)) isolation {
	cwd := a.workspaceOf(req)
	if a.Cfg == nil || !a.Cfg.Worktrees.Enabled {
		return isolation{cwd: cwd}
	}
	if v, ok := req.Context["cwd"].(string); ok && strings.TrimSpace(v) != "" {
		return isolation{cwd: cwd}
	}
	ws := strings.TrimSpace(a.Cfg.Workspace)
	if ws == "" {
		notify("plan", "已开启 worktree 隔离，但还没选工作区：这次没有可建副本的仓库，在工作区里直接执行", 2, "warn")
		return isolation{cwd: cwd}
	}
	m := a.worktreeManager()
	if m == nil {
		return isolation{cwd: cwd}
	}
	if meta, ok := m.Get(taskID); ok {
		notify("plan", "这次任务在它的 worktree 里执行："+meta.Path, 2, "info")
		return isolation{roots: []string{meta.Path}, cwd: meta.Path}
	}
	meta, err := m.Create(ctx, taskID, ws, "")
	if err != nil {
		notify("plan", "worktree 没建成（"+err.Error()+"），这次在工作区里直接执行", 2, "warn")
		return isolation{cwd: cwd}
	}
	notify("plan", "这次任务在它的 worktree 里执行："+meta.Path+"（分支 "+meta.Branch+"）", 2, "info")
	return isolation{roots: []string{meta.Path}, cwd: meta.Path}
}

func (a *Agent) settleWorktree(taskID string) {
	if a.Cfg == nil || !a.Cfg.Worktrees.Enabled {
		return
	}
	m := a.worktreeManager()
	if m == nil {
		return
	}
	meta, ok := m.Get(taskID)
	if !ok {
		return
	}
	ctx := context.Background()
	if a.Cfg.Worktrees.AutoDelete {
		dirty, detail, err := worktree.Dirty(ctx, meta.Path)
		switch {
		case err != nil:
			a.notifyWorktree("收尾时没读出来 worktree 的状态（"+err.Error()+"），这次不删它", "warn")
		case dirty:
			a.notifyWorktree("worktree 里有未提交的改动（"+detail+"），没有自动删除："+meta.Path+"（分支 "+meta.Branch+"）", "warn")
		default:
			if err := m.Remove(ctx, taskID, false); err != nil {
				a.notifyWorktree("worktree 没能自动删掉："+err.Error(), "warn")
			}
		}
	}
	if keep := a.Cfg.Worktrees.MaxCount; keep > 0 {
		removed, keptDirty := m.PruneClean(ctx, keep)
		if len(removed) > 0 {
			a.notifyWorktree("worktree 超过上限，已清掉最旧的 "+strconv.Itoa(len(removed))+" 个", "info")
		}
		if len(keptDirty) > 0 {
			a.notifyWorktree("还有 "+strconv.Itoa(len(keptDirty))+" 个 worktree 因为里面有未提交的改动而没被清理，超过上限的部分先留着", "warn")
		}
	}
}

func (a *Agent) notifyWorktree(msg, kind string) {
	if a.Notifier == nil {
		return
	}
	a.Notifier.OnProgress(types.ProgressEvent{Phase: "done", Message: msg, Progress: 100, Kind: kind})
}
