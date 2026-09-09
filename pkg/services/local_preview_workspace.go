package services

import (
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultLocalPreviewLeaseTTL    = 2 * time.Minute
	DefaultLocalPreviewIdleTimeout = 30 * time.Minute
)

var (
	ErrLocalPreviewSessionConflict   = errors.New("another local preview session is already active for this site")
	ErrLocalPreviewSessionMismatch   = errors.New("local preview session article does not match request")
	ErrLocalPreviewSessionNotFound   = errors.New("local preview session is not active")
	ErrLocalPreviewSessionNotStale   = errors.New("local preview session is still active")
	ErrLocalPreviewSessionExpired    = errors.New("local preview session lease has expired")
	ErrLocalPreviewSessionReclaiming = errors.New("local preview session is being reclaimed")
	ErrLocalPreviewSessionReleasing  = errors.New("local preview session is being released")
)

// LocalPreviewWorkspace is the site-scoped unsaved-content workspace and its
// current editor owner/article metadata. The generator project belongs to the
// site and is reused when the owner selects another article.
type LocalPreviewWorkspace struct {
	SiteID      string
	DraftID     string
	ArticlePath string
	ContentDir  string
	ProjectDir  string
	Revision    uint64
	LastSeenAt  time.Time
}

// LocalPreviewReclaim is an opaque ownership token for reclaiming one stale
// workspace. Callers must either finish or cancel the claim before another
// session for the same site can be created.
type LocalPreviewReclaim struct {
	siteID  string
	draftID string
	token   uint64
}

type localPreviewReclaimState struct {
	draftID string
	token   uint64
}

// LocalPreviewRelease is an opaque ownership token for detaching an active
// workspace before its filesystem cleanup runs. Callers must either finish or
// cancel the claim after stopping the generator process.
type LocalPreviewRelease struct {
	siteID  string
	draftID string
	token   uint64
}

type localPreviewReleaseState struct {
	draftID string
	token   uint64
}

// LocalPreviewIngressLease protects preview preparation from a concurrent
// release/reclaim transition. Callers release it after the process/port and
// proxy target are fixed, before serving a potentially long-lived stream.
type LocalPreviewIngressLease struct {
	gate *sync.RWMutex
	once sync.Once
}

func (lease *LocalPreviewIngressLease) Release() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.gate != nil {
			lease.gate.RUnlock()
		}
	})
}

// LocalPreviewWorkspaceManager owns ephemeral shadow content directories and,
// for Eleventy, a temporary project-root overlay. Hugo keeps the original
// repository as its generator source root; Eleventy receives production
// project references with only content/public materialized into the overlay.
type LocalPreviewWorkspaceManager struct {
	root             string
	mu               sync.Mutex
	sessions         map[string]LocalPreviewWorkspace
	reclaiming       map[string]localPreviewReclaimState
	releasing        map[string]localPreviewReleaseState
	siteGates        map[string]*sync.RWMutex
	nextReclaimToken uint64
	nextReleaseToken uint64
	closed           bool
	leaseTTL         time.Duration
	now              func() time.Time
	removeWorkspace  func(string) error
}

func NewLocalPreviewWorkspaceManager(root string) (*LocalPreviewWorkspaceManager, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("local preview workspace root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local preview workspace root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0700); err != nil {
		return nil, fmt.Errorf("create local preview workspace root: %w", err)
	}
	return &LocalPreviewWorkspaceManager{
		root:            absRoot,
		sessions:        make(map[string]LocalPreviewWorkspace),
		reclaiming:      make(map[string]localPreviewReclaimState),
		releasing:       make(map[string]localPreviewReleaseState),
		siteGates:       make(map[string]*sync.RWMutex),
		leaseTTL:        DefaultLocalPreviewLeaseTTL,
		now:             time.Now,
		removeWorkspace: os.RemoveAll,
	}, nil
}

var (
	defaultLocalPreviewWorkspaceOnce sync.Once
	defaultLocalPreviewWorkspace     *LocalPreviewWorkspaceManager
	defaultLocalPreviewWorkspaceErr  error
)

