package services

import (
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const DefaultLocalPreviewLeaseTTL = 2 * time.Minute

var (
	ErrLocalPreviewSessionConflict   = errors.New("another local preview session is already active for this site")
	ErrLocalPreviewSessionMismatch   = errors.New("local preview session article does not match request")
	ErrLocalPreviewSessionNotFound   = errors.New("local preview session is not active")
	ErrLocalPreviewSessionNotStale   = errors.New("local preview session is still active")
	ErrLocalPreviewSessionExpired    = errors.New("local preview session lease has expired")
	ErrLocalPreviewSessionReclaiming = errors.New("local preview session is being reclaimed")
)

// LocalPreviewWorkspace is the active unsaved-content workspace for one site.
// Each site is intentionally limited to one active browser-document session so
// separate tabs cannot silently mix unsaved content.
type LocalPreviewWorkspace struct {
	SiteID      string
	DraftID     string
	ArticlePath string
	ContentDir  string
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

// LocalPreviewWorkspaceManager owns ephemeral shadow content directories. The
// original repository remains the Hugo source root for configuration, theme,
// layouts, static files and assets; only contentDir is redirected to this
// workspace.
type LocalPreviewWorkspaceManager struct {
	root             string
	mu               sync.Mutex
	sessions         map[string]LocalPreviewWorkspace
	reclaiming       map[string]localPreviewReclaimState
	nextReclaimToken uint64
	closed           bool
	leaseTTL         time.Duration
	now              func() time.Time
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
		root:       absRoot,
		sessions:   make(map[string]LocalPreviewWorkspace),
		reclaiming: make(map[string]localPreviewReclaimState),
		leaseTTL:   DefaultLocalPreviewLeaseTTL,
		now:        time.Now,
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

// Update creates the site's workspace on the first request and applies the
// newest editor revision. Older in-flight HTTP requests become harmless no-ops
// instead of overwriting newer editor state. Requests renew a lease only while
// it is still valid; an expired session must be reclaimed before it can restart.
func (m *LocalPreviewWorkspaceManager) Update(runtime config.SiteRuntime, draftID, articlePath string, revision uint64, content []byte) (LocalPreviewWorkspace, bool, bool, error) {
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
	if m.reclaimingLocked(runtime.ID) {
		return LocalPreviewWorkspace{}, false, false, ErrLocalPreviewSessionReclaiming
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
		if workspace.ArticlePath != filepath.ToSlash(articlePath) {
			return LocalPreviewWorkspace{}, false, false, ErrLocalPreviewSessionMismatch
		}
		workspace.LastSeenAt = now
		if revision <= workspace.Revision {
			m.sessions[runtime.ID] = workspace
			return workspace, false, false, nil
		}
	} else {
		workspaceRoot := filepath.Join(m.root, runtime.ID, draftID)
		contentDir := filepath.Join(workspaceRoot, "content")
		if err := os.RemoveAll(workspaceRoot); err != nil {
			return LocalPreviewWorkspace{}, false, false, fmt.Errorf("reset local preview workspace: %w", err)
		}
		if err := copyLocalPreviewContentTree(sourceContentDir, contentDir); err != nil {
			_ = os.RemoveAll(workspaceRoot)
			return LocalPreviewWorkspace{}, false, false, err
		}
		workspace = LocalPreviewWorkspace{
			SiteID:      runtime.ID,
			DraftID:     draftID,
			ArticlePath: filepath.ToSlash(articlePath),
			ContentDir:  contentDir,
			LastSeenAt:  now,
		}
		created = true
	}

	target := SafeJoin(workspace.ContentDir, "", articlePath)
	if target == "" {
		return LocalPreviewWorkspace{}, false, false, fmt.Errorf("invalid local preview workspace path")
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
// Hugo process may already be stopping.
func (m *LocalPreviewWorkspaceManager) Heartbeat(siteID, draftID string) (LocalPreviewWorkspace, error) {
	if err := validateDraftID(draftID); err != nil {
		return LocalPreviewWorkspace{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return LocalPreviewWorkspace{}, fmt.Errorf("local preview workspace manager is closed")
	}
	if m.reclaimingLocked(siteID) {
		return LocalPreviewWorkspace{}, ErrLocalPreviewSessionReclaiming
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
	workspace, ok := m.sessions[siteID]
	if !ok {
		return LocalPreviewWorkspace{}, false, false
	}
	return workspace, true, m.staleLocked(workspace, m.currentTimeLocked())
}

// SyncContentResource mirrors a content-directory resource change made through
// the normal CMS media API into an already-active shadow workspace. Static
// resources do not need this because Hugo still uses the original source root.
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
	if m.reclaimingLocked(runtime.ID) {
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reclaimingLocked(siteID) {
		return false, ErrLocalPreviewSessionReclaiming
	}

	workspace, ok := m.sessions[siteID]
	if !ok {
		return false, nil
	}
	if workspace.DraftID != draftID {
		return false, ErrLocalPreviewSessionConflict
	}
	return m.removeWorkspaceLocked(siteID, workspace)
}

// ClaimStale atomically marks an expired workspace as reclaiming. While the
// claim is held, heartbeat/update/release/resource-sync operations for the same
// site cannot mutate or revive the session. This claim must be acquired before
// stopping Hugo so a racing heartbeat cannot leave a fresh session with a
// stopped process.
func (m *LocalPreviewWorkspaceManager) ClaimStale(siteID string) (LocalPreviewReclaim, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return LocalPreviewReclaim{}, false, fmt.Errorf("local preview workspace manager is closed")
	}
	if m.reclaimingLocked(siteID) {
		return LocalPreviewReclaim{}, false, ErrLocalPreviewSessionReclaiming
	}
	workspace, ok := m.sessions[siteID]
	if !ok {
		return LocalPreviewReclaim{}, false, nil
	}
	if !m.staleLocked(workspace, m.currentTimeLocked()) {
		return LocalPreviewReclaim{}, false, ErrLocalPreviewSessionNotStale
	}
	m.nextReclaimToken++
	state := localPreviewReclaimState{draftID: workspace.DraftID, token: m.nextReclaimToken}
	m.reclaiming[siteID] = state
	return LocalPreviewReclaim{siteID: siteID, draftID: workspace.DraftID, token: state.token}, true, nil
}

// FinishReclaim removes the workspace owned by a previously acquired stale
// claim. The token and draft ID prevent an old cleanup attempt from deleting a
// different session if the lifecycle changes in the future.
func (m *LocalPreviewWorkspaceManager) FinishReclaim(claim LocalPreviewReclaim) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.reclaiming[claim.siteID]
	if !ok || state.token != claim.token || state.draftID != claim.draftID {
		return false, ErrLocalPreviewSessionReclaiming
	}
	defer delete(m.reclaiming, claim.siteID)
	workspace, ok := m.sessions[claim.siteID]
	if !ok {
		return false, nil
	}
	if workspace.DraftID != claim.draftID {
		return false, ErrLocalPreviewSessionConflict
	}
	return m.removeWorkspaceLocked(claim.siteID, workspace)
}

// CancelReclaim releases a stale claim without removing the workspace. It is
// used when Hugo cannot be stopped; the expired session remains stale and can
// be reclaimed again, but it still cannot be revived by heartbeat/update.
func (m *LocalPreviewWorkspaceManager) CancelReclaim(claim LocalPreviewReclaim) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.reclaiming[claim.siteID]
	if ok && state.token == claim.token && state.draftID == claim.draftID {
		delete(m.reclaiming, claim.siteID)
	}
}

// ReleaseStale atomically claims and removes an expired workspace. Callers that
// must stop the Hugo process before deleting the workspace should use
// ClaimStale followed by FinishReclaim instead.
func (m *LocalPreviewWorkspaceManager) ReleaseStale(siteID string) (bool, error) {
	claim, claimed, err := m.ClaimStale(siteID)
	if err != nil || !claimed {
		return false, err
	}
	return m.FinishReclaim(claim)
}

func (m *LocalPreviewWorkspaceManager) removeWorkspaceLocked(siteID string, workspace LocalPreviewWorkspace) (bool, error) {
	workspaceRoot := filepath.Dir(workspace.ContentDir)
	if err := os.RemoveAll(workspaceRoot); err != nil {
		return false, fmt.Errorf("remove local preview workspace: %w", err)
	}
	delete(m.sessions, siteID)
	return true, nil
}

func (m *LocalPreviewWorkspaceManager) Active(siteID string) (LocalPreviewWorkspace, bool) {
	workspace, active, _ := m.Status(siteID)
	return workspace, active
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
