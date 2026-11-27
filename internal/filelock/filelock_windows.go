//go:build windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// openMeansHeld reports whether a failed open of the lockfile means "someone
// else is in the lock" rather than a broken path. Both codes are what Windows
// answers while another handle holds the byte range or is being scanned by
// a filter driver right after the file was created; neither says anything
// about the file itself being unusable.
func openMeansHeld(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// Lock a single byte at offset zero; unlock must use the same range.
func lockFile(f *os.File) error {
	var ov windows.Overlapped
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ov)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	var ov windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ov)
}
