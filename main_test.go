package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSetupRouterRejectsShortSessionSecret(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv("GIN_MODE", "")
	t.Setenv("SESSION_SECRET", "too-short")

	_, err := SetupRouter()
	if err == nil || !strings.Contains(err.Error(), "at least 32") {
		t.Fatalf("SetupRouter() error = %v, want minimum-length error", err)
	}
}

func TestSetupRouterRequiresSessionSecretInReleaseMode(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv("GIN_MODE", "")
	t.Setenv("SESSION_SECRET", "")

	_, err := SetupRouter()
	if err == nil || !strings.Contains(err.Error(), "required in release mode") {
		t.Fatalf("SetupRouter() error = %v, want required-secret error", err)
	}
}

func TestSetupRouterAcceptsStrongSessionSecret(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv("GIN_MODE", "")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 32))

	router, err := SetupRouter()
	if err != nil {
		t.Fatalf("SetupRouter() unexpected error: %v", err)
	}
	if router == nil {
		t.Fatal("SetupRouter() returned a nil router")
	}
}

func TestAdminSecurityHeaders(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv("GIN_MODE", "")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 32))

	router, err := SetupRouter()
	if err != nil {
		t.Fatalf("SetupRouter() unexpected error: %v", err)
	}

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/admin/login", nil))
	if loginRecorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("admin response is missing X-Content-Type-Options")
	}
	if loginRecorder.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Error("admin response is missing X-Frame-Options")
	}

}

func TestMarkdownPreviewDoesNotUseIframe(t *testing.T) {
	content, err := os.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read admin template: %v", err)
	}
	template := string(content)
	if !strings.Contains(template, `id="markdown-preview"`) {
		t.Fatal("template is missing the Markdown preview container")
	}
	if strings.Contains(template, "<iframe") {
		t.Fatal("Markdown preview must not use an iframe")
	}
}

func TestHTTPServerDoesNotCapRequestBodyDuration(t *testing.T) {
	server := newHTTPServer(http.NotFoundHandler())

	if server.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", server.ReadHeaderTimeout, 10*time.Second)
	}
	if server.ReadTimeout != 0 {
		t.Fatalf("ReadTimeout = %v, want no global request body deadline", server.ReadTimeout)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %v, want no global deadline that expires during uploads", server.WriteTimeout)
	}
}

func TestSetupRouterHonorsReleaseModeFromEnvironment(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv("GIN_MODE", gin.ReleaseMode)
	t.Setenv("SESSION_SECRET", "")

	_, err := SetupRouter()
	if err == nil || !strings.Contains(err.Error(), "required in release mode") {
		t.Fatalf("SetupRouter() error = %v, want required-secret error", err)
	}
	if gin.Mode() != gin.ReleaseMode {
		t.Fatalf("gin.Mode() = %q, want %q", gin.Mode(), gin.ReleaseMode)
	}
}

func TestSetupRouterRejectsInvalidGinMode(t *testing.T) {
	originalMode := gin.Mode()
	t.Cleanup(func() { gin.SetMode(originalMode) })
	t.Setenv("GIN_MODE", "production")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 32))

	_, err := SetupRouter()
	if err == nil || !strings.Contains(err.Error(), "invalid GIN_MODE") {
		t.Fatalf("SetupRouter() error = %v, want invalid-mode error", err)
	}
}
