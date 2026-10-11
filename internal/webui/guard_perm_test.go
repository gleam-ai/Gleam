package webui

import "runtime"

func isUnixPerm() bool { return runtime.GOOS != "windows" }