func DefaultLocalPreviewWorkspaceManager() (*LocalPreviewWorkspaceManager, error) {
	defaultLocalPreviewWorkspaceOnce.Do(func() {
		root, err := os.MkdirTemp("", "hugo-cms-local-preview-*")
		if err != nil {
			defaultLocalPreviewWorkspaceErr = fmt.Errorf("create local preview temporary root: %w", err)
			return
		}
		defaultLocalPreviewWorkspace, defaultLocalPreviewWorkspaceErr = NewLocalPreviewWorkspaceManager(root)
		if defaultLocalPreviewWorkspaceErr != nil {
			_ = os.RemoveAll(root)
		}
	})
	return defaultLocalPreviewWorkspace, defaultLocalPreviewWorkspaceErr
}

func (m *LocalPreviewWorkspaceManager) LeaseTTL() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leaseTTL
}

func (m *LocalPreviewWorkspaceManager) currentTimeLocked() time.Time {
	if m.now == nil {
		return time.Now().UTC()
	}
	return m.now().UTC()
}

func (m *LocalPreviewWorkspaceManager) staleLocked(workspace LocalPreviewWorkspace, now time.Time) bool {
	if workspace.LastSeenAt.IsZero() {
		return true
	}
	return now.Sub(workspace.LastSeenAt) > m.leaseTTL
}

func (m *LocalPreviewWorkspaceManager) reclaimingLocked(siteID string) bool {
	_, ok := m.reclaiming[siteID]
	return ok
}

func (m *LocalPreviewWorkspaceManager) releasingLocked(siteID string) bool {
	_, ok := m.releasing[siteID]
	return ok
}

func (m *LocalPreviewWorkspaceManager) transitionErrorLocked(siteID string) error {
	if m.reclaimingLocked(siteID) {
		return ErrLocalPreviewSessionReclaiming
	}
	if m.releasingLocked(siteID) {
		return ErrLocalPreviewSessionReleasing
	}
	return nil
}

func (m *LocalPreviewWorkspaceManager) siteGate(siteID string) *sync.RWMutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	gate := m.siteGates[siteID]
	if gate == nil {
		gate = &sync.RWMutex{}
		m.siteGates[siteID] = gate
	}
	return gate
}

// AcquireIngress returns one consistent workspace snapshot while holding the
// site's read gate. Callers must release the returned lease after preparing a
// fixed proxy target; release/reclaim claims use the corresponding write gate.
func (m *LocalPreviewWorkspaceManager) AcquireIngress(siteID string) (LocalPreviewIngressLease, LocalPreviewWorkspace, bool, bool) {
	gate := m.siteGate(siteID)
	gate.RLock()

	m.mu.Lock()
	transitioning := m.closed || m.transitionErrorLocked(siteID) != nil
	workspace, active := m.sessions[siteID]
	m.mu.Unlock()
	if transitioning {
		gate.RUnlock()
		return LocalPreviewIngressLease{}, LocalPreviewWorkspace{}, false, true
	}
	return LocalPreviewIngressLease{gate: gate}, workspace, active, false
}

// Update creates the site's workspace on the first request and applies the
// newest revision for the selected article. The same owner may switch article
// paths without recreating the workspace or restarting the generator. Older
// in-flight HTTP requests become harmless no-ops instead of overwriting newer
// editor state. Requests renew a lease only while it is still valid; an
// expired session must be reclaimed before it can restart.
func (m *LocalPreviewWorkspaceManager) Update(runtime config.SiteRuntime, draftID, articlePath string, revision uint64, content []byte) (LocalPreviewWorkspace, bool, bool, error) {
	return m.update(runtime, draftID, articlePath, revision, content, nil)
}

// UpdateWithBeforeWrite applies an update after running beforeWrite once all
// session and path validation has succeeded. The hook is used to invalidate
// generator-derived state immediately before the shadow file changes, which
// prevents a consumer from observing the previous build as current.
func (m *LocalPreviewWorkspaceManager) UpdateWithBeforeWrite(runtime config.SiteRuntime, draftID, articlePath string, revision uint64, content []byte, beforeWrite func() error) (LocalPreviewWorkspace, bool, bool, error) {
	return m.update(runtime, draftID, articlePath, revision, content, beforeWrite)
}

