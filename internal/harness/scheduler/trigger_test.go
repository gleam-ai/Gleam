package scheduler

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestScheduler_TriggerNow(t *testing.T) {
	var mu sync.Mutex
	fired := []string{}
	s, err := Open(filepath.Join(t.TempDir(), "s.json"), func(j Job) {
		mu.Lock()
		fired = append(fired, j.Name)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddJob("hook-job", "", 3600, "回调目标", "auto"); err != nil {
		t.Fatal(err)
	}

	ok, err := s.TriggerNow("hook-job")
	if err != nil || !ok {
		t.Fatalf("TriggerNow = %v, %v", ok, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(fired)
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 1 || fired[0] != "hook-job" {
		t.Errorf("触发记录 = %v", fired)
	}
	jobs := s.ListJobs()
	if jobs[0].LastRun == nil {
		t.Error("LastRun 应被更新")
	}
}

func TestScheduler_TriggerNowRejectsOverlap(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	s, err := Open(filepath.Join(t.TempDir(), "s.json"), func(Job) {
		started <- struct{}{}
		<-release
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddJob("j1", "", 3600, "g", "auto"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.TriggerNow("j1"); !ok || err != nil {
		t.Fatalf("TriggerNow = %v, %v", ok, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("第一轮应启动")
	}
	if ok, err := s.TriggerNow("j1"); ok || err == nil {
		t.Fatal("运行中的任务应拒绝再次触发")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		busy := s.inflight["j1"]
		s.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ok, err := s.TriggerNow("j1"); !ok || err != nil {
		t.Fatalf("上一轮结束后应允许再次触发：%v, %v", ok, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("第二轮应启动")
	}
}

func TestScheduler_TriggerNowErrors(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "s.json"), nil)
	s.AddJob("j1", "0 9 * * *", 0, "g", "auto")

	if _, err := s.TriggerNow("nope"); err == nil {
		t.Error("不存在的任务应报错")
	}
	_ = s.SetEnabled("j1", false)
	if _, err := s.TriggerNow("j1"); err == nil {
		t.Error("已停用任务应报错")
	}
}
