package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMarkdownPreviewSupportsGFMAndEscapesUnsafeHTML(t *testing.T) {
	runtime := testRuntime(t.TempDir())
	body := "# Heading\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\n<script>alert(1)</script>\n\n[bad](javascript:alert(1))"

	got, err := RenderMarkdownPreview(runtime, "posts/example.md", body, map[string]interface{}{
		"title": "<unsafe>",
		"draft": false,
	})
	if err != nil {
		t.Fatalf("RenderMarkdownPreview() error = %v", err)
	}
	for _, expected := range []string{"<h1>Heading</h1>", "<table>", "&lt;unsafe&gt;", "<dd>false</dd>"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("RenderMarkdownPreview() = %q, want %q", got, expected)
		}
	}
	for _, unsafe := range []string{"<script", "javascript:"} {
		if strings.Contains(strings.ToLower(got), unsafe) {
			t.Fatalf("RenderMarkdownPreview() retained unsafe content %q: %s", unsafe, got)
		}
	}
}

func TestRenderMarkdownPreviewRewritesExistingRelativeImages(t *testing.T) {
	repo := t.TempDir()
	runtime := testRuntime(repo)
	runtime.ID = "docs site"
	imagePath := filepath.Join(repo, runtime.ContentDir, "posts", "bundle", "image.png")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := RenderMarkdownPreview(runtime, "posts/bundle/index.md", "![alt](image.png)", nil)
	if err != nil {
		t.Fatalf("RenderMarkdownPreview() error = %v", err)
	}
	if !strings.Contains(got, "/admin/api/media/raw?path=content%2Fposts%2Fbundle%2Fimage.png&amp;site=docs+site") {
		t.Fatalf("RenderMarkdownPreview() image URL = %q", got)
	}
}

func TestRenderMarkdownPreviewRejectsLargeDocuments(t *testing.T) {
	_, err := RenderMarkdownPreview(testRuntime(t.TempDir()), "index.md", strings.Repeat("a", MaxMarkdownPreviewSize+1), nil)
	if !errors.Is(err, ErrMarkdownPreviewTooLarge) {
		t.Fatalf("RenderMarkdownPreview() error = %v, want ErrMarkdownPreviewTooLarge", err)
	}
}
