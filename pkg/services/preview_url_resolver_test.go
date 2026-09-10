package services

import (
	"context"
	"hugo-cms/pkg/config"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestNewPreviewURLResolverSupportsConfiguredGenerators(t *testing.T) {
	for _, generator := range []string{"hugo", "eleventy", "11ty"} {
		resolver, err := NewPreviewURLResolver(generator)
		if err != nil {
			t.Fatalf("NewPreviewURLResolver(%q) error = %v", generator, err)
		}
		if resolver == nil {
			t.Fatalf("NewPreviewURLResolver(%q) returned nil", generator)
		}
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

func TestHugoPreviewURLResolverAllowsAnotherArticleInSiteWorkspace(t *testing.T) {
	resolver := &hugoPreviewURLResolver{run: func(context.Context, config.SiteRuntime, string) ([]byte, error) {
		return []byte("path,url\ncontent/posts/two.md,/posts/two/\n"), nil
	}}
	runtime := config.SiteRuntime{ID: "tech", ContentDir: "/tmp/content", LocalPreview: config.LocalPreviewConfig{URL: "https://tech.preview.example.com/"}}
	workspace := LocalPreviewWorkspace{ArticlePath: "posts/one.md", ContentDir: "/tmp/content"}
	got, err := resolver.ResolveArticleURL(context.Background(), runtime, workspace, "posts/two.md")
	if err != nil {
		t.Fatalf("ResolveArticleURL() error = %v", err)
	}
	if got != "https://tech.preview.example.com/posts/two/" {
		t.Fatalf("resolved URL = %q", got)
	}
}

func TestEleventyPreviewURLResolverUsesGeneratorMetadataAndLocalOrigin(t *testing.T) {
	runtime := config.SiteRuntime{
		ID:           "daily-blog",
		RepoPath:     "/repo",
		ContentDir:   "/tmp/shadow/daily-blog/content",
		Generator:    "eleventy",
		LocalPreview: config.LocalPreviewConfig{URL: "https://daily-blog.preview.example.com/preview/"},
	}
	workspace := LocalPreviewWorkspace{
		SiteID:      runtime.ID,
		ArticlePath: "posts/one.md",
		ContentDir:  runtime.ContentDir,
		Revision:    3,
	}
	resolver := &eleventyPreviewURLResolver{run: func(_ context.Context, got config.SiteRuntime) ([]byte, error) {
		if got.ContentDir != workspace.ContentDir {
			t.Fatalf("resolver content directory = %q, want %q", got.ContentDir, workspace.ContentDir)
		}
		return []byte(`[{"inputPath":"/tmp/shadow/daily-blog/content/posts/one.md","outputPath":"/tmp/output/custom/index.html","url":"https://production.example.com/custom/"}]`), nil
	}}

	got, err := resolver.ResolveArticleURL(context.Background(), runtime, workspace, workspace.ArticlePath)
	if err != nil {
		t.Fatalf("ResolveArticleURL() error = %v", err)
	}
	if got != "https://daily-blog.preview.example.com/custom/" {
		t.Fatalf("resolved URL = %q", got)
	}
}

func TestResolveRunningEleventyArticleURLWaitsForMetadataAndRewritesOrigin(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != eleventyLocalPreviewMetadataPath {
			t.Fatalf("request path = %q, want %q", r.URL.Path, eleventyLocalPreviewMetadataPath)
		}
		if got := r.URL.Query().Get("path"); got != "posts/one.md" {
			t.Fatalf("article path query = %q", got)
		}
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"resolved","url":"/custom/one/?draft=1#section"}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{
		ID:           "daily-blog",
		Generator:    "eleventy",
		LocalPreview: config.LocalPreviewConfig{URL: "https://daily.preview.example.com/base/"},
	}

	got, err := resolveRunningEleventyArticleURL(context.Background(), runtime, port, "posts/one.md")
	if err != nil {
		t.Fatalf("resolveRunningEleventyArticleURL() error = %v", err)
	}
	if got != "https://daily.preview.example.com/custom/one/?draft=1#section" {
		t.Fatalf("resolved URL = %q", got)
	}
	if attempts < 2 {
		t.Fatalf("metadata requests = %d, want at least 2", attempts)
	}
}

func TestEleventyResolverUsesDedicatedProjectAndOutput(t *testing.T) {
	production := t.TempDir()
	if err := os.MkdirAll(filepath.Join(production, "src", "posts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(production, "public"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(production, "src", "posts", "one.md"), []byte("production"), 0644); err != nil {
		t.Fatal(err)
	}

	shadow := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shadow, "posts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadow, "posts", "one.md"), []byte("draft"), 0644); err != nil {
		t.Fatal(err)
	}

	liveProject := t.TempDir()
	if err := createEleventyLocalPreviewProjectOverlay(production, liveProject, "src", "public", shadow); err != nil {
		t.Fatalf("create live overlay: %v", err)
	}
	liveOutput := filepath.Join(liveProject, "public")
	if err := os.WriteFile(filepath.Join(liveOutput, "existing.html"), []byte("live"), 0644); err != nil {
		t.Fatal(err)
	}

	runtime := config.SiteRuntime{
		RepoPath:                   liveProject,
		ContentDir:                 filepath.Join(liveProject, "src"),
		ProductionContentDir:       "src",
		PublicDir:                  "public",
		LocalPreviewProjectDir:     liveProject,
		LocalPreviewSourceRepoPath: production,
	}
	resolverProject, resolverOutput, cleanup, err := prepareEleventyResolverProject(runtime)
	if err != nil {
		t.Fatalf("prepareEleventyResolverProject() error = %v", err)
	}
	defer cleanup()
	if resolverProject == liveProject || resolverOutput == liveOutput {
		t.Fatalf("resolver reused live project/output: project=%q output=%q", resolverProject, resolverOutput)
	}
	if _, err := os.Stat(filepath.Join(liveOutput, "existing.html")); err != nil {
		t.Fatalf("live output was removed by resolver preparation: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resolverOutput, "resolved.html"), []byte("resolver"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(liveOutput, "resolved.html")); !os.IsNotExist(err) {
		t.Fatalf("resolver output leaked into live output: %v", err)
	}
}

func TestParseEleventyJSONMatchesRelativeInputAndDataPageURL(t *testing.T) {
	runtime := config.SiteRuntime{ContentDir: `C:\preview\daily-blog\content`}
	output := []byte(`[{"inputPath":"content/posts/one.md","data":{"page":{"url":"/posts/one/"}}}]`)
	got, err := parseEleventyJSON(output, runtime, "posts/one.md")
	if err != nil {
		t.Fatalf("parseEleventyJSON() error = %v", err)
	}
	if got != "/posts/one/" {
		t.Fatalf("parseEleventyJSON() = %q", got)
	}
}

func TestEleventyPreviewURLResolverAllowsAnotherArticleInSiteWorkspace(t *testing.T) {
	resolver := &eleventyPreviewURLResolver{run: func(context.Context, config.SiteRuntime) ([]byte, error) {
		return []byte(`[{"inputPath":"/tmp/content/posts/two.md","url":"/posts/two/"}]`), nil
	}}
	runtime := config.SiteRuntime{ID: "daily-blog", ContentDir: "/tmp/content", LocalPreview: config.LocalPreviewConfig{URL: "https://daily-blog.preview.example.com/"}}
	workspace := LocalPreviewWorkspace{ArticlePath: "posts/one.md", ContentDir: "/tmp/content"}
	got, err := resolver.ResolveArticleURL(context.Background(), runtime, workspace, "posts/two.md")
	if err != nil {
		t.Fatalf("ResolveArticleURL() error = %v", err)
	}
	if got != "https://daily-blog.preview.example.com/posts/two/" {
		t.Fatalf("resolved URL = %q", got)
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
