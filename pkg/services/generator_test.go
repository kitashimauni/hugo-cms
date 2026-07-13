package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testGeneratorAdapter struct {
	start     func() error
	stop      func() error
	isRunning func() bool
}

func (adapter testGeneratorAdapter) Name() string { return "test" }

func (adapter testGeneratorAdapter) StartPreview(_ config.SiteRuntime) error {
	if adapter.start != nil {
		return adapter.start()
	}
	return nil
}

func (adapter testGeneratorAdapter) StopPreview() error {
	if adapter.stop != nil {
		return adapter.stop()
	}
	return nil
}

func (adapter testGeneratorAdapter) IsPreviewRunning() bool {
	if adapter.isRunning != nil {
		return adapter.isRunning()
	}
	return true
}

func (adapter testGeneratorAdapter) Build(_ config.SiteRuntime) (string, error) { return "", nil }

func (adapter testGeneratorAdapter) CreateContent(_ config.SiteRuntime, _ string) (string, error) {
	return "", nil
}

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

func TestPreviewRuntimeSiteUsesSitePreviewProxyBase(t *testing.T) {
	site := previewRuntimeSite(config.SiteConfig{
		ID:         "docs site",
		PreviewURL: "/",
	})

	if site.PreviewURL != "/admin/preview/docs%20site/" {
		t.Fatalf("PreviewURL = %q, want site preview proxy base", site.PreviewURL)
	}
}

func TestGeneratorCommandSpecUsesMiseRuntime(t *testing.T) {
	runtime := config.SiteRuntime{
		RepoPath: "/data/repos/site",
		Runtime:  "mise",
	}

	name, args := generatorCommandSpec(runtime, "hugo", "version")
	if name != "mise" {
		t.Fatalf("command name = %q, want mise", name)
	}
	wantArgs := []string{"exec", "-C", ".", "--", "hugo", "version"}
	if len(args) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
	for i := range args {
		if args[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", args, wantArgs)
		}
	}
}

func TestGeneratorCommandSpecUsesDirectRuntimeByDefault(t *testing.T) {
	name, args := generatorCommandSpec(config.SiteRuntime{RepoPath: "/data/repos/site"}, "hugo", "version")
	if name != "hugo" {
		t.Fatalf("command name = %q, want hugo", name)
	}
	if len(args) != 1 || args[0] != "version" {
		t.Fatalf("args = %#v, want version", args)
	}
}

func TestGeneratorCommandKeepsRepoPathAsOnlyWorkingDirectory(t *testing.T) {
	repoPath := filepath.Join("testdata", "site")
	tests := []struct {
		name    string
		runtime string
		command string
		args    []string
		want    []string
	}{
		{name: "direct Hugo", runtime: "direct", command: "hugo", args: []string{"version"}, want: []string{"hugo", "version"}},
		{name: "mise Hugo", runtime: "mise", command: "hugo", args: []string{"version"}, want: []string{"mise", "exec", "-C", ".", "--", "hugo", "version"}},
		{name: "direct Eleventy package manager", runtime: "direct", command: "npm", args: []string{"exec", "--", "eleventy"}, want: []string{"npm", "exec", "--", "eleventy"}},
		{name: "mise Eleventy package manager", runtime: "mise", command: "npm", args: []string{"exec", "--", "eleventy"}, want: []string{"mise", "exec", "-C", ".", "--", "npm", "exec", "--", "eleventy"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := config.SiteRuntime{RepoPath: repoPath, Runtime: tt.runtime}
			cmd := generatorCommand(runtime, tt.command, tt.args...)
			if cmd.Dir != repoPath {
				t.Fatalf("command dir = %q, want %q", cmd.Dir, repoPath)
			}
			if len(cmd.Args) != len(tt.want) {
				t.Fatalf("command args = %#v, want %#v", cmd.Args, tt.want)
			}
			for i := range tt.want {
				if cmd.Args[i] != tt.want[i] {
					t.Fatalf("command args = %#v, want %#v", cmd.Args, tt.want)
				}
			}
		})
	}
}

