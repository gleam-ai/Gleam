//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

var procGetConsoleCP = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleCP")

func hasConsole() (has bool) {
	defer func() {
		if recover() != nil {
			has = false
		}
	}()
	r, _, _ := procGetConsoleCP.Call()
	return r != 0
}

func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

var _ = exec.Cmd{}
