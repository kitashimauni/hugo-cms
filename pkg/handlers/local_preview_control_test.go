package handlers

import (
	"encoding/json"
	"hugo-cms/pkg/config"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetLocalPreviewStatusIncludesPersistentPolicy(t *testing.T) {
	originalSites := config.Sites
	t.Cleanup(func() { config.Sites = originalSites })
	enabled := true
	config.Sites = []config.SiteConfig{{
		ID: "always-on",
		Preview: config.SitePreviewConfig{LocalPreview: config.LocalPreviewConfig{
			Enabled:  &enabled,
			AlwaysOn: true,
			URL:      "https://always-on.preview.example.com/",
			Refresh: config.LocalPreviewRefreshConfig{
				Times:    []string{"04:00"},
				Timezone: "Asia/Tokyo",
			},
		}},
	}}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/admin/api/preview/local/status", GetLocalPreviewStatus)
	request := httptest.NewRequest(http.MethodGet, "/admin/api/preview/local/status?site=always-on", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["always_on"] != true || payload["supervisor_state"] != "stopped" || payload["refresh_timezone"] != "Asia/Tokyo" {
		t.Fatalf("status payload = %#v", payload)
	}
}
