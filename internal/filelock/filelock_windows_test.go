//go:build windows

package filelock

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Windows can refuse to open a lockfile while another handle is in the lock.
// That refusal is contention, not a broken path: it must surface as ErrLocked
// so Acquire keeps retrying instead of returning at once. The raw OS error
// stays attached, because the same code is also what a real permission
// problem looks like once the retry budget runs out.
func TestOpenRefusedMeansHeld(t *testing.T) {
	for _, code := range []error{windows.ERROR_ACCESS_DENIED, windows.ERROR_SHARING_VIOLATION} {
		err := &fs.PathError{Op: "open", Path: "x.lock", Err: code}
		if !openMeansHeld(err) {
			t.Errorf("open refused with %v should count as held", code)
		}
	}
	for _, code := range []error{windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND} {
		if openMeansHeld(&fs.PathError{Op: "open", Path: "x.lock", Err: code}) {
			t.Errorf("open failed with %v is a real error, not contention", code)
		}
	}
}

func TestTryAcquire_OpenRefusedIsRetried(t *testing.T) {
	// No portable way to make Windows refuse an open with exactly this code,
	// so the stand-in returns what the OS answers for a held lockfile.
	opens := 0
	old := openLockFile
	openLockFile = func(string) (*os.File, error) {
		opens++
		return nil, &fs.PathError{Op: "open", Path: "x.lock", Err: windows.ERROR_ACCESS_DENIED}
	}
	t.Cleanup(func() { openLockFile = old })

	_, err := TryAcquire("x.lock")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("refused open: want ErrLocked, got %v", err)
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("the raw OS error must stay attached: %v", err)
	}
	opens = 0
	if _, err := Acquire("x.lock", 40*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("Acquire should keep seeing contention: %v", err)
	}
	if opens < 2 {
		t.Errorf("Acquire opened %d time(s); a refused open must be retried, not returned at once", opens)
	}
}
