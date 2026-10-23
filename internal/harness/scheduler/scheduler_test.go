package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScheduler_AddJobValidation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "schedules.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, cron string
		interval   int
		goal       string
		wantErr    bool
	}{
		{"ok-cron", "0 9 * * *", 0, "目标", false},
		{"ok-interval", "", 30, "目标", false},
		{"no-trigger", "", 0, "目标", true},
		{"no-goal", "0 9 * * *", 0, "", true},
		{"bad-cron", "99 * * * *", 0, "目标", true},
		{"tiny-interval", "", 1, "目标", true},
		{"bad-mode", "0 9 * * *", 0, "目标", true},
	}
	_ = cases[6]
	if _, err := s.AddJob("bad-mode", "0 9 * * *", 0, "目标", "weird"); err == nil {
		t.Error("非法模式应报错")
	}
	for i, c := range cases[:6] {
		_, err := s.AddJob(c.name, c.cron, c.interval, c.goal, "auto")
		if (err != nil) != c.wantErr {
			t.Errorf("case %d %s: err=%v wantErr=%v", i, c.name, err, c.wantErr)
		}
	}
	if s.Count() != 2 {
		t.Errorf("Count = %d", s.Count())
	}
}

func TestScheduler_DuplicateNameRejected(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "schedules.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddJob("daily", "0 9 * * *", 0, "整理桌面", "auto"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddJob("daily", "*/5 * * * *", 0, "覆盖用目标", "auto"); err == nil {
		t.Fatal("同名任务应被拒绝，而不是静默替换")
	}
	jobs := s.ListJobs()
	if len(jobs) != 1 || jobs[0].Goal != "整理桌面" || jobs[0].Cron != "0 9 * * *" {
		t.Fatalf("拒绝后原任务不应被改动: %+v", jobs)
	}
}

func TestScheduler_JobNameMustStayAddressable(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "schedules.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"每日巡检 9 点", false},
		{"check.disk", false},
		{"backup_2026-09", false},
		{"每日巡检 09:00", true},
		{"巡检：每日", true},
		{"巡检/每日", true},
		{"a/b", true},
		{"a\\b", true},
		{"../../etc/passwd", true},
		{"带?查询", true},
		{"带#锚点", true},
		{"带\x00控制符", true},
		{strings.Repeat("巡检", 33), true},
	}
	for _, c := range cases {
		_, err := s.AddJob(c.name, "0 9 * * *", 0, "目标", "auto")
		if (err != nil) != c.wantErr {
			t.Errorf("名字 %q: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
	}
	if got := s.Count(); got != 3 {
		t.Errorf("被拒名字不该入库，Count = %d，期望 3", got)
	}
}

func TestScheduler_Persistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	s, _ := Open(path, nil)
	if _, err := s.AddJob("daily", "0 9 * * *", 0, "整理桌面", "auto"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs := s2.ListJobs()
	if len(jobs) != 1 || jobs[0].Name != "daily" || jobs[0].Goal != "整理桌面" {
		t.Fatalf("重载任务 = %v", jobs)
	}
	if jobs[0].NextRun == nil {
		t.Error("重载后应补算 NextRun")
	}
}

func TestScheduler_AddJobDetailedPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")
	s, _ := Open(path, nil)
	if _, err := s.AddJobDetailed("nl", "30 18 * * 1-5", 0, "整理下载目录", "auto", "工作日18:30", "工作日 18:30（周一至周五）"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs := s2.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("任务数 = %d", len(jobs))
	}
	if jobs[0].WhenText != "工作日18:30" || jobs[0].ScheduleText == "" {
		t.Errorf("自然语言时间说明未持久化: when=%q text=%q", jobs[0].WhenText, jobs[0].ScheduleText)
	}
}

