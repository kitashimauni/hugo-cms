package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetupRouterRejectsShortSessionSecret(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
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
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 32))

	router, err := SetupRouter()
	if err != nil {
		t.Fatalf("SetupRouter() unexpected error: %v", err)
	}
	if router == nil {
		t.Fatal("SetupRouter() returned a nil router")
	}
}

func TestAdminSecurityHeadersAndPreviewAuthentication(t *testing.T) {
	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(originalMode) })
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

	previewRecorder := httptest.NewRecorder()
	router.ServeHTTP(previewRecorder, httptest.NewRequest(http.MethodGet, "/preview-path", nil))
	if previewRecorder.Code != http.StatusFound {
		t.Fatalf("unauthenticated preview status = %d, want %d", previewRecorder.Code, http.StatusFound)
	}
	if location := previewRecorder.Header().Get("Location"); location != "/admin/login" {
		t.Fatalf("unauthenticated preview redirect = %q, want %q", location, "/admin/login")
	}
}

func TestPreviewFrameIsSandboxedWithoutSameOrigin(t *testing.T) {
	content, err := os.ReadFile("templates/index.html")
	if err != nil {
		t.Fatalf("read admin template: %v", err)
	}
	template := string(content)
	if !strings.Contains(template, `sandbox="allow-forms allow-scripts"`) {
		t.Fatal("preview iframe is missing the expected sandbox")
	}
	if strings.Contains(template, "allow-same-origin") {
		t.Fatal("preview iframe must not use allow-same-origin")
	}
}
