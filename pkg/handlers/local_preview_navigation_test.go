package handlers

import (
	"bytes"
	"encoding/json"
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
	touched   bool
}

func (m *fakeLocalPreviewNavigationWorkspaceManager) Status(string) (services.LocalPreviewWorkspace, bool, bool) {
	return m.workspace, m.active, m.stale
}

func (m *fakeLocalPreviewNavigationWorkspaceManager) TouchArticle(config.SiteRuntime, string, string) (services.LocalPreviewWorkspace, error) {
	m.touched = true
	return m.workspace, nil
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

	var ensuredSite config.SiteConfig
	response := executeLocalPreviewNavigationRequest(t, site.ID, localPreviewNavigationDependencies{
		workspaceManager: workspaceManager,
		ensureReady: func(site config.SiteConfig) error {
			ensuredSite = site
			return nil
		},
	}, "draft-1", "one.md")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "navigated" || int(payload["revision"].(float64)) != 7 {
		t.Fatalf("response = %#v", payload)
	}
	if ensuredSite.ContentDir != workspace.ContentDir {
		t.Fatalf("Hugo content directory = %q, want shadow directory %q", ensuredSite.ContentDir, workspace.ContentDir)
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
	ensureCalls := 0
	dependencies := localPreviewNavigationDependencies{
		workspaceManager: workspaceManager,
		ensureReady: func(config.SiteConfig) error {
			ensureCalls++
			return nil
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
	if ensureCalls != 0 {
		t.Fatalf("EnsureReady calls = %d, want 0 for rejected requests", ensureCalls)
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
	ensureCalls := 0
	response := executeLocalPreviewNavigationRequest(t, site.ID, localPreviewNavigationDependencies{
		workspaceManager: workspaceManager,
		ensureReady: func(config.SiteConfig) error {
			ensureCalls++
			return nil
		},
	}, "draft-1", "one.md")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	if ensureCalls != 0 || workspaceManager.touched {
		t.Fatalf("stale session was processed: ensureCalls=%d touched=%v", ensureCalls, workspaceManager.touched)
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
