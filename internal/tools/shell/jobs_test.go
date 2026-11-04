package shell

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A platform-specific command that runs for a while and keeps producing output.
func longRunningCmd() string {
	if runtime.GOOS == "windows" {
		return `ping -n 30 127.0.0.1`
	}
	return `for i in 1 2 3 4 5 6 7 8 9 10; do echo tick-$i; sleep 0.2; done`
}

func quickEchoCmd(text string) string {
	if runtime.GOOS == "windows" {
		return `echo ` + text
	}
	return `echo ` + text
}

func TestJobs_StartListOutput(t *testing.T) {
	j := NewJobs()
	job, err := j.Start(context.Background(), longRunningCmd(), "", nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = j.Kill(job.ID) }()

	list := j.List()
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
	if list[0].ID != job.ID {
		t.Errorf("List id = %s, want %s", list[0].ID, job.ID)
	}
	if !list[0].Running {
		t.Error("job should be running")
	}

	// give the output time to accumulate
	time.Sleep(300 * time.Millisecond)
	stdout, _, running := job.Output()
	if !running {
		t.Error("still should be running")
	}
	if stdout == "" {
		t.Error("expected some stdout")
	}
}

func TestJobs_KillTerminates(t *testing.T) {
	j := NewJobs()
	job, err := j.Start(context.Background(), longRunningCmd(), "", nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := j.Kill(job.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	// after Kill the job should be gone from the table
	if _, ok := j.Get(job.ID); ok {
		t.Error("job should be removed after Kill")
	}
	// a second Kill should report the job as missing
	if err := j.Kill(job.ID); err == nil {
		t.Error("second Kill should error")
	}
}

func TestJobs_JobListCap(t *testing.T) {
	j := NewJobs()
	ids := make([]string, 0, jobListCap)
	for i := 0; i < jobListCap; i++ {
		job, err := j.Start(context.Background(), longRunningCmd(), "", nil)
		if err != nil {
			t.Fatalf("Start #%d: %v", i, err)
		}
		ids = append(ids, job.ID)
	}
	defer func() {
		for _, id := range ids {
			_ = j.Kill(id)
		}
	}()
	// the cap+1-th start must be rejected
	if _, err := j.Start(context.Background(), longRunningCmd(), "", nil); err == nil {
		t.Fatal("expected cap rejection")
	} else if !strings.Contains(err.Error(), "已达上限") {
		t.Errorf("error should mention cap, got %v", err)
	}
}

func TestJobs_QuickCommandCapturesExitCode(t *testing.T) {
	j := NewJobs()
	job, err := j.Start(context.Background(), quickEchoCmd("hello"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// wait for the job to finish on its own
	select {
	case <-job.done:
	case <-time.After(3 * time.Second):
		t.Fatal("echo did not finish in time")
	}
	snap := job.Snapshot()
	if snap.Running {
		t.Error("echo should be done")
	}
	if snap.ExitCode == nil || *snap.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", snap.ExitCode)
	}
}

// Tool layer

func TestShellExec_BackgroundReturnsJobID(t *testing.T) {
	jobs := NewJobs()
	tool := New(5 * time.Second)
	tool.Jobs = jobs
	out, err := tool.Execute(context.Background(), map[string]any{
		"command":    quickEchoCmd("bg-test"),
		"background": true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := out.(map[string]any)
	if m["background"] != true {
		t.Errorf("background flag not set: %v", m)
	}
	id, ok := m["job_id"].(string)
	if !ok || id == "" {
		t.Fatalf("no job_id returned: %v", m)
	}
	// wait for the job to finish, then clean up
	if job, ok := jobs.Get(id); ok {
		<-job.done
		_ = jobs.Kill(id)
	}
}

func TestJobsListOutputKillTools(t *testing.T) {
	jobs := NewJobs()
	job, err := jobs.Start(context.Background(), longRunningCmd(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = jobs.Kill(job.ID) }()

	listTool := NewJobsList(jobs)
	listOut, err := listTool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	lm := listOut.(map[string]any)
	if lm["count"] != 1 {
		t.Errorf("jobs.list count = %v, want 1", lm["count"])
	}

	outTool := NewJobsOutput(jobs)
	outRes, err := outTool.Execute(context.Background(), map[string]any{"id": job.ID})
	if err != nil {
		t.Fatal(err)
	}
	om := outRes.(map[string]any)
	if om["id"] != job.ID {
		t.Errorf("jobs.output id = %v", om["id"])
	}
	if _, ok := om["running"]; !ok {
		t.Error("jobs.output missing running field")
	}

	killTool := NewJobsKill(jobs)
	killRes, err := killTool.Execute(context.Background(), map[string]any{"id": job.ID})
	if err != nil {
		t.Fatal(err)
	}
	km := killRes.(map[string]any)
	if km["killed"] != true {
		t.Errorf("jobs.kill killed = %v", km["killed"])
	}
}

func TestJobsOutputTool_UnknownID(t *testing.T) {
	jobs := NewJobs()
	tool := NewJobsOutput(jobs)
	_, err := tool.Execute(context.Background(), map[string]any{"id": "nonexistent"})
	if err == nil {
		t.Fatal("unknown id should error")
	}
}

// Ring buffer

func TestRingBuf_WrapsWithoutGrowth(t *testing.T) {
	rb := newRingBuf(8)
	for i := 0; i < 100; i++ {
		fmt.Fprintf(rb, "%d", i%10)
	}
	s := rb.String()
	if len(s) > 8 {
		t.Errorf("ring buf grew beyond cap: len=%d", len(s))
	}
}
