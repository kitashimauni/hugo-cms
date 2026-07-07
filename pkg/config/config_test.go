package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSecurityConfig(t *testing.T) {
	originalUsers := AllowedGitHubUsers
	originalAllowAll := AllowAllGitHubUsers
	t.Cleanup(func() {
		AllowedGitHubUsers = originalUsers
		AllowAllGitHubUsers = originalAllowAll
	})

	tests := []struct {
		name     string
		users    []string
		allowAll bool
		ginMode  string
		wantErr  bool
	}{
		{
			name:    "rejects an empty allowlist by default",
			wantErr: true,
		},
		{
			name:     "allows an explicit development override",
			allowAll: true,
		},
		{
			name:     "rejects the allow-all override in release mode",
			allowAll: true,
			ginMode:  "release",
			wantErr:  true,
		},
		{
			name:     "rejects allow-all when release mode has whitespace",
			allowAll: true,
			ginMode:  " release ",
			wantErr:  true,
		},
		{
			name:  "allows a configured user",
			users: []string{"octocat"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			AllowedGitHubUsers = tt.users
			AllowAllGitHubUsers = tt.allowAll
			t.Setenv("GIN_MODE", tt.ginMode)

			err := ValidateSecurityConfig()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateSecurityConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsUserAllowed(t *testing.T) {
	originalUsers := AllowedGitHubUsers
	originalAllowAll := AllowAllGitHubUsers
	t.Cleanup(func() {
		AllowedGitHubUsers = originalUsers
		AllowAllGitHubUsers = originalAllowAll
	})

	AllowedGitHubUsers = nil
	AllowAllGitHubUsers = false
	if IsUserAllowed("octocat") {
		t.Fatal("IsUserAllowed() should fail closed when no users are configured")
	}

	AllowAllGitHubUsers = true
	if !IsUserAllowed("octocat") {
		t.Fatal("IsUserAllowed() should honor the explicit allow-all override")
	}

	AllowedGitHubUsers = []string{"OctoCat"}
	AllowAllGitHubUsers = false
	if !IsUserAllowed("octocat") {
		t.Fatal("IsUserAllowed() should compare usernames case-insensitively")
	}
}

func TestLoadSiteRegistryAppliesDefaultSite(t *testing.T) {
	originalSites := Sites
	originalDefaultSiteID := DefaultSiteID
	originalRepoPath := RepoPath
	originalGenerator := SiteGenerator
	originalContentDir := ContentDir
	originalStaticDir := StaticDir
	originalPublicDir := PublicDir
	originalPreviewURL := PreviewURL
	originalPort := HugoServerPort
	originalBind := HugoServerBind
	originalSitesConfigPath := SitesConfigPath
	t.Cleanup(func() {
		Sites = originalSites
		DefaultSiteID = originalDefaultSiteID
		RepoPath = originalRepoPath
		SiteGenerator = originalGenerator
		ContentDir = originalContentDir
		StaticDir = originalStaticDir
		PublicDir = originalPublicDir
		PreviewURL = originalPreviewURL
		HugoServerPort = originalPort
		HugoServerBind = originalBind
		SitesConfigPath = originalSitesConfigPath
	})

	configPath := filepath.Join(t.TempDir(), "sites.yml")
	err := os.WriteFile(configPath, []byte(`
default_site: notes
sites:
  - id: blog
    repo_path: C:/sites/blog
    generator: hugo
  - id: notes
    repo_path: C:/sites/notes
    generator: eleventy
    content_dir: src
    static_dir: public-assets
    public_dir: _site
    preview_url: /preview/
    hugo_server_port: "1320"
`), 0644)
	if err != nil {
		t.Fatalf("write registry config: %v", err)
	}

	DefaultSiteID = "default"
	SitesConfigPath = configPath
	loadSiteRegistry()

	if DefaultSiteID != "notes" {
		t.Fatalf("DefaultSiteID = %q, want notes", DefaultSiteID)
	}
	if RepoPath != "C:/sites/notes" {
		t.Fatalf("RepoPath = %q, want notes path", RepoPath)
	}
	if SiteGenerator != "eleventy" {
		t.Fatalf("SiteGenerator = %q, want eleventy", SiteGenerator)
	}
	if ContentDir != "src" || StaticDir != "public-assets" || PublicDir != "_site" {
		t.Fatalf("content/static/public dirs = %q/%q/%q, want src/public-assets/_site", ContentDir, StaticDir, PublicDir)
	}
	if HugoServerPort != "1320" {
		t.Fatalf("HugoServerPort = %q, want 1320", HugoServerPort)
	}
}

func TestNormalizeSiteConfigRejectsUnsafeDirs(t *testing.T) {
	site := normalizeSiteConfig(SiteConfig{
		ID:         "unsafe",
		ContentDir: "../content",
		StaticDir:  "/tmp/static",
		PublicDir:  ".",
	})

	if site.ContentDir != "content" {
		t.Fatalf("ContentDir = %q, want fallback content", site.ContentDir)
	}
	if site.StaticDir != "static" {
		t.Fatalf("StaticDir = %q, want fallback static", site.StaticDir)
	}
	if site.PublicDir != "public" {
		t.Fatalf("PublicDir = %q, want fallback public", site.PublicDir)
	}
}
