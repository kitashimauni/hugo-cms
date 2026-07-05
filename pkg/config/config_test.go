package config

import "testing"

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
