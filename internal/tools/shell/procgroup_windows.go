//go:build windows

package shell

import (
	"os/exec"
	"syscall"
)

// setProcessGroup Windows：设置 CREATE_NEW_PROCESS_GROUP 标志。
// 注意：Windows 的 Job Object 需要更复杂的 API 调用，这里先用简单方案：
// 依赖 WaitDelay 来处理孤儿进程，同时尝试杀死主进程。
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// CREATE_NEW_PROCESS_GROUP 让子进程成为新进程组的根
	cmd.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP
}

// killProcessTree Windows：杀死主进程。
// TODO: 完整实现需要枚举子进程（CreateToolhelp32Snapshot），
// 但目前先杀主进程，WaitDelay 会处理剩余的管道持有者。
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
