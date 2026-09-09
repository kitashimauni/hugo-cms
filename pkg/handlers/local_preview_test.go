package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/http"
	"net/http/httptest"
	"testing"

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
		{name: "ready", method: http.MethodGet, path: "/__hugo_cms_ready"},
		{name: "metadata", method: http.MethodGet, path: "/__hugo_cms_metadata?path=content/posts/one.md"},
		{name: "invalidate", method: http.MethodPost, path: "/__hugo_cms_invalidate"},
		{name: "ready dot segment", method: http.MethodGet, path: "/foo/../__hugo_cms_ready"},
		{name: "metadata encoded dot segment", method: http.MethodGet, path: "/foo/%2e%2e/__hugo_cms_metadata"},
		{name: "invalidate dot segment", method: http.MethodPost, path: "/foo/../__hugo_cms_invalidate"},
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

	for _, path := range []string{"/__hugo_cms_reload.js", "/__hugo_cms_live_reload"} {
		request := httptest.NewRequest(http.MethodGet, "https://tech.preview.example.com"+path, nil)
		request.Host = "tech.preview.example.com"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("LiveReload path %q status = %d, want 204", path, response.Code)
		}
	}
	if proxy.calls != 2 {
		t.Fatalf("LiveReload proxy calls = %d, want 2", proxy.calls)
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
