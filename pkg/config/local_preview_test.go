package config

import (
	"strings"
	"testing"
)

func preserveLocalPreviewGlobals(t *testing.T) {
	t.Helper()
	originalEnabled := LocalLivePreviewEnabled
	originalDomain := PreviewDomain
	originalScheme := PreviewScheme
	originalSites := Sites
	t.Cleanup(func() {
		LocalLivePreviewEnabled = originalEnabled
		PreviewDomain = originalDomain
		PreviewScheme = originalScheme
		Sites = originalSites
	})
}

func TestLocalPreviewURL(t *testing.T) {
	preserveLocalPreviewGlobals(t)
	PreviewDomain = "preview.example.com"
	PreviewScheme = "https"

	got, err := LocalPreviewURL("tech")
	if err != nil {
		t.Fatalf("LocalPreviewURL() error = %v", err)
	}
	if got != "https://tech.preview.example.com/" {
		t.Fatalf("LocalPreviewURL() = %q", got)
	}
}

func TestLocalPreviewURLRejectsInvalidSiteID(t *testing.T) {
	preserveLocalPreviewGlobals(t)
	PreviewDomain = "preview.example.com"
	PreviewScheme = "https"

	for _, siteID := range []string{"Tech", "foo.bar", "-tech", "tech-", "tech_blog"} {
		t.Run(siteID, func(t *testing.T) {
			if _, err := LocalPreviewURL(siteID); err == nil {
				t.Fatalf("LocalPreviewURL(%q) should fail", siteID)
			}
		})
	}
}

func TestResolveLocalPreviewHost(t *testing.T) {
	preserveLocalPreviewGlobals(t)
	PreviewDomain = "preview.example.com"
	PreviewScheme = "https"
	enabled := true
	Sites = []SiteConfig{
		{ID: "tech", Preview: SitePreviewConfig{LocalPreview: LocalPreviewConfig{Enabled: &enabled}}},
	}

	for _, host := range []string{
		"tech.preview.example.com",
		"TECH.PREVIEW.EXAMPLE.COM",
		"tech.preview.example.com:443",
		"tech.preview.example.com.",
	} {
		t.Run(host, func(t *testing.T) {
			site, err := ResolveLocalPreviewHost(host)
			if err != nil {
				t.Fatalf("ResolveLocalPreviewHost(%q) error = %v", host, err)
			}
			if site.ID != "tech" {
				t.Fatalf("site.ID = %q, want tech", site.ID)
			}
		})
	}
}

func TestResolveLocalPreviewHostRejectsUntrustedHosts(t *testing.T) {
	preserveLocalPreviewGlobals(t)
	PreviewDomain = "preview.example.com"
	PreviewScheme = "https"
	enabled := true
	Sites = []SiteConfig{
		{ID: "tech", Preview: SitePreviewConfig{LocalPreview: LocalPreviewConfig{Enabled: &enabled}}},
	}

	for _, host := range []string{
		"preview.example.com",
		"foo.tech.preview.example.com",
		"tech.preview.example.com.evil.example",
		"unknown.preview.example.com",
		"tech.example.com",
		"tech.preview.example.com/path",
		"tech.preview.example.com@evil.example",
	} {
		t.Run(host, func(t *testing.T) {
			if _, err := ResolveLocalPreviewHost(host); err == nil {
				t.Fatalf("ResolveLocalPreviewHost(%q) should fail", host)
			}
		})
	}
}

func TestResolveLocalPreviewHostRejectsDisabledSite(t *testing.T) {
	preserveLocalPreviewGlobals(t)
	PreviewDomain = "preview.example.com"
	PreviewScheme = "https"
	disabled := false
	Sites = []SiteConfig{
		{ID: "tech", Preview: SitePreviewConfig{LocalPreview: LocalPreviewConfig{Enabled: &disabled}}},
	}

	_, err := ResolveLocalPreviewHost("tech.preview.example.com")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("ResolveLocalPreviewHost() error = %v, want disabled error", err)
	}
}

func TestNormalizeSiteConfigInheritsLocalPreviewDefault(t *testing.T) {
	preserveLocalPreviewGlobals(t)
	LocalLivePreviewEnabled = true
	PreviewDomain = "preview.example.com"
	PreviewScheme = "https"

	site := normalizeSiteConfig(SiteConfig{ID: "tech"})
	if site.Preview.LocalPreview.Enabled == nil || !*site.Preview.LocalPreview.Enabled {
		t.Fatal("local preview should inherit enabled global default")
	}
	if site.Preview.LocalPreview.URL != "https://tech.preview.example.com/" {
		t.Fatalf("local preview URL = %q", site.Preview.LocalPreview.URL)
	}
}

func TestValidateLocalPreviewBaseSettings(t *testing.T) {
	preserveLocalPreviewGlobals(t)

	tests := []struct {
		name   string
		domain string
		scheme string
		wantOK bool
	}{
		{name: "https domain", domain: "preview.example.com", scheme: "https", wantOK: true},
		{name: "http domain", domain: "preview.example.com", scheme: "http", wantOK: true},
		{name: "missing domain", domain: "", scheme: "https"},
		{name: "wildcard domain", domain: "*.preview.example.com", scheme: "https"},
		{name: "domain with scheme", domain: "https://preview.example.com", scheme: "https"},
		{name: "domain with port", domain: "preview.example.com:443", scheme: "https"},
		{name: "invalid scheme", domain: "preview.example.com", scheme: "ftp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			PreviewDomain = normalizePreviewDomain(tt.domain)
			PreviewScheme = normalizePreviewScheme(tt.scheme)
			err := validateLocalPreviewBaseSettings(true)
			if (err == nil) != tt.wantOK {
				t.Fatalf("validateLocalPreviewBaseSettings() error = %v, wantOK %v", err, tt.wantOK)
			}
		})
	}
}
