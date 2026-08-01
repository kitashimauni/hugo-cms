package services

import (
	"context"
	"encoding/json"
	"errors"
	"hugo-cms/pkg/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakePreviewProvider struct {
	deployment PreviewDeployment
	triggerErr error
	statusErr  error
	deleteErr  error
	deletedID  string
}

func (provider *fakePreviewProvider) Trigger(context.Context, string, string) (PreviewDeployment, error) {
	return provider.deployment, provider.triggerErr
}

func (provider *fakePreviewProvider) Status(context.Context, string) (PreviewDeployment, error) {
	return provider.deployment, provider.statusErr
}

func (provider *fakePreviewProvider) URL(context.Context, string) (string, error) {
	return provider.deployment.URL, nil
}

func (provider *fakePreviewProvider) Delete(_ context.Context, deploymentID string) error {
	provider.deletedID = deploymentID
	return provider.deleteErr
}

func (provider *fakePreviewProvider) Retry(context.Context, string) (PreviewDeployment, error) {
	return provider.deployment, nil
}

func TestCommitAndPushDraftPreviewUsesTemporaryIndexWithoutCheckout(t *testing.T) {
	runtime, remotePath := setupDraftPreviewRepository(t)
	writeDraftTestFile(t, filepath.Join(runtime.RepoPath, "content", "selected.md"), "selected changed\n")
	writeDraftTestFile(t, filepath.Join(runtime.RepoPath, "content", "unrelated.md"), "unrelated changed\n")

	beforeHead := strings.TrimSpace(runGitOutputForDraftTest(t, runtime.RepoPath, "rev-parse", "HEAD"))
	branch, commitSHA, err := commitAndPushDraftPreview(context.Background(), runtime, "test-token", "draft-1", []string{"content/selected.md"}, localDraftPush(t, runtime))
	if err != nil {
		t.Fatalf("commitAndPushDraftPreview() error = %v", err)
	}
	if branch != "cms-preview/draft-1" || !commitSHAPattern.MatchString(commitSHA) {
		t.Fatalf("branch/commit = %q/%q", branch, commitSHA)
	}
	afterHead := strings.TrimSpace(runGitOutputForDraftTest(t, runtime.RepoPath, "rev-parse", "HEAD"))
	if afterHead != beforeHead {
		t.Fatalf("working checkout HEAD changed from %s to %s", beforeHead, afterHead)
	}
	if currentBranch := strings.TrimSpace(runGitOutputForDraftTest(t, runtime.RepoPath, "branch", "--show-current")); currentBranch != "main" {
		t.Fatalf("current branch = %q, want main", currentBranch)
	}
	if staged := strings.TrimSpace(runGitOutputForDraftTest(t, runtime.RepoPath, "diff", "--cached", "--name-only")); staged != "" {
		t.Fatalf("real Git index was modified: %q", staged)
	}
	if got := runGitOutputForDraftTest(t, runtime.RepoPath, "show", commitSHA+":content/selected.md"); got != "selected changed\n" {
		t.Fatalf("selected content = %q", got)
	}
	if got := runGitOutputForDraftTest(t, runtime.RepoPath, "show", commitSHA+":content/unrelated.md"); got != "unrelated initial\n" {
		t.Fatalf("unrelated content leaked into draft commit: %q", got)
	}
	remoteCommit := strings.TrimSpace(runGitOutputForDraftTest(t, remotePath, "rev-parse", "refs/heads/cms-preview/draft-1"))
	if remoteCommit != commitSHA {
		t.Fatalf("remote commit = %q, want %q", remoteCommit, commitSHA)
	}
}

