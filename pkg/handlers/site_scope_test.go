package handlers

import (
	"encoding/json"
	"hugo-cms/pkg/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	originalSnippetPaths := append([]string(nil), config.SnippetPaths...)
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
		config.SnippetPaths = originalSnippetPaths
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
			SnippetPaths:   []string{filepath.Join(defaultRepo, ".vscode", "md.code-snippets")},
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
			SnippetPaths:   []string{filepath.Join(selectedRepo, ".vscode", "md.code-snippets")},
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

func TestSiteScopedSnippetsUseSelectedSitePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreSiteScopeConfig(t)

	defaultRepo := t.TempDir()
	selectedRepo := t.TempDir()
	writeSnippetFile(t, filepath.Join(defaultRepo, ".vscode", "md.code-snippets"), `{
  "Default Snippet": {
    "prefix": "default",
    "body": "default body",
    "description": "default site"
  }
}`)
	writeSnippetFile(t, filepath.Join(selectedRepo, ".vscode", "md.code-snippets"), `{
  "Selected Snippet": {
    "prefix": "selected",
    "body": "selected body",
    "description": "selected site"
  }
}`)

	config.DefaultSiteID = "default"
	config.Sites = []config.SiteConfig{
		{
			ID:             "default",
			RepoPath:       defaultRepo,
			Generator:      "hugo",
			ContentDir:     "content",
			StaticDir:      "static",
			PublicDir:      "public",
			PreviewURL:     "/",
			HugoServerPort: "1314",
			HugoServerBind: "127.0.0.1",
			SnippetPaths:   []string{filepath.Join(defaultRepo, ".vscode", "md.code-snippets")},
		},
		{
			ID:             "docs",
			RepoPath:       selectedRepo,
			Generator:      "eleventy",
			ContentDir:     "src",
			StaticDir:      "static",
			PublicDir:      "public",
			PreviewURL:     "/docs/",
			HugoServerPort: "1315",
			HugoServerBind: "127.0.0.1",
			SnippetPaths:   []string{filepath.Join(selectedRepo, ".vscode", "md.code-snippets")},
		},
	}
	config.ApplySiteRuntime(config.Sites[0])

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/snippets?site=docs", nil)

	SiteScoped(GetSnippets)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("GetSnippets() status = %d, body = %s", w.Code, w.Body.String())
	}
	var snippets map[string]SnippetDef
	if err := json.Unmarshal(w.Body.Bytes(), &snippets); err != nil {
		t.Fatalf("unmarshal snippets: %v", err)
	}
	if _, ok := snippets["Selected Snippet"]; !ok {
		t.Fatalf("snippets = %#v, want selected site snippet", snippets)
	}
	if _, ok := snippets["Default Snippet"]; ok {
		t.Fatalf("snippets = %#v, should not include default site snippet", snippets)
	}
}

func writeSnippetFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create snippet dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write snippet file: %v", err)
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
