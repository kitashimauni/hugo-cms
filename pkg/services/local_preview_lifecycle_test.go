package services

import (
	"errors"
	"testing"
)

func TestLocalPreviewLifecycleReservesStablePorts(t *testing.T) {
	lifecycle, err := NewLocalPreviewLifecycle(15000, 15001)
	if err != nil {
		t.Fatalf("NewLocalPreviewLifecycle() error = %v", err)
	}

	available := func(port int) bool { return port != 15000 }
	first, err := lifecycle.Reserve("tech", available)
	if err != nil {
		t.Fatalf("Reserve(tech) error = %v", err)
	}
	if first.Port != 15001 || first.State != LocalPreviewStopped {
		t.Fatalf("first slot = %#v, want port 15001 stopped", first)
	}

	again, err := lifecycle.Reserve("tech", available)
	if err != nil {
		t.Fatalf("Reserve(tech) second error = %v", err)
	}
	if again.Port != first.Port {
		t.Fatalf("second reservation port = %d, want stable %d", again.Port, first.Port)
	}

	if _, err := lifecycle.Reserve("daily", available); err == nil {
		t.Fatal("Reserve(daily) should fail when no usable port remains")
	}

	if err := lifecycle.Release("tech"); err != nil {
		t.Fatalf("Release(tech) error = %v", err)
	}
	second, err := lifecycle.Reserve("daily", available)
	if err != nil {
		t.Fatalf("Reserve(daily) after release error = %v", err)
	}
	if second.Port != 15001 {
		t.Fatalf("daily port = %d, want reused 15001", second.Port)
	}
}

func TestLocalPreviewLifecycleTransitions(t *testing.T) {
	lifecycle, err := NewLocalPreviewLifecycle(15100, 15100)
	if err != nil {
		t.Fatalf("NewLocalPreviewLifecycle() error = %v", err)
	}
	if _, err := lifecycle.Reserve("tech", nil); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}

	starting, err := lifecycle.Transition("tech", LocalPreviewStarting, nil)
	if err != nil || starting.State != LocalPreviewStarting {
		t.Fatalf("starting transition = %#v, %v", starting, err)
	}
	ready, err := lifecycle.Transition("tech", LocalPreviewReady, nil)
	if err != nil || ready.State != LocalPreviewReady {
		t.Fatalf("ready transition = %#v, %v", ready, err)
	}
	if _, err := lifecycle.Transition("tech", LocalPreviewStarting, nil); err == nil {
		t.Fatal("ready -> starting should be rejected")
	}
	stopping, err := lifecycle.Transition("tech", LocalPreviewStopping, nil)
	if err != nil || stopping.State != LocalPreviewStopping {
		t.Fatalf("stopping transition = %#v, %v", stopping, err)
	}
	stopped, err := lifecycle.Transition("tech", LocalPreviewStopped, nil)
	if err != nil || stopped.State != LocalPreviewStopped {
		t.Fatalf("stopped transition = %#v, %v", stopped, err)
	}
	if err := lifecycle.Release("tech"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestLocalPreviewLifecycleRecordsFailure(t *testing.T) {
	lifecycle, err := NewLocalPreviewLifecycle(15200, 15200)
	if err != nil {
		t.Fatalf("NewLocalPreviewLifecycle() error = %v", err)
	}
	if _, err := lifecycle.Reserve("tech", nil); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if _, err := lifecycle.Transition("tech", LocalPreviewStarting, nil); err != nil {
		t.Fatalf("starting error = %v", err)
	}

	failed, err := lifecycle.Transition("tech", LocalPreviewFailed, errors.New("bind failed"))
	if err != nil {
		t.Fatalf("failed transition error = %v", err)
	}
	if failed.Error != "bind failed" {
		t.Fatalf("failed.Error = %q", failed.Error)
	}
	if err := lifecycle.Release("tech"); err != nil {
		t.Fatalf("failed slot should be releasable: %v", err)
	}
}

func TestNewLocalPreviewLifecycleRejectsInvalidRange(t *testing.T) {
	for _, tc := range [][2]int{{0, 10}, {10, 9}, {1, 70000}} {
		if _, err := NewLocalPreviewLifecycle(tc[0], tc[1]); err == nil {
			t.Fatalf("range %d-%d should fail", tc[0], tc[1])
		}
	}
}
