package services

import (
	"context"
	"hugo-cms/pkg/config"
	"reflect"
	"testing"
)

func TestNewPreviewURLResolverSupportsHugoOnly(t *testing.T) {
	resolver, err := NewPreviewURLResolver("hugo")
	if err != nil {
		t.Fatalf("NewPreviewURLResolver(hugo) error = %v", err)
	}
	if resolver == nil {
		t.Fatal("NewPreviewURLResolver(hugo) returned nil")
	}
	if _, err := NewPreviewURLResolver("eleventy"); err == nil {
		t.Fatal("NewPreviewURLResolver(eleventy) should require a dedicated resolver")
	}
}

func TestHugoPreviewURLResolverUsesShadowContentAndLocalOrigin(t *testing.T) {
	runtime := config.SiteRuntime{
		ID:           "tech",
		RepoPath:     "/repo",
		ContentDir:   "/tmp/shadow/tech/content",
		Generator:    "hugo",
		LocalPreview: config.LocalPreviewConfig{URL: "https://tech.preview.example.com/preview/"},
	}
	workspace := LocalPreviewWorkspace{
		SiteID:      "tech",
		DraftID:     "draft-1",
		ArticlePath: "posts/one.md",
		ContentDir:  runtime.ContentDir,
		Revision:    9,
	}
	var gotRuntime config.SiteRuntime
	var gotPreviewURL string
	resolver := &hugoPreviewURLResolver{run: func(_ context.Context, got config.SiteRuntime, previewURL string) ([]byte, error) {
		gotRuntime = got
		gotPreviewURL = previewURL
		return []byte("path,slug,title,date,expiryDate,publishDate,draft,permalink\ncontent/posts/one.md,custom,\"One, article\",,,,,https://production.example.com/preview/custom/\n"), nil
	}}

	got, err := resolver.ResolveArticleURL(context.Background(), runtime, workspace, "posts/one.md")
	if err != nil {
		t.Fatalf("ResolveArticleURL() error = %v", err)
	}
	if got != "https://tech.preview.example.com/preview/custom/" {
		t.Fatalf("resolved URL = %q", got)
	}
	if gotRuntime.ContentDir != workspace.ContentDir {
		t.Fatalf("resolver content directory = %q, want %q", gotRuntime.ContentDir, workspace.ContentDir)
	}
	if gotPreviewURL != runtime.LocalPreview.URL {
		t.Fatalf("resolver preview URL = %q, want %q", gotPreviewURL, runtime.LocalPreview.URL)
	}
}

func TestHugoListAllArgsUseShadowContentAndNoBuildLock(t *testing.T) {
	want := []string{
		"list", "all", "--source", ".", "--environment", localPreviewHugoEnvironment,
		"--noBuildLock",
	}
	if got := hugoListAllArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("hugoListAllArgs() = %#v, want %#v", got, want)
	}
	runtime := config.SiteRuntime{ContentDir: "/tmp/shadow/content"}
	wantEnvironment := []string{
		"HUGO_CONTENTDIR=/tmp/shadow/content",
		"HUGO_BASEURL=https://tech.preview.example.com/",
	}
	if got := hugoListAllEnvironment(runtime, "https://tech.preview.example.com/"); !reflect.DeepEqual(got, wantEnvironment) {
		t.Fatalf("hugoListAllEnvironment() = %#v, want %#v", got, wantEnvironment)
	}
}

func TestParseHugoListAllMatchesContentPathAndHandlesCSV(t *testing.T) {
	runtime := config.SiteRuntime{ContentDir: "/tmp/shadow/content"}
	output := []byte("\ufeffpath,slug,title,date,expiryDate,publishDate,draft,permalink\r\n" +
		"content/posts/one.md,custom,\"One, article\",,,,,https://example.com/custom/\r\n")
	permalink, err := parseHugoListAll(output, runtime, "posts/one.md")
	if err != nil {
		t.Fatalf("parseHugoListAll() error = %v", err)
	}
	if permalink != "https://example.com/custom/" {
		t.Fatalf("permalink = %q", permalink)
	}
}

func TestParseHugoListAllMatchesRelativeAndAbsoluteContentPaths(t *testing.T) {
	runtime := config.SiteRuntime{ContentDir: `C:\preview\tech\content`}
	for _, listedPath := range []string{
		"posts/one.md",
		"content/posts/one.md",
		`C:\preview\tech\content\posts\one.md`,
	} {
		output := []byte("path,url\n" + listedPath + ",/posts/one/\n")
		got, err := parseHugoListAll(output, runtime, "posts/one.md")
		if err != nil {
			t.Fatalf("parseHugoListAll(%q) error = %v", listedPath, err)
		}
		if got != "/posts/one/" {
			t.Fatalf("parseHugoListAll(%q) = %q", listedPath, got)
		}
	}
}

func TestHugoPreviewURLResolverRejectsMismatchedArticle(t *testing.T) {
	resolver := &hugoPreviewURLResolver{run: func(context.Context, config.SiteRuntime, string) ([]byte, error) {
		t.Fatal("Hugo should not run for a mismatched article")
		return nil, nil
	}}
	runtime := config.SiteRuntime{ID: "tech", ContentDir: "/tmp/content", LocalPreview: config.LocalPreviewConfig{URL: "https://tech.preview.example.com/"}}
	workspace := LocalPreviewWorkspace{ArticlePath: "posts/one.md", ContentDir: "/tmp/content"}
	if _, err := resolver.ResolveArticleURL(context.Background(), runtime, workspace, "posts/two.md"); err == nil {
		t.Fatal("ResolveArticleURL() should reject a mismatched article")
	}
}

func TestLocalPreviewArticleURLRewritesOnlyOrigin(t *testing.T) {
	got, err := localPreviewArticleURL("https://tech.preview.example.com/preview/", "https://production.example.com/preview/posts/one/?draft=1")
	if err != nil {
		t.Fatalf("localPreviewArticleURL() error = %v", err)
	}
	if got != "https://tech.preview.example.com/preview/posts/one/?draft=1" {
		t.Fatalf("URL = %q", got)
	}
	got, err = localPreviewArticleURL("https://tech.preview.example.com/preview/", "posts/one/?draft=1#section")
	if err != nil {
		t.Fatalf("localPreviewArticleURL() relative error = %v", err)
	}
	if got != "https://tech.preview.example.com/preview/posts/one/?draft=1#section" {
		t.Fatalf("relative URL = %q", got)
	}
	if _, err := localPreviewArticleURL("https://tech.preview.example.com/", "javascript:alert(1)"); err == nil {
		t.Fatal("localPreviewArticleURL() should reject non-HTTP schemes")
	}
}
