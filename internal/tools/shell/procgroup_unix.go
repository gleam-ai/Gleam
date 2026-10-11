//go:build !windows

package shell

import (
	"os/exec"
	"syscall"
)

// setProcessGroup Unix：把子进程放进独立进程组，取消时可整组杀死。
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTree Unix：杀死整个进程组（负号表示进程组 ID）。
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// 负号表示杀进程组而非单个进程
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
