package services

import (
	"context"
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLocalPreviewStartupTimeout = 15 * time.Second
	defaultLocalPreviewProbeInterval  = 50 * time.Millisecond
	defaultLocalPreviewStartAttempts  = 3
	localPreviewStderrLimit           = 64 << 10
)

type localPreviewCommandFactory func(context.Context, config.SiteRuntime, int, string) (*exec.Cmd, error)

type managedLocalPreviewProcess struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
	stderr *cappedBuffer

	mu      sync.RWMutex
	waitErr error
}

func (p *managedLocalPreviewProcess) setWaitErr(err error) {
	p.mu.Lock()
	p.waitErr = err
	p.mu.Unlock()
}

func (p *managedLocalPreviewProcess) processError() error {
	p.mu.RLock()
	err := p.waitErr
	p.mu.RUnlock()
	if err == nil {
		err = errors.New("local preview process exited")
	}
	if p.stderr == nil {
		return err
	}
	stderr := strings.TrimSpace(p.stderr.String())
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}

func (p *managedLocalPreviewProcess) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

type cappedBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newCappedBuffer(max int) *cappedBuffer {
	return &cappedBuffer{max: max}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	originalLen := len(p)
	if b.max <= 0 {
		return originalLen, nil
	}
	if len(p) >= b.max {
		b.buf = append(b.buf[:0], p[len(p)-b.max:]...)
		return originalLen, nil
	}
	if overflow := len(b.buf) + len(p) - b.max; overflow > 0 {
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:len(b.buf)-overflow]
	}
	b.buf = append(b.buf, p...)
	return originalLen, nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.buf...))
}

// LocalPreviewManager owns Hugo preview child processes and connects the
// Phase 1 lifecycle contract to lazy startup, readiness probing and proxying.
// It intentionally does not own TLS or viewer authentication; those remain
// preview-ingress responsibilities.
type LocalPreviewManager struct {
	lifecycle *LocalPreviewLifecycle

	mu        sync.Mutex
	processes map[string]*managedLocalPreviewProcess
	siteLocks map[string]*sync.Mutex

	commandFactory localPreviewCommandFactory
	startupTimeout time.Duration
	probeInterval  time.Duration
	startAttempts  int
}

func NewLocalPreviewManager(lifecycle *LocalPreviewLifecycle) *LocalPreviewManager {
	if lifecycle == nil {
		lifecycle = NewDefaultLocalPreviewLifecycle()
	}
	return &LocalPreviewManager{
		lifecycle:      lifecycle,
		processes:      make(map[string]*managedLocalPreviewProcess),
		siteLocks:      make(map[string]*sync.Mutex),
		commandFactory: hugoLocalPreviewCommand,
		startupTimeout: defaultLocalPreviewStartupTimeout,
		probeInterval:  defaultLocalPreviewProbeInterval,
		startAttempts:  defaultLocalPreviewStartAttempts,
	}
}

var defaultLocalPreviewManager = NewLocalPreviewManager(nil)

func DefaultLocalPreviewManager() *LocalPreviewManager {
	return defaultLocalPreviewManager
}

func (m *LocalPreviewManager) siteLock(siteID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock, ok := m.siteLocks[siteID]
	if !ok {
		lock = &sync.Mutex{}
		m.siteLocks[siteID] = lock
	}
	return lock
}

func (m *LocalPreviewManager) process(siteID string) *managedLocalPreviewProcess {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processes[siteID]
}

func (m *LocalPreviewManager) setProcess(siteID string, process *managedLocalPreviewProcess) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if process == nil {
		delete(m.processes, siteID)
		return
	}
	m.processes[siteID] = process
}

func (m *LocalPreviewManager) removeProcessIfCurrent(siteID string, process *managedLocalPreviewProcess) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processes[siteID] == process {
		delete(m.processes, siteID)
	}
}

func (m *LocalPreviewManager) Status(siteID string) (LocalPreviewProcessSlot, bool) {
	return m.lifecycle.Get(siteID)
}

