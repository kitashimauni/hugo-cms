package services

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestWithSiteRuntimeLockWaitsForScopedRuntimeRestore(t *testing.T) {
	restoreServiceSiteScopeConfig(t)

	defaultRepo := t.TempDir()
	otherRepo := t.TempDir()
	config.ApplySiteRuntime(config.SiteConfig{
		RepoPath:       defaultRepo,
		Generator:      "hugo",
		ContentDir:     "content",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/",
		HugoServerPort: "1314",
		HugoServerBind: "127.0.0.1",
	})

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan string, 1)

	go func() {
		_ = WithSiteRuntime(config.SiteConfig{
			ID:             "other",
			RepoPath:       otherRepo,
			Generator:      "eleventy",
			ContentDir:     "src",
			StaticDir:      "assets",
			PublicDir:      "_site",
			PreviewURL:     "/other/",
			HugoServerPort: "1315",
			HugoServerBind: "127.0.0.1",
		}, func() error {
			close(started)
			<-release
			return nil
		})
	}()

	<-started
	go func() {
		_ = WithSiteRuntimeLock(func() error {
			done <- config.RepoPath
			return nil
		})
	}()

	select {
	case repoPath := <-done:
		close(release)
		t.Fatalf("WithSiteRuntimeLock ran while scoped runtime was active, repo path = %q", repoPath)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case repoPath := <-done:
		if repoPath != defaultRepo {
			t.Fatalf("WithSiteRuntimeLock saw repo path = %q, want restored default %q", repoPath, defaultRepo)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WithSiteRuntimeLock did not run after scoped runtime was released")
	}
}

func TestArticleCacheIsKeyedBySiteRuntime(t *testing.T) {
	restoreServiceSiteScopeConfig(t)

	defaultRepo := t.TempDir()
	otherRepo := t.TempDir()
	writeMarkdown(t, filepath.Join(defaultRepo, "content", "one.md"), "---\ntitle: One\n---\n")
	writeMarkdown(t, filepath.Join(otherRepo, "src", "two.md"), "---\ntitle: Two\n---\n")

	config.ApplySiteRuntime(config.SiteConfig{
		RepoPath:       defaultRepo,
		Generator:      "hugo",
		ContentDir:     "content",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/",
		HugoServerPort: "1314",
		HugoServerBind: "127.0.0.1",
	})
	defaultArticles, err := GetArticlesCache()
	if err != nil {
		t.Fatalf("GetArticlesCache(default) error = %v", err)
	}
	if len(defaultArticles) != 1 || defaultArticles[0].Path != "one.md" {
		t.Fatalf("default articles = %#v, want one.md", defaultArticles)
	}

	writeMarkdown(t, filepath.Join(defaultRepo, "content", "new.md"), "---\ntitle: New\n---\n")

	if err := WithSiteRuntime(config.SiteConfig{
		ID:             "other",
		RepoPath:       otherRepo,
		Generator:      "eleventy",
		ContentDir:     "src",
		StaticDir:      "assets",
		PublicDir:      "_site",
		PreviewURL:     "/other/",
		HugoServerPort: "1315",
		HugoServerBind: "127.0.0.1",
	}, func() error {
		otherArticles, err := GetArticlesCache()
		if err != nil {
			return err
		}
		if len(otherArticles) != 1 || otherArticles[0].Path != "two.md" {
			t.Fatalf("other articles = %#v, want two.md", otherArticles)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithSiteRuntime(other) error = %v", err)
	}

	defaultArticles, err = GetArticlesCache()
	if err != nil {
		t.Fatalf("GetArticlesCache(default cached) error = %v", err)
	}
	if len(defaultArticles) != 1 || defaultArticles[0].Path != "one.md" {
		t.Fatalf("default cached articles = %#v, want cached one.md", defaultArticles)
	}

	InvalidateCache()
	defaultArticles, err = GetArticlesCache()
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
