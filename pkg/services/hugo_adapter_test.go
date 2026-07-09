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

func TestHugoArgsIncludeConfiguredContentDir(t *testing.T) {
	originalRepoPath := config.RepoPath
	originalContentDir := config.ContentDir
	originalPublicDir := config.PublicDir
	originalPort := config.HugoServerPort
	originalBind := config.HugoServerBind
	t.Cleanup(func() {
		config.RepoPath = originalRepoPath
		config.ContentDir = originalContentDir
		config.PublicDir = originalPublicDir
		config.HugoServerPort = originalPort
		config.HugoServerBind = originalBind
	})

	config.RepoPath = "repo"
	config.ContentDir = "src"
	config.PublicDir = "_site"
	config.HugoServerPort = "1320"
	config.HugoServerBind = "127.0.0.1"

	if got := argValue(hugoServerArgs(), "--contentDir"); got != "src" {
		t.Fatalf("hugoServerArgs contentDir = %q, want src", got)
	}
	if got := argValue(hugoBuildArgs(), "--contentDir"); got != "src" {
		t.Fatalf("hugoBuildArgs contentDir = %q, want src", got)
	}
	if got := argValue(hugoBuildArgs(), "--destination"); got != "_site" {
		t.Fatalf("hugoBuildArgs destination = %q, want _site", got)
	}
	newArgs := hugoNewContentArgs("posts/a.md")
	if len(newArgs) < 3 || newArgs[0] != "new" || newArgs[1] != "content" {
		t.Fatalf("hugoNewContentArgs = %#v, want hugo new content subcommand", newArgs)
	}
	if got := argValue(newArgs, "--contentDir"); got != "src" {
		t.Fatalf("hugoNewContentArgs contentDir = %q, want src", got)
	}
	if got := newArgs[len(newArgs)-1]; got != "posts/a.md" {
		t.Fatalf("hugoNewContentArgs path = %q, want posts/a.md", got)
	}
}

func argValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}