func (m *LocalPreviewManager) EnsureReady(site config.SiteConfig) (LocalPreviewProcessSlot, error) {
	if site.Preview.LocalPreview.Enabled == nil || !*site.Preview.LocalPreview.Enabled {
		return LocalPreviewProcessSlot{}, fmt.Errorf("local preview is disabled for site %q", site.ID)
	}
	generator := strings.TrimSpace(site.Generator)
	if generator != "" && !strings.EqualFold(generator, "hugo") {
		return LocalPreviewProcessSlot{}, fmt.Errorf("local live preview currently supports Hugo only, got %q", site.Generator)
	}

	previewURL := strings.TrimSpace(site.Preview.LocalPreview.URL)
	if previewURL == "" {
		var err error
		previewURL, err = config.LocalPreviewURL(site.ID)
		if err != nil {
			return LocalPreviewProcessSlot{}, err
		}
	}
	runtime := config.NewSiteRuntime(site)

	lock := m.siteLock(site.ID)
	lock.Lock()
	defer lock.Unlock()

	if slot, ok := m.lifecycle.Get(site.ID); ok {
		process := m.process(site.ID)
		switch slot.State {
		case LocalPreviewReady:
			if process != nil && !process.exited() {
				return slot, nil
			}
			_, _ = m.lifecycle.Transition(site.ID, LocalPreviewFailed, errors.New("local preview process is not running"))
		case LocalPreviewFailed:
			// Cleanup below before allocating another port.
		case LocalPreviewStopped:
			// Release the stale reservation below.
		case LocalPreviewStarting, LocalPreviewStopping:
			return LocalPreviewProcessSlot{}, fmt.Errorf("local preview for site %q is unexpectedly %s", site.ID, slot.State)
		}
		m.cleanupFailedSlotLocked(site.ID, process, nil)
	}

	var lastErr error
	for attempt := 1; attempt <= m.startAttempts; attempt++ {
		slot, err := m.lifecycle.Reserve(site.ID, localPreviewPortAvailable)
		if err != nil {
			return LocalPreviewProcessSlot{}, err
		}
		if _, err := m.lifecycle.Transition(site.ID, LocalPreviewStarting, nil); err != nil {
			return LocalPreviewProcessSlot{}, err
		}

		process, err := m.startProcess(runtime, slot.Port, previewURL)
		if err != nil {
			lastErr = err
			m.cleanupFailedSlotLocked(site.ID, process, err)
			continue
		}

		if err := m.waitUntilReady(process, slot.Port); err != nil {
			lastErr = err
			m.cleanupFailedSlotLocked(site.ID, process, err)
			continue
		}

		readySlot, err := m.lifecycle.Transition(site.ID, LocalPreviewReady, nil)
		if err != nil {
			m.cleanupFailedSlotLocked(site.ID, process, err)
			return LocalPreviewProcessSlot{}, err
		}
		if process.exited() {
			lastErr = process.processError()
			m.cleanupFailedSlotLocked(site.ID, process, lastErr)
			continue
		}
		return readySlot, nil
	}

	if lastErr == nil {
		lastErr = errors.New("local preview failed to start")
	}
	return LocalPreviewProcessSlot{}, fmt.Errorf("failed to start local preview for site %q after %d attempts: %w", site.ID, m.startAttempts, lastErr)
}

func (m *LocalPreviewManager) startProcess(runtime config.SiteRuntime, port int, previewURL string) (*managedLocalPreviewProcess, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd, err := m.commandFactory(ctx, runtime, port, previewURL)
	if err != nil {
		cancel()
		return nil, err
	}

	stderr := newCappedBuffer(localPreviewStderrLimit)
	// Hugo may emit useful startup/build diagnostics to either stream. Keep the
	// bounded tail of both without allowing child output to grow memory without
	// limit or leak CMS secrets through the browser response.
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	process := &managedLocalPreviewProcess{
		cmd:    cmd,
		cancel: cancel,
		done:   make(chan struct{}),
		stderr: stderr,
	}
	m.setProcess(runtime.ID, process)

	go func() {
		process.setWaitErr(cmd.Wait())
		close(process.done)
		m.handleProcessExit(runtime.ID, process)
	}()

	return process, nil
}