func (m *LocalPreviewWorkspaceManager) update(runtime config.SiteRuntime, draftID, articlePath string, revision uint64, content []byte, beforeWrite func() error) (LocalPreviewWorkspace, bool, bool, error) {
	if err := validateDraftID(draftID); err != nil {
		return LocalPreviewWorkspace{}, false, false, err
	}
	articlePath = filepath.Clean(strings.TrimSpace(articlePath))
	if articlePath == "." || filepath.IsAbs(articlePath) {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("invalid local preview article path")
	}
	if revision == 0 {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("local preview revision must be greater than zero")
	}
	if runtime.ID == "" {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("local preview site ID is required")
	}

	sourceContentDir := SafeJoin(runtime.RepoPath, "", runtime.ContentDir)
	if sourceContentDir == "" {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("invalid local preview content directory")
	}
	if SafeJoin(sourceContentDir, "", articlePath) == "" {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("invalid local preview article path")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("local preview workspace manager is closed")
	}
	if err := m.transitionErrorLocked(runtime.ID); err != nil {
		return LocalPreviewWorkspace{}, false, false, err
	}
	now := m.currentTimeLocked()

	workspace, exists := m.sessions[runtime.ID]
	created := false
	if exists {
		if m.staleLocked(workspace, now) {
			return LocalPreviewWorkspace{}, false, false, ErrLocalPreviewSessionExpired
		}
		if workspace.DraftID != draftID {
			return LocalPreviewWorkspace{}, false, false, ErrLocalPreviewSessionConflict
		}
		if revision <= workspace.Revision {
			workspace.LastSeenAt = now
			m.sessions[runtime.ID] = workspace
			return workspace, false, false, nil
		}
		if workspace.ArticlePath != filepath.ToSlash(articlePath) {
			// Keep the site workspace and advance the selected article only for a
			// newer request. A late request for the previous article is therefore a
			// harmless no-op instead of moving the active selection backwards.
			workspace.ArticlePath = filepath.ToSlash(articlePath)
		}
		workspace.LastSeenAt = now
	} else {
		workspaceRoot := filepath.Join(m.root, runtime.ID)
		if err := os.RemoveAll(workspaceRoot); err != nil {
			return LocalPreviewWorkspace{}, false, false, fmt.Errorf("reset local preview workspace: %w", err)
		}

		contentDir := filepath.Join(workspaceRoot, "content")
		projectDir := ""
		if isEleventyLocalPreviewGenerator(runtime.Generator) {
			inputDir, err := eleventyLocalPreviewInputDir(runtime)
			if err != nil {
				return LocalPreviewWorkspace{}, false, false, err
			}
			publicDir, err := eleventyLocalPreviewPublicDir(runtime)
			if err != nil {
				return LocalPreviewWorkspace{}, false, false, err
			}
			contentDir = filepath.Join(workspaceRoot, inputDir)
			if err := createEleventyLocalPreviewProjectOverlay(runtime.RepoPath, workspaceRoot, inputDir, publicDir, sourceContentDir); err != nil {
				_ = os.RemoveAll(workspaceRoot)
				return LocalPreviewWorkspace{}, false, false, err
			}
			projectDir = workspaceRoot
		} else if err := copyLocalPreviewContentTree(sourceContentDir, contentDir); err != nil {
			_ = os.RemoveAll(workspaceRoot)
			return LocalPreviewWorkspace{}, false, false, err
		}
		workspace = LocalPreviewWorkspace{
			SiteID:      runtime.ID,
			DraftID:     draftID,
			ArticlePath: filepath.ToSlash(articlePath),
			ContentDir:  contentDir,
			ProjectDir:  projectDir,
			LastSeenAt:  now,
		}
		created = true
	}

	target := SafeJoin(workspace.ContentDir, "", articlePath)
	if target == "" {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("invalid local preview workspace path")
	}
	if beforeWrite != nil {
		if err := beforeWrite(); err != nil {
			if created {
				_ = os.RemoveAll(filepath.Dir(workspace.ContentDir))
			}
			return LocalPreviewWorkspace{}, false, false, err
		}
	}
	if err := writeLocalPreviewFileAtomic(target, content); err != nil {
		if created {
			_ = os.RemoveAll(filepath.Dir(workspace.ContentDir))
		}
		return LocalPreviewWorkspace{}, false, false, err
	}
	workspace.Revision = revision
	workspace.LastSeenAt = now
	m.sessions[runtime.ID] = workspace
	return workspace, created, true, nil
}

