//go:build windows

package services

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func configureManagedCommand(_ *exec.Cmd) {
}

func killManagedProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}

	killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	output, err := killCmd.CombinedOutput()
	if err == nil {
		return nil
	}

	// If the wrapper has already exited, Process.Kill returns os.ErrProcessDone.
	// Otherwise this still gives us a best-effort fallback for the top-level
	// process, while preserving taskkill's diagnostic if both paths fail.
	if killErr := cmd.Process.Kill(); killErr == nil || errors.Is(killErr, os.ErrProcessDone) {
		return nil
	}
	return fmt.Errorf("kill process tree: %w: %s", err, output)
}
