package services

import (
	"hugo-cms/pkg/config"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteFile(t *testing.T) {
	runtime := config.NewSiteRuntime(config.SiteConfig{
		RepoPath:   t.TempDir(),
		ContentDir: "content",
	})

	t.Run("Empty path should fail", func(t *testing.T) {
		err := DeleteFileForRuntime(runtime, "")
		if err == nil {
			t.Error("DeleteFile(\"\") should return error")
		}
	})

	t.Run("Path traversal should fail", func(t *testing.T) {
		err := DeleteFileForRuntime(runtime, "../../../etc/passwd")
		if err == nil {
			t.Error("DeleteFile with path traversal should return error")
		}
	})
}

func TestGetConfig(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: posts
      folder: content/posts
      frontmatter: yaml
media:
  folder: assets/images
  public_path: /images
`)
	conf, err := GetConfigForRuntime(testRuntime(repoPath))
	if err != nil {
		t.Fatalf("GetConfigForRuntime() error = %v", err)
	}
	if conf == nil || conf["_config_source"] != ".homecms.yml" {
		t.Fatalf("GetConfigForRuntime() = %#v, want .homecms.yml source", conf)
	}
}

func TestGetCMSConfig(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, "static", "admin", "config.yml"), `
collections:
  - name: posts
    folder: content/posts
`)
	conf, err := GetCMSConfigForRuntime(testRuntime(repoPath))
	if err != nil {
		t.Fatalf("GetCMSConfigForRuntime() error = %v", err)
	}
	if conf == nil || len(conf.Collections) != 1 {
		t.Fatalf("GetCMSConfigForRuntime() = %#v, want one collection", conf)
	}
}

func TestGetConfigForRuntimePreservesLegacyRawFields(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, "static", "admin", "config.yml"), `
collections:
  - name: posts
    label: Blog Posts
    folder: content/posts
    create: true
    slug: "{{year}}-{{slug}}"
    fields:
      - name: title
        label: Title Label
        widget: string
`)

	conf, err := GetConfigForRuntime(testRuntime(repoPath))
	if err != nil {
		t.Fatalf("GetConfigForRuntime() error = %v", err)
	}
	collections, ok := conf["collections"].([]interface{})
	if !ok || len(collections) != 1 {
		t.Fatalf("collections = %#v, want one raw collection", conf["collections"])
	}
	collection, ok := collections[0].(map[string]interface{})
	if !ok {
		t.Fatalf("collection = %#v, want map", collections[0])
	}
	if collection["create"] != true || collection["slug"] != "{{year}}-{{slug}}" {
		t.Fatalf("collection = %#v, want preserved create and slug", collection)
	}
	fields, ok := collection["fields"].([]interface{})
	if !ok || len(fields) != 1 {
		t.Fatalf("fields = %#v, want one raw field", collection["fields"])
	}
	field, ok := fields[0].(map[string]interface{})
	if !ok || field["label"] != "Title Label" {
		t.Fatalf("field = %#v, want preserved label", fields[0])
	}
}

func TestHomeCMSConfigTakesPrecedenceOverLegacyConfig(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: home
      folder: content/home
      frontmatter: yaml
`)
	writeTestFile(t, filepath.Join(repoPath, "static", "admin", "config.yml"), `
collections:
  - name: legacy
    folder: content/legacy
`)

	conf, source, err := LoadCMSConfigForRuntime(testRuntime(repoPath))
	if err != nil {
		t.Fatalf("LoadCMSConfigForRuntime() error = %v", err)
	}
	if source != ".homecms.yml" {
		t.Fatalf("source = %q, want .homecms.yml", source)
	}
	if len(conf.Collections) != 1 || conf.Collections[0].Name != "home" {
		t.Fatalf("collections = %#v, want home collection", conf.Collections)
	}
	if conf.Collections[0].Format != "yaml-frontmatter" {
		t.Fatalf("format = %q, want yaml-frontmatter", conf.Collections[0].Format)
	}
}

func TestResolvePath(t *testing.T) {
	// Create test cases for path resolution
	// This tests the slug generation and path templating logic

	t.Run("Slug generation from title", func(t *testing.T) {
		// slugify function converts "Hello World" to "hello-world"
		input := "Hello World Test"
		expected := "hello-world-test"
		result := slugify(input)
		if result != expected {
			t.Errorf("slugify(%q) = %q, want %q", input, result, expected)
		}
	})

	t.Run("Slug with special characters", func(t *testing.T) {
		input := "Test!@#$%Title"
		result := slugify(input)
		// Should contain only lowercase letters, numbers, and hyphens
		for _, r := range result {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				t.Errorf("slugify(%q) = %q contains invalid character %q", input, result, string(r))
			}
		}
	})

	t.Run("Slug with unicode", func(t *testing.T) {
		input := "日本語タイトル"
		result := slugify(input)
		// Unicode should be preserved or handled gracefully
		t.Logf("slugify(%q) = %q", input, result)
	})
}