// Heartbeat renews a live lease without changing content. Once the lease has
// expired, the browser must reclaim/restart instead of reviving a session whose
// generator process may already be stopping.
func (m *LocalPreviewWorkspaceManager) Heartbeat(siteID, draftID string) (LocalPreviewWorkspace, error) {
	if err := validateDraftID(draftID); err != nil {
		return LocalPreviewWorkspace{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return LocalPreviewWorkspace{}, fmt.Errorf("local preview workspace manager is closed")
	}
	if err := m.transitionErrorLocked(siteID); err != nil {
		return LocalPreviewWorkspace{}, err
	}
	workspace, ok := m.sessions[siteID]
	if !ok {
		return LocalPreviewWorkspace{}, ErrLocalPreviewSessionNotFound
	}
	if workspace.DraftID != draftID {
		return LocalPreviewWorkspace{}, ErrLocalPreviewSessionConflict
	}
	now := m.currentTimeLocked()
	if m.staleLocked(workspace, now) {
		return LocalPreviewWorkspace{}, ErrLocalPreviewSessionExpired
	}
	workspace.LastSeenAt = now
	m.sessions[siteID] = workspace
	return workspace, nil
}

// Status reports the active workspace and whether its lease has expired.
func (m *LocalPreviewWorkspaceManager) Status(siteID string) (LocalPreviewWorkspace, bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.transitionErrorLocked(siteID) != nil {
		return LocalPreviewWorkspace{}, false, false
	}
	workspace, ok := m.sessions[siteID]
	if !ok {
		return LocalPreviewWorkspace{}, false, false
	}
	return workspace, true, m.staleLocked(workspace, m.currentTimeLocked())
}

// IdleWorkspaces returns active site workspaces that have not received an
// editor update or heartbeat within timeout. The caller must claim each
// returned workspace before stopping its generator so a concurrent editor
// request cannot revive it during cleanup.
func (m *LocalPreviewWorkspaceManager) IdleWorkspaces(timeout time.Duration) []LocalPreviewWorkspace {
	if timeout <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	now := m.currentTimeLocked()
	idle := make([]LocalPreviewWorkspace, 0)
	for siteID, workspace := range m.sessions {
		if m.transitionErrorLocked(siteID) != nil || workspace.LastSeenAt.IsZero() {
			continue
		}
		if now.Sub(workspace.LastSeenAt) >= timeout {
			idle = append(idle, workspace)
		}
	}
	return idle
}

// SyncContentResource mirrors a content-directory resource change made through
// the normal CMS media API into an already-active shadow workspace. Static
// resources do not need this because the generator overlay still references
// the production static tree.
func (m *LocalPreviewWorkspaceManager) SyncContentResource(runtime config.SiteRuntime, repoPath string, deleted bool) (bool, error) {
	if runtime.ID == "" {
		return false, fmt.Errorf("local preview site ID is required")
	}
	sourceContentDir := SafeJoin(runtime.RepoPath, "", runtime.ContentDir)
	if sourceContentDir == "" {
		return false, fmt.Errorf("invalid local preview content directory")
	}
	sourcePath := SafeJoin(runtime.RepoPath, "", filepath.Clean(strings.TrimSpace(repoPath)))
	if sourcePath == "" {
		return false, fmt.Errorf("invalid local preview resource path")
	}
	relative, err := filepath.Rel(sourceContentDir, sourcePath)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false, fmt.Errorf("local preview workspace manager is closed")
	}
	if m.transitionErrorLocked(runtime.ID) != nil {
		return false, nil
	}
	workspace, active := m.sessions[runtime.ID]
	if !active || m.staleLocked(workspace, m.currentTimeLocked()) {
		return false, nil
	}
	target := SafeJoin(workspace.ContentDir, "", relative)
	if target == "" {
		return false, fmt.Errorf("invalid local preview resource target")
	}
	if deleted {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove local preview content resource: %w", err)
		}
		return true, nil
	}

	info, err := os.Lstat(sourcePath)
	if err != nil {
		return false, fmt.Errorf("stat local preview content resource: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("local preview content resource must be a regular file")
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return false, fmt.Errorf("read local preview content resource: %w", err)
	}
	if err := writeLocalPreviewFileAtomic(target, content); err != nil {
		return false, err
	}
	return true, nil
}