func TestDraftPreviewMatchesWorkingTreeDetectsStaleContent(t *testing.T) {
	runtime, _ := setupDraftPreviewRepository(t)
	selectedPath := filepath.Join(runtime.RepoPath, "content", "selected.md")
	writeDraftTestFile(t, selectedPath, "selected changed\n")
	branch, commitSHA, err := commitAndPushDraftPreview(context.Background(), runtime, "test-token", "draft-verify", []string{"content/selected.md"}, localDraftPush(t, runtime))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := DraftPreviewState{SiteID: runtime.ID, DraftID: "draft-verify", Branch: branch, CommitSHA: commitSHA, Status: PreviewDeploymentReady, URL: "https://abc.pages.dev", CreatedAt: now, UpdatedAt: now}
	match, err := DraftPreviewMatchesWorkingTree(context.Background(), runtime, state, []string{"content/selected.md"})
	if err != nil || !match {
		t.Fatalf("initial match = %v, error = %v", match, err)
	}
	writeDraftTestFile(t, selectedPath, "edited after deployment\n")
	match, err = DraftPreviewMatchesWorkingTree(context.Background(), runtime, state, []string{"content/selected.md"})
	if err != nil {
		t.Fatal(err)
	}
	if match {
		t.Fatal("DraftPreviewMatchesWorkingTree() accepted stale deployed content")
	}
}

func TestCommitAndPushDraftPreviewRejectsUnsafeIdentifiersAndPaths(t *testing.T) {
	runtime, _ := setupDraftPreviewRepository(t)
	pushCalled := false
	push := func(string, string, ...string) (string, error) {
		pushCalled = true
		return "", nil
	}
	tests := []struct {
		draftID string
		paths   []string
	}{
		{draftID: "../escape", paths: []string{"content/selected.md"}},
		{draftID: "UPPER", paths: []string{"content/selected.md"}},
		{draftID: "draft-1", paths: []string{"../outside.md"}},
		{draftID: "draft-1", paths: []string{".git/config"}},
		{draftID: "draft-1", paths: nil},
	}
	for _, test := range tests {
		if _, _, err := commitAndPushDraftPreview(context.Background(), runtime, "token", test.draftID, test.paths, push); err == nil {
			t.Fatalf("draft=%q paths=%#v should fail", test.draftID, test.paths)
		}
	}
	if pushCalled {
		t.Fatal("unsafe input reached push")
	}
}