func (m *LocalPreviewManager) waitUntilReady(process *managedLocalPreviewProcess, port int) error {
	deadline := time.NewTimer(m.startupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(m.probeInterval)
	defer ticker.Stop()

	address := net.JoinHostPort(LocalPreviewBindAddress, strconv.Itoa(port))
	for {
		select {
		case <-process.done:
			return process.processError()
		case <-deadline.C:
			return fmt.Errorf("local preview did not become ready within %s", m.startupTimeout)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
			if err != nil {
				continue
			}
			_ = conn.Close()
			select {
			case <-process.done:
				return process.processError()
			default:
				return nil
			}
		}
	}
}

func (m *LocalPreviewManager) cleanupFailedSlotLocked(siteID string, process *managedLocalPreviewProcess, processErr error) {
	if process != nil {
		process.cancel()
		select {
		case <-process.done:
		case <-time.After(time.Second):
			if process.cmd.Process != nil {
				_ = process.cmd.Process.Kill()
			}
			select {
			case <-process.done:
			case <-time.After(time.Second):
			}
		}
		m.removeProcessIfCurrent(siteID, process)
	}

	slot, ok := m.lifecycle.Get(siteID)
	if !ok {
		return
	}
	if processErr == nil {
		processErr = errors.New("local preview process is not running")
	}
	switch slot.State {
	case LocalPreviewStarting, LocalPreviewReady, LocalPreviewStopping:
		_, _ = m.lifecycle.Transition(siteID, LocalPreviewFailed, processErr)
	}
	if slot, ok = m.lifecycle.Get(siteID); ok && slot.State == LocalPreviewFailed {
		_, _ = m.lifecycle.Transition(siteID, LocalPreviewStopped, nil)
	}
	_ = m.lifecycle.Release(siteID)
}

func (m *LocalPreviewManager) handleProcessExit(siteID string, process *managedLocalPreviewProcess) {
	lock := m.siteLock(siteID)
	lock.Lock()
	defer lock.Unlock()

	if m.process(siteID) != process {
		return
	}
	m.removeProcessIfCurrent(siteID, process)

	slot, ok := m.lifecycle.Get(siteID)
	if !ok {
		return
	}
	switch slot.State {
	case LocalPreviewReady:
		_, _ = m.lifecycle.Transition(siteID, LocalPreviewFailed, process.processError())
	case LocalPreviewStopping:
		_, _ = m.lifecycle.Transition(siteID, LocalPreviewStopped, nil)
	}
}

func (m *LocalPreviewManager) Stop(ctx context.Context, siteID string) error {
	lock := m.siteLock(siteID)
	lock.Lock()
	defer lock.Unlock()

	process := m.process(siteID)
	slot, ok := m.lifecycle.Get(siteID)
	if !ok {
		return nil
	}

	if process == nil {
		if slot.State == LocalPreviewFailed {
			_, _ = m.lifecycle.Transition(siteID, LocalPreviewStopped, nil)
		}
		return m.lifecycle.Release(siteID)
	}

	if slot.State == LocalPreviewStarting || slot.State == LocalPreviewReady {
		if _, err := m.lifecycle.Transition(siteID, LocalPreviewStopping, nil); err != nil {
			return err
		}
	}
	process.cancel()

	select {
	case <-process.done:
	case <-ctx.Done():
		if process.cmd.Process != nil {
			_ = process.cmd.Process.Kill()
		}
		return fmt.Errorf("stopping local preview for site %q: %w", siteID, ctx.Err())
	}
	m.removeProcessIfCurrent(siteID, process)

	if current, exists := m.lifecycle.Get(siteID); exists {
		switch current.State {
		case LocalPreviewStopping:
			_, _ = m.lifecycle.Transition(siteID, LocalPreviewStopped, nil)
		case LocalPreviewFailed:
			_, _ = m.lifecycle.Transition(siteID, LocalPreviewStopped, nil)
		}
	}
	return m.lifecycle.Release(siteID)
}

func (m *LocalPreviewManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	siteIDs := make([]string, 0, len(m.processes))
	for siteID := range m.processes {
		siteIDs = append(siteIDs, siteID)
	}
	m.mu.Unlock()

	var errs []error
	for _, siteID := range siteIDs {
		if err := m.Stop(ctx, siteID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *LocalPreviewManager) Proxy(w http.ResponseWriter, r *http.Request, site config.SiteConfig) error {
	slot, err := m.EnsureReady(site)
	if err != nil {
		return err
	}
	proxy, err := newLocalPreviewReverseProxy(site, slot.Port)
	if err != nil {
		return err
	}
	proxy.ServeHTTP(w, r)
	return nil
}

func newLocalPreviewReverseProxy(site config.SiteConfig, port int) (*httputil.ReverseProxy, error) {
	previewURL := strings.TrimSpace(site.Preview.LocalPreview.URL)
	if previewURL == "" {
		var err error
		previewURL, err = config.LocalPreviewURL(site.ID)
		if err != nil {
			return nil, err
		}
	}
	external, err := url.Parse(previewURL)
	if err != nil || external.Scheme == "" || external.Host == "" {
		return nil, fmt.Errorf("invalid local preview URL %q", previewURL)
	}
	target, _ := url.Parse("http://" + net.JoinHostPort(LocalPreviewBindAddress, strconv.Itoa(port)))

	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = request.In.Host
			request.SetXForwarded()
			request.Out.Header.Set("X-Forwarded-Proto", external.Scheme)
			request.Out.Header.Set("X-Forwarded-Host", external.Host)
		},
		ModifyResponse: func(response *http.Response) error {
			location := response.Header.Get("Location")
			if location == "" {
				return nil
			}
			parsed, err := url.Parse(location)
			if err != nil || parsed.Host == "" {
				return nil
			}
			if !isInternalLocalPreviewHost(parsed.Host, port) {
				return nil
			}
			parsed.Scheme = external.Scheme
			parsed.Host = external.Host
			response.Header.Set("Location", parsed.String())
			return nil
		},
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
			slog.Error("Local preview upstream error", "site", site.ID, "error", proxyErr)
			http.Error(writer, "local preview upstream unavailable", http.StatusBadGateway)
		},
	}
	return proxy, nil
}

