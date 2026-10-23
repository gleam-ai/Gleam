package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"gleam/internal/atomicfile"
)

var JobNamePattern = regexp.MustCompile(`^[\w\p{Han}\-. ]{1,64}$`)

type Job struct {
	Name         string     `json:"name"`
	Cron         string     `json:"cron,omitempty"`
	IntervalSec  int        `json:"interval_sec,omitempty"`
	WatchPath    string     `json:"watch_path,omitempty"`
	WatchPattern string     `json:"watch_pattern,omitempty"`
	Goal         string     `json:"goal"`
	Mode         string     `json:"mode,omitempty"`
	Notify       string     `json:"notify,omitempty"`
	Enabled      bool       `json:"enabled"`
	OneShot      bool       `json:"one_shot,omitempty"`
	WhenText     string     `json:"when_text,omitempty"`
	ScheduleText string     `json:"schedule_text,omitempty"`
	LastRun      *time.Time `json:"last_run,omitempty"`
	NextRun      *time.Time `json:"next_run,omitempty"`
}

const (
	NotifyOnFailure = "on_failure"
	NotifyAlways    = "always"
	NotifyNever     = "never"
)

func NormalizeNotify(v string) (string, error) {
	switch trim(v) {
	case "":
		return NotifyOnFailure, nil
	case NotifyOnFailure, NotifyAlways, NotifyNever:
		return trim(v), nil
	}
	return "", fmt.Errorf("非法通知策略 %q（可用：%s / %s / %s）", v, NotifyAlways, NotifyOnFailure, NotifyNever)
}

func (j Job) ShouldNotify(succeeded bool) bool {
	policy, err := NormalizeNotify(j.Notify)
	if err != nil {
		policy = NotifyOnFailure
	}
	switch policy {
	case NotifyNever:
		return false
	case NotifyAlways:
		return true
	default:
		return !succeeded
	}
}

type FireFunc func(j Job)

type Scheduler struct {
	mu      sync.Mutex
	path    string
	jobs    map[string]*Job
	fire    FireFunc
	stop    chan struct{}
	running bool

	watch    map[string]watchSnap
	lastFire map[string]time.Time
	inflight map[string]bool
}

type watchSnap struct {
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size"`
	Count   int       `json:"count"`
}

func Open(path string, fire FireFunc) (*Scheduler, error) {
	s := &Scheduler{
		path:     path,
		jobs:     map[string]*Job{},
		fire:     fire,
		stop:     make(chan struct{}),
		watch:    map[string]watchSnap{},
		lastFire: map[string]time.Time{},
		inflight: map[string]bool{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var jobs []*Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		_ = os.Rename(path, path+".corrupt")
		return s, nil
	}
	now := time.Now()
	for _, j := range jobs {
		if j == nil || j.Name == "" {
			continue
		}
		s.recomputeNext(j, now)
		s.jobs[j.Name] = j
	}
	return s, nil
}

func (s *Scheduler) SetFire(f FireFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fire = f
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	go s.loop()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stop)
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.tick(now)
		}
	}
}

func (s *Scheduler) tick(now time.Time) {
	var due []*Job
	s.mu.Lock()
	fire := s.fire
	for _, j := range s.jobs {
		if !j.Enabled {
			continue
		}
		if j.NextRun != nil && !now.Before(*j.NextRun) {
			due = append(due, j)
			continue
		}
		if j.WatchPath != "" {
			s.checkWatch(j, now)
		}
	}
	for _, j := range due {
		nowT := now
		j.LastRun = &nowT
		s.recomputeNext(j, now)
		if j.OneShot {
			j.Enabled = false
			j.NextRun = nil
		}
	}
	if len(due) > 0 {
		_ = s.saveLocked()
	}
	s.mu.Unlock()
	for _, j := range due {
		if fire != nil {
			s.start(j.Name, fire, *j)
		}
	}
}

// start keeps the same job from stacking: if the previous run is still going,
// this trigger is dropped instead of starting another goroutine that mixes up
// the working directory and conversation.
func (s *Scheduler) start(name string, fire FireFunc, j Job) {
	if !s.reserve(name) {
		return
	}
	s.launch(name, fire, j)
}

func (s *Scheduler) reserve(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[name] {
		return false
	}
	s.inflight[name] = true
	return true
}

func (s *Scheduler) launch(name string, fire FireFunc, j Job) {
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inflight, name)
			s.mu.Unlock()
		}()
		fire(j)
	}()
}

func (s *Scheduler) checkWatch(j *Job, now time.Time) {
	snap := s.snapshot(j.WatchPath, j.WatchPattern)
	if prev, ok := s.watch[j.Name]; ok && snap != prev {
		if last, ok2 := s.lastFire[j.Name]; !ok2 || now.Sub(last) > 10*time.Second {
			nowT := now
			j.LastRun = &nowT
			s.lastFire[j.Name] = now
			if j.NextRun == nil {
				s.recomputeNext(j, now)
			}
			if s.fire != nil && !s.inflight[j.Name] {
				s.inflight[j.Name] = true
				job := *j
				s.launch(j.Name, s.fire, job)
			}
		}
	}
	s.watch[j.Name] = snap
}