func TestDraftPreviewStoreRoundTripAndIdentityProtection(t *testing.T) {
	store, err := NewDraftPreviewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	state := DraftPreviewState{
		SiteID:          "docs site",
		DraftID:         "draft-1",
		Branch:          "cms-preview/draft-1",
		CommitSHA:       testCommitSHA,
		DeploymentID:    "deployment-1",
		Status:          PreviewDeploymentReady,
		URL:             "https://immutable.docs.pages.dev",
		AccessProtected: true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Get(state.SiteID, state.DraftID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.CommitSHA != state.CommitSHA || got.URL != state.URL || !got.AccessProtected {
		t.Fatalf("Get() = %#v", got)
	}
	_, path := store.statePath(state.SiteID, state.DraftID)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]interface{}
	if err := json.Unmarshal(content, &encoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := encoded["token"]; exists || strings.Contains(string(content), "CLOUDFLARE_API_TOKEN") {
		t.Fatalf("state exposed token data: %s", content)
	}
	if err := store.Delete(state.SiteID, state.DraftID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(state.SiteID, state.DraftID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Get() after Delete error = %v", err)
	}
}

func TestDraftPreviewStoreListsCleanupTombstones(t *testing.T) {
	store, err := NewDraftPreviewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, state := range []DraftPreviewState{
		{SiteID: "docs", DraftID: "pending", Branch: "cms-preview/pending", CommitSHA: testCommitSHA, Status: PreviewDeploymentFailed, CleanupPending: true, CreatedAt: now, UpdatedAt: now},
		{SiteID: "docs", DraftID: "active", Branch: "cms-preview/active", CommitSHA: testCommitSHA, Status: PreviewDeploymentBuilding, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.Save(state); err != nil {
			t.Fatal(err)
		}
	}
	states, err := store.ListCleanupPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].DraftID != "pending" {
		t.Fatalf("cleanup-pending states = %#v", states)
	}
}

func TestApplyProviderDeploymentNeverExposesWrongOrUnreadyURL(t *testing.T) {
	store, err := NewDraftPreviewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := DraftPreviewState{SiteID: "docs", DraftID: "draft-1", Branch: "cms-preview/draft-1", CommitSHA: testCommitSHA, Status: PreviewDeploymentQueued, CreatedAt: now, UpdatedAt: now}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	building := PreviewDeployment{ID: "deployment-1", Branch: state.Branch, CommitSHA: state.CommitSHA, Status: PreviewDeploymentBuilding, URL: "https://stale.example"}
	got, err := applyProviderDeployment(store, state, building)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "" {
		t.Fatalf("building deployment exposed URL %q", got.URL)
	}
	wrong := building
	wrong.CommitSHA = "ffffffffffffffffffffffffffffffffffffffff"
	if _, err := applyProviderDeployment(store, state, wrong); !IsPreviewProviderError(err, PreviewProviderInvalidReply) {
		t.Fatalf("wrong commit error = %v", err)
	}
}

func TestCleanupDraftPreviewDeletesRemoteProviderLocalRefAndState(t *testing.T) {
	runtime, remotePath := setupDraftPreviewRepository(t)
	writeDraftTestFile(t, filepath.Join(runtime.RepoPath, "content", "selected.md"), "changed\n")
	branch, commitSHA, err := commitAndPushDraftPreview(context.Background(), runtime, "token", "draft-1", []string{"content/selected.md"}, localDraftPush(t, runtime))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewDraftPreviewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := DraftPreviewState{SiteID: runtime.ID, DraftID: "draft-1", Branch: branch, CommitSHA: commitSHA, DeploymentID: "deployment-1", Status: PreviewDeploymentReady, URL: "https://immutable.example", CreatedAt: now, UpdatedAt: now}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	provider := &fakePreviewProvider{}
	if err := cleanupDraftPreview(context.Background(), runtime, "token", "draft-1", store, provider, localDraftPush(t, runtime)); err != nil {
		t.Fatalf("cleanupDraftPreview() error = %v", err)
	}
	if provider.deletedID != "deployment-1" {
		t.Fatalf("provider deleted ID = %q", provider.deletedID)
	}
	if command := exec.Command("git", "rev-parse", "--verify", "refs/heads/cms-preview/draft-1"); func() bool { command.Dir = remotePath; return command.Run() == nil }() {
		t.Fatal("remote preview branch still exists")
	}
	if command := exec.Command("git", "rev-parse", "--verify", "refs/heads/cms-preview/draft-1"); func() bool { command.Dir = runtime.RepoPath; return command.Run() == nil }() {
		t.Fatal("local preview branch still exists")
	}
	if _, err := store.Get(runtime.ID, "draft-1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state still exists: %v", err)
	}
}

func setupDraftPreviewRepository(t *testing.T) (config.SiteRuntime, string) {
	t.Helper()
	repoPath := t.TempDir()
	remotePath := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remotePath, 0755); err != nil {
		t.Fatal(err)
	}
	runGitForDraftTest(t, remotePath, "init", "--bare")
	runGitForDraftTest(t, repoPath, "init", "-b", "main")
	writeDraftTestFile(t, filepath.Join(repoPath, "content", "selected.md"), "selected initial\n")
	writeDraftTestFile(t, filepath.Join(repoPath, "content", "unrelated.md"), "unrelated initial\n")
	runGitForDraftTest(t, repoPath, "add", ".")
	runGitForDraftTest(t, repoPath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	runGitForDraftTest(t, repoPath, "remote", "add", "origin", remotePath)
	runGitForDraftTest(t, repoPath, "push", "-u", "origin", "main")
	runtime := config.NewSiteRuntime(config.SiteConfig{ID: "docs", RepoPath: repoPath, ContentDir: "content", StaticDir: "static", PublicDir: "public"})
	runtime.GitBranch = "main"
	runtime.GitRemote = "origin"
	runtime.GitUserName = "Test"
	runtime.GitUserEmail = "test@example.com"
	return runtime, remotePath
}

func localDraftPush(t *testing.T, runtime config.SiteRuntime) gitPushFunc {
	t.Helper()
	return func(directory, _ string, args ...string) (string, error) {
		command := exec.Command("git", args...)
		command.Dir = directory
		output, err := command.CombinedOutput()
		return string(output), err
	}
}

func writeDraftTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runGitForDraftTest(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func runGitOutputForDraftTest(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(output)
}
