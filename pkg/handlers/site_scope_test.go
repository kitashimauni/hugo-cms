package handlers

import (
	"encoding/json"
	"hugo-cms/pkg/config"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func restoreSiteScopeConfig(t *testing.T) {
	t.Helper()

	originalRepoPath := config.RepoPath
	originalContentDir := config.ContentDir
	originalStaticDir := config.StaticDir
	originalPublicDir := config.PublicDir
	originalPublicPath := config.PublicPath
	originalPreviewURL := config.PreviewURL
	originalSiteGenerator := config.SiteGenerator
	originalDefaultSiteID := config.DefaultSiteID
	originalSites := append([]config.SiteConfig(nil), config.Sites...)

	t.Cleanup(func() {
		config.RepoPath = originalRepoPath
		config.ContentDir = originalContentDir
		config.StaticDir = originalStaticDir
		config.PublicDir = originalPublicDir
		config.PublicPath = originalPublicPath
		config.PreviewURL = originalPreviewURL
		config.SiteGenerator = originalSiteGenerator
		config.DefaultSiteID = originalDefaultSiteID
		config.Sites = originalSites
	})
}

func TestSiteScopedAppliesRequestedSiteAndRestoresRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreSiteScopeConfig(t)

	defaultRepo := t.TempDir()
	selectedRepo := t.TempDir()
	config.DefaultSiteID = "default"
	config.Sites = []config.SiteConfig{
		{
			ID:             "default",
			Name:           "Default",
			RepoPath:       defaultRepo,
			Generator:      "hugo",
			ContentDir:     "content",
			StaticDir:      "static",
			PublicDir:      "public",
			PreviewURL:     "/",
			HugoServerPort: "1314",
			HugoServerBind: "127.0.0.1",
		},
		{
			ID:             "docs",
			Name:           "Docs",
			RepoPath:       selectedRepo,
			Generator:      "eleventy",
			ContentDir:     "src",
			StaticDir:      "public-assets",
			PublicDir:      "_site",
			PreviewURL:     "/docs/",
			HugoServerPort: "1315",
			HugoServerBind: "127.0.0.1",
		},
	}
	config.ApplySiteRuntime(config.Sites[0])

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/config?site=docs", nil)

	SiteScoped(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"site":        currentSiteID(c),
			"repo_path":   config.RepoPath,
			"content_dir": config.ContentDir,
			"generator":   config.SiteGenerator,
		})
	})(c)

	if w.Code != http.StatusOK {
		t.Fatalf("SiteScoped() status = %d, body = %s", w.Code, w.Body.String())
	}
	if config.RepoPath != defaultRepo {
		t.Fatalf("RepoPath after scoped request = %q, want restored %q", config.RepoPath, defaultRepo)
	}
	if config.ContentDir != "content" {
		t.Fatalf("ContentDir after scoped request = %q, want content", config.ContentDir)
	}
	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response["site"] != "docs" ||
		response["repo_path"] != selectedRepo ||
		response["content_dir"] != "src" ||
		response["generator"] != "eleventy" {
		t.Fatalf("response = %#v, want selected site runtime fields", response)
	}
}

func TestSiteScopedRejectsUnknownSite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreSiteScopeConfig(t)

	config.DefaultSiteID = "default"
	config.Sites = []config.SiteConfig{{ID: "default", RepoPath: t.TempDir(), ContentDir: "content", StaticDir: "static", PublicDir: "public"}}
	config.ApplySiteRuntime(config.Sites[0])

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/articles?site=missing", nil)

	SiteScoped(func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("SiteScoped() status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