func TestStartPreviewForSiteDoesNotHoldAdapterMapLockWhileStarting(t *testing.T) {
	originalPreviewAdapters := previewAdapters
	originalRepoPath := config.RepoPath
	originalContentDir := config.ContentDir
	originalStaticDir := config.StaticDir
	originalPublicDir := config.PublicDir
	originalPreviewURL := config.PreviewURL
	originalPort := config.HugoServerPort
	originalBind := config.HugoServerBind
	t.Cleanup(func() {
		previewAdaptersMu.Lock()
		previewAdapters = originalPreviewAdapters
		previewAdaptersMu.Unlock()

		config.RepoPath = originalRepoPath
		config.ContentDir = originalContentDir
		config.StaticDir = originalStaticDir
		config.PublicDir = originalPublicDir
		config.PreviewURL = originalPreviewURL
		config.HugoServerPort = originalPort
		config.HugoServerBind = originalBind
	})

	site := config.SiteConfig{
		ID:             "lock-test",
		RepoPath:       t.TempDir(),
		Generator:      "hugo",
		ContentDir:     "content",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/",
		HugoServerBind: "127.0.0.1",
		HugoServerPort: "1314",
	}

	previewAdaptersMu.Lock()
	previewAdapters = map[string]GeneratorAdapter{
		sitePreviewKey(config.NewSiteRuntime(site)): testGeneratorAdapter{
			start: func() error {
				if !IsPreviewRunningForSite(site) {
					return fmt.Errorf("IsPreviewRunningForSite() = false, want true")
				}
				return nil
			},
			isRunning: func() bool { return true },
		},
	}
	previewAdaptersMu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- StartPreviewForSite(site)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StartPreviewForSite() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartPreviewForSite() deadlocked while starting preview")
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

func TestValidateEleventyProjectRequiresDeclaredDependency(t *testing.T) {
	repoPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoPath, "package.json"), []byte(`{"devDependencies":{}}`), 0644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}

	if err := validateEleventyProject(repoPath); err == nil {
		t.Fatal("validateEleventyProject() should require @11ty/eleventy dependency")
	}
}

func TestDetectEleventyPackageManagerMatchesLockfile(t *testing.T) {
	tests := []struct {
		name     string
		lockfile string
		wantName string
		wantBin  string
		wantArgs []string
	}{
		{
			name:     "npm package lock",
			lockfile: "package-lock.json",
			wantName: "npm",
			wantBin:  "npm",
			wantArgs: []string{"exec", "--", "eleventy"},
		},
		{
			name:     "pnpm lock",
			lockfile: "pnpm-lock.yaml",
			wantName: "pnpm",
			wantBin:  "pnpm",
			wantArgs: []string{"exec", "eleventy"},
		},
		{
			name:     "yarn lock",
			lockfile: "yarn.lock",
			wantName: "yarn",
			wantBin:  "yarn",
			wantArgs: []string{"exec", "eleventy"},
		},
		{
			name:     "bun text lock",
			lockfile: "bun.lock",
			wantName: "bun",
			wantBin:  "bunx",
			wantArgs: []string{"eleventy"},
		},
		{
			name:     "bun binary lock",
			lockfile: "bun.lockb",
			wantName: "bun",
			wantBin:  "bunx",
			wantArgs: []string{"eleventy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath := t.TempDir()
			if err := os.WriteFile(filepath.Join(repoPath, "package.json"), []byte(`{"devDependencies":{"@11ty/eleventy":"^3.0.0"}}`), 0644); err != nil {
				t.Fatalf("write package.json: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repoPath, tt.lockfile), []byte(`lock`), 0644); err != nil {
				t.Fatalf("write lock file: %v", err)
			}

			pm, err := detectEleventyPackageManager(repoPath)
			if err != nil {
				t.Fatalf("detectEleventyPackageManager() error = %v", err)
			}
			if pm.Name != tt.wantName || pm.Bin != tt.wantBin {
				t.Fatalf("package manager = %#v, want name %q bin %q", pm, tt.wantName, tt.wantBin)
			}
			if len(pm.Args) != len(tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", pm.Args, tt.wantArgs)
			}
			for i := range pm.Args {
				if pm.Args[i] != tt.wantArgs[i] {
					t.Fatalf("args = %#v, want %#v", pm.Args, tt.wantArgs)
				}
			}
		})
	}
}

func TestGeneratorProcessEnvironmentDropsSecrets(t *testing.T) {
	t.Setenv("SESSION_SECRET", "super-secret")
	t.Setenv("GITHUB_CLIENT_SECRET", "oauth-secret")
	t.Setenv("PATH", "test-path")
	t.Setenv("MISE_TRUSTED_CONFIG_PATHS", "/data/repos/techblog")

	env := generatorProcessEnvironment()
	foundTrustedPaths := false
	for _, entry := range env {
		if entry == "SESSION_SECRET=super-secret" || entry == "GITHUB_CLIENT_SECRET=oauth-secret" {
			t.Fatalf("generatorProcessEnvironment leaked secret entry %q", entry)
		}
		if entry == "MISE_TRUSTED_CONFIG_PATHS=/data/repos/techblog" {
			foundTrustedPaths = true
		}
	}
	if !foundTrustedPaths {
		t.Fatal("generatorProcessEnvironment omitted MISE_TRUSTED_CONFIG_PATHS")
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

	log, err := NewEleventyAdapter().CreateContent(config.CurrentSiteRuntime(), "posts/new.md")
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
