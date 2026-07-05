package services

import (
	"slices"
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

	got := gitAuthEnvironment(base, "/safe/askpass", "new-token")

	wantPresent := []string{
		"PATH=/usr/bin",
		"GIT_ASKPASS=/safe/askpass",
		"GIT_TOKEN=new-token",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
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
