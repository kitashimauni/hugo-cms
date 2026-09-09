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
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLocalPreviewStartupTimeout  = 2 * time.Minute
	DefaultLocalPreviewStopTimeout     = 10 * time.Second
	defaultLocalPreviewProbeInterval   = 50 * time.Millisecond
	defaultLocalPreviewStartAttempts   = 3
	localPreviewStderrLimit            = 64 << 10
	localPreviewHugoEnvironment        = "development"
	localPreviewStartupTimeoutEnv      = "HUGO_CMS_LOCAL_PREVIEW_STARTUP_TIMEOUT"
	localPreviewIdleTimeoutEnv         = "HUGO_CMS_LOCAL_PREVIEW_IDLE_TIMEOUT"
	eleventyLocalPreviewReadyPath      = "/__hugo_cms_ready"
	eleventyLocalPreviewMetadataPath   = "/__hugo_cms_metadata"
	eleventyLocalPreviewInvalidatePath = "/__hugo_cms_invalidate"
)

// IsLocalPreviewControlPath reports whether requestPath resolves to an
// internal Eleventy control endpoint. Canonicalizing the path before matching
// keeps dot-segment variants from reaching the loopback wrapper through the
// public preview ingress.
func IsLocalPreviewControlPath(requestPath string) bool {
	switch path.Clean(requestPath) {
	case eleventyLocalPreviewReadyPath, eleventyLocalPreviewMetadataPath, eleventyLocalPreviewInvalidatePath:
		return true
	default:
		return false
	}
}

var (
	errLocalPreviewShuttingDown         = errors.New("local preview manager is shutting down")
	ErrLocalPreviewMetadataInvalidation = errors.New("local preview metadata invalidation failed")
)

type localPreviewCommandFactory func(context.Context, config.SiteRuntime, int, string) (*exec.Cmd, error)

type managedLocalPreviewProcess struct {
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	done        chan struct{} // process termination; closed before cleanup starts
	cleanupDone chan struct{}
	stderr      *cappedBuffer
	cleanup     func() error

	mu         sync.RWMutex
	waitErr    error
	cleanupErr error
}

func (p *managedLocalPreviewProcess) setWaitErr(err error) {
	p.mu.Lock()
	p.waitErr = err
	p.mu.Unlock()
}

func (p *managedLocalPreviewProcess) setCleanupErr(err error) {
	p.mu.Lock()
	p.cleanupErr = err
	p.mu.Unlock()
}

func (p *managedLocalPreviewProcess) cleanupError() error {
	p.mu.RLock()
	err := p.cleanupErr
	p.mu.RUnlock()
	return err
}

func (p *managedLocalPreviewProcess) processError() error {
	p.mu.RLock()
	err := p.waitErr
	p.mu.RUnlock()
	if err == nil {
		err = errors.New("local preview process exited")
	}
	return p.errorWithStderr(err)
}

func (p *managedLocalPreviewProcess) errorWithStderr(err error) error {
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

func (p *managedLocalPreviewProcess) finishCleanup(siteID string) {
	var err error
	if p.cleanup != nil {
		err = p.cleanup()
	}
	p.setCleanupErr(err)
	close(p.cleanupDone)
	if err != nil {
		slog.Error("Local preview cleanup failed after process termination", "site", siteID, "error", err)
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

// LocalPreviewManager owns generator preview child processes and connects the
// Phase 1 lifecycle contract to lazy startup, readiness probing and proxying.
// It intentionally does not own TLS or viewer authentication; those remain
// preview-ingress responsibilities.
type LocalPreviewManager struct {
	lifecycle *LocalPreviewLifecycle

	mu           sync.Mutex
	processes    map[string]*managedLocalPreviewProcess
	siteLocks    map[string]*sync.Mutex
	shuttingDown bool

	commandFactory localPreviewCommandFactory
	startupTimeout time.Duration
	probeInterval  time.Duration
	startAttempts  int
	idleTimeout    time.Duration
}

func NewLocalPreviewManager(lifecycle *LocalPreviewLifecycle) *LocalPreviewManager {
	if lifecycle == nil {
		lifecycle = NewDefaultLocalPreviewLifecycle()
	}
	return &LocalPreviewManager{
		lifecycle:      lifecycle,
		processes:      make(map[string]*managedLocalPreviewProcess),
		siteLocks:      make(map[string]*sync.Mutex),
		commandFactory: generatorLocalPreviewCommand,
		startupTimeout: configuredLocalPreviewStartupTimeout(),
		probeInterval:  defaultLocalPreviewProbeInterval,
		startAttempts:  defaultLocalPreviewStartAttempts,
		idleTimeout:    configuredLocalPreviewIdleTimeout(),
	}
}

func configuredLocalPreviewStartupTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv(localPreviewStartupTimeoutEnv))
	if value == "" {
		return defaultLocalPreviewStartupTimeout
	}
	timeout, err := time.ParseDuration(value)
	if err == nil && timeout > 0 {
		return timeout
	}
	slog.Warn("Invalid local preview startup timeout; using default", "value", value, "default", defaultLocalPreviewStartupTimeout)
	return defaultLocalPreviewStartupTimeout
}

func configuredLocalPreviewIdleTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv(localPreviewIdleTimeoutEnv))
	if value == "" {
		return DefaultLocalPreviewIdleTimeout
	}
	timeout, err := time.ParseDuration(value)
	if err == nil && timeout >= 0 {
		return timeout
	}
	slog.Warn("Invalid local preview idle timeout; using default", "value", value, "default", DefaultLocalPreviewIdleTimeout)
	return DefaultLocalPreviewIdleTimeout
}

