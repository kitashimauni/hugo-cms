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
)

var (
	ErrLocalPreviewSessionConflict = errors.New("another local preview session is already active for this site")
	ErrLocalPreviewSessionMismatch = errors.New("local preview session article does not match request")
)

// LocalPreviewWorkspace is the active unsaved-content workspace for one site.
// Phase 3 intentionally limits each site to one active draft/session so two
// browser tabs cannot silently mix unsaved content.
type LocalPreviewWorkspace struct {
	SiteID      string
	DraftID     string
	ArticlePath string
	ContentDir  string
	Revision    uint64
}

// LocalPreviewWorkspaceManager owns ephemeral shadow content directories. The
// original repository remains the Hugo source root for configuration, theme,
// layouts, static files and assets; only contentDir is redirected to this
// workspace.
type LocalPreviewWorkspaceManager struct {
	root     string
	mu       sync.Mutex
	sessions map[string]LocalPreviewWorkspace
	closed   bool
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
		root:     absRoot,
		sessions: make(map[string]LocalPreviewWorkspace),
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

// Update creates the site's workspace on the first request and applies the
// newest editor revision. Older in-flight HTTP requests become harmless no-ops
// instead of overwriting newer editor state.
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

	workspace, exists := m.sessions[runtime.ID]
	created := false
	if exists {
		if workspace.DraftID != draftID {
			return LocalPreviewWorkspace{}, false, false, ErrLocalPreviewSessionConflict
		}
		if workspace.ArticlePath != filepath.ToSlash(articlePath) {
			return LocalPreviewWorkspace{}, false, false, ErrLocalPreviewSessionMismatch
		}
		if revision <= workspace.Revision {
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
	m.sessions[runtime.ID] = workspace
	return workspace, created, true, nil
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
		// The media file is outside contentDir (for example static/). Hugo still
		// reads it directly from the original repository, so no shadow sync is
		// required.
		return false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false, fmt.Errorf("local preview workspace manager is closed")
	}
	workspace, active := m.sessions[runtime.ID]
	if !active {
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

	workspace, ok := m.sessions[siteID]
	if !ok {
		return false, nil
	}
	if workspace.DraftID != draftID {
		return false, ErrLocalPreviewSessionConflict
	}
	workspaceRoot := filepath.Dir(workspace.ContentDir)
	if err := os.RemoveAll(workspaceRoot); err != nil {
		return false, fmt.Errorf("remove local preview workspace: %w", err)
	}
	delete(m.sessions, siteID)
	return true, nil
}

func (m *LocalPreviewWorkspaceManager) Active(siteID string) (LocalPreviewWorkspace, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	workspace, ok := m.sessions[siteID]
	return workspace, ok
}

func (m *LocalPreviewWorkspaceManager) Shutdown() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.sessions = make(map[string]LocalPreviewWorkspace)
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
		// Windows cannot atomically replace an existing path. The fully-written
		// temporary file is used for the fallback replacement.
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
