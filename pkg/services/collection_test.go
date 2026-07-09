package services

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"os"
	"path/filepath"
	"testing"
)

func restoreCollectionTestConfig(t *testing.T, repoPath, contentDir, staticDir string) {
	t.Helper()

	originalRepoPath := config.RepoPath
	originalContentDir := config.ContentDir
	originalStaticDir := config.StaticDir
	t.Cleanup(func() {
		config.RepoPath = originalRepoPath
		config.ContentDir = originalContentDir
		config.StaticDir = originalStaticDir
	})

	config.RepoPath = repoPath
	config.ContentDir = contentDir
	config.StaticDir = staticDir
}

func TestGetCollectionForPathNormalizesLegacyCollectionFolder(t *testing.T) {
	repoPath := t.TempDir()
	restoreCollectionTestConfig(t, repoPath, "src", "static")

	configPath := filepath.Join(repoPath, "static", "admin", "config.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`
collections:
  - name: posts
    folder: content/posts
    fields:
      - name: title
        widget: string
        default: Untitled
`), 0644); err != nil {
		t.Fatalf("write CMS config: %v", err)
	}

	collectionPath := filepath.ToSlash(filepath.Join(config.ContentDir, "posts", "draft.md"))
	collection, err := GetCollectionForPath(collectionPath)
	if err != nil {
		t.Fatalf("GetCollectionForPath(%q) returned error: %v", collectionPath, err)
	}
	if collection.Name != "posts" {
		t.Fatalf("collection name = %q, want posts", collection.Name)
	}
}

func TestCollectionFolderWithinContentMapsLegacyContentFolder(t *testing.T) {
	restoreCollectionTestConfig(t, t.TempDir(), "src", "static")

	folder, err := CollectionFolderWithinContent(models.Collection{Folder: "content/posts"})
	if err != nil {
		t.Fatalf("CollectionFolderWithinContent() returned error: %v", err)
	}
	if folder != "src/posts" {
		t.Fatalf("folder = %q, want src/posts", folder)
	}
}