func TestScheduler_FireOnTick(t *testing.T) {
	var mu sync.Mutex
	fired := []Job{}
	s, _ := Open(filepath.Join(t.TempDir(), "schedules.json"), nil)
	s.SetFire(func(j Job) { mu.Lock(); fired = append(fired, j); mu.Unlock() })

	if _, err := s.AddJob("soon", "", 5, "每5秒目标", "auto"); err != nil {
		t.Fatal(err)
	}
	job := s.ListJobs()[0]
	s.tick(time.Now().Add(6 * time.Second))
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 1 {
		t.Fatalf("触发次数 = %d", len(fired))
	}
	if fired[0].Goal != "每5秒目标" {
		t.Errorf("触发目标 = %q", fired[0].Goal)
	}
	if _, err := s.AddJob("once", "0 9 * * *", 0, "一次性", "auto"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.jobs["once"].OneShot = true
	s.jobs["once"].NextRun = ptrTime(time.Now().Add(-time.Minute))
	s.mu.Unlock()
	s.tick(time.Now())
	jobs := s.ListJobs()
	for _, j := range jobs {
		if j.Name == "once" && j.Enabled {
			t.Error("OneShot 触发后应停用")
		}
	}
	_ = job
}

func TestScheduler_WatchTrigger(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "watched.txt")
	os.WriteFile(file, []byte("v1"), 0o644)

	var mu sync.Mutex
	fired := 0
	s, _ := Open(filepath.Join(t.TempDir(), "s.json"), nil)
	s.SetFire(func(j Job) { mu.Lock(); fired++; mu.Unlock() })

	if _, err := s.AddJob("watcher", "", 0, "文件变了", "auto"); err == nil {
		t.Error("缺少触发源应报错")
	}
	s.mu.Lock()
	s.jobs["watcher"] = &Job{Name: "watcher", WatchPath: file, Goal: "文件变了", Enabled: true}
	s.mu.Unlock()

	s.tick(time.Now())
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(file, []byte("v2 longer"), 0o644)
	s.tick(time.Now().Add(1 * time.Second))
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	n := fired
	mu.Unlock()
	if n != 1 {
		t.Errorf("监听触发次数 = %d", n)
	}
	s.tick(time.Now().Add(2 * time.Second))
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if fired != 1 {
		t.Errorf("去抖期内不应再次触发: %d", fired)
	}
}

func TestScheduler_EnableDisableDelete(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "s.json"), nil)
	s.AddJob("j1", "0 9 * * *", 0, "g", "auto")
	if err := s.SetEnabled("j1", false); err != nil {
		t.Fatal(err)
	}
	jobs := s.ListJobs()
	if jobs[0].Enabled {
		t.Error("应已停用")
	}
	if err := s.SetEnabled("nope", true); err == nil {
		t.Error("不存在的任务应报错")
	}
	ok, err := s.DeleteJob("j1")
	if err != nil || !ok {
		t.Fatalf("DeleteJob = %v %v", ok, err)
	}
	if s.Count() != 0 {
		t.Error("删除后应为空")
	}
}

func TestScheduler_UnfireableNextRunStops(t *testing.T) {
	var mu sync.Mutex
	fired := 0
	s, _ := Open(filepath.Join(t.TempDir(), "s.json"), func(Job) {
		mu.Lock()
		fired++
		mu.Unlock()
	})
	if _, err := s.AddJob("broken", "0 9 * * *", 0, "目标", "auto"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.jobs["broken"].Cron = "not a cron"
	s.jobs["broken"].NextRun = ptrTime(time.Now().Add(-time.Minute))
	s.mu.Unlock()

	s.tick(time.Now())
	time.Sleep(100 * time.Millisecond)
	s.tick(time.Now())
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	n := fired
	mu.Unlock()
	if n != 1 {
		t.Fatalf("无法计算下次时间的任务只应触发一次，实际 %d", n)
	}
	jobs := s.ListJobs()
	if jobs[0].NextRun != nil {
		t.Errorf("非法 cron 的 NextRun 应清空，实际 %v", jobs[0].NextRun)
	}
}

func TestScheduler_SameJobDoesNotOverlap(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	s, _ := Open(filepath.Join(t.TempDir(), "s.json"), func(Job) {
		started <- struct{}{}
		<-release
	})
	if _, err := s.AddJob("slow", "", 5, "慢任务", "auto"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.tick(now.Add(6 * time.Second))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("第一轮应启动")
	}
	s.tick(now.Add(12 * time.Second))
	select {
	case <-started:
		t.Fatal("上一轮未结束时不应再启动")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		busy := s.inflight["slow"]
		s.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.tick(now.Add(18 * time.Second))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("上一轮结束后应恢复触发")
	}
}

func TestScheduler_WatchPatternFiltersUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "a.log")
	txtFile := filepath.Join(dir, "b.txt")
	os.WriteFile(logFile, []byte("v1"), 0o644)
	os.WriteFile(txtFile, []byte("v1"), 0o644)

	var mu sync.Mutex
	fired := 0
	s, _ := Open(filepath.Join(t.TempDir(), "s.json"), func(Job) {
		mu.Lock()
		fired++
		mu.Unlock()
	})
	s.mu.Lock()
	s.jobs["logs"] = &Job{Name: "logs", WatchPath: dir, WatchPattern: "*.log", Goal: "日志变了", Enabled: true}
	s.mu.Unlock()

	base := time.Now()
	s.tick(base)
	os.WriteFile(txtFile, []byte("v2 much longer"), 0o644)
	s.tick(base.Add(time.Second))
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if fired != 0 {
		t.Errorf("不匹配 *.log 的文件变化不应触发，实际 %d 次", fired)
	}
	mu.Unlock()

	os.WriteFile(logFile, []byte("v2 longer"), 0o644)
	s.tick(base.Add(2 * time.Second))
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if fired != 1 {
		t.Errorf("匹配文件变化应触发 1 次，实际 %d 次", fired)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
