package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeManagedProcess struct {
	mu        sync.Mutex
	startErr  error
	waitCh    chan error
	killOnce  sync.Once
	killCount int
}

func newFakeManagedProcess() *fakeManagedProcess {
	return &fakeManagedProcess{waitCh: make(chan error, 1)}
}

func (process *fakeManagedProcess) Start() error {
	return process.startErr
}

func (process *fakeManagedProcess) Wait() error {
	return <-process.waitCh
}

func (process *fakeManagedProcess) Kill() error {
	process.mu.Lock()
	process.killCount++
	process.mu.Unlock()
	process.killOnce.Do(func() {
		process.waitCh <- errors.New("killed")
	})
	return nil
}

func TestProcessManagerTracksNaturalExit(t *testing.T) {
	manager := &ProcessManager{}
	process := newFakeManagedProcess()
	exited := make(chan error, 1)

	if err := manager.Start(func() managedProcess { return process }, func(err error) {
		exited <- err
	}); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if !manager.Running() {
		t.Fatal("manager should report a running process")
	}

	process.waitCh <- nil
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("exit callback error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for process exit")
	}
	if manager.Running() {
		t.Fatal("manager should clear a naturally exited process")
	}
}

func TestProcessManagerStopWaitsBeforeReplacement(t *testing.T) {
	manager := &ProcessManager{}
	first := newFakeManagedProcess()
	if err := manager.Start(func() managedProcess { return first }, nil); err != nil {
		t.Fatalf("start first process: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if manager.Running() {
		t.Fatal("manager should not report the stopped process as running")
	}

	second := newFakeManagedProcess()
	if err := manager.Start(func() managedProcess { return second }, nil); err != nil {
		t.Fatalf("start replacement process: %v", err)
	}
	if !manager.Running() {
		t.Fatal("replacement process should remain registered")
	}

	if err := manager.Stop(ctx); err != nil {
		t.Fatalf("stop replacement process: %v", err)
	}
}

func TestProcessManagerDoesNotRegisterFailedStart(t *testing.T) {
	manager := &ProcessManager{}
	process := newFakeManagedProcess()
	process.startErr = errors.New("start failed")

	if err := manager.Start(func() managedProcess { return process }, nil); err == nil {
		t.Fatal("Start() should return the process start error")
	}
	if manager.Running() {
		t.Fatal("failed process must not be registered")
	}
}