func (s *Scheduler) snapshot(path, pattern string) watchSnap {
	info, err := os.Stat(path)
	if err != nil {
		return watchSnap{}
	}
	if !info.IsDir() {
		return watchSnap{ModTime: info.ModTime(), Size: info.Size(), Count: 1}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return watchSnap{}
	}
	// The directory snapshot aggregates only matching entries: using the
	// directory mtime would change the snapshot for an unrelated new file and
	// falsely trigger the watch job.
	var snap watchSnap
	for _, e := range entries {
		if pattern != "" {
			if ok, _ := filepath.Match(pattern, e.Name()); !ok {
				continue
			}
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		snap.Count++
		snap.Size += st.Size()
		if st.ModTime().After(snap.ModTime) {
			snap.ModTime = st.ModTime()
		}
	}
	return snap
}

func (s *Scheduler) recomputeNext(j *Job, now time.Time) {
	// Clear before computing: when no next run exists (invalid or never-matching
	// cron) the field must stay nil, or the stale past time makes every tick
	// consider it due and fires forever.
	j.NextRun = nil
	switch {
	case j.Cron != "":
		if c, err := ParseCron(j.Cron); err == nil {
			if next := c.Next(now); !next.IsZero() {
				j.NextRun = &next
			}
		}
	case j.IntervalSec > 0:
		next := now.Add(time.Duration(j.IntervalSec) * time.Second)
		j.NextRun = &next
	case j.WatchPath != "":
		// Watch jobs are triggered by file changes and have no fixed next run.
	}
}

func (s *Scheduler) AddJob(name, cron string, intervalSec int, goal, mode string) (Job, error) {
	return s.AddJobDetailed(name, cron, intervalSec, goal, mode, "", "")
}

func (s *Scheduler) AddJobDetailed(name, cron string, intervalSec int, goal, mode, whenText, scheduleText string) (Job, error) {
	name = trim(name)
	if name == "" {
		return Job{}, fmt.Errorf("任务名不能为空")
	}
	if !JobNamePattern.MatchString(name) {
		return Job{}, fmt.Errorf("任务名只能使用中文字母数字与 - . _ 空格（最长 64 字），不能含 / 等路径字符")
	}
	if goal == "" {
		return Job{}, fmt.Errorf("目标不能为空")
	}
	if cron == "" && intervalSec <= 0 {
		return Job{}, fmt.Errorf("需要提供 cron 表达式或 interval_sec")
	}
	if cron != "" {
		if _, err := ParseCron(cron); err != nil {
			return Job{}, err
		}
	}
	if intervalSec > 0 && intervalSec < 5 {
		return Job{}, fmt.Errorf("间隔最小为 5 秒")
	}
	if mode != "" && mode != "auto" && mode != "plan_first" && mode != "interactive" {
		return Job{}, fmt.Errorf("非法执行模式 %q", mode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[name]; exists {
		return Job{}, fmt.Errorf("任务名 %q 已存在，请换一个", name)
	}
	job := &Job{
		Name: name, Cron: cron, IntervalSec: intervalSec, Goal: goal, Mode: mode,
		Enabled: true, WhenText: trim(whenText), ScheduleText: trim(scheduleText),
	}
	s.recomputeNext(job, time.Now())
	s.jobs[name] = job
	if err := s.saveLocked(); err != nil {
		return *job, err
	}
	return *job, nil
}

func (s *Scheduler) DeleteJob(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[name]; !ok {
		return false, nil
	}
	delete(s.jobs, name)
	delete(s.watch, name)
	delete(s.lastFire, name)
	return true, s.saveLocked()
}

func (s *Scheduler) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[name]
	if !ok {
		return fmt.Errorf("任务 %q 不存在", name)
	}
	j.Enabled = enabled
	if enabled {
		s.recomputeNext(j, time.Now())
	}
	return s.saveLocked()
}

func (s *Scheduler) SetNotify(name, policy string) error {
	norm, err := NormalizeNotify(policy)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[name]
	if !ok {
		return fmt.Errorf("任务 %q 不存在", name)
	}
	if norm == NotifyOnFailure {
		j.Notify = ""
	} else {
		j.Notify = norm
	}
	return s.saveLocked()
}

func (s *Scheduler) TriggerNow(name string) (bool, error) {
	s.mu.Lock()
	j, ok := s.jobs[name]
	if !ok {
		s.mu.Unlock()
		return false, fmt.Errorf("任务 %q 不存在", name)
	}
	if !j.Enabled {
		s.mu.Unlock()
		return false, fmt.Errorf("任务 %q 已停用", name)
	}
	if s.inflight[name] {
		s.mu.Unlock()
		return false, fmt.Errorf("任务 %q 正在运行，请等本轮结束", name)
	}
	nowT := time.Now()
	j.LastRun = &nowT
	s.recomputeNext(j, nowT)
	if j.OneShot {
		j.Enabled = false
		j.NextRun = nil
	}
	fire := s.fire
	job := *j
	if fire != nil {
		s.inflight[name] = true
	}
	err := s.saveLocked()
	s.mu.Unlock()
	if fire != nil {
		s.launch(name, fire, job)
	}
	return true, err
}

func (s *Scheduler) ListJobs() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out
}

func (s *Scheduler) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

func (s *Scheduler) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].Name < jobs[k].Name })
	data, err := json.MarshalIndent(jobs, "", " ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o644)
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