// Release is idempotent for a site without an active workspace. A stale tab is
// never allowed to release another tab's active session.
func (m *LocalPreviewWorkspaceManager) Release(siteID, draftID string) (bool, error) {
	if err := validateDraftID(draftID); err != nil {
		return false, err
	}
	gate := m.siteGate(siteID)
	gate.Lock()
	m.mu.Lock()
	if err := m.transitionErrorLocked(siteID); err != nil {
		m.mu.Unlock()
		gate.Unlock()
		return false, err
	}

	workspace, ok := m.sessions[siteID]
	if !ok {
		m.mu.Unlock()
		gate.Unlock()
		return false, nil
	}
	if workspace.DraftID != draftID {
		m.mu.Unlock()
		gate.Unlock()
		return false, ErrLocalPreviewSessionConflict
	}
	cleanupPath, err := m.detachWorkspaceLocked(siteID, workspace)
	m.mu.Unlock()
	gate.Unlock()
	if err != nil {
		return false, err
	}
	m.cleanupWorkspaceAsync(siteID, cleanupPath)
	return true, nil
}

// ClaimStale atomically marks an expired workspace as reclaiming. While the
// claim is held, heartbeat/update/release/resource-sync operations for the same
// site cannot mutate or revive the session. This claim must be acquired before
// stopping the generator so a racing heartbeat cannot leave a fresh session
// with a stopped process.
func (m *LocalPreviewWorkspaceManager) ClaimStale(siteID string) (LocalPreviewReclaim, bool, error) {
	gate := m.siteGate(siteID)
	gate.Lock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewReclaim{}, false, fmt.Errorf("local preview workspace manager is closed")
	}
	if err := m.transitionErrorLocked(siteID); err != nil {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewReclaim{}, false, err
	}
	workspace, ok := m.sessions[siteID]
	if !ok {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewReclaim{}, false, nil
	}
	if !m.staleLocked(workspace, m.currentTimeLocked()) {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewReclaim{}, false, ErrLocalPreviewSessionNotStale
	}
	m.nextReclaimToken++
	state := localPreviewReclaimState{draftID: workspace.DraftID, token: m.nextReclaimToken}
	m.reclaiming[siteID] = state
	m.mu.Unlock()
	gate.Unlock()
	return LocalPreviewReclaim{siteID: siteID, draftID: workspace.DraftID, token: state.token}, true, nil
}

// FinishReclaim removes the workspace owned by a previously acquired stale
// claim. The token and draft ID prevent an old cleanup attempt from deleting a
// different session if the lifecycle changes in the future.
func (m *LocalPreviewWorkspaceManager) FinishReclaim(claim LocalPreviewReclaim) (bool, error) {
	gate := m.siteGate(claim.siteID)
	gate.Lock()
	m.mu.Lock()
	state, ok := m.reclaiming[claim.siteID]
	if !ok || state.token != claim.token || state.draftID != claim.draftID {
		m.mu.Unlock()
		gate.Unlock()
		return false, ErrLocalPreviewSessionReclaiming
	}
	workspace, ok := m.sessions[claim.siteID]
	if !ok {
		delete(m.reclaiming, claim.siteID)
		m.mu.Unlock()
		gate.Unlock()
		return false, nil
	}
	if workspace.DraftID != claim.draftID {
		m.mu.Unlock()
		gate.Unlock()
		return false, ErrLocalPreviewSessionConflict
	}
	cleanupPath, err := m.detachWorkspaceLocked(claim.siteID, workspace)
	if err != nil {
		m.mu.Unlock()
		gate.Unlock()
		return false, err
	}
	delete(m.reclaiming, claim.siteID)
	m.mu.Unlock()
	gate.Unlock()
	m.cleanupWorkspaceAsync(claim.siteID, cleanupPath)
	return true, nil
}

