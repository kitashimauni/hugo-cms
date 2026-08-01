package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAuthenticatedGitHubURL(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		want    string
		wantErr bool
	}{
		{
			name:   "HTTPS GitHub remote",
			remote: "https://github.com/example/site.git",
			want:   "https://oauth2@github.com/example/site.git",
		},
		{
			name:    "SSH URL",
			remote:  "ssh://git@github.com/example/site.git",
			wantErr: true,
		},
		{
			name:    "scp-like SSH URL",
			remote:  "git@github.com:example/site.git",
			wantErr: true,
		},
		{
			name:    "custom SSH alias",
			remote:  "github:example/site.git",
			wantErr: true,
		},
		{
			name:    "non-GitHub host",
			remote:  "https://example.com/example/site.git",
			wantErr: true,
		},
		{
			name:    "embedded credentials",
			remote:  "https://user:password@github.com/example/site.git",
			wantErr: true,
		},
		{
			name:    "missing repository path",
			remote:  "https://github.com/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := authenticatedGitHubURL(tt.remote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("authenticatedGitHubURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("authenticatedGitHubURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGitAuthEnvironmentRemovesInheritedGitAuthentication(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"GIT_ASKPASS=attacker",
		"git_token=old-token",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=malicious-helper",
		"GIT_CONFIG_KEY_1=http.extraHeader",
		"GIT_CONFIG_VALUE_1=Authorization: secret",
	}

	authenticatedURL := "https://oauth2@github.com/example/site.git"
	got := gitAuthEnvironment(base, "/safe/askpass", "new-token", authenticatedURL)

	wantPresent := []string{
		"PATH=/usr/bin",
		"GIT_ASKPASS=/safe/askpass",
		"GIT_TOKEN=new-token",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=url." + authenticatedURL + ".insteadOf",
		"GIT_CONFIG_VALUE_1=" + authenticatedURL,
	}
	for _, entry := range wantPresent {
		if !slices.Contains(got, entry) {
			t.Errorf("gitAuthEnvironment() missing %q: %v", entry, got)
		}
	}
	for _, entry := range got {
		if entry == "GIT_ASKPASS=attacker" ||
			entry == "git_token=old-token" ||
			entry == "GIT_CONFIG_VALUE_0=malicious-helper" ||
			entry == "GIT_CONFIG_KEY_1=http.extraHeader" ||
			entry == "GIT_CONFIG_VALUE_1=Authorization: secret" {
			t.Errorf("gitAuthEnvironment() preserved inherited authentication setting %q", entry)
		}
	}
}

func TestReadRawRemoteURLIgnoresInsteadOfRewrite(t *testing.T) {
	repoPath := t.TempDir()
	runGitForTest(t, repoPath, "init")
	runGitForTest(t, repoPath, "remote", "add", "origin", "https://github.com/example/site.git")

	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.git@github.com:.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/")

	got, err := readRawRemoteURL(context.Background(), repoPath, "origin")
	if err != nil {
		t.Fatalf("readRawRemoteURL() unexpected error: %v", err)
	}
	if got != "https://github.com/example/site.git" {
		t.Fatalf("readRawRemoteURL() = %q, want raw configured URL", got)
	}
}

func TestGitAuthEnvironmentPreventsURLRewrite(t *testing.T) {
	authenticatedURL := "https://oauth2@github.com/example/site.git"
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	configContent := "[url \"ssh://attacker.invalid/\"]\n\tinsteadOf = https://\n"
	if err := os.WriteFile(globalConfig, []byte(configContent), 0600); err != nil {
		t.Fatalf("write global Git config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

	cmd := exec.Command("git", "ls-remote", "--get-url", authenticatedURL)
	cmd.Env = gitAuthEnvironment(os.Environ(), filepath.Join(t.TempDir(), "askpass"), "token", authenticatedURL)

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git URL expansion failed: %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != authenticatedURL {
		t.Fatalf("expanded URL = %q, want %q", got, authenticatedURL)
	}
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
