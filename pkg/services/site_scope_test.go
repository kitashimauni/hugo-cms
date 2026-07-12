package services

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"os"
	"path/filepath"
	"testing"
)

func restoreServiceSiteScopeConfig(t *testing.T) {
	t.Helper()

	originalRepoPath := config.RepoPath
	originalContentDir := config.ContentDir
	originalStaticDir := config.StaticDir
	originalPublicDir := config.PublicDir
	originalPublicPath := config.PublicPath
	originalPreviewURL := config.PreviewURL
	originalSiteGenerator := config.SiteGenerator
	originalHugoServerPort := config.HugoServerPort
	originalHugoServerBind := config.HugoServerBind
	originalArticleMediaDir := config.ArticleMediaDir
	originalStaticMediaDir := config.StaticMediaDir
	originalSnippetPaths := append([]string(nil), config.SnippetPaths...)

	cacheMutex.Lock()
	originalArticleCaches := articleCaches
	articleCaches = map[string][]models.Article{}
	cacheMutex.Unlock()

	t.Cleanup(func() {
		config.RepoPath = originalRepoPath
		config.ContentDir = originalContentDir
		config.StaticDir = originalStaticDir
		config.PublicDir = originalPublicDir
		config.PublicPath = originalPublicPath
		config.PreviewURL = originalPreviewURL
		config.SiteGenerator = originalSiteGenerator
		config.HugoServerPort = originalHugoServerPort
		config.HugoServerBind = originalHugoServerBind
		config.ArticleMediaDir = originalArticleMediaDir
		config.StaticMediaDir = originalStaticMediaDir
		config.SnippetPaths = originalSnippetPaths

		cacheMutex.Lock()
		articleCaches = originalArticleCaches
		cacheMutex.Unlock()
	})
}

func TestArticleCacheIsKeyedBySiteRuntime(t *testing.T) {
	restoreServiceSiteScopeConfig(t)

	defaultRepo := t.TempDir()
	otherRepo := t.TempDir()
	writeMarkdown(t, filepath.Join(defaultRepo, "content", "one.md"), "---\ntitle: One\n---\n")
	writeMarkdown(t, filepath.Join(otherRepo, "src", "two.md"), "---\ntitle: Two\n---\n")

	defaultRuntime := config.NewSiteRuntime(config.SiteConfig{
		ID:             "default",
		RepoPath:       defaultRepo,
		Generator:      "hugo",
		ContentDir:     "content",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/",
		HugoServerPort: "1314",
		HugoServerBind: "127.0.0.1",
	})
	otherRuntime := config.NewSiteRuntime(config.SiteConfig{
		ID:             "other",
		RepoPath:       otherRepo,
		Generator:      "eleventy",
		ContentDir:     "src",
		StaticDir:      "assets",
		PublicDir:      "_site",
		PreviewURL:     "/other/",
		HugoServerPort: "1315",
		HugoServerBind: "127.0.0.1",
	})

	defaultArticles, err := GetArticlesCacheForRuntime(defaultRuntime)
	if err != nil {
		t.Fatalf("GetArticlesCache(default) error = %v", err)
	}
	if len(defaultArticles) != 1 || defaultArticles[0].Path != "one.md" {
		t.Fatalf("default articles = %#v, want one.md", defaultArticles)
	}

	writeMarkdown(t, filepath.Join(defaultRepo, "content", "new.md"), "---\ntitle: New\n---\n")

	otherArticles, err := GetArticlesCacheForRuntime(otherRuntime)
	if err != nil {
		t.Fatalf("GetArticlesCache(other) error = %v", err)
	}
	if len(otherArticles) != 1 || otherArticles[0].Path != "two.md" {
		t.Fatalf("other articles = %#v, want two.md", otherArticles)
	}

	defaultArticles, err = GetArticlesCacheForRuntime(defaultRuntime)
	if err != nil {
		t.Fatalf("GetArticlesCache(default cached) error = %v", err)
	}
	if len(defaultArticles) != 1 || defaultArticles[0].Path != "one.md" {
		t.Fatalf("default cached articles = %#v, want cached one.md", defaultArticles)
	}

	InvalidateCacheForRuntime(defaultRuntime)
	defaultArticles, err = GetArticlesCacheForRuntime(defaultRuntime)
	if err != nil {
		t.Fatalf("GetArticlesCache(default invalidated) error = %v", err)
	}
	if len(defaultArticles) != 2 {
		t.Fatalf("default invalidated article count = %d, want 2", len(defaultArticles))
	}
}

func writeMarkdown(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
