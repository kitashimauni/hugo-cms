package services

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	root := "/tmp/repo"

	absTarget := "/etc/passwd"
	if runtime.GOOS == "windows" {
		absTarget = "C:\\Windows\\System32\\drivers\\etc\\hosts"
	}

	tests := []struct {
		name     string
		sub      string
		target   string
		wantPath string // Empty string means expect "" (invalid)
	}{
		{
			name:     "Valid Path",
			sub:      "content",
			target:   "posts/hello.md",
			wantPath: filepath.Join(root, "content", "posts", "hello.md"),
		},
		{
			name:     "Simple Traversal",
			sub:      "content",
			target:   "../config.yaml",
			wantPath: "",
		},
		{
			name:     "Deep Traversal",
			sub:      "content",
			target:   "../../etc/passwd",
			wantPath: "",
		},
		{
			name:   "Valid Nested Traversal",
			sub:    "content",
			target: "posts/../about.md",
			// Resolves to content/about.md which is inside content
			wantPath: filepath.Join(root, "content", "about.md"),
		},
		{
			name:     "Absolute Path Injection",
			sub:      "content",
			target:   absTarget,
			wantPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeJoin(root, tt.sub, tt.target)
			// Normalize paths for comparison (Windows/Unix separators)
			// But SafeJoin returns empty on error, so literal comparison is mostly fine except separators

			// If wantPath is empty, we expect empty
			if tt.wantPath == "" {
				if got != "" {
					t.Errorf("SafeJoin() = %q, want empty string", got)
				}
			} else {
				if got != tt.wantPath {
					t.Errorf("SafeJoin() = %q, want %q", got, tt.wantPath)
				}
			}
		})
	}
}

func TestSafeJoinRejectsSymlinkEscape(t *testing.T) {
	baseDir := t.TempDir()
	root := filepath.Join(baseDir, "repo")
	contentDir := filepath.Join(root, "content")
	outsideDir := filepath.Join(baseDir, "outside")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		t.Fatalf("create content directory: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("create outside directory: %v", err)
	}

	linkPath := filepath.Join(contentDir, "linked")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlinks are not available in this environment: %v", err)
	}

	if got := SafeJoin(root, "content", "linked/secret.md"); got != "" {
		t.Fatalf("SafeJoin() followed a symlink outside the root: %q", got)
	}
}
