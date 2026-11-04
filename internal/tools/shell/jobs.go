package shell

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// Job is a background job: a long-running process plus a bounded output buffer.
// When output exceeds jobOutputCap the oldest bytes are dropped and the newest kept (ring semantics).
type Job struct {
	ID        string
	Command   string
	CWD       string
	StartedAt time.Time

	cmd *exec.Cmd

	mu       sync.Mutex
	stdout   *ringBuf
	stderr   *ringBuf
	exitCode *int
	done     chan struct{}
	killed   bool
}

const (
	// jobOutputCap bounds the memory per stream so unbounded output like npm run dev cannot exhaust it.
	jobOutputCap = 64 * 1024
	// jobListCap bounds the number of concurrently alive jobs; beyond it some must be killed first or waited out.
	jobListCap = 32
)

// Jobs is the in-process job table. All methods are thread-safe.
type Jobs struct {
	mu sync.Mutex
	m  map[string]*Job
}

func NewJobs() *Jobs { return &Jobs{m: map[string]*Job{}} }

// Start launches a background job. The command format matches shell.exec.
func (j *Jobs) Start(ctx context.Context, command, cwd string, env []string) (*Job, error) {
	j.mu.Lock()
	if len(j.m) >= jobListCap {
		j.mu.Unlock()
		return nil, fmt.Errorf("后台作业已达上限 %d，请先 jobs.kill 一些", jobListCap)
	}
	j.mu.Unlock()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	setProcessGroup(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = env

	job := &Job{
		ID:        "job-" + types.NewID(),
		Command:   command,
		CWD:       cwd,
		StartedAt: time.Now(),
		cmd:       cmd,
		stdout:    newRingBuf(jobOutputCap),
		stderr:    newRingBuf(jobOutputCap),
		done:      make(chan struct{}),
	}
	cmd.Stdout = job.stdout
	cmd.Stderr = job.stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动失败: %w", err)
	}
	j.mu.Lock()
	j.m[job.ID] = job
	j.mu.Unlock()

	go func() {
		err := cmd.Wait()
		job.mu.Lock()
		if err != nil {
			code := -1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
			job.exitCode = &code
		} else {
			code := 0
			job.exitCode = &code
		}
		job.mu.Unlock()
		close(job.done)
		// Unlike foreground shell.exec, the job keeps running even if ctx is canceled:
		// a background job's lifecycle is controlled by jobs.kill, not by the caller's ctx.
		_ = ctx
	}()
	return job, nil
}

func (j *Jobs) Get(id string) (*Job, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.m[id]
	return job, ok
}

// List returns snapshots ordered by start time, newest first.
func (j *Jobs) List() []JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]JobSnapshot, 0, len(j.m))
	for _, job := range j.m {
		out = append(out, job.Snapshot())
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt.After(out[b].StartedAt) })
	return out
}

// Kill terminates the job and removes it from the table. Killing an already-finished job is just cleanup.
func (j *Jobs) Kill(id string) error {
	j.mu.Lock()
	job, ok := j.m[id]
	if !ok {
		j.mu.Unlock()
		return fmt.Errorf("作业 %s 不存在", id)
	}
	delete(j.m, id)
	j.mu.Unlock()

	job.mu.Lock()
	alreadyDone := job.exitCode != nil
	job.killed = true
	job.mu.Unlock()
	if !alreadyDone {
		_ = killProcessTree(job.cmd)
		<-job.done
	}
	return nil
}

// JobSnapshot is a read-only view for the UI / model.
type JobSnapshot struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	CWD       string    `json:"cwd,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Running   bool      `json:"running"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	Killed    bool      `json:"killed,omitempty"`
}

func (job *Job) Snapshot() JobSnapshot {
	job.mu.Lock()
	defer job.mu.Unlock()
	return JobSnapshot{
		ID:        job.ID,
		Command:   job.Command,
		CWD:       job.CWD,
		StartedAt: job.StartedAt,
		Running:   job.exitCode == nil,
		ExitCode:  job.exitCode,
		Killed:    job.killed,
	}
}

