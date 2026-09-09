package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEleventyLocalPreviewProjectOverlayKeepsProjectRelativePaths(t *testing.T) {
	repo := t.TempDir()
	for _, directory := range []string{
		filepath.Join(repo, "src", "posts"),
		filepath.Join(repo, "_includes"),
		filepath.Join(repo, "_data"),
		filepath.Join(repo, "_layouts"),
		filepath.Join(repo, "public"),
	} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "posts", "production.md"), []byte("production"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "_includes", "post.njk"), []byte("include"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "_data", "site.json"), []byte(`{"name":"daily-blog"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "public", "production.html"), []byte("must not be copied"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"devDependencies":{"@11ty/eleventy":"^3.0.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0644); err != nil {
		t.Fatal(err)
	}

	shadow := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shadow, "posts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadow, "posts", "draft.md"), []byte("unsaved"), 0644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	if err := createEleventyLocalPreviewProjectOverlay(repo, project, "src", "public", shadow); err != nil {
		t.Fatalf("createEleventyLocalPreviewProjectOverlay() error = %v", err)
	}

	assertFileContent(t, filepath.Join(project, "src", "posts", "draft.md"), "unsaved")
	if _, err := os.Stat(filepath.Join(project, "src", "posts", "production.md")); !os.IsNotExist(err) {
		t.Fatalf("production content leaked into shadow input: %v", err)
	}
	assertFileContent(t, filepath.Join(project, "_includes", "post.njk"), "include")
	assertFileContent(t, filepath.Join(project, "_data", "site.json"), `{"name":"daily-blog"}`)
	if _, err := os.Stat(filepath.Join(project, "public", "production.html")); !os.IsNotExist(err) {
		t.Fatalf("production public output leaked into preview output: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, "public", "generated"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "public", "generated", "image.txt"), []byte("preview"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "public", "generated", "image.txt")); !os.IsNotExist(err) {
		t.Fatalf("preview output modified production public directory: %v", err)
	}

}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("content at %s = %q, want %q", path, content, want)
	}
}