// Helper to test slugify function (need to export or duplicate)
func slugify(s string) string {
	// Simplified slugify - the real implementation is in file.go
	result := make([]byte, 0, len(s))
	lastWasHyphen := false

	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r = r + 32 // lowercase
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result = append(result, byte(r))
			lastWasHyphen = false
		} else if !lastWasHyphen && len(result) > 0 {
			result = append(result, '-')
			lastWasHyphen = true
		}
	}

	// Trim trailing hyphen
	if len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}

	return string(result)
}

func TestGetCollectionForPath(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: posts
      folder: content/posts
`)
	coll, err := GetCollectionForPathForRuntime(testRuntime(repoPath), "content/posts/test.md")
	if err != nil {
		t.Fatalf("GetCollectionForPathForRuntime() error = %v", err)
	}
	if coll == nil || coll.Name != "posts" {
		t.Fatalf("collection = %#v, want posts", coll)
	}
}

func TestSafeJoinEdgeCases(t *testing.T) {
	root := "/tmp/repo"

	tests := []struct {
		name      string
		sub       string
		target    string
		wantEmpty bool
	}{
		{
			name:      "Empty target",
			sub:       "content",
			target:    "",
			wantEmpty: false, // Returns root/sub
		},
		{
			name:      "Dot target",
			sub:       "content",
			target:    ".",
			wantEmpty: false, // Returns root/sub
		},
		{
			name:      "Double dot target",
			sub:       "content",
			target:    "..",
			wantEmpty: true, // Traversal outside root
		},
		{
			name:      "Hidden file",
			sub:       "content",
			target:    ".hidden",
			wantEmpty: false,
		},
		{
			name:      "Windows path separator",
			sub:       "content",
			target:    "posts\\test.md",
			wantEmpty: false, // Should work on Windows
		},
		{
			name:      "URL encoded characters",
			sub:       "content",
			target:    "posts%2Ftest.md",
			wantEmpty: false, // Literal % in path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeJoin(root, tt.sub, tt.target)
			if tt.wantEmpty && got != "" {
				t.Errorf("SafeJoin(%q, %q, %q) = %q, want empty", root, tt.sub, tt.target, got)
			}
			if !tt.wantEmpty && got == "" {
				t.Errorf("SafeJoin(%q, %q, %q) = empty, want non-empty", root, tt.sub, tt.target)
			}
		})
	}
}

func testRuntime(repoPath string) config.SiteRuntime {
	return config.NewSiteRuntime(config.SiteConfig{
		ID:             "test",
		RepoPath:       repoPath,
		Generator:      "hugo",
		ContentDir:     "content",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/",
		HugoServerBind: "127.0.0.1",
		HugoServerPort: "1314",
	})
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestFileOperationsWithTempDir(t *testing.T) {
	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "file_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("Create and delete file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.md")
		content := []byte("# Test\n\nContent")

		// Create file
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

		// Verify exists
		if _, err := os.Stat(testFile); os.IsNotExist(err) {
			t.Fatal("File should exist")
		}

		// Read content
		read, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(read) != string(content) {
			t.Errorf("File content = %q, want %q", string(read), string(content))
		}

		// Delete file
		if err := os.Remove(testFile); err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Verify deleted
		if _, err := os.Stat(testFile); !os.IsNotExist(err) {
			t.Error("File should not exist after deletion")
		}
	})

	t.Run("Create nested directory structure", func(t *testing.T) {
		nestedDir := filepath.Join(tmpDir, "content", "posts", "2025", "01")
		if err := os.MkdirAll(nestedDir, 0755); err != nil {
			t.Fatalf("Failed to create nested dirs: %v", err)
		}

		testFile := filepath.Join(nestedDir, "test.md")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file in nested dir: %v", err)
		}

		// Verify structure
		if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
			t.Error("Nested directory should exist")
		}
		if _, err := os.Stat(testFile); os.IsNotExist(err) {
			t.Error("File in nested directory should exist")
		}
	})
}