// Output returns the current stdout/stderr snapshot and whether the job is still running.
func (job *Job) Output() (stdout, stderr string, running bool) {
	job.mu.Lock()
	running = job.exitCode == nil
	job.mu.Unlock()
	return job.stdout.String(), job.stderr.String(), running
}

// Ring buffer

// ringBuf is a byte ring buffer: once full it overwrites the oldest data. String() returns the contents in chronological order.
type ringBuf struct {
	mu  sync.Mutex
	buf []byte
	cap int
	pos int // next write position
}

func newRingBuf(cap int) *ringBuf { return &ringBuf{buf: make([]byte, cap), cap: cap} }

func (r *ringBuf) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	for _, b := range p {
		r.buf[r.pos] = b
		r.pos = (r.pos + 1) % r.cap
	}
	return n, nil
}

func (r *ringBuf) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	// If the ring has not wrapped yet, everything before pos is the data; otherwise reassemble by rotating at pos.
	// Simplified: always reassemble by rotating at pos, letting strings.TrimLeft drop the leading zero bytes.
	out := make([]byte, 0, r.cap)
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return strings.TrimLeft(string(out), "\x00")
}

// Toolset

// JobsListTool exposes jobs.list.
type JobsListTool struct{ Jobs *Jobs }

func NewJobsList(j *Jobs) types.Tool { return &JobsListTool{j} }
func (t *JobsListTool) Name() string { return "jobs.list" }
func (t *JobsListTool) Description() string {
	return "列出所有后台作业：id、命令、是否仍在运行、退出码。"
}
func (t *JobsListTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *JobsListTool) Schema() map[string]any {
	return toolutil.Schema("列出后台作业", nil, map[string]any{})
}
func (t *JobsListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	list := t.Jobs.List()
	return map[string]any{"count": len(list), "jobs": list}, nil
}

// JobsOutputTool exposes jobs.output.
type JobsOutputTool struct{ Jobs *Jobs }

func NewJobsOutput(j *Jobs) types.Tool { return &JobsOutputTool{j} }
func (t *JobsOutputTool) Name() string { return "jobs.output" }
func (t *JobsOutputTool) Description() string {
	return "读取指定后台作业的当前 stdout/stderr 与运行状态。"
}
func (t *JobsOutputTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *JobsOutputTool) Schema() map[string]any {
	return toolutil.Schema("读取后台作业输出", []string{"id"}, map[string]any{
		"id": toolutil.SchemaProp("作业 ID", "string"),
	})
}
func (t *JobsOutputTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	id, err := toolutil.RequireStr(args, "id")
	if err != nil {
		return nil, err
	}
	job, ok := t.Jobs.Get(id)
	if !ok {
		return nil, fmt.Errorf("作业 %s 不存在", id)
	}
	stdout, stderr, running := job.Output()
	out := map[string]any{"id": id, "running": running, "stdout": stdout, "stderr": stderr}
	if snap := job.Snapshot(); snap.ExitCode != nil {
		out["exit_code"] = *snap.ExitCode
	}
	return out, nil
}

// JobsKillTool exposes jobs.kill.
type JobsKillTool struct{ Jobs *Jobs }

func NewJobsKill(j *Jobs) types.Tool { return &JobsKillTool{j} }
func (t *JobsKillTool) Name() string { return "jobs.kill" }
func (t *JobsKillTool) Description() string {
	return "终止指定后台作业（同时杀掉其子进程）。已结束的作业调用等于清理记录。"
}
func (t *JobsKillTool) Permission() types.Permission { return types.PermissionUserApproved }
func (t *JobsKillTool) Schema() map[string]any {
	return toolutil.Schema("终止后台作业", []string{"id"}, map[string]any{
		"id": toolutil.SchemaProp("作业 ID", "string"),
	})
}
func (t *JobsKillTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	id, err := toolutil.RequireStr(args, "id")
	if err != nil {
		return nil, err
	}
	if err := t.Jobs.Kill(id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "killed": true}, nil
}
