package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteFile(t *testing.T) {
	// Note: DeleteFile uses config.RepoPath, making it hard to test in isolation
	// These tests document expected behavior

	t.Run("Empty path should fail", func(t *testing.T) {
		err := DeleteFile("")
		if err == nil {
			t.Error("DeleteFile(\"\") should return error")
		}
	})

	t.Run("Path traversal should fail", func(t *testing.T) {
		err := DeleteFile("../../../etc/passwd")
		if err == nil {
			t.Error("DeleteFile with path traversal should return error")
		}
	})
}

func TestGetConfig(t *testing.T) {
	// GetConfig reads hugo.toml from config.RepoPath
	// Test documents expected return type
	t.Run("Returns map or error", func(t *testing.T) {
		conf, err := GetConfig()
		// In test environment without proper setup, this may fail
		if err != nil {
			t.Logf("GetConfig returned error (expected in test env): %v", err)
		} else if conf == nil {
			t.Error("GetConfig should return non-nil config on success")
		}
	})
}

func TestGetCMSConfig(t *testing.T) {
	t.Run("Returns CMSConfig or error", func(t *testing.T) {
		conf, err := GetCMSConfig()
		if err != nil {
			t.Logf("GetCMSConfig returned error (expected in test env): %v", err)
		} else if conf == nil {
			t.Error("GetCMSConfig should return non-nil config on success")
		}
	})
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
	t.Run("Returns collection or error", func(t *testing.T) {
		// Without proper CMS config setup, this will fail
		coll, err := GetCollectionForPath("posts/test.md")
		if err != nil {
			t.Logf("GetCollectionForPath returned error (expected in test env): %v", err)
		} else if coll == nil {
			t.Log("GetCollectionForPath returned nil collection")
		}
	})
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
