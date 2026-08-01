package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSitePreviewConfigNormalizesAndReachesRuntime(t *testing.T) {
	var site SiteConfig
	if err := yaml.Unmarshal([]byte(`
id: docs
repo_path: repo
preview:
  markdown:
    enabled: false
  deployment:
    provider: CLOUDFLARE_PAGES
    access_protected: true
    cloudflare_pages:
      account_id: " account-id "
      project_name: " docs-site "
      token_env: " DOCS_CF_TOKEN "
`), &site); err != nil {
		t.Fatalf("unmarshal site config: %v", err)
	}
	site = normalizeSiteConfig(site)
	if err := validateSitePreviewConfig(site); err != nil {
		t.Fatalf("validateSitePreviewConfig() error = %v", err)
	}
	runtime := NewSiteRuntime(site)
	if runtime.MarkdownPreviewEnabled {
		t.Fatal("explicit markdown preview enabled=false was not preserved")
	}
	deployment := runtime.PreviewDeployment
	if deployment.Provider != "cloudflare_pages" || deployment.CloudflarePages.AccountID != "account-id" || deployment.CloudflarePages.ProjectName != "docs-site" {
		t.Fatalf("deployment config = %#v", deployment)
	}
	if deployment.CloudflarePages.APITokenEnv != "DOCS_CF_TOKEN" {
		t.Fatalf("token env = %q", deployment.CloudflarePages.APITokenEnv)
	}
	if !deployment.AccessProtected {
		t.Fatal("access_protected was not preserved")
	}
}

func TestSitePreviewConfigDefaultsMarkdownAndTokenEnvironment(t *testing.T) {
	site := normalizeSiteConfig(SiteConfig{
		ID: "docs",
		Preview: SitePreviewConfig{Deployment: DeploymentPreviewConfig{
			Provider: "cloudflare_pages",
			CloudflarePages: CloudflarePagesConfig{
				AccountID:   "account-id",
				ProjectName: "docs-site",
			},
		}},
	})
	if site.Preview.Markdown.Enabled == nil || !*site.Preview.Markdown.Enabled {
		t.Fatal("markdown preview should be enabled when omitted")
	}
	if site.Preview.Deployment.CloudflarePages.APITokenEnv != "CLOUDFLARE_API_TOKEN" {
		t.Fatalf("token env = %q", site.Preview.Deployment.CloudflarePages.APITokenEnv)
	}
}

func TestSitePreviewConfigRejectsIncompleteOrUnknownProvider(t *testing.T) {
	tests := []SiteConfig{
		{Preview: SitePreviewConfig{Deployment: DeploymentPreviewConfig{Provider: "unknown"}}},
		{Preview: SitePreviewConfig{Deployment: DeploymentPreviewConfig{Provider: "cloudflare_pages"}}},
	}
	for _, site := range tests {
		site = normalizeSiteConfig(site)
		if err := validateSitePreviewConfig(site); err == nil {
			t.Fatalf("validateSitePreviewConfig(%#v) should fail", site.Preview.Deployment)
		}
	}
}

func TestCloudflareTokenEnvironmentNameIsNotExposedAsJSON(t *testing.T) {
	site := SiteConfig{Preview: SitePreviewConfig{Deployment: DeploymentPreviewConfig{
		Provider: "cloudflare_pages",
		CloudflarePages: CloudflarePagesConfig{
			AccountID:   "account-id",
			ProjectName: "docs-site",
			APITokenEnv: "SECRET_TOKEN_ENV",
		},
	}}}
	content, err := json.Marshal(site)
	if err != nil {
		t.Fatalf("marshal site config: %v", err)
	}
	if strings.Contains(string(content), "SECRET_TOKEN_ENV") {
		t.Fatalf("site config JSON exposed token environment name: %s", content)
	}
}
