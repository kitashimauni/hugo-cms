package services

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"os"
	"path/filepath"
	"testing"
)

func TestGetCollectionForPathNormalizesLegacyCollectionFolder(t *testing.T) {
	repoPath := t.TempDir()
	runtime := config.NewSiteRuntime(config.SiteConfig{
		ID:             "test",
		RepoPath:       repoPath,
		ContentDir:     "src",
		StaticDir:      "static",
		PublicDir:      "public",
		HugoServerBind: "127.0.0.1",
		HugoServerPort: "1314",
	})

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

	collectionPath := filepath.ToSlash(filepath.Join(runtime.ContentDir, "posts", "draft.md"))
	collection, err := GetCollectionForPathForRuntime(runtime, collectionPath)
	if err != nil {
		t.Fatalf("GetCollectionForPath(%q) returned error: %v", collectionPath, err)
	}
	if collection.Name != "posts" {
		t.Fatalf("collection name = %q, want posts", collection.Name)
	}
}

func TestCollectionFolderWithinContentMapsLegacyContentFolder(t *testing.T) {
	runtime := config.NewSiteRuntime(config.SiteConfig{RepoPath: t.TempDir(), ContentDir: "src", StaticDir: "static"})

	folder, err := CollectionFolderWithinContentForRuntime(runtime, models.Collection{Folder: "content/posts"})
	if err != nil {
		t.Fatalf("CollectionFolderWithinContent() returned error: %v", err)
	}
	if folder != "src/posts" {
		t.Fatalf("folder = %q, want src/posts", folder)
	}
}