var defaultLocalPreviewManager = NewLocalPreviewManager(nil)

func DefaultLocalPreviewManager() *LocalPreviewManager {
	return defaultLocalPreviewManager
}

// IdleTimeout returns the site runtime idle timeout. A zero duration
// explicitly disables automatic idle cleanup.
func (m *LocalPreviewManager) IdleTimeout() time.Duration {
	return m.idleTimeout
}

// StopIdle releases workspaces that have exceeded the runtime idle timeout.
// It uses the same claim -> process stop -> workspace detach sequence as an
// explicit stop, so an article switch never enters this path.
func (m *LocalPreviewManager) StopIdle(ctx context.Context, workspaceManager *LocalPreviewWorkspaceManager) error {
	if workspaceManager == nil || m.idleTimeout <= 0 {
		return nil
	}
	var errs []error
	for _, workspace := range workspaceManager.IdleWorkspaces(m.idleTimeout) {
		claim, claimed, err := workspaceManager.ClaimRelease(workspace.SiteID, workspace.DraftID)
		if err != nil {
			if errors.Is(err, ErrLocalPreviewSessionReleasing) || errors.Is(err, ErrLocalPreviewSessionReclaiming) {
				continue
			}
			errs = append(errs, fmt.Errorf("claim idle local preview workspace for site %q: %w", workspace.SiteID, err))
			continue
		}
		if !claimed {
			continue
		}
		if err := m.Stop(ctx, workspace.SiteID); err != nil {
			workspaceManager.CancelRelease(claim)
			errs = append(errs, fmt.Errorf("stop idle local preview for site %q: %w", workspace.SiteID, err))
			continue
		}
		if _, err := workspaceManager.FinishRelease(claim); err != nil {
			workspaceManager.CancelRelease(claim)
			errs = append(errs, fmt.Errorf("detach idle local preview workspace for site %q: %w", workspace.SiteID, err))
		}
	}
	return errors.Join(errs...)
}

// RunLocalPreviewIdleReaper periodically applies the configured idle timeout
// until ctx is cancelled. The server owns the context so shutdown can stop
// the reaper before the workspace manager is closed.
func RunLocalPreviewIdleReaper(ctx context.Context, manager *LocalPreviewManager, workspaceManager *LocalPreviewWorkspaceManager) {
	if manager == nil || workspaceManager == nil || manager.idleTimeout <= 0 {
		return
	}
	interval := time.Minute
	if half := manager.idleTimeout / 2; half > 0 && half < interval {
		interval = half
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stopCtx, cancel := context.WithTimeout(ctx, DefaultLocalPreviewStopTimeout)
			if err := manager.StopIdle(stopCtx, workspaceManager); err != nil {
				slog.Warn("Failed to stop idle Local Live Preview runtime", "error", err)
			}
			cancel()
		}
	}
}

func (m *LocalPreviewManager) BeginShutdown() {
	m.mu.Lock()
	m.shuttingDown = true
	m.mu.Unlock()
}

func (m *LocalPreviewManager) isShuttingDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shuttingDown
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
	return m.ensureReadyRuntime(config.NewSiteRuntime(site))
}

