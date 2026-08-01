package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"hugo-cms/pkg/config"
)

func TestCreateDraftPreviewPullRequestCreatesDraftPR(t *testing.T) {
	repo := t.TempDir()
	runGitForPullRequestTest(t, repo, "init")
	runGitForPullRequestTest(t, repo, "remote", "add", "origin", "https://github.com/example/site.git")

	var posted map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"html_url":"https://github.com/example/site/pull/42"}`))
	}))
	defer server.Close()

	runtime := config.SiteRuntime{ID: "site", RepoPath: repo, GitRemote: "origin", GitBranch: "main"}
	state := DraftPreviewState{
		SiteID: "site", DraftID: "draft-1", Branch: "cms-preview/draft-1",
		CommitSHA: "0123456789abcdef0123456789abcdef01234567", Status: PreviewDeploymentReady,
		URL: "https://abc.pages.dev", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	got, err := createDraftPreviewPullRequest(context.Background(), runtime, "test-token", state, "posts/hello.md", githubPullRequestClient{baseURL: server.URL, httpClient: server.Client()})
	if err != nil {
		t.Fatalf("createDraftPreviewPullRequest() error = %v", err)
	}
	if got != "https://github.com/example/site/pull/42" {
		t.Fatalf("URL = %q", got)
	}
	if posted["head"] != state.Branch || posted["base"] != "main" || posted["draft"] != true {
		t.Fatalf("payload = %#v", posted)
	}
}

func runGitForPullRequestTest(t *testing.T, directory string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = filepath.Clean(directory)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
