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

func TestRequestedRuntimeResolvesSelectedSiteWithoutMutatingGlobals(t *testing.T) {
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

	runtime, err := requestedRuntime(c)
	if err != nil {
		t.Fatalf("requestedRuntime() error = %v", err)
	}
	if config.RepoPath != defaultRepo {
		t.Fatalf("RepoPath after runtime resolution = %q, want unchanged default %q", config.RepoPath, defaultRepo)
	}
	if config.ContentDir != "content" {
		t.Fatalf("ContentDir after runtime resolution = %q, want content", config.ContentDir)
	}
	if currentSiteID(c) != "docs" ||
		runtime.RepoPath != selectedRepo ||
		runtime.ContentDir != "src" ||
		runtime.Generator != "eleventy" {
		t.Fatalf("runtime = %#v, want selected site runtime fields", runtime)
	}
}

func TestGetSnippetsUsesSelectedSitePaths(t *testing.T) {
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

	GetSnippets(c)

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

func TestGetConfigIncludesValidationWarnings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreSiteScopeConfig(t)

	repoPath := t.TempDir()
	writeSnippetFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: posts
      folder: content/posts
      path: "{{slug}}"
      fields:
        - { name: title, widget: string }
preview:
  url_field: permalink
`)
	config.DefaultSiteID = "default"
	config.Sites = []config.SiteConfig{{
		ID:             "default",
		RepoPath:       repoPath,
		Generator:      "hugo",
		ContentDir:     "content",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/",
		HugoServerPort: "1314",
		HugoServerBind: "127.0.0.1",
	}}
	config.ApplySiteRuntime(config.Sites[0])

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)

	GetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig() status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	meta, ok := body["_cms"].(map[string]interface{})
	if !ok {
		t.Fatalf("_cms = %#v, want metadata map", body["_cms"])
	}
	warnings, ok := meta["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Fatalf("warnings = %#v, want validation warnings", meta["warnings"])
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

func TestSelectedSiteHandlersRejectUnknownSite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restoreSiteScopeConfig(t)

	config.DefaultSiteID = "default"
	config.Sites = []config.SiteConfig{{ID: "default", RepoPath: t.TempDir(), ContentDir: "content", StaticDir: "static", PublicDir: "public"}}
	config.ApplySiteRuntime(config.Sites[0])

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/articles?site=missing", nil)

	GetSnippets(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetSnippets() status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
