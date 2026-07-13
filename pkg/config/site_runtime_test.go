package config

import (
	"path/filepath"
	"testing"
)

func TestNewSiteRuntimeCopiesSiteConfigAndGlobalProcessSettings(t *testing.T) {
	originalGitRemote := GitRemote
	originalGitBranch := GitBranch
	originalGitUserName := GitUserName
	originalGitUserEmail := GitUserEmail
	t.Cleanup(func() {
		GitRemote = originalGitRemote
		GitBranch = originalGitBranch
		GitUserName = originalGitUserName
		GitUserEmail = originalGitUserEmail
	})

	GitRemote = "upstream"
	GitBranch = "publish"
	GitUserName = "CMS Bot"
	GitUserEmail = "cms@example.com"

	site := SiteConfig{
		ID:             "docs",
		Name:           "Docs",
		RepoPath:       filepath.Join("C:", "sites", "docs"),
		Generator:      "hugo",
		Runtime:        "mise",
		ContentDir:     "content",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/preview/",
		HugoServerBind: "127.0.0.1",
		HugoServerPort: "1314",
		SnippetPaths:   []string{"one.code-snippets"},
	}

	runtime := NewSiteRuntime(site)
	if runtime.ID != site.ID || runtime.RepoPath != site.RepoPath || runtime.ContentDir != site.ContentDir || runtime.Runtime != site.Runtime {
		t.Fatalf("runtime = %#v, want site fields copied", runtime)
	}
	if runtime.PublicPath != filepath.Join(site.RepoPath, site.PublicDir) {
		t.Fatalf("PublicPath = %q", runtime.PublicPath)
	}
	if runtime.GitRemote != "upstream" || runtime.GitBranch != "publish" {
		t.Fatalf("git settings = %q/%q", runtime.GitRemote, runtime.GitBranch)
	}

	site.SnippetPaths[0] = "mutated"
	if runtime.SnippetPaths[0] != "one.code-snippets" {
		t.Fatalf("runtime snippets were not copied: %#v", runtime.SnippetPaths)
	}
}

func TestSiteRuntimeSiteConfigCopiesSlices(t *testing.T) {
	runtime := SiteRuntime{
		ID:           "docs",
		SnippetPaths: []string{"one.code-snippets"},
	}

	site := runtime.SiteConfig()
	site.SnippetPaths[0] = "mutated"
	if runtime.SnippetPaths[0] != "one.code-snippets" {
		t.Fatalf("runtime snippets were mutated through SiteConfig(): %#v", runtime.SnippetPaths)
	}
}
