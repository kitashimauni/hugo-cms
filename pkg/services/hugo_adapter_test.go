package services

import (
	"hugo-cms/pkg/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHugoAdapterCreateContentReturnsMatchedCollectionFormatError(t *testing.T) {
	repoPath := t.TempDir()
	configPath := filepath.Join(repoPath, "static", "admin", "config.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	cmsConfig := `collections:
  - name: posts
    folder: content/posts
    format: unsupported-format
    fields:
      - name: title
        widget: string
`
	if err := os.WriteFile(configPath, []byte(cmsConfig), 0644); err != nil {
		t.Fatalf("write CMS config: %v", err)
	}

	originalRepoPath := config.RepoPath
	config.RepoPath = repoPath
	t.Cleanup(func() { config.RepoPath = originalRepoPath })

	log, err := NewHugoAdapter().CreateContent("posts/new.md")
	if err == nil {
		t.Fatal("CreateContent() should return the matched collection format error")
	}
	if !strings.Contains(err.Error(), "unsupported front matter format") {
		t.Fatalf("CreateContent() error = %v, want unsupported format", err)
	}
	if log != "Failed to generate content from CMS config" {
		t.Fatalf("CreateContent() log = %q", log)
	}
	if _, statErr := os.Stat(filepath.Join(repoPath, "content", "posts", "new.md")); !os.IsNotExist(statErr) {
		t.Fatalf("CreateContent() unexpectedly created the file: %v", statErr)
	}
}
