//go:build windows

package services

import (
	"fmt"
	"os/exec"
)

func configureLocalPreviewCommand(cmd *exec.Cmd) {
	cmd.WaitDelay = localPreviewProcessWaitDelay
}

// Windows does not provide the Unix process-group semantics used by the
// package-manager wrappers. Keep the same lifecycle contract with the best
// available direct-process termination; Unix/Linux uses the full tree path.
func signalLocalPreviewProcess(cmd *exec.Cmd, _ bool) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func localPreviewProcessDescription(cmd *exec.Cmd) string {
	if cmd == nil || cmd.Process == nil {
		return "pid=unknown process_group=unsupported"
	}
	return fmt.Sprintf("pid=%d process_group=unsupported", cmd.Process.Pid)
}
