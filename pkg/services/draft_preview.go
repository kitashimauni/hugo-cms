package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	draftIDPattern             = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	commitSHAPattern           = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	draftPreviewOperationLocks = struct {
		sync.Mutex
		items map[string]*draftPreviewOperationLock
	}{items: make(map[string]*draftPreviewOperationLock)}
)

type draftPreviewOperationLock struct {
	mutex sync.Mutex
	users int
}

type gitPushFunc func(dir, token string, args ...string) (string, error)

const previewBranchPrefix = "cms-preview/"

type DraftPreviewState struct {
	SiteID          string                  `json:"site_id"`
	DraftID         string                  `json:"draft_id"`
	Branch          string                  `json:"branch"`
	CommitSHA       string                  `json:"commit_sha"`
	DeploymentID    string                  `json:"deployment_id,omitempty"`
	Status          PreviewDeploymentStatus `json:"status"`
	URL             string                  `json:"url,omitempty"`
	FailureReason   string                  `json:"failure_reason,omitempty"`
	AccessProtected bool                    `json:"access_protected"`
	CleanupPending  bool                    `json:"cleanup_pending,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type DraftPreviewStore struct {
	root string
	mu   sync.Mutex
}

func NewDraftPreviewStore(root string) (*DraftPreviewStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("draft preview state root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve draft preview state root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0700); err != nil {
		return nil, fmt.Errorf("create draft preview state root: %w", err)
	}
	return &DraftPreviewStore{root: absRoot}, nil
}

func (store *DraftPreviewStore) Save(state DraftPreviewState) error {
	if err := validateDraftPreviewState(state); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	directory, path := store.statePath(state.SiteID, state.DraftID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create draft preview state directory: %w", err)
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode draft preview state: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create temporary draft preview state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect draft preview state: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write draft preview state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync draft preview state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close draft preview state: %w", err)
	}
	// Windows cannot replace an existing destination with os.Rename. The
	// state remains recoverable because the old file is removed only after the
	// fully-written temporary file has been closed and synced.
	if err := os.Rename(temporaryPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr != nil {
			return fmt.Errorf("replace draft preview state: %w", err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove previous draft preview state: %w", err)
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			return fmt.Errorf("replace draft preview state: %w", err)
		}
	}
	return nil
}

func (store *DraftPreviewStore) Get(siteID, draftID string) (DraftPreviewState, error) {
	if strings.TrimSpace(siteID) == "" {
		return DraftPreviewState{}, fmt.Errorf("site ID is required")
	}
	if err := validateDraftID(draftID); err != nil {
		return DraftPreviewState{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	_, path := store.statePath(siteID, draftID)
	content, err := os.ReadFile(path)
	if err != nil {
		return DraftPreviewState{}, err
	}
	var state DraftPreviewState
	if err := json.Unmarshal(content, &state); err != nil {
		return DraftPreviewState{}, fmt.Errorf("decode draft preview state: %w", err)
	}
	if state.SiteID != siteID || state.DraftID != draftID {
		return DraftPreviewState{}, fmt.Errorf("draft preview state identity mismatch")
	}
	if err := validateDraftPreviewState(state); err != nil {
		return DraftPreviewState{}, fmt.Errorf("invalid stored draft preview state: %w", err)
	}
	return state, nil
}

func (store *DraftPreviewStore) Delete(siteID, draftID string) error {
	if strings.TrimSpace(siteID) == "" {
		return fmt.Errorf("site ID is required")
	}
	if err := validateDraftID(draftID); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	_, path := store.statePath(siteID, draftID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete draft preview state: %w", err)
	}
	return nil
}

// ListCleanupPending returns persisted cleanup tombstones so a caller can
// retry cleanup after a process restart.
func (store *DraftPreviewStore) ListCleanupPending() ([]DraftPreviewState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	states := make([]DraftPreviewState, 0)
	err := filepath.WalkDir(store.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var state DraftPreviewState
		if err := json.Unmarshal(content, &state); err != nil {
			return fmt.Errorf("decode cleanup-pending draft preview state %q: %w", path, err)
		}
		if err := validateDraftPreviewState(state); err != nil {
			return fmt.Errorf("invalid cleanup-pending draft preview state %q: %w", path, err)
		}
		if state.CleanupPending {
			states = append(states, state)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list cleanup-pending draft previews: %w", err)
	}
	return states, nil
}

func (store *DraftPreviewStore) statePath(siteID, draftID string) (string, string) {
	hash := sha256.Sum256([]byte(siteID))
	directory := filepath.Join(store.root, hex.EncodeToString(hash[:]))
	return directory, filepath.Join(directory, draftID+".json")
}

// CommitAndPushDraftPreview builds a commit from the current working tree by
// using a private temporary index. It never changes HEAD or checks out the
// preview branch in the CMS repository.
func CommitAndPushDraftPreview(ctx context.Context, runtime config.SiteRuntime, token, draftID string, paths []string) (string, string, error) {
	return commitAndPushDraftPreview(ctx, runtime, token, draftID, paths, func(_ string, token string, args ...string) (string, error) {
		return ExecuteGitWithTokenForRuntime(runtime, token, args...)
	})
}

// DraftPreviewMatchesWorkingTree verifies that the files covered by a draft
// still produce the exact committed tree. This is the server-side counterpart
// of the UI's stale marker and prevents a client from creating a PR after
// editing content that has not been deployed.
func DraftPreviewMatchesWorkingTree(ctx context.Context, runtime config.SiteRuntime, state DraftPreviewState, paths []string) (bool, error) {
	if err := validateDraftPreviewState(state); err != nil {
		return false, err
	}
	if state.SiteID != runtime.ID {
		return false, fmt.Errorf("draft preview does not belong to the selected site")
	}
	validatedPaths, err := validateDraftPaths(runtime.RepoPath, paths)
	if err != nil {
		return false, err
	}
	commandContext, cancel := context.WithTimeout(ctx, config.GitCommandTimeout)
	defer cancel()

	unlock := LockRepositoryOperation()
	defer unlock()
	index, err := os.CreateTemp("", "homecms-verify-index-*")
	if err != nil {
		return false, fmt.Errorf("create verification Git index: %w", err)
	}
	indexPath := index.Name()
	if err := index.Close(); err != nil {
		os.Remove(indexPath)
		return false, fmt.Errorf("close verification Git index: %w", err)
	}
	if err := os.Remove(indexPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("prepare verification Git index: %w", err)
	}
	defer os.Remove(indexPath)

	env := temporaryIndexEnvironment(indexPath)
	if _, err := runGitCommand(commandContext, runtime.RepoPath, env, "read-tree", state.CommitSHA); err != nil {
		return false, fmt.Errorf("initialize verification Git index: %w", err)
	}
	addArgs := append([]string{"add", "-A", "--"}, validatedPaths...)
	if _, err := runGitCommand(commandContext, runtime.RepoPath, env, addArgs...); err != nil {
		return false, fmt.Errorf("stage verification paths: %w", err)
	}
	workingTree, err := runGitCommand(commandContext, runtime.RepoPath, env, "write-tree")
	if err != nil {
		return false, fmt.Errorf("write verification tree: %w", err)
	}
	committedTree, err := runGitCommand(commandContext, runtime.RepoPath, nil, "rev-parse", state.CommitSHA+"^{tree}")
	if err != nil {
		return false, fmt.Errorf("read deployed draft tree: %w", err)
	}
	return strings.TrimSpace(workingTree) == strings.TrimSpace(committedTree), nil
}

func commitAndPushDraftPreview(ctx context.Context, runtime config.SiteRuntime, token, draftID string, paths []string, push gitPushFunc) (string, string, error) {
	commandContext, cancel := context.WithTimeout(ctx, config.GitCommandTimeout)
	defer cancel()
	ctx = commandContext
	if err := validateDraftID(draftID); err != nil {
		return "", "", err
	}
	validatedPaths, err := validateDraftPaths(runtime.RepoPath, paths)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", "", fmt.Errorf("GitHub token is required")
	}
	branch := previewBranchPrefix + draftID
	if err := validatePreviewBranch(branch); err != nil {
		return "", "", err
	}

	unlock := LockRepositoryOperation()
	defer unlock()

	index, err := os.CreateTemp("", "homecms-draft-index-*")
	if err != nil {
		return "", "", fmt.Errorf("create temporary Git index: %w", err)
	}
	indexPath := index.Name()
	if err := index.Close(); err != nil {
		os.Remove(indexPath)
		return "", "", fmt.Errorf("close temporary Git index: %w", err)
	}
	if err := os.Remove(indexPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("prepare temporary Git index: %w", err)
	}
	defer os.Remove(indexPath)

	baseCommit, localBranchExists, err := draftBaseCommit(ctx, runtime, branch)
	if err != nil {
		return "", "", err
	}
	env := temporaryIndexEnvironment(indexPath)
	if _, err := runGitCommand(ctx, runtime.RepoPath, env, "read-tree", baseCommit); err != nil {
		return "", "", fmt.Errorf("initialize draft Git index: %w", err)
	}
	addArgs := []string{"add", "-A", "--"}
	addArgs = append(addArgs, validatedPaths...)
	if output, err := runGitCommand(ctx, runtime.RepoPath, env, addArgs...); err != nil {
		return "", "", fmt.Errorf("stage draft paths: %w: %s", err, strings.TrimSpace(output))
	}
	treeSHA, err := runGitCommand(ctx, runtime.RepoPath, env, "write-tree")
	if err != nil {
		return "", "", fmt.Errorf("write draft tree: %w", err)
	}
	treeSHA = strings.TrimSpace(treeSHA)
	baseTree, err := runGitCommand(ctx, runtime.RepoPath, nil, "rev-parse", baseCommit+"^{tree}")
	if err != nil {
		return "", "", fmt.Errorf("read draft base tree: %w", err)
	}

	commitSHA := baseCommit
	if treeSHA != strings.TrimSpace(baseTree) {
		commitEnv := append(temporaryIndexEnvironment(indexPath),
			"GIT_AUTHOR_NAME="+runtime.GitUserName,
			"GIT_AUTHOR_EMAIL="+runtime.GitUserEmail,
			"GIT_COMMITTER_NAME="+runtime.GitUserName,
			"GIT_COMMITTER_EMAIL="+runtime.GitUserEmail,
		)
		commitSHA, err = runGitCommand(ctx, runtime.RepoPath, commitEnv,
			"commit-tree", treeSHA, "-p", baseCommit, "-m", "Update draft "+draftID+" via HomeCMS")
		if err != nil {
			return "", "", fmt.Errorf("create draft commit: %w", err)
		}
		commitSHA = strings.TrimSpace(commitSHA)
	}
	if err := validateCommitSHA(commitSHA); err != nil {
		return "", "", fmt.Errorf("Git returned an invalid draft commit: %w", err)
	}

	ref := "refs/heads/" + branch
	updateArgs := []string{"update-ref", ref, commitSHA}
	if localBranchExists {
		updateArgs = append(updateArgs, baseCommit)
	}
	if _, err := runGitCommand(ctx, runtime.RepoPath, nil, updateArgs...); err != nil {
		return "", "", fmt.Errorf("update local draft branch: %w", err)
	}
	pushLog, err := push(runtime.RepoPath, token, "push", runtime.GitRemote, ref+":"+ref)
	if err != nil {
		return "", "", fmt.Errorf("push draft branch: %w: %s", err, strings.TrimSpace(pushLog))
	}
	return branch, commitSHA, nil
}

func UpdateDraftPreview(ctx context.Context, runtime config.SiteRuntime, token, draftID string, paths []string, store *DraftPreviewStore, provider PreviewDeploymentProvider) (DraftPreviewState, error) {
	if store == nil || provider == nil {
		return DraftPreviewState{}, fmt.Errorf("draft preview store and provider are required")
	}
	if strings.TrimSpace(runtime.ID) == "" {
		return DraftPreviewState{}, fmt.Errorf("site ID is required")
	}
	if err := validateDraftID(draftID); err != nil {
		return DraftPreviewState{}, err
	}
	unlockOperation := lockDraftPreviewOperation(runtime.ID, draftID)
	defer unlockOperation()
	branch, commitSHA, err := CommitAndPushDraftPreview(ctx, runtime, token, draftID, paths)
	if err != nil {
		return DraftPreviewState{}, err
	}
	now := time.Now().UTC()
	state := DraftPreviewState{
		SiteID:          runtime.ID,
		DraftID:         draftID,
		Branch:          branch,
		CommitSHA:       commitSHA,
		Status:          PreviewDeploymentQueued,
		AccessProtected: runtime.PreviewDeployment.AccessProtected,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if previous, getErr := store.Get(runtime.ID, draftID); getErr == nil {
		state.CreatedAt = previous.CreatedAt
	} else if !errors.Is(getErr, os.ErrNotExist) {
		return DraftPreviewState{}, getErr
	}
	if err := store.Save(state); err != nil {
		return DraftPreviewState{}, err
	}
	deployment, err := provider.Trigger(ctx, branch, commitSHA)
	if err != nil {
		if IsPreviewProviderError(err, PreviewProviderNotFound) {
			return state, nil
		}
		return state, err
	}
	return applyProviderDeployment(store, state, deployment)
}

func RefreshDraftPreview(ctx context.Context, siteID, draftID string, store *DraftPreviewStore, provider PreviewDeploymentProvider) (DraftPreviewState, error) {
	if store == nil || provider == nil {
		return DraftPreviewState{}, fmt.Errorf("draft preview store and provider are required")
	}
	if strings.TrimSpace(siteID) == "" {
		return DraftPreviewState{}, fmt.Errorf("site ID is required")
	}
	if err := validateDraftID(draftID); err != nil {
		return DraftPreviewState{}, err
	}
	unlockOperation := lockDraftPreviewOperation(siteID, draftID)
	defer unlockOperation()
	return refreshDraftPreview(ctx, siteID, draftID, store, provider)
}

func refreshDraftPreview(ctx context.Context, siteID, draftID string, store *DraftPreviewStore, provider PreviewDeploymentProvider) (DraftPreviewState, error) {
	state, err := store.Get(siteID, draftID)
	if err != nil {
		return DraftPreviewState{}, err
	}
	var deployment PreviewDeployment
	if state.DeploymentID == "" {
		deployment, err = provider.Trigger(ctx, state.Branch, state.CommitSHA)
	} else {
		deployment, err = provider.Status(ctx, state.DeploymentID)
	}
	if err != nil {
		if IsPreviewProviderError(err, PreviewProviderNotFound) && state.DeploymentID == "" {
			return state, nil
		}
		return state, err
	}
	return applyProviderDeployment(store, state, deployment)
}

func RetryDraftPreview(ctx context.Context, siteID, draftID string, store *DraftPreviewStore, provider PreviewDeploymentProvider) (DraftPreviewState, error) {
	if store == nil || provider == nil {
		return DraftPreviewState{}, fmt.Errorf("draft preview store and provider are required")
	}
	if strings.TrimSpace(siteID) == "" {
		return DraftPreviewState{}, fmt.Errorf("site ID is required")
	}
	if err := validateDraftID(draftID); err != nil {
		return DraftPreviewState{}, err
	}
	unlockOperation := lockDraftPreviewOperation(siteID, draftID)
	defer unlockOperation()
	state, err := store.Get(siteID, draftID)
	if err != nil {
		return DraftPreviewState{}, err
	}
	if state.DeploymentID == "" {
		return refreshDraftPreview(ctx, siteID, draftID, store, provider)
	}
	deployment, err := provider.Retry(ctx, state.DeploymentID)
	if err != nil {
		return state, err
	}
	return applyProviderDeployment(store, state, deployment)
}

func CleanupDraftPreview(ctx context.Context, runtime config.SiteRuntime, token, draftID string, store *DraftPreviewStore, provider PreviewDeploymentProvider) error {
	return cleanupDraftPreview(ctx, runtime, token, draftID, store, provider, func(_ string, token string, args ...string) (string, error) {
		return ExecuteGitWithTokenForRuntime(runtime, token, args...)
	})
}

func cleanupDraftPreview(ctx context.Context, runtime config.SiteRuntime, token, draftID string, store *DraftPreviewStore, provider PreviewDeploymentProvider, push gitPushFunc) error {
	if store == nil || provider == nil {
		return fmt.Errorf("draft preview store and provider are required")
	}
	if strings.TrimSpace(runtime.ID) == "" {
		return fmt.Errorf("site ID is required")
	}
	if err := validateDraftID(draftID); err != nil {
		return err
	}
	unlockOperation := lockDraftPreviewOperation(runtime.ID, draftID)
	defer unlockOperation()
	state, err := store.Get(runtime.ID, draftID)
	if err != nil {
		return err
	}
	state.CleanupPending = true
	state.UpdatedAt = time.Now().UTC()
	if err := store.Save(state); err != nil {
		return err
	}

	unlock := LockRepositoryOperation()
	deleteLog, deleteBranchErr := push(runtime.RepoPath, token, "push", runtime.GitRemote, ":refs/heads/"+state.Branch)
	unlock()
	if deleteBranchErr != nil {
		return fmt.Errorf("delete remote draft branch: %w: %s", deleteBranchErr, strings.TrimSpace(deleteLog))
	}
	if state.DeploymentID != "" {
		if err := provider.Delete(ctx, state.DeploymentID); err != nil && !IsPreviewProviderError(err, PreviewProviderNotFound) {
			return err
		}
	}

	unlock = LockRepositoryOperation()
	_, localDeleteErr := runGitCommand(ctx, runtime.RepoPath, nil, "update-ref", "-d", "refs/heads/"+state.Branch, state.CommitSHA)
	unlock()
	if localDeleteErr != nil {
		return fmt.Errorf("delete local draft branch: %w", localDeleteErr)
	}
	return store.Delete(runtime.ID, draftID)
}

func applyProviderDeployment(store *DraftPreviewStore, state DraftPreviewState, deployment PreviewDeployment) (DraftPreviewState, error) {
	if deployment.Branch != state.Branch || !strings.EqualFold(deployment.CommitSHA, state.CommitSHA) {
		return state, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: "match", Err: fmt.Errorf("provider deployment does not match draft commit")}
	}
	state.DeploymentID = deployment.ID
	state.Status = deployment.Status
	state.URL = ""
	state.FailureReason = ""
	if deployment.Status == PreviewDeploymentReady {
		if err := validatePreviewURL(deployment.URL); err != nil {
			return state, &PreviewProviderError{Kind: PreviewProviderInvalidReply, Operation: "url", Err: err}
		}
		state.URL = deployment.URL
	} else if deployment.Status == PreviewDeploymentFailed {
		state.FailureReason = deployment.FailureReason
	}
	state.UpdatedAt = time.Now().UTC()
	if err := store.Save(state); err != nil {
		return state, err
	}
	return state, nil
}

func draftBaseCommit(ctx context.Context, runtime config.SiteRuntime, branch string) (string, bool, error) {
	localRef := "refs/heads/" + branch
	if output, err := runGitCommand(ctx, runtime.RepoPath, nil, "rev-parse", "--verify", localRef+"^{commit}"); err == nil {
		commit := strings.TrimSpace(output)
		if err := validateCommitSHA(commit); err != nil {
			return "", false, err
		}
		return commit, true, nil
	}
	baseRef := "refs/heads/" + runtime.GitBranch
	output, err := runGitCommand(ctx, runtime.RepoPath, nil, "rev-parse", "--verify", baseRef+"^{commit}")
	if err != nil {
		return "", false, fmt.Errorf("resolve production branch %q: %w", runtime.GitBranch, err)
	}
	commit := strings.TrimSpace(output)
	if err := validateCommitSHA(commit); err != nil {
		return "", false, err
	}
	return commit, false, nil
}

func runGitCommand(ctx context.Context, directory string, extraEnv []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	if extraEnv != nil {
		command.Env = extraEnv
	}
	output, err := command.CombinedOutput()
	return string(output), err
}

func temporaryIndexEnvironment(indexPath string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "GIT_INDEX_FILE") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "GIT_INDEX_FILE="+indexPath)
}

func validateDraftID(draftID string) error {
	if !draftIDPattern.MatchString(draftID) {
		return fmt.Errorf("invalid draft ID: must be 1-64 lowercase letters, digits, or internal hyphens")
	}
	return nil
}

func validatePreviewBranch(branch string) error {
	if !strings.HasPrefix(branch, previewBranchPrefix) {
		return fmt.Errorf("preview branch must start with %q", previewBranchPrefix)
	}
	return validateDraftID(strings.TrimPrefix(branch, previewBranchPrefix))
}

func validateCommitSHA(commitSHA string) error {
	if !commitSHAPattern.MatchString(commitSHA) {
		return fmt.Errorf("commit SHA must be a full 40-character hexadecimal SHA-1")
	}
	return nil
}

func validateDraftPaths(repoPath string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one draft path is required")
	}
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, value := range paths {
		if strings.ContainsRune(value, '\x00') || filepath.IsAbs(value) {
			return nil, fmt.Errorf("invalid draft path %q", value)
		}
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
		if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean == ".git" || strings.HasPrefix(clean, ".git/") {
			return nil, fmt.Errorf("invalid draft path %q", value)
		}
		if SafeJoin(repoPath, "", clean) == "" {
			return nil, fmt.Errorf("draft path escapes repository: %q", value)
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result, nil
}

func validateDraftPreviewState(state DraftPreviewState) error {
	if strings.TrimSpace(state.SiteID) == "" {
		return fmt.Errorf("site ID is required")
	}
	if err := validateDraftID(state.DraftID); err != nil {
		return err
	}
	if state.Branch != previewBranchPrefix+state.DraftID {
		return fmt.Errorf("draft branch does not match draft ID")
	}
	if err := validateCommitSHA(state.CommitSHA); err != nil {
		return err
	}
	if state.DeploymentID != "" {
		if err := validateDeploymentID(state.DeploymentID); err != nil {
			return err
		}
	}
	if state.URL != "" {
		if state.Status != PreviewDeploymentReady {
			return fmt.Errorf("only a ready deployment may have a URL")
		}
		if err := validatePreviewURL(state.URL); err != nil {
			return err
		}
	}
	if state.FailureReason != "" && state.Status != PreviewDeploymentFailed {
		return fmt.Errorf("only a failed deployment may have a failure reason")
	}
	switch state.Status {
	case PreviewDeploymentQueued, PreviewDeploymentBuilding, PreviewDeploymentReady, PreviewDeploymentFailed:
	default:
		return fmt.Errorf("invalid deployment status %q", state.Status)
	}
	if state.CreatedAt.IsZero() || state.UpdatedAt.IsZero() {
		return fmt.Errorf("draft preview timestamps are required")
	}
	return nil
}

func validatePreviewURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("provider returned an invalid preview URL")
	}
	return nil
}

func lockDraftPreviewOperation(siteID, draftID string) func() {
	key := siteID + "\x00" + draftID
	draftPreviewOperationLocks.Lock()
	item := draftPreviewOperationLocks.items[key]
	if item == nil {
		item = &draftPreviewOperationLock{}
		draftPreviewOperationLocks.items[key] = item
	}
	item.users++
	draftPreviewOperationLocks.Unlock()

	item.mutex.Lock()
	return func() {
		item.mutex.Unlock()
		draftPreviewOperationLocks.Lock()
		item.users--
		if item.users == 0 {
			delete(draftPreviewOperationLocks.items, key)
		}
		draftPreviewOperationLocks.Unlock()
	}
}
