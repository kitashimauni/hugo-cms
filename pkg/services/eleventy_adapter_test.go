package services

import (
	"hugo-cms/pkg/config"
	"testing"
)

func TestEleventyServerArgsMountPreviewAtConfiguredBase(t *testing.T) {
	runtime := PreviewRuntimeForSite(config.SiteConfig{
		ID:             "docs site",
		RepoPath:       "repo",
		Generator:      "eleventy",
		ContentDir:     "content",
		PublicDir:      "_site",
		HugoServerPort: "1320",
	})

	args := eleventyServerArgs(runtime)
	if got := argValue(args, "--pathprefix"); got != "/admin/preview/docs%20site/" {
		t.Fatalf("eleventyServerArgs pathprefix = %q, want authenticated preview base", got)
	}
	if got := argValue(args, "--port"); got != "1320" {
		t.Fatalf("eleventyServerArgs port = %q, want 1320", got)
	}
	if got := argValue(args, "--input"); got != "content" {
		t.Fatalf("eleventyServerArgs input = %q, want content", got)
	}
	if got := argValue(args, "--output"); got != "_site" {
		t.Fatalf("eleventyServerArgs output = %q, want _site", got)
	}
}
