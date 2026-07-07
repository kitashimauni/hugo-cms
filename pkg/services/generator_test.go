package services

import (
	"hugo-cms/pkg/config"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultGeneratorAdapterIsHugo(t *testing.T) {
	adapter := CurrentGeneratorAdapter()
	if adapter == nil {
		t.Fatal("CurrentGeneratorAdapter() returned nil")
	}
	if adapter.Name() != "hugo" {
		t.Fatalf("adapter name = %q, want hugo", adapter.Name())
	}
}

func TestSetGeneratorAdapterRejectsNil(t *testing.T) {
	if err := SetGeneratorAdapter(nil); err == nil {
		t.Fatal("SetGeneratorAdapter(nil) should fail")
	}
}

func TestNewGeneratorAdapterSupportsEleventy(t *testing.T) {
	adapter, err := NewGeneratorAdapter("eleventy")
	if err != nil {
		t.Fatalf("NewGeneratorAdapter(eleventy) error = %v", err)
	}
	if adapter.Name() != "eleventy" {
		t.Fatalf("adapter name = %q, want eleventy", adapter.Name())
	}
}

func TestNewGeneratorAdapterRejectsUnsupported(t *testing.T) {
	if _, err := NewGeneratorAdapter("jekyll"); err == nil {
		t.Fatal("NewGeneratorAdapter(jekyll) should fail")
	}
}

func TestValidateEleventyProjectRequiresPackageAndLock(t *testing.T) {
	repoPath := t.TempDir()
	if err := validateEleventyProject(repoPath); err == nil {
		t.Fatal("validateEleventyProject() should require package.json")
	}

	if err := os.WriteFile(filepath.Join(repoPath, "package.json"), []byte(`{"devDependencies":{"@11ty/eleventy":"latest"}}`), 0644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := validateEleventyProject(repoPath); err == nil {
		t.Fatal("validateEleventyProject() should require a lock file")
	}

	if err := os.WriteFile(filepath.Join(repoPath, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
	if err := validateEleventyProject(repoPath); err != nil {
		t.Fatalf("validateEleventyProject() error = %v, want nil", err)
	}
}

func TestGeneratorProcessEnvironmentDropsSecrets(t *testing.T) {
	t.Setenv("SESSION_SECRET", "super-secret")
	t.Setenv("GITHUB_CLIENT_SECRET", "oauth-secret")
	t.Setenv("PATH", "test-path")

	env := generatorProcessEnvironment()
	for _, entry := range env {
		if entry == "SESSION_SECRET=super-secret" || entry == "GITHUB_CLIENT_SECRET=oauth-secret" {
			t.Fatalf("generatorProcessEnvironment leaked secret entry %q", entry)
		}
	}
}

func TestEleventyCreateContentFromCollection(t *testing.T) {
	originalRepoPath := config.RepoPath
	originalContentDir := config.ContentDir
	originalStaticDir := config.StaticDir
	t.Cleanup(func() {
		config.RepoPath = originalRepoPath
		config.ContentDir = originalContentDir
		config.StaticDir = originalStaticDir
	})

	repoPath := t.TempDir()
	config.RepoPath = repoPath
	config.ContentDir = "src"
	config.StaticDir = "admin-static"

	configPath := filepath.Join(repoPath, config.StaticDir, "admin")
	if err := os.MkdirAll(configPath, 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "config.yml"), []byte(`
collections:
  - name: posts
    folder: src/posts
    format: yaml-frontmatter
    fields:
      - name: title
        widget: string
        default: Draft
`), 0644); err != nil {
		t.Fatalf("write cms config: %v", err)
	}

	log, err := NewEleventyAdapter().CreateContent("posts/new.md")
	if err != nil {
		t.Fatalf("CreateContent() error = %v, log = %q", err, log)
	}
	content, err := os.ReadFile(filepath.Join(repoPath, "src", "posts", "new.md"))
	if err != nil {
		t.Fatalf("read generated content: %v", err)
	}
	if string(content) == "" || content[0] != '-' {
		t.Fatalf("generated content = %q, want YAML front matter", content)
	}
}

func TestParseGitStatusPorcelainZ(t *testing.T) {
	out := []byte(" M content/posts/hello world.md\x00R  content/posts/new name.md\x00content/posts/old name.md\x00?? content/posts/日本語.md\x00")
	entries := parseGitStatusPorcelainZ(out)
	if len(entries) != 3 {
		t.Fatalf("parseGitStatusPorcelainZ() returned %d entries: %#v", len(entries), entries)
	}
	if entries[0] != " M content/posts/hello world.md" {
		t.Fatalf("entries[0] = %q", entries[0])
	}
	if entries[1] != "R  content/posts/new name.md" {
		t.Fatalf("entries[1] = %q", entries[1])
	}
	if entries[2] != "?? content/posts/日本語.md" {
		t.Fatalf("entries[2] = %q", entries[2])
	}
}
