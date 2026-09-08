package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeLocalPreviewNavigationWorkspaceManager struct {
	workspace services.LocalPreviewWorkspace
	active    bool
	stale     bool
}

func (m *fakeLocalPreviewNavigationWorkspaceManager) Status(string) (services.LocalPreviewWorkspace, bool, bool) {
	return m.workspace, m.active, m.stale
}

func TestNavigateLocalPreviewUsesShadowContentAndPreservesProductionContent(t *testing.T) {
	site, runtime := localPreviewNavigationTestSite(t)
	configureLocalPreviewNavigationTestSite(t, site)

	productionArticle := filepath.Join(runtime.RepoPath, runtime.ContentDir, "one.md")
	if err := os.WriteFile(productionArticle, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	workspaceManager, err := services.NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceManager.Shutdown() })
	workspace, _, _, err := workspaceManager.Update(runtime, "draft-1", "one.md", 7, []byte("draft"))
	if err != nil {
		t.Fatal(err)
	}

	var resolvedRuntime config.SiteRuntime
	response := executeLocalPreviewNavigationRequest(t, site.ID, localPreviewNavigationDependencies{
		workspaceManager: workspaceManager,
		resolveArticleURL: func(_ context.Context, runtime config.SiteRuntime, gotWorkspace services.LocalPreviewWorkspace, path string) (string, error) {
			resolvedRuntime = runtime
			if gotWorkspace.ContentDir != workspace.ContentDir || path != "one.md" {
				t.Fatalf("resolver arguments = runtime=%#v workspace=%#v path=%q", runtime, gotWorkspace, path)
			}
			return "https://tech.preview.example.com/posts/one/", nil
		},
	}, "draft-1", "one.md")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "resolved" || payload["article_url"] != "https://tech.preview.example.com/posts/one/" || int(payload["revision"].(float64)) != 7 {
		t.Fatalf("response = %#v", payload)
	}
	if resolvedRuntime.ContentDir != workspace.ContentDir {
		t.Fatalf("resolver content directory = %q, want shadow directory %q", resolvedRuntime.ContentDir, workspace.ContentDir)
	}
	production, err := os.ReadFile(productionArticle)
	if err != nil {
		t.Fatal(err)
	}
	if string(production) != "original" {
		t.Fatalf("production content = %q, want original", production)
	}
	shadow, err := os.ReadFile(filepath.Join(workspace.ContentDir, "one.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(shadow) != "draft" {
		t.Fatalf("shadow content = %q, want draft", shadow)
	}
}

func TestNavigateLocalPreviewRejectsWrongOwnerAndPath(t *testing.T) {
	site, runtime := localPreviewNavigationTestSite(t)
	configureLocalPreviewNavigationTestSite(t, site)
	workspaceManager, err := services.NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceManager.Shutdown() })
	if _, _, _, err := workspaceManager.Update(runtime, "draft-1", "one.md", 1, []byte("draft")); err != nil {
		t.Fatal(err)
	}
	resolveCalls := 0
	dependencies := localPreviewNavigationDependencies{
		workspaceManager: workspaceManager,
		resolveArticleURL: func(context.Context, config.SiteRuntime, services.LocalPreviewWorkspace, string) (string, error) {
			resolveCalls++
			return "", nil
		},
	}

	ownerResponse := executeLocalPreviewNavigationRequest(t, site.ID, dependencies, "draft-2", "one.md")
	if ownerResponse.Code != http.StatusConflict {
		t.Fatalf("wrong owner status = %d, want 409", ownerResponse.Code)
	}
	pathResponse := executeLocalPreviewNavigationRequest(t, site.ID, dependencies, "draft-1", "other.md")
	if pathResponse.Code != http.StatusConflict {
		t.Fatalf("wrong path status = %d, want 409", pathResponse.Code)
	}
	if resolveCalls != 0 {
		t.Fatalf("resolver calls = %d, want 0 for rejected requests", resolveCalls)
	}
}

func TestNavigateLocalPreviewRejectsStaleSession(t *testing.T) {
	site, _ := localPreviewNavigationTestSite(t)
	configureLocalPreviewNavigationTestSite(t, site)
	workspaceManager := &fakeLocalPreviewNavigationWorkspaceManager{
		workspace: services.LocalPreviewWorkspace{
			SiteID:      site.ID,
			DraftID:     "draft-1",
			ArticlePath: "one.md",
			Revision:    4,
		},
		active: true,
		stale:  true,
	}
	resolveCalls := 0
	response := executeLocalPreviewNavigationRequest(t, site.ID, localPreviewNavigationDependencies{
		workspaceManager: workspaceManager,
		resolveArticleURL: func(context.Context, config.SiteRuntime, services.LocalPreviewWorkspace, string) (string, error) {
			resolveCalls++
			return "", nil
		},
	}, "draft-1", "one.md")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	if resolveCalls != 0 {
		t.Fatalf("stale session was processed: resolver calls=%d", resolveCalls)
	}
}

func TestNavigateLocalPreviewReturnsErrorWhenResolverFails(t *testing.T) {
	site, _ := localPreviewNavigationTestSite(t)
	configureLocalPreviewNavigationTestSite(t, site)
	workspaceManager := &fakeLocalPreviewNavigationWorkspaceManager{
		workspace: services.LocalPreviewWorkspace{
			SiteID:      site.ID,
			DraftID:     "draft-1",
			ArticlePath: "one.md",
			ContentDir:  filepath.Join(site.RepoPath, "shadow", "content"),
		},
		active: true,
	}
	response := executeLocalPreviewNavigationRequest(t, site.ID, localPreviewNavigationDependencies{
		workspaceManager: workspaceManager,
		resolveArticleURL: func(context.Context, config.SiteRuntime, services.LocalPreviewWorkspace, string) (string, error) {
			return "", errors.New("generator failed")
		},
	}, "draft-1", "one.md")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

func executeLocalPreviewNavigationRequest(t *testing.T, siteID string, dependencies localPreviewNavigationDependencies, draftID, articlePath string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/admin/api/preview/local/navigate", func(c *gin.Context) {
		navigateLocalPreview(c, dependencies)
	})
	body, err := json.Marshal(map[string]string{"draft_id": draftID, "path": articlePath})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/api/preview/local/navigate?site="+siteID, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func localPreviewNavigationTestSite(t *testing.T) (config.SiteConfig, config.SiteRuntime) {
	t.Helper()
	enabled := true
	site := config.SiteConfig{
		ID:         "tech",
		Generator:  "hugo",
		RepoPath:   t.TempDir(),
		ContentDir: "content",
		Preview: config.SitePreviewConfig{
			LocalPreview: config.LocalPreviewConfig{Enabled: &enabled, URL: "http://tech.preview.example.com/"},
		},
	}
	if err := os.MkdirAll(filepath.Join(site.RepoPath, site.ContentDir), 0755); err != nil {
		t.Fatal(err)
	}
	return site, config.NewSiteRuntime(site)
}

func configureLocalPreviewNavigationTestSite(t *testing.T, site config.SiteConfig) {
	t.Helper()
	previousSites := config.Sites
	previousDefaultSiteID := config.DefaultSiteID
	config.Sites = []config.SiteConfig{site}
	config.DefaultSiteID = site.ID
	t.Cleanup(func() {
		config.Sites = previousSites
		config.DefaultSiteID = previousDefaultSiteID
	})
}
