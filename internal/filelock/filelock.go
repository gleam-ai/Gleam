// Package filelock provides advisory cross-process exclusive locks backed by
// the OS (flock on Unix, LockFileEx on Windows). The OS releases the lock
// automatically when the holding process exits, so a crash cannot wedge it.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrLocked reports that another process already holds the lock.
var ErrLocked = errors.New("filelock: 锁被其他进程持有")

// Lock is a held cross-process exclusive lock on a lockfile.
type Lock struct {
	f *os.File
}

// openLockFile is a seam so tests can stand in for the OS refusing the open.
var openLockFile = func(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

// TryAcquire opens (creating if needed) the lockfile at path and attempts the
// exclusive lock once, returning ErrLocked if another process holds it. The
// caller must Release the returned Lock.
func TryAcquire(path string) (*Lock, error) {
	f, err := openLockFile(path)
	if err != nil {
		// On Windows the open itself can be refused while another handle
		// holds or is waiting on the lock ("Access is denied"), so LockFileEx
		// never gets to report contention. Without this mapping Acquire
		// returned at once and a concurrent writer lost the key it was
		// saving. The raw OS error stays in the message so a real permission
		// problem is still readable when the retry budget runs out.
		if openMeansHeld(err) {
			return nil, fmt.Errorf("%w: 打开锁文件: %w", ErrLocked, err)
		}
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Acquire retries TryAcquire until timeout, returning ErrLocked if the lock is
// still held when the deadline passes. A timeout <= 0 means a single attempt.
func Acquire(path string, timeout time.Duration) (*Lock, error) {
	if timeout <= 0 {
		return TryAcquire(path)
	}
	deadline := time.Now().Add(timeout)
	for {
		lk, err := TryAcquire(path)
		if !errors.Is(err, ErrLocked) {
			return lk, err
		}
		if !time.Now().Before(deadline) {
			return nil, ErrLocked
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// Release releases the lock and closes the lockfile. It is safe to call once.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockFile(l.f)
	cerr := l.f.Close()
	l.f = nil
	if err != nil {
		return err
	}
	return cerr
}

// WithLock acquires the lock, runs fn, then releases it. fn's error is returned
// unchanged; an acquisition failure is returned without running fn.
func WithLock(path string, timeout time.Duration, fn func() error) error {
	lk, err := Acquire(path, timeout)
	if err != nil {
		return err
	}
	defer lk.Release()
	return fn()
}
