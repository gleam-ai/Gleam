package filelock

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquire_Exclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	a, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryAcquire(path); !errors.Is(err, ErrLocked) {
		t.Errorf("second acquire: want ErrLocked, got %v", err)
	}
	if err := a.Release(); err != nil {
		t.Fatal(err)
	}
	b, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	_ = b.Release()
}

func TestAcquire_Timeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	a, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	start := time.Now()
	if _, err := Acquire(path, 60*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Errorf("want ErrLocked on timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("Acquire returned before the deadline: %v", elapsed)
	}
}

func TestWithLock_RunsAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	ran := false
	if err := WithLock(path, time.Second, func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("fn did not run")
	}
	lk, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("lock not released after WithLock: %v", err)
	}
	_ = lk.Release()
	sentinel := errors.New("boom")
	if err := WithLock(path, time.Second, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("want fn error propagated, got %v", err)
	}
}

// TestFileLock_CrossProcess proves the lock is OS-level, not just in-process:
// a separate process must see it held, then free after release.
func TestFileLock_CrossProcess(t *testing.T) {
	path := os.Getenv("FILELOCK_PATH")
	switch os.Getenv("FILELOCK_MODE") {
	case "expect-locked":
		lk, err := TryAcquire(path)
		if errors.Is(err, ErrLocked) {
			os.Exit(0)
		}
		if lk != nil {
			_ = lk.Release()
		}
		fmt.Fprintf(os.Stderr, "expected ErrLocked, got %v\n", err)
		os.Exit(1)
	case "expect-free":
		lk, err := TryAcquire(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "expected free lock, got %v\n", err)
			os.Exit(1)
		}
		_ = lk.Release()
		os.Exit(0)
	}

	path = filepath.Join(t.TempDir(), "x.lock")
	lk, err := TryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if code := runLockChild(t, path, "expect-locked"); code != 0 {
		t.Errorf("child should have seen the lock held, exit=%d", code)
	}
	if err := lk.Release(); err != nil {
		t.Fatal(err)
	}
	if code := runLockChild(t, path, "expect-free"); code != 0 {
		t.Errorf("child should have acquired after release, exit=%d", code)
	}
}

func runLockChild(t *testing.T, path, mode string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFileLock_CrossProcess$")
	cmd.Env = append(os.Environ(), "FILELOCK_PATH="+path, "FILELOCK_MODE="+mode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Logf("child(%s) output: %s", mode, out)
			return ee.ExitCode()
		}
		t.Fatalf("child(%s) failed to run: %v", mode, err)
	}
	return 0
}