func (m *LocalPreviewManager) ensureReadyRuntime(runtime config.SiteRuntime) (LocalPreviewProcessSlot, error) {
	if m.isShuttingDown() {
		return LocalPreviewProcessSlot{}, errLocalPreviewShuttingDown
	}
	if runtime.LocalPreview.Enabled == nil || !*runtime.LocalPreview.Enabled {
		return LocalPreviewProcessSlot{}, fmt.Errorf("local preview is disabled for site %q", runtime.ID)
	}
	if !localPreviewGeneratorSupported(runtime.Generator) {
		return LocalPreviewProcessSlot{}, fmt.Errorf("local live preview is not supported for generator %q", runtime.Generator)
	}

	previewURL := strings.TrimSpace(runtime.LocalPreview.URL)
	if previewURL == "" {
		var err error
		previewURL, err = config.LocalPreviewURL(runtime.ID)
		if err != nil {
			return LocalPreviewProcessSlot{}, err
		}
	}

	lock := m.siteLock(runtime.ID)
	lock.Lock()
	defer lock.Unlock()

	if m.isShuttingDown() {
		return LocalPreviewProcessSlot{}, errLocalPreviewShuttingDown
	}

	if slot, ok := m.lifecycle.Get(runtime.ID); ok {
		process := m.process(runtime.ID)
		switch slot.State {
		case LocalPreviewReady:
			if process != nil && !process.exited() {
				return slot, nil
			}
			_, _ = m.lifecycle.Transition(runtime.ID, LocalPreviewFailed, errors.New("local preview process is not running"))
		case LocalPreviewFailed:
			// Cleanup below before allocating another port.
		case LocalPreviewStopped:
			// Release the stale reservation below.
		case LocalPreviewStarting, LocalPreviewStopping:
			return LocalPreviewProcessSlot{}, fmt.Errorf("local preview for site %q is unexpectedly %s", runtime.ID, slot.State)
		}
		m.cleanupFailedSlotLocked(runtime.ID, process, nil)
	}

	startAttempts := m.startAttempts
	if isEleventyLocalPreviewGenerator(runtime.Generator) {
		// A failed Eleventy initial build is usually expensive. Retrying it
		// immediately repeats the same full build without changing the cause.
		startAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= startAttempts; attempt++ {
		if m.isShuttingDown() {
			return LocalPreviewProcessSlot{}, errLocalPreviewShuttingDown
		}

		slot, err := m.lifecycle.Reserve(runtime.ID, localPreviewPortAvailable)
		if err != nil {
			return LocalPreviewProcessSlot{}, err
		}
		if _, err := m.lifecycle.Transition(runtime.ID, LocalPreviewStarting, nil); err != nil {
			return LocalPreviewProcessSlot{}, err
		}

		process, err := m.startProcess(runtime, slot.Port, previewURL)
		if err != nil {
			lastErr = err
			m.cleanupFailedSlotLocked(runtime.ID, process, err)
			continue
		}
		if m.isShuttingDown() {
			m.cleanupFailedSlotLocked(runtime.ID, process, errLocalPreviewShuttingDown)
			return LocalPreviewProcessSlot{}, errLocalPreviewShuttingDown
		}

		if err := m.waitUntilReady(process, runtime, slot.Port); err != nil {
			lastErr = err
			m.cleanupFailedSlotLocked(runtime.ID, process, err)
			continue
		}

		readySlot, err := m.lifecycle.Transition(runtime.ID, LocalPreviewReady, nil)
		if err != nil {
			m.cleanupFailedSlotLocked(runtime.ID, process, err)
			return LocalPreviewProcessSlot{}, err
		}
		if m.isShuttingDown() {
			m.cleanupFailedSlotLocked(runtime.ID, process, errLocalPreviewShuttingDown)
			return LocalPreviewProcessSlot{}, errLocalPreviewShuttingDown
		}
		if process.exited() {
			lastErr = process.processError()
			m.cleanupFailedSlotLocked(runtime.ID, process, lastErr)
			continue
		}
		return readySlot, nil
	}

	if lastErr == nil {
		lastErr = errors.New("local preview failed to start")
	}
	return LocalPreviewProcessSlot{}, fmt.Errorf("failed to start local preview for site %q after %d attempts: %w", runtime.ID, startAttempts, lastErr)
}

func (m *LocalPreviewManager) startProcess(runtime config.SiteRuntime, port int, previewURL string) (*managedLocalPreviewProcess, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd, err := m.commandFactory(ctx, runtime, port, previewURL)
	if err != nil {
		cancel()
		return nil, err
	}
	cleanup := localPreviewProcessCleanup(runtime, cmd.Dir)

	stderr := newCappedBuffer(localPreviewStderrLimit)
	// Generator processes may emit useful startup/build diagnostics to either
	// stream. Keep the
	// bounded tail of both without allowing child output to grow memory without
	// limit or leak CMS secrets through the browser response.
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		if cleanupErr := cleanup(); cleanupErr != nil {
			slog.Error("Local preview cleanup failed after process start error", "site", runtime.ID, "error", cleanupErr)
		}
		return nil, err
	}

	process := &managedLocalPreviewProcess{
		cmd:         cmd,
		cancel:      cancel,
		done:        make(chan struct{}),
		cleanupDone: make(chan struct{}),
		stderr:      stderr,
		cleanup:     cleanup,
	}
	m.setProcess(runtime.ID, process)

	go func() {
		process.setWaitErr(cmd.Wait())
		close(process.done)
		go process.finishCleanup(runtime.ID)
		m.handleProcessExit(runtime.ID, process)
	}()

	return process, nil
}

