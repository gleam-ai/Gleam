//go:build !windows

package main

import "syscall"

func hasConsole() bool { return true }

func procAttr() *syscall.SysProcAttr { return nil }
