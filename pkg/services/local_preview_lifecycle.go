package services

import (
	"fmt"
	"strings"
	"sync"
)

const (
	LocalPreviewBindAddress    = "127.0.0.1"
	DefaultLocalPreviewPortMin = 14100
	DefaultLocalPreviewPortMax = 14999
)

type LocalPreviewProcessState string

const (
	LocalPreviewStopped  LocalPreviewProcessState = "stopped"
	LocalPreviewStarting LocalPreviewProcessState = "starting"
	LocalPreviewReady    LocalPreviewProcessState = "ready"
	LocalPreviewFailed   LocalPreviewProcessState = "failed"
	LocalPreviewStopping LocalPreviewProcessState = "stopping"
)

type LocalPreviewProcessSlot struct {
	SiteID string
	Port   int
	State  LocalPreviewProcessState
	Error  string
}

// LocalPreviewLifecycle reserves internal loopback ports and records process
// state. LocalPreviewManager owns command construction, readiness checks,
// proxying, process shutdown and retry on bind races.
type LocalPreviewLifecycle struct {
	mu        sync.Mutex
	portMin   int
	portMax   int
	nextPort  int
	bySite    map[string]LocalPreviewProcessSlot
	portOwner map[int]string
}

func NewLocalPreviewLifecycle(portMin, portMax int) (*LocalPreviewLifecycle, error) {
	if portMin < 1 || portMax > 65535 || portMin > portMax {
		return nil, fmt.Errorf("invalid local preview port range %d-%d", portMin, portMax)
	}
	return &LocalPreviewLifecycle{
		portMin:   portMin,
		portMax:   portMax,
		nextPort:  portMin,
		bySite:    make(map[string]LocalPreviewProcessSlot),
		portOwner: make(map[int]string),
	}, nil
}

func NewDefaultLocalPreviewLifecycle() *LocalPreviewLifecycle {
	lifecycle, err := NewLocalPreviewLifecycle(DefaultLocalPreviewPortMin, DefaultLocalPreviewPortMax)
	if err != nil {
		panic(err)
	}
	return lifecycle
}

// Reserve returns one stable slot per site. portAvailable probes loopback
// availability; a nil callback treats every unreserved port as available. A
// child-process bind failure must release/re-reserve and retry.
func (l *LocalPreviewLifecycle) Reserve(siteID string, portAvailable func(int) bool) (LocalPreviewProcessSlot, error) {
	siteID = strings.TrimSpace(siteID)
	if siteID == "" {
		return LocalPreviewProcessSlot{}, fmt.Errorf("site id is required")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if slot, ok := l.bySite[siteID]; ok {
		return slot, nil
	}

	capacity := l.portMax - l.portMin + 1
	for checked := 0; checked < capacity; checked++ {
		port := l.nextPort
		l.nextPort++
		if l.nextPort > l.portMax {
			l.nextPort = l.portMin
		}
		if _, reserved := l.portOwner[port]; reserved {
			continue
		}
		if portAvailable != nil && !portAvailable(port) {
			continue
		}

		slot := LocalPreviewProcessSlot{SiteID: siteID, Port: port, State: LocalPreviewStopped}
		l.bySite[siteID] = slot
		l.portOwner[port] = siteID
		return slot, nil
	}

	return LocalPreviewProcessSlot{}, fmt.Errorf("no local preview ports available in range %d-%d", l.portMin, l.portMax)
}

func (l *LocalPreviewLifecycle) Get(siteID string) (LocalPreviewProcessSlot, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	slot, ok := l.bySite[siteID]
	return slot, ok
}

func (l *LocalPreviewLifecycle) Transition(siteID string, next LocalPreviewProcessState, processErr error) (LocalPreviewProcessSlot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	slot, ok := l.bySite[siteID]
	if !ok {
		return LocalPreviewProcessSlot{}, fmt.Errorf("local preview slot for site %q is not reserved", siteID)
	}
	if !validLocalPreviewTransition(slot.State, next) {
		return LocalPreviewProcessSlot{}, fmt.Errorf("invalid local preview transition %s -> %s for site %q", slot.State, next, siteID)
	}

	slot.State = next
	if processErr != nil {
		slot.Error = processErr.Error()
	} else if next != LocalPreviewFailed {
		slot.Error = ""
	}
	l.bySite[siteID] = slot
	return slot, nil
}

func (l *LocalPreviewLifecycle) Release(siteID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	slot, ok := l.bySite[siteID]
	if !ok {
		return nil
	}
	if slot.State != LocalPreviewStopped && slot.State != LocalPreviewFailed {
		return fmt.Errorf("cannot release local preview slot for site %q while state is %s", siteID, slot.State)
	}
	delete(l.bySite, siteID)
	delete(l.portOwner, slot.Port)
	return nil
}

func validLocalPreviewTransition(current, next LocalPreviewProcessState) bool {
	switch current {
	case LocalPreviewStopped:
		return next == LocalPreviewStarting
	case LocalPreviewStarting:
		return next == LocalPreviewReady || next == LocalPreviewFailed || next == LocalPreviewStopping
	case LocalPreviewReady:
		return next == LocalPreviewFailed || next == LocalPreviewStopping
	case LocalPreviewFailed:
		return next == LocalPreviewStarting || next == LocalPreviewStopped
	case LocalPreviewStopping:
		return next == LocalPreviewStopped || next == LocalPreviewFailed
	default:
		return false
	}
}
