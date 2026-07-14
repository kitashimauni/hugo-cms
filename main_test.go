package main

import (
	"context"
	"hugo-cms/pkg/config"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestAdminSecurityHeadersAndPreviewAuthentication(t *testing.T) {
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

func TestPreviewSiteFromReferer(t *testing.T) {
	originalSites := config.Sites
	t.Cleanup(func() {
		config.Sites = originalSites
	})

	config.Sites = []config.SiteConfig{
		{ID: "docs site", RepoPath: "C:/sites/docs"},
	}

	site, ok := previewSiteFromReferer("http://localhost:8080/admin/preview/docs%20site/posts/hello/")
	if !ok {
		t.Fatal("previewSiteFromReferer() did not find site")
	}
	if site.ID != "docs site" {
		t.Fatalf("site.ID = %q, want docs site", site.ID)
	}
}

func TestPreviewRedirectTargetPreservesPathAndQuery(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://localhost:8080/images/logo.png?v=1", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	target := previewRedirectTarget(req, config.SiteConfig{ID: "docs site"})
	if target != "/admin/preview/docs%20site/images/logo.png?v=1" {
		t.Fatalf("previewRedirectTarget() = %q", target)
	}
}

func TestPreviewProxyDirectorPreservesPathEscapingAndQuery(t *testing.T) {
	req, err := http.NewRequest(
		http.MethodGet,
		"http://cms.example/admin/preview/docs%20site/assets/a%2Fb.css?q=a%2Fb&x=1+2",
		nil,
	)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	target, err := url.Parse("http://127.0.0.1:1314")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	newPreviewProxy(target).Director(req)

	if req.URL.Scheme != "http" || req.URL.Host != "127.0.0.1:1314" {
		t.Fatalf("proxy target = %s://%s", req.URL.Scheme, req.URL.Host)
	}
	if got := req.URL.EscapedPath(); got != "/admin/preview/docs%20site/assets/a%2Fb.css" {
		t.Fatalf("EscapedPath() = %q", got)
	}
	if req.URL.RawQuery != "q=a%2Fb&x=1+2" {
		t.Fatalf("RawQuery = %q", req.URL.RawQuery)
	}
	if req.Host != "127.0.0.1:1314" {
		t.Fatalf("Host = %q, want upstream host", req.Host)
	}
}

func TestSitePreviewAddressSupportsIPv6(t *testing.T) {
	address := sitePreviewAddress(config.SiteConfig{
		HugoServerBind: "::1",
		HugoServerPort: "1314",
	})

	if address != "[::1]:1314" {
		t.Fatalf("sitePreviewAddress() = %q, want IPv6 host-port", address)
	}
}

func TestWaitForPreviewPortReturnsWhenListening(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}

	err = waitForPreviewPort(context.Background(), config.SiteConfig{
		HugoServerBind: "127.0.0.1",
		HugoServerPort: port,
	}, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("waitForPreviewPort() error = %v", err)
	}
}
