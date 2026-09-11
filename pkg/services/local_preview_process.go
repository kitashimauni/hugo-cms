package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	localPreviewProcessGracePeriod = 2 * time.Second
	localPreviewProcessKillWait    = 2 * time.Second
	localPreviewProcessWaitDelay   = 500 * time.Millisecond
)

// terminateManagedLocalPreviewProcess stops the generator process tree before
// callers release the lifecycle slot. The command context is cancelled only
// after the tree has been signalled and Wait has observed termination; doing
// it earlier would kill only the package-manager parent and strand children.
func terminateManagedLocalPreviewProcess(ctx context.Context, siteID string, process *managedLocalPreviewProcess) error {
	if process == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if process.exited() {
		cancelProcessContext(process)
		return nil
	}

	// Tests and startup races may provide a synthetic process without an OS
	// process. Preserve the existing cancellation contract for that case.
	if process.cmd == nil || process.cmd.Process == nil {
		cancelProcessContext(process)
		select {
		case <-process.done:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("stopping local preview for site %q: %w", siteID, ctx.Err())
		}
	}

	gracefulErr := signalLocalPreviewProcess(process.cmd, false)
	if gracefulErr != nil && !process.exited() {
		slog.Warn("Failed to send graceful Local Live Preview stop signal",
			"site", siteID,
			"process", localPreviewProcessDescription(process.cmd),
			"error", gracefulErr,
		)
	}

	gracefulTimer := time.NewTimer(localPreviewProcessGracePeriod)
	defer gracefulTimer.Stop()
	select {
	case <-process.done:
		cancelProcessContext(process)
		return nil
	case <-ctx.Done():
		// The caller deadline is also a reason to escalate immediately. If the
		// process tree is killed successfully, stopping still succeeded.
	case <-gracefulTimer.C:
	}

	forceErr := signalLocalPreviewProcess(process.cmd, true)
	if forceErr != nil && !process.exited() {
		slog.Warn("Failed to send forced Local Live Preview stop signal",
			"site", siteID,
			"process", localPreviewProcessDescription(process.cmd),
			"error", forceErr,
		)
	}

	killTimer := time.NewTimer(localPreviewProcessKillWait)
	defer killTimer.Stop()
	select {
	case <-process.done:
		cancelProcessContext(process)
		return nil
	case <-killTimer.C:
		cancelProcessContext(process)
		description := localPreviewProcessDescription(process.cmd)
		if forceErr != nil {
			err := fmt.Errorf("local preview process tree did not terminate for site %q (%s): %w", siteID, description, forceErr)
			slog.Error("Local preview process tree termination timed out", "site", siteID, "process", description, "error", err)
			return err
		}
		err := fmt.Errorf("local preview process tree did not terminate for site %q (%s)", siteID, description)
		slog.Error("Local preview process tree termination timed out", "site", siteID, "process", description, "error", err)
		return err
	}
}

func cancelProcessContext(process *managedLocalPreviewProcess) {
	if process != nil && process.cancel != nil {
		process.cancel()
	}
}
