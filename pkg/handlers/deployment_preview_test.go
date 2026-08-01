package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDeploymentDraftPathsIncludesOnlyReferencedMedia(t *testing.T) {
	repo := t.TempDir()
	runtime := config.SiteRuntime{RepoPath: repo, ContentDir: "content", StaticDir: "static"}
	writeDeploymentTestFile(t, filepath.Join(repo, "content", "posts", "hello.md"), "---\ntitle: Hello\n---\n![hero](/images/hero.png)\n![local](local.webp)\n")
	writeDeploymentTestFile(t, filepath.Join(repo, "static", "images", "hero.png"), "image")
	writeDeploymentTestFile(t, filepath.Join(repo, "static", "images", "unrelated.png"), "image")
	writeDeploymentTestFile(t, filepath.Join(repo, "content", "posts", "local.webp"), "image")

	got, err := deploymentDraftPaths(runtime, "posts/hello.md")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"content/posts/hello.md", "static/images/hero.png", "content/posts/local.webp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deploymentDraftPaths() = %#v, want %#v", got, want)
	}
}

func TestDeploymentStateResponseOnlyLinksFailedCloudflareDeploymentLogs(t *testing.T) {
	now := time.Now().UTC()
	runtime := config.SiteRuntime{PreviewDeployment: config.DeploymentPreviewConfig{
		Provider:        "cloudflare_pages",
		CloudflarePages: config.CloudflarePagesConfig{AccountID: "account", ProjectName: "project"},
	}}
	state := services.DraftPreviewState{DeploymentID: "deployment", Status: services.PreviewDeploymentFailed, CreatedAt: now, UpdatedAt: now}
	response := deploymentStateResponse(runtime, state)
	if response["log_url"] != "https://dash.cloudflare.com/account/pages/view/project/deployment" {
		t.Fatalf("log_url = %q", response["log_url"])
	}
	state.Status = services.PreviewDeploymentBuilding
	response = deploymentStateResponse(runtime, state)
	if _, ok := response["log_url"]; ok {
		t.Fatal("building deployment unexpectedly exposed a log link")
	}
}

func TestHandleDeploymentErrorReturnsConflictForPublishInvariantFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, err := range []error{
		services.ErrDraftPreviewArticleMismatch,
		services.ErrDraftPreviewNotReady,
		services.ErrDraftPreviewStale,
		services.ErrDraftPreviewBranchMoved,
	} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		handleDeploymentError(context, "publish", err)
		if recorder.Code != http.StatusConflict {
			t.Fatalf("error %v status = %d, want 409", err, recorder.Code)
		}
	}
}

func writeDeploymentTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
