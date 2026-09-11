//go:build !windows

package services

import (
	"fmt"
	"os/exec"
	"syscall"
)

func configureLocalPreviewCommand(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.WaitDelay = localPreviewProcessWaitDelay
}

func signalLocalPreviewProcess(cmd *exec.Cmd, force bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	if err := syscall.Kill(-cmd.Process.Pid, signal); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

func localPreviewProcessTreeAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	err := syscall.Kill(-cmd.Process.Pid, 0)
	return err == nil || err == syscall.EPERM
}

func localPreviewProcessDescription(cmd *exec.Cmd) string {
	if cmd == nil || cmd.Process == nil {
		return "pid=unknown process_group=unknown"
	}
	return fmt.Sprintf("pid=%d process_group=%d", cmd.Process.Pid, cmd.Process.Pid)
}
