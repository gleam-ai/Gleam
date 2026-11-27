package agent

import (
	"context"
	"testing"
	"time"
)

// clockOnlyDeadline is a context whose deadline has passed on the clock while
// Err is still nil -- the state a real context is in when its timer goroutine
// has not been scheduled yet.
type clockOnlyDeadline struct {
	context.Context
	deadline time.Time
}

func (c clockOnlyDeadline) Deadline() (time.Time, bool) { return c.deadline, true }

func TestPastDeadline(t *testing.T) {
	bg := context.Background()
	if pastDeadline(bg) {
		t.Error("a context without a deadline is never past it")
	}
	if pastDeadline(clockOnlyDeadline{Context: bg, deadline: time.Now().Add(time.Minute)}) {
		t.Error("a future deadline is not past")
	}
	if !pastDeadline(clockOnlyDeadline{Context: bg, deadline: time.Now().Add(-time.Second)}) {
		t.Error("a deadline that passed on the clock is past even while Err is still nil")
	}
	cancelled, cancel := context.WithCancel(bg)
	cancel()
	if !pastDeadline(cancelled) {
		t.Error("a cancelled step context counts as past its deadline")
	}
}
