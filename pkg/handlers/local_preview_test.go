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