// CancelReclaim releases a stale claim without removing the workspace. It is
// used when the generator cannot be stopped; the expired session remains stale
// and can be reclaimed again, but it still cannot be revived by heartbeat/update.
func (m *LocalPreviewWorkspaceManager) CancelReclaim(claim LocalPreviewReclaim) {
	gate := m.siteGate(claim.siteID)
	gate.Lock()
	m.mu.Lock()
	state, ok := m.reclaiming[claim.siteID]
	if ok && state.token == claim.token && state.draftID == claim.draftID {
		delete(m.reclaiming, claim.siteID)
		m.mu.Unlock()
		gate.Unlock()
		return
	}
	m.mu.Unlock()
	gate.Unlock()
}

// ClaimRelease atomically marks an active workspace as releasing. The claim
// must be held while the generator process is stopped so preview requests
// cannot restart it against the old workspace before it is detached.
func (m *LocalPreviewWorkspaceManager) ClaimRelease(siteID, draftID string) (LocalPreviewRelease, bool, error) {
	if err := validateDraftID(draftID); err != nil {
		return LocalPreviewRelease{}, false, err
	}
	gate := m.siteGate(siteID)
	gate.Lock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewRelease{}, false, fmt.Errorf("local preview workspace manager is closed")
	}
	if err := m.transitionErrorLocked(siteID); err != nil {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewRelease{}, false, err
	}
	workspace, ok := m.sessions[siteID]
	if !ok {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewRelease{}, false, nil
	}
	if workspace.DraftID != draftID {
		m.mu.Unlock()
		gate.Unlock()
		return LocalPreviewRelease{}, false, ErrLocalPreviewSessionConflict
	}
	m.nextReleaseToken++
	state := localPreviewReleaseState{draftID: draftID, token: m.nextReleaseToken}
	m.releasing[siteID] = state
	m.mu.Unlock()
	gate.Unlock()
	return LocalPreviewRelease{siteID: siteID, draftID: draftID, token: state.token}, true, nil
}

// FinishRelease detaches a claimed workspace and schedules its physical
// removal after the session lock has been released.
func (m *LocalPreviewWorkspaceManager) FinishRelease(claim LocalPreviewRelease) (bool, error) {
	gate := m.siteGate(claim.siteID)
	gate.Lock()
	m.mu.Lock()
	state, ok := m.releasing[claim.siteID]
	if !ok || state.token != claim.token || state.draftID != claim.draftID {
		m.mu.Unlock()
		gate.Unlock()
		return false, ErrLocalPreviewSessionReleasing
	}
	workspace, ok := m.sessions[claim.siteID]
	if !ok {
		delete(m.releasing, claim.siteID)
		m.mu.Unlock()
		gate.Unlock()
		return false, nil
	}
	if workspace.DraftID != claim.draftID {
		m.mu.Unlock()
		gate.Unlock()
		return false, ErrLocalPreviewSessionConflict
	}
	cleanupPath, err := m.detachWorkspaceLocked(claim.siteID, workspace)
	if err != nil {
		m.mu.Unlock()
		gate.Unlock()
		return false, err
	}
	delete(m.releasing, claim.siteID)
	m.mu.Unlock()
	gate.Unlock()
	m.cleanupWorkspaceAsync(claim.siteID, cleanupPath)
	return true, nil
}

