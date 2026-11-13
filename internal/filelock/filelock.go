// Package filelock provides advisory cross-process exclusive locks backed by
// the OS (flock on Unix, LockFileEx on Windows). The OS releases the lock
// automatically when the holding process exits, so a crash cannot wedge it.
package filelock

import (
	"errors"
	"os"
	"time"
)

// ErrLocked reports that another process already holds the lock.
var ErrLocked = errors.New("filelock: 锁被其他进程持有")

// Lock is a held cross-process exclusive lock on a lockfile.
type Lock struct {
	f *os.File
}

// TryAcquire opens (creating if needed) the lockfile at path and attempts the
// exclusive lock once, returning ErrLocked if another process holds it. The
// caller must Release the returned Lock.
func TryAcquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
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
