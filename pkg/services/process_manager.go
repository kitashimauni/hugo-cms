package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

type managedProcess interface {
	Start() error
	Wait() error
	Kill() error
}

type execManagedProcess struct {
	cmd *exec.Cmd
}

func newExecManagedProcess(cmd *exec.Cmd) *execManagedProcess {
	configureManagedCommand(cmd)
	return &execManagedProcess{cmd: cmd}
}

func (process *execManagedProcess) Start() error {
	return process.cmd.Start()
}

func (process *execManagedProcess) Wait() error {
	return process.cmd.Wait()
}

func (process *execManagedProcess) Kill() error {
	if process.cmd.Process == nil {
		return os.ErrProcessDone
	}
	return killManagedProcessTree(process.cmd)
}

type ProcessManager struct {
	mu         sync.Mutex
	process    managedProcess
	generation uint64
	done       chan struct{}
}

func (manager *ProcessManager) Start(factory func() managedProcess, onExit func(error)) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.process != nil {
		return nil
	}

	process := factory()
	if process == nil {
		return fmt.Errorf("process factory returned nil")
	}
	if err := process.Start(); err != nil {
		return err
	}

	manager.generation++
	generation := manager.generation
	done := make(chan struct{})
	manager.process = process
	manager.done = done

	go func() {
		err := process.Wait()

		manager.mu.Lock()
		if manager.generation == generation && manager.process == process {
			manager.process = nil
			manager.done = nil
		}
		manager.mu.Unlock()

		close(done)
		if onExit != nil {
			onExit(err)
		}
	}()
	return nil
}

func (manager *ProcessManager) Stop(ctx context.Context) error {
	manager.mu.Lock()
	process := manager.process
	done := manager.done
	manager.mu.Unlock()

	if process == nil {
		return nil
	}

	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for process exit: %w", ctx.Err())
	}
}

func (manager *ProcessManager) Running() bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.process != nil
}
