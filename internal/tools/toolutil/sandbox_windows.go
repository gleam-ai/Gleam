package toolutil

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func resolveExistingPath(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	// EvalSymlinks does not resolve junctions on recent Go versions.
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &os.PathError{Op: "resolve", Path: path, Err: err}
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 256)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", &os.PathError{Op: "resolve", Path: path, Err: err}
		}
		if n >= uint32(len(buf)) {
			buf = make([]uint16, n+1)
			continue
		}
		real := windows.UTF16ToString(buf[:n])
		if strings.HasPrefix(real, `\\?\UNC\`) {
			return `\\` + strings.TrimPrefix(real, `\\?\UNC\`), nil
		}
		return strings.TrimPrefix(real, `\\?\`), nil
	}
}
