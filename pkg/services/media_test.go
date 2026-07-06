package services

import (
	"bytes"
	"hugo-cms/pkg/config"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testFileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest("POST", "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(int64(body.Len())); err != nil {
		t.Fatalf("ParseMultipartForm() error: %v", err)
	}
	return request.MultipartForm.File["file"][0]
}

func withMediaConfig(t *testing.T, repoPath string) {
	t.Helper()

	originalRepoPath := config.RepoPath
	originalStaticDir := config.StaticMediaDir
	originalArticleDir := config.ArticleMediaDir
	originalMaxSize := config.MaxUploadSize
	t.Cleanup(func() {
		config.RepoPath = originalRepoPath
		config.StaticMediaDir = originalStaticDir
		config.ArticleMediaDir = originalArticleDir
		config.MaxUploadSize = originalMaxSize
	})

	config.RepoPath = repoPath
	config.StaticMediaDir = "images"
	config.ArticleMediaDir = "images"
	config.MaxUploadSize = 1024 * 1024
}

func TestListMediaFiles(t *testing.T) {
	repoPath := t.TempDir()
	withMediaConfig(t, repoPath)

	staticDir := filepath.Join(repoPath, "static", "images")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("create static directory: %v", err)
	}

	for _, name := range []string{"photo.jpg", "clip.mp4", "document.pdf", "script.svg"} {
		if err := os.WriteFile(filepath.Join(staticDir, name), []byte("test"), 0644); err != nil {
			t.Fatalf("create test media %q: %v", name, err)
		}
	}

	files, err := ListMediaFiles("static", "")
	if err != nil {
		t.Fatalf("ListMediaFiles() unexpected error: %v", err)
	}
	if len(files) != 1 || files[0].Name != "photo.jpg" {
		t.Fatalf("ListMediaFiles() = %#v, want only photo.jpg", files)
	}

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
		// SVG is intentionally rejected because it can contain executable content.
		allowedExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp4", ".webm", ".pdf"}
		disallowedExts := []string{".svg", ".exe", ".sh", ".bat", ".php", ".html", ".js"}

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

func TestSaveMediaFileRejectsArticlePathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	repoPath := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoPath, "content"), 0755); err != nil {
		t.Fatalf("create content directory: %v", err)
	}
	withMediaConfig(t, repoPath)

	header := testFileHeader(t, "image.png", []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	})
	if _, err := SaveMediaFile(header, "content", "../../outside/index.md"); err == nil {
		t.Fatal("SaveMediaFile() should reject an article path outside content")
	}

	if _, err := os.Stat(filepath.Join(baseDir, "outside")); !os.IsNotExist(err) {
		t.Fatalf("outside directory should not be created, stat error = %v", err)
	}
}

func TestSaveMediaFileRejectsMismatchedContent(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoPath, "static", "images"), 0755); err != nil {
		t.Fatalf("create static directory: %v", err)
	}
	withMediaConfig(t, repoPath)

	header := testFileHeader(t, "not-an-image.jpg", []byte("%PDF-1.7\n"))
	if _, err := SaveMediaFile(header, "static", ""); err == nil {
		t.Fatal("SaveMediaFile() should reject content that does not match its extension")
	}
}

func TestSaveMediaFileWritesValidatedImage(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoPath, "static", "images"), 0755); err != nil {
		t.Fatalf("create static directory: %v", err)
	}
	withMediaConfig(t, repoPath)

	header := testFileHeader(t, "image.png", []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	})
	media, err := SaveMediaFile(header, "static", "")
	if err != nil {
		t.Fatalf("SaveMediaFile() unexpected error: %v", err)
	}
	if !ValidateMediaRepoPath(media.RepoPath) {
		t.Fatalf("SaveMediaFile() returned invalid repository path %q", media.RepoPath)
	}
	if _, err := os.Stat(filepath.Join(repoPath, filepath.FromSlash(media.RepoPath))); err != nil {
		t.Fatalf("saved media file not found: %v", err)
	}
}

func TestValidateMediaRepoPath(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoPath, "static", "images"), 0755); err != nil {
		t.Fatalf("create static directory: %v", err)
	}
	withMediaConfig(t, repoPath)

	tests := []struct {
		path string
		want bool
	}{
		{path: "static/images/photo.jpg", want: true},
		{path: "static/images/clip.mp4", want: true},
		{path: "static/images/document.pdf", want: true},
		{path: "content/posts/photo.png", want: true},
		{path: ".git/config", want: false},
		{path: "static/../.git/config.jpg", want: false},
		{path: "static/images/script.svg", want: false},
		{path: "../../outside/photo.jpg", want: false},
	}

	for _, tt := range tests {
		if got := ValidateMediaRepoPath(tt.path); got != tt.want {
			t.Errorf("ValidateMediaRepoPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
