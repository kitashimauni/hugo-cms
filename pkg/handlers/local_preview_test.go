package handlers

import (
	"errors"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLocalPreviewIngressFailsClosedForUnknownPreviewHost(t *testing.T) {
	originalDomain := config.PreviewDomain
	originalScheme := config.PreviewScheme
	originalSites := config.Sites
	t.Cleanup(func() {
		config.PreviewDomain = originalDomain
		config.PreviewScheme = originalScheme
		config.Sites = originalSites
	})

	config.PreviewDomain = "preview.example.com"
	config.PreviewScheme = "https"
	config.Sites = nil

	lifecycle, err := services.NewLocalPreviewLifecycle(14100, 14100)
	if err != nil {
		t.Fatalf("NewLocalPreviewLifecycle() error = %v", err)
	}
	manager := services.NewLocalPreviewManager(lifecycle)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LocalPreviewIngress(manager))
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	previewRequest := httptest.NewRequest(http.MethodGet, "http://unknown.preview.example.com/health", nil)
	previewRequest.Host = "unknown.preview.example.com"
	previewResponse := httptest.NewRecorder()
	router.ServeHTTP(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown preview host status = %d, want 404", previewResponse.Code)
	}

	cmsRequest := httptest.NewRequest(http.MethodGet, "http://cms.example.com/health", nil)
	cmsRequest.Host = "cms.example.com"
	cmsResponse := httptest.NewRecorder()
	router.ServeHTTP(cmsResponse, cmsRequest)
	if cmsResponse.Code != http.StatusOK {
		t.Fatalf("normal CMS host status = %d, want 200", cmsResponse.Code)
	}
}

func TestLocalPreviewIngressHidesControlEndpoints(t *testing.T) {
	originalDomain := config.PreviewDomain
	originalScheme := config.PreviewScheme
	originalSites := config.Sites
	t.Cleanup(func() {
		config.PreviewDomain = originalDomain
		config.PreviewScheme = originalScheme
		config.Sites = originalSites
	})

	config.PreviewDomain = "preview.example.com"
	config.PreviewScheme = "https"
	enabled := true
	config.Sites = []config.SiteConfig{{
		ID: "tech",
		Preview: config.SitePreviewConfig{
			LocalPreview: config.LocalPreviewConfig{Enabled: &enabled},
		},
	}}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	proxy := &localPreviewProxySpy{}
	router.Use(localPreviewIngress(proxy))

	for _, testCase := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "ready", method: http.MethodGet, path: "/__homecms_ready"},
		{name: "metadata", method: http.MethodGet, path: "/__homecms_metadata?path=content/posts/one.md"},
		{name: "invalidate", method: http.MethodPost, path: "/__homecms_invalidate"},
		{name: "ready dot segment", method: http.MethodGet, path: "/foo/../__homecms_ready"},
		{name: "metadata encoded dot segment", method: http.MethodGet, path: "/foo/%2e%2e/__homecms_metadata"},
		{name: "invalidate dot segment", method: http.MethodPost, path: "/foo/../__homecms_invalidate"},
		{name: "legacy ready", method: http.MethodGet, path: "/__hugo_cms_ready"},
		{name: "legacy metadata", method: http.MethodGet, path: "/__hugo_cms_metadata?path=content/posts/one.md"},
		{name: "legacy invalidate", method: http.MethodPost, path: "/__hugo_cms_invalidate"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, "https://tech.preview.example.com"+testCase.path, nil)
			request.Host = "tech.preview.example.com"
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("control endpoint status = %d, want 404", response.Code)
			}
		})
	}
	if proxy.calls != 0 {
		t.Fatalf("control endpoint proxy calls = %d, want 0", proxy.calls)
	}

	for _, path := range []string{"/__homecms_reload.js", "/__homecms_live_reload", "/__hugo_cms_reload.js", "/__hugo_cms_live_reload"} {
		request := httptest.NewRequest(http.MethodGet, "https://tech.preview.example.com"+path, nil)
		request.Host = "tech.preview.example.com"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("LiveReload path %q status = %d, want 204", path, response.Code)
		}
	}
	if proxy.calls != 4 {
		t.Fatalf("LiveReload proxy calls = %d, want 4", proxy.calls)
	}
}

func TestLocalPreviewIngressReleasesGateBeforeStreaming(t *testing.T) {
	originalDomain := config.PreviewDomain
	originalScheme := config.PreviewScheme
	originalSites := config.Sites
	t.Cleanup(func() {
		config.PreviewDomain = originalDomain
		config.PreviewScheme = originalScheme
		config.Sites = originalSites
	})

	config.PreviewDomain = "preview.example.com"
	config.PreviewScheme = "https"
	enabled := true
	siteID := "streaming"
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "content"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "content", "one.md"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	config.Sites = []config.SiteConfig{{
		ID:         siteID,
		RepoPath:   repo,
		ContentDir: "content",
		Preview: config.SitePreviewConfig{
			LocalPreview: config.LocalPreviewConfig{Enabled: &enabled},
		},
	}}

	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.NewSiteRuntime(config.Sites[0])
	if _, _, _, err := workspaceManager.Update(runtime, "one.md", 1, []byte("draft")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, claimed, err := workspaceManager.BeginCleanup(siteID)
		if err == nil && claimed {
			_, _ = workspaceManager.FinishCleanup(&cleanup)
		}
	})

	proxy := &localPreviewPreparedProxySpy{
		prepared: make(chan struct{}),
		serving:  make(chan struct{}),
		allow:    make(chan struct{}),
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(localPreviewIngress(proxy))

	request := httptest.NewRequest(http.MethodGet, "https://"+siteID+".preview.example.com/", nil)
	request.Host = siteID + ".preview.example.com"
	response := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		router.ServeHTTP(response, request)
		close(requestDone)
	}()

	select {
	case <-proxy.prepared:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy target was not prepared")
	}
	select {
	case <-proxy.serving:
	case <-time.After(2 * time.Second):
		t.Fatal("streaming handler did not start")
	}

	cleanup, claimed, err := workspaceManager.BeginCleanup(siteID)
	if err != nil || !claimed {
		t.Fatalf("BeginCleanup() claimed=%v err=%v while stream was active", claimed, err)
	}
	if released, err := workspaceManager.FinishCleanup(&cleanup); err != nil || !released {
		t.Fatalf("FinishCleanup() released=%v err=%v", released, err)
	}
	close(proxy.allow)
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("streaming request did not finish")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("streaming response status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

type localPreviewProxySpy struct {
	calls int
}

func (p *localPreviewProxySpy) ProxyRuntime(w http.ResponseWriter, _ *http.Request, _ config.SiteRuntime) error {
	p.calls++
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type localPreviewPreparedProxySpy struct {
	prepared chan struct{}
	serving  chan struct{}
	allow    chan struct{}
}

func (p *localPreviewPreparedProxySpy) PrepareProxyRuntime(config.SiteRuntime) (http.Handler, error) {
	close(p.prepared)
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(p.serving)
		<-p.allow
		w.WriteHeader(http.StatusNoContent)
	}), nil
}

func (p *localPreviewPreparedProxySpy) ProxyRuntime(http.ResponseWriter, *http.Request, config.SiteRuntime) error {
	return errors.New("ProxyRuntime should not be used when PrepareProxyRuntime is available")
}
