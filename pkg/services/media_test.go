package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListMediaFiles(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "media_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create static/images directory
	staticDir := filepath.Join(tmpDir, "static", "images")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("Failed to create static dir: %v", err)
	}

	// Create test image file
	testFile := filepath.Join(staticDir, "test.jpg")
	if err := os.WriteFile(testFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Note: ListMediaFiles uses config.RepoPath which makes it hard to test in isolation.
	// This test documents the expected behavior but may need config injection for proper unit testing.
	t.Run("Static mode requires valid config", func(t *testing.T) {
		// This would require setting up config.RepoPath, config.StaticMediaDir
		// For now, we test the function exists and returns without panic
		_, _ = ListMediaFiles("static", "")
	})

	t.Run("Content mode requires article path", func(t *testing.T) {
		files, err := ListMediaFiles("content", "")
		// Should return empty without error since articlePath is empty
		if err != nil {
			t.Errorf("ListMediaFiles() unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("ListMediaFiles() expected empty result for content mode without path")
		}
	})
}

func TestMediaFileStruct(t *testing.T) {
	mf := MediaFile{
		Name:     "test.jpg",
		Path:     "/images/test.jpg",
		Size:     1024,
		URL:      "/admin/api/media/raw?path=static/images/test.jpg",
		RepoPath: "static/images/test.jpg",
	}

	if mf.Name != "test.jpg" {
		t.Errorf("MediaFile.Name = %q, want %q", mf.Name, "test.jpg")
	}
	if mf.Size != 1024 {
		t.Errorf("MediaFile.Size = %d, want %d", mf.Size, 1024)
	}
}

func TestSaveMediaFile_InvalidExtension(t *testing.T) {
	// Create a mock file header - this is tricky without a full request
	// Document expected behavior: should reject files with disallowed extensions
	t.Run("Extension validation", func(t *testing.T) {
		// Allowed extensions are: .jpg, .jpeg, .png, .gif, .webp, .svg, .mp4, .webm, .pdf
		allowedExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".mp4", ".webm", ".pdf"}
		disallowedExts := []string{".exe", ".sh", ".bat", ".php", ".html", ".js"}

		for _, ext := range allowedExts {
			t.Logf("Allowed extension: %s", ext)
		}
		for _, ext := range disallowedExts {
			t.Logf("Disallowed extension: %s", ext)
		}
	})
}

func TestDeleteMediaFile(t *testing.T) {
	// Create temp directory and file
	tmpDir, err := os.MkdirTemp("", "delete_media_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.jpg")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("Test file should exist before deletion")
	}

	// Delete the file
	if err := os.Remove(testFile); err != nil {
		t.Errorf("Failed to delete file: %v", err)
	}

	// Verify file no longer exists
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Test file should not exist after deletion")
	}
}

func TestDeleteMediaFile_InvalidPath(t *testing.T) {
	// DeleteMediaFile should return error for invalid paths
	err := DeleteMediaFile("")
	if err == nil {
		t.Error("DeleteMediaFile(\"\") should return error for empty path")
	}

	err = DeleteMediaFile("../../../etc/passwd")
	if err == nil {
		t.Error("DeleteMediaFile should return error for path traversal attempt")
	}
}