func (m *LocalPreviewManager) waitUntilReady(process *managedLocalPreviewProcess, runtime config.SiteRuntime, port int) error {
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
			return process.errorWithStderr(fmt.Errorf("local preview did not become ready within %s", m.startupTimeout))
		case <-ticker.C:
			if isEleventyLocalPreviewGenerator(runtime.Generator) {
				ready, err := localPreviewHTTPReady(address)
				if err != nil || !ready {
					continue
				}
				select {
				case <-process.done:
					return process.processError()
				default:
					return nil
				}
			}
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

func localPreviewHTTPReady(address string) (bool, error) {
	client := &http.Client{Timeout: 250 * time.Millisecond}
	request, err := http.NewRequest(http.MethodGet, "http://"+address+eleventyLocalPreviewReadyPath, nil)
	if err != nil {
		return false, err
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK, nil
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
	m.shuttingDown = true
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
	return m.ProxyRuntime(w, r, config.NewSiteRuntime(site))
}

// InvalidateArticleURL marks the next Eleventy watch build as required before
// shadow content is written. A running process is optional: a process started
// after the write will always build the latest workspace content from scratch.
func (m *LocalPreviewManager) InvalidateArticleURL(runtime config.SiteRuntime) error {
	if !isEleventyLocalPreviewGenerator(runtime.Generator) {
		return nil
	}
	process := m.process(runtime.ID)
	if process == nil || process.exited() {
		return nil
	}
	slot, ok := m.Status(runtime.ID)
	if !ok || slot.State != LocalPreviewReady {
		return nil
	}
	address := net.JoinHostPort(LocalPreviewBindAddress, strconv.Itoa(slot.Port))
	request, err := http.NewRequest(http.MethodPost, "http://"+address+eleventyLocalPreviewInvalidatePath, nil)
	if err != nil {
		return fmt.Errorf("%w: create request: %v", ErrLocalPreviewMetadataInvalidation, err)
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		if process.exited() {
			return nil
		}
		return fmt.Errorf("%w: %v", ErrLocalPreviewMetadataInvalidation, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%w: endpoint returned %s", ErrLocalPreviewMetadataInvalidation, response.Status)
	}
	return nil
}

// ResolveArticleURL resolves an Eleventy article through the metadata map kept
// by the already-running preview process. Hugo keeps its existing resolver
// because its list command is inexpensive and already reflects its watch state.
func (m *LocalPreviewManager) ResolveArticleURL(ctx context.Context, runtime config.SiteRuntime, workspace LocalPreviewWorkspace, articlePath string) (string, error) {
	if !isEleventyLocalPreviewGenerator(runtime.Generator) {
		return ResolvePreviewArticleURL(ctx, runtime, workspace, articlePath)
	}

	articlePath = filepath.Clean(strings.TrimSpace(articlePath))
	if articlePath == "." || filepath.IsAbs(articlePath) {
		return "", fmt.Errorf("invalid preview article path")
	}
	if workspace.ContentDir == "" {
		return "", fmt.Errorf("preview workspace content directory is required")
	}
	slot, err := m.ensureReadyRuntime(runtime)
	if err != nil {
		return "", fmt.Errorf("ensure Eleventy local preview ready: %w", err)
	}
	process := m.process(runtime.ID)
	if process == nil || process.exited() {
		return "", fmt.Errorf("Eleventy local preview process is not running")
	}
	return resolveRunningEleventyArticleURL(ctx, runtime, slot.Port, articlePath)
}

func (m *LocalPreviewManager) ProxyRuntime(w http.ResponseWriter, r *http.Request, runtime config.SiteRuntime) error {
	proxy, err := m.PrepareProxyRuntime(runtime)
	if err != nil {
		return err
	}
	proxy.ServeHTTP(w, r)
	return nil
}

// PrepareProxyRuntime ensures that the generator process and its listening
// port are ready, then builds the reverse proxy for that fixed target. The
// caller may release workspace transition gates after this method returns;
// ServeHTTP can remain active for a long-lived HTTP or WebSocket stream.
func (m *LocalPreviewManager) PrepareProxyRuntime(runtime config.SiteRuntime) (http.Handler, error) {
	slot, err := m.ensureReadyRuntime(runtime)
	if err != nil {
		return nil, err
	}
	proxy, err := newLocalPreviewReverseProxy(runtime, slot.Port)
	if err != nil {
		return nil, err
	}
	return proxy, nil
}

func newLocalPreviewReverseProxy(runtime config.SiteRuntime, port int) (*httputil.ReverseProxy, error) {
	previewURL := strings.TrimSpace(runtime.LocalPreview.URL)
	if previewURL == "" {
		var err error
		previewURL, err = config.LocalPreviewURL(runtime.ID)
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
			slog.Error("Local preview upstream error", "site", runtime.ID, "error", proxyErr)
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

func generatorLocalPreviewCommand(ctx context.Context, runtime config.SiteRuntime, port int, previewURL string) (*exec.Cmd, error) {
	switch strings.ToLower(strings.TrimSpace(runtime.Generator)) {
	case "", "hugo":
		return hugoLocalPreviewCommand(ctx, runtime, port, previewURL)
	case "eleventy", "11ty":
		return eleventyLocalPreviewCommand(ctx, runtime, port)
	default:
		return nil, fmt.Errorf("local live preview is not supported for generator %q", runtime.Generator)
	}
}

func localPreviewGeneratorSupported(generator string) bool {
	switch strings.ToLower(strings.TrimSpace(generator)) {
	case "", "hugo", "eleventy", "11ty":
		return true
	default:
		return false
	}
}

func hugoLocalPreviewCommand(ctx context.Context, runtime config.SiteRuntime, port int, previewURL string) (*exec.Cmd, error) {
	args, err := hugoLocalPreviewArgs(runtime, port, previewURL)
	if err != nil {
		return nil, err
	}
	return generatorCommandContext(ctx, runtime, "hugo", args...), nil
}

func eleventyLocalPreviewCommand(ctx context.Context, runtime config.SiteRuntime, port int) (*exec.Cmd, error) {
	pm, err := detectEleventyPackageManager(runtime.RepoPath)
	if err != nil {
		return nil, err
	}
	projectDir, outputDir, err := prepareEleventyLocalPreviewProject(runtime)
	if err != nil {
		return nil, err
	}
	args, err := eleventyLocalPreviewArgs(runtime, port, outputDir)
	if err != nil {
		if runtime.LocalPreviewProjectDir == "" {
			_ = os.RemoveAll(projectDir)
		}
		return nil, err
	}
	commandRuntime := runtime
	commandRuntime.RepoPath = projectDir
	commandRuntime.ContentDir, err = eleventyLocalPreviewInputDir(runtime)
	if err != nil {
		if runtime.LocalPreviewProjectDir == "" {
			_ = os.RemoveAll(projectDir)
		}
		return nil, err
	}
	commandRuntime.LocalPreviewProjectDir = projectDir
	return generatorCommandContextWithEnv(
		ctx,
		commandRuntime,
		[]string{"NODE_ENV=development", "ELEVENTY_ENV=development"},
		pm.Bin,
		eleventyNodeCommandArgs(pm, args[1], args[2:]...)...,
	), nil
}

func eleventyLocalPreviewArgs(runtime config.SiteRuntime, port int, outputDir string) ([]string, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid local preview port %d", port)
	}
	if strings.TrimSpace(outputDir) == "" || !filepath.IsAbs(outputDir) {
		return nil, fmt.Errorf("Eleventy local preview output directory must be absolute")
	}
	inputDir, err := eleventyLocalPreviewInputDir(runtime)
	if err != nil {
		return nil, err
	}
	scriptPath, err := eleventyLocalPreviewScriptPath()
	if err != nil {
		return nil, err
	}
	return []string{
		"node", scriptPath,
		"--serve",
		"--input", inputDir,
		"--output", outputDir,
		"--port", strconv.Itoa(port),
		"--host", LocalPreviewBindAddress,
	}, nil
}

func eleventyLocalPreviewScriptPath() (string, error) {
	const scriptName = "eleventy-local-preview.cjs"
	candidates := make([]string, 0, 2)
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "scripts", scriptName))
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		for current := workingDirectory; ; current = filepath.Dir(current) {
			candidates = append(candidates, filepath.Join(current, "scripts", scriptName))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Eleventy local preview helper script is unavailable")
}

func prepareEleventyLocalPreviewProject(runtime config.SiteRuntime) (string, string, error) {
	projectDir := strings.TrimSpace(runtime.LocalPreviewProjectDir)
	workspaceProject := projectDir != ""
	if workspaceProject {
		var err error
		projectDir, err = filepath.Abs(projectDir)
		if err != nil {
			return "", "", fmt.Errorf("resolve Eleventy local preview project root: %w", err)
		}
	} else {
		baseDir := filepath.Join(os.TempDir(), "hugo-cms-local-preview")
		if err := os.MkdirAll(baseDir, 0700); err != nil {
			return "", "", fmt.Errorf("create Eleventy local preview project root: %w", err)
		}
		var err error
		projectDir, err = os.MkdirTemp(baseDir, "project-")
		if err != nil {
			return "", "", fmt.Errorf("allocate Eleventy local preview project root: %w", err)
		}
	}
	inputDir, err := eleventyLocalPreviewInputDir(runtime)
	if err != nil {
		if !workspaceProject {
			_ = os.RemoveAll(projectDir)
		}
		return "", "", err
	}
	publicDir, err := eleventyLocalPreviewPublicDir(runtime)
	if err != nil {
		if !workspaceProject {
			_ = os.RemoveAll(projectDir)
		}
		return "", "", err
	}

	if !workspaceProject {
		contentSource := strings.TrimSpace(runtime.ContentDir)
		if !filepath.IsAbs(contentSource) {
			contentSource = filepath.Join(runtime.RepoPath, inputDir)
		}
		if err := createEleventyLocalPreviewProjectOverlay(runtime.RepoPath, projectDir, inputDir, publicDir, contentSource); err != nil {
			_ = os.RemoveAll(projectDir)
			return "", "", err
		}
	} else if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("Eleventy local preview project root is unavailable: %w", err)
	}

	outputDir := filepath.Join(projectDir, publicDir)
	if err := os.RemoveAll(outputDir); err != nil {
		if !workspaceProject {
			_ = os.RemoveAll(projectDir)
		}
		return "", "", fmt.Errorf("reset Eleventy local preview output: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		if !workspaceProject {
			_ = os.RemoveAll(projectDir)
		}
		return "", "", fmt.Errorf("create Eleventy local preview output: %w", err)
	}
	return projectDir, outputDir, nil
}

func localPreviewProcessCleanup(runtime config.SiteRuntime, projectDir string) func() error {
	if !isEleventyLocalPreviewGenerator(runtime.Generator) {
		return func() error { return nil }
	}
	// The workspace manager owns workspace directories. Removing their output
	// here would duplicate release cleanup and could race with a new session
	// reusing the same draft directory.
	if runtime.LocalPreviewProjectDir != "" {
		return func() error { return nil }
	}
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return func() error { return nil }
	}
	projectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return func() error {
			return fmt.Errorf("resolve Eleventy local preview cleanup path: %w", err)
		}
	}
	return func() error {
		if err := os.RemoveAll(projectDir); err != nil {
			return fmt.Errorf("remove Eleventy local preview project %q: %w", projectDir, err)
		}
		return nil
	}
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
		"--environment", localPreviewHugoEnvironment,
		"--contentDir", runtime.ContentDir,
		"--bind", LocalPreviewBindAddress,
		"--port", strconv.Itoa(port),
		"--baseURL", previewURL,
		"--appendPort=false",
		"--liveReloadPort", strconv.Itoa(liveReloadPort),
		"--renderToMemory",
		"--buildDrafts",
		"--buildFuture",
		"--buildExpired",
		"--watch",
		"--noHTTPCache",
	}, nil
}
