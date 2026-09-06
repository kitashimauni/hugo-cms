package services

import (
	"context"
	"errors"
	"hugo-cms/pkg/config"
	"os/exec"
	"testing"
	"time"
)

func TestLocalPreviewManagerShutdownCatchesStartupAfterSnapshot(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)

	factoryEntered := make(chan struct{})
	releaseFactory := make(chan struct{})
	manager.commandFactory = func(ctx context.Context, runtime config.SiteRuntime, port int, previewURL string) (*exec.Cmd, error) {
		close(factoryEntered)
		<-releaseFactory
		return testLocalPreviewCommand(ctx, runtime, port, previewURL)
	}

	ensureDone := make(chan error, 1)
	go func() {
		_, err := manager.EnsureReady(site)
		ensureDone <- err
	}()

	select {
	case <-factoryEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("EnsureReady() did not reach command factory")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := manager.Shutdown(shutdownCtx); err != nil {
		cancel()
		t.Fatalf("Shutdown() error = %v", err)
	}
	cancel()

	// At this point Shutdown has already taken its process snapshot while the
	// startup is still between slot reservation and process registration.
	close(releaseFactory)

	select {
	case err := <-ensureDone:
		if !errors.Is(err, errLocalPreviewShuttingDown) {
			t.Fatalf("EnsureReady() error = %v, want shutting down", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EnsureReady() did not finish after shutdown")
	}

	if _, ok := manager.Status(site.ID); ok {
		t.Fatal("startup racing with Shutdown() left a lifecycle slot behind")
	}
	if process := manager.process(site.ID); process != nil {
		t.Fatal("startup racing with Shutdown() left a child process behind")
	}
}