// CancelRelease leaves the workspace active when stopping the generator
// failed. This keeps the session recoverable instead of deleting it while its
// process may still be serving the workspace.
func (m *LocalPreviewWorkspaceManager) CancelRelease(claim LocalPreviewRelease) {
	gate := m.siteGate(claim.siteID)
	gate.Lock()
	m.mu.Lock()
	state, ok := m.releasing[claim.siteID]
	if ok && state.token == claim.token && state.draftID == claim.draftID {
		delete(m.releasing, claim.siteID)
		m.mu.Unlock()
		gate.Unlock()
		return
	}
	m.mu.Unlock()
	gate.Unlock()
}

// ReleaseStale atomically claims and removes an expired workspace. Callers that
// must stop the generator process before deleting the workspace should use
// ClaimStale followed by FinishReclaim instead.
func (m *LocalPreviewWorkspaceManager) ReleaseStale(siteID string) (bool, error) {
	claim, claimed, err := m.ClaimStale(siteID)
	if err != nil || !claimed {
		return false, err
	}
	return m.FinishReclaim(claim)
}

func (m *LocalPreviewWorkspaceManager) detachWorkspaceLocked(siteID string, workspace LocalPreviewWorkspace) (string, error) {
	workspaceRoot := filepath.Dir(workspace.ContentDir)
	cleanupParent := filepath.Join(m.root, ".cleanup")
	if err := os.MkdirAll(cleanupParent, 0700); err != nil {
		return "", fmt.Errorf("create local preview cleanup directory: %w", err)
	}
	cleanupPath, err := os.MkdirTemp(cleanupParent, "workspace-")
	if err != nil {
		return "", fmt.Errorf("allocate local preview cleanup path: %w", err)
	}
	if err := os.Remove(cleanupPath); err != nil {
		return "", fmt.Errorf("prepare local preview cleanup path: %w", err)
	}
	if err := os.Rename(workspaceRoot, cleanupPath); err != nil {
		if os.IsNotExist(err) {
			delete(m.sessions, siteID)
			return "", nil
		}
		return "", fmt.Errorf("detach local preview workspace: %w", err)
	}
	delete(m.sessions, siteID)
	return cleanupPath, nil
}

func (m *LocalPreviewWorkspaceManager) Active(siteID string) (LocalPreviewWorkspace, bool) {
	workspace, active, _ := m.Status(siteID)
	return workspace, active
}

// IsTransitioning reports whether a workspace is being reclaimed or released
// and must not be reused by a new preview request.
func (m *LocalPreviewWorkspaceManager) IsTransitioning(siteID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.transitionErrorLocked(siteID) != nil
}

func (m *LocalPreviewWorkspaceManager) cleanupWorkspaceAsync(siteID, cleanupPath string) {
	if cleanupPath == "" {
		return
	}
	removeWorkspace := m.removeWorkspace
	if removeWorkspace == nil {
		removeWorkspace = os.RemoveAll
	}
	go func() {
		if err := removeWorkspace(cleanupPath); err != nil {
			slog.Error("Local preview workspace cleanup failed", "site", siteID, "path", cleanupPath, "error", err)
		}
	}()
}

func (m *LocalPreviewWorkspaceManager) Shutdown() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.sessions = make(map[string]LocalPreviewWorkspace)
	m.reclaiming = make(map[string]localPreviewReclaimState)
	m.releasing = make(map[string]localPreviewReleaseState)
	root := m.root
	m.mu.Unlock()
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove local preview workspace root: %w", err)
	}
	return nil
}

func copyLocalPreviewContentTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("stat local preview content directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("local preview content directory must be a real directory")
	}

	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("local preview content symlink is not supported: %s", rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported local preview content file type: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyLocalPreviewFile(path, target, entryInfo.Mode().Perm())
	})
}

func copyLocalPreviewFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func writeLocalPreviewFileAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create local preview article directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".local-preview-*")
	if err != nil {
		return fmt.Errorf("create local preview temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write local preview content: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr != nil {
			return fmt.Errorf("replace local preview content: %w", err)
		}
		if removeErr := os.Remove(path); removeErr != nil {
			return fmt.Errorf("remove previous local preview content: %w", removeErr)
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			return fmt.Errorf("replace local preview content: %w", err)
		}
	}
	return nil
}
