//go:build !unix && !windows

package filelock

import "os"

// Platforms without flock/LockFileEx degrade to no cross-process locking; the
// caller's in-process mutex still applies. Not a build target we ship.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