func isInternalLocalPreviewHost(host string, port int) bool {
	hostname, portText, err := net.SplitHostPort(host)
	if err != nil || portText != strconv.Itoa(port) {
		return false
	}
	return hostname == LocalPreviewBindAddress || strings.EqualFold(hostname, "localhost")
}

func localPreviewPortAvailable(port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort(LocalPreviewBindAddress, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func hugoLocalPreviewCommand(ctx context.Context, runtime config.SiteRuntime, port int, previewURL string) (*exec.Cmd, error) {
	args, err := hugoLocalPreviewArgs(runtime, port, previewURL)
	if err != nil {
		return nil, err
	}
	return generatorCommandContext(ctx, runtime, "hugo", args...), nil
}

func hugoLocalPreviewArgs(runtime config.SiteRuntime, port int, previewURL string) ([]string, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid local preview port %d", port)
	}
	parsed, err := url.Parse(previewURL)
	if err != nil || parsed.Hostname() == "" || parsed.Port() != "" {
		return nil, fmt.Errorf("invalid local preview URL %q", previewURL)
	}

	liveReloadPort := 0
	switch parsed.Scheme {
	case "https":
		liveReloadPort = 443
	case "http":
		liveReloadPort = 80
	default:
		return nil, fmt.Errorf("unsupported local preview scheme %q", parsed.Scheme)
	}

	return []string{
		"server",
		"--source", ".",
		"--contentDir", runtime.ContentDir,
		"--bind", LocalPreviewBindAddress,
		"--port", strconv.Itoa(port),
		"--baseURL", previewURL,
		"--appendPort=false",
		"--liveReloadPort", strconv.Itoa(liveReloadPort),
		"--renderToMemory",
		"--buildDrafts",
		"--buildFuture",
		"--watch",
		"--noHTTPCache",
	}, nil
}
