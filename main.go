package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/handlers"
	"hugo-cms/pkg/services"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func configureGinMode() error {
	mode := strings.TrimSpace(os.Getenv("GIN_MODE"))
	if mode == "" {
		return nil
	}

	switch mode {
	case gin.DebugMode, gin.ReleaseMode, gin.TestMode:
		gin.SetMode(mode)
		return nil
	default:
		return fmt.Errorf("invalid GIN_MODE %q", mode)
	}
}

func sitePreviewProxyHandler(c *gin.Context) {
	// Preview pages need same-origin referrers so root-relative links and
	// assets can be routed back to the selected site instead of the default
	// root proxy. Other admin routes keep the stricter no-referrer policy.
	c.Header("Referrer-Policy", "same-origin")

	siteID := strings.TrimSpace(c.Param("site"))
	site, ok := config.GetSite(siteID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown site: " + siteID})
		return
	}

	if err := services.StartPreviewForSite(site); err != nil {
		slog.Warn("Failed to start site preview", "site", siteID, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Preview unavailable"})
		return
	}
	if err := waitForPreviewPort(c.Request.Context(), site, 5*time.Second); err != nil {
		slog.Warn("Site preview did not become ready", "site", siteID, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Preview unavailable"})
		return
	}

	previewProxyURL, err := url.Parse("http://" + sitePreviewAddress(site))
	if err != nil {
		slog.Warn("Invalid site preview address", "site", siteID, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Preview unavailable"})
		return
	}
	proxy := newPreviewProxy(previewProxyURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		slog.Warn("Site preview proxy failed", "site", siteID, "error", err)
		http.Error(w, "Preview unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func newPreviewProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = target.Host
	}
	return proxy
}

func sitePreviewAddress(site config.SiteConfig) string {
	return net.JoinHostPort(site.HugoServerBind, site.HugoServerPort)
}

func waitForPreviewPort(parent context.Context, site config.SiteConfig, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	address := sitePreviewAddress(site)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("preview port %s not ready: %w", address, lastErr)
			}
			return fmt.Errorf("preview port %s not ready: %w", address, ctx.Err())
		case <-ticker.C:
		}
	}
}

func previewSiteFromReferer(referer string) (config.SiteConfig, bool) {
	if strings.TrimSpace(referer) == "" {
		return config.SiteConfig{}, false
	}
	refererURL, err := url.Parse(referer)
	if err != nil {
		return config.SiteConfig{}, false
	}

	parts := strings.Split(strings.TrimPrefix(refererURL.EscapedPath(), "/"), "/")
	if len(parts) < 3 || parts[0] != "admin" || parts[1] != "preview" {
		return config.SiteConfig{}, false
	}

	siteID, err := url.PathUnescape(parts[2])
	if err != nil {
		return config.SiteConfig{}, false
	}
	return config.GetSite(siteID)
}

func previewRedirectTarget(req *http.Request, site config.SiteConfig) string {
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	target := "/admin/preview/" + url.PathEscape(site.ID) + path
	if req.URL.RawQuery != "" {
		target += "?" + req.URL.RawQuery
	}
	return target
}

func SetupRouter() (*gin.Engine, error) {
	if err := configureGinMode(); err != nil {
		return nil, err
	}

	appURL := config.GetAppURL()
	r := gin.Default()

	// Determine if we are running on HTTPS
	isSecure := strings.HasPrefix(appURL, "https://")

	// Session Setup
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		if gin.Mode() == gin.ReleaseMode {
			return nil, fmt.Errorf("SESSION_SECRET is required in release mode")
		}
		slog.Warn("SESSION_SECRET is not set, using temporary random secret",
			"warning", "insecure for production")
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic("Failed to generate random session secret: " + err.Error())
		}
		secret = base64.StdEncoding.EncodeToString(b)
	}
	if len(secret) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET must be at least 32 characters")
	}
	authKey := sha512.Sum512([]byte("hugo-cms/session-auth/" + secret))
	encryptionKey := sha256.Sum256([]byte("hugo-cms/session-encryption/" + secret))
	store := cookie.NewStore(authKey[:], encryptionKey[:])
	store.Options(sessions.Options{
		Path:     "/", // Cookie valid for whole domain
		MaxAge:   86400 * 7,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("mysession", store))

	// Static Files & Templates
	r.LoadHTMLGlob("templates/*")

	// --- Health Check Endpoints (Public) ---
	r.GET("/health", handlers.HealthCheck)
	r.GET("/ready", handlers.ReadinessCheck)

	// --- Admin Routes ---
	admin := r.Group("/admin")
	admin.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	})
	{
		// Public Auth
		admin.GET("/login", handlers.LoginPage)
		admin.GET("/login/github", handlers.GithubLogin)
		admin.GET("/auth/callback", handlers.AuthCallback)
		admin.GET("/logout", handlers.Logout)

		// Protected Admin
		authorized := admin.Group("/")
		authorized.Use(handlers.AuthRequired)
		authorized.Use(handlers.TokenValidation) // Periodically validate GitHub token
		{
			// CMS Static Assets (Protected)
			authorized.Static("/static", "./static")

			authorized.GET("/", func(c *gin.Context) { c.HTML(http.StatusOK, "index.html", nil) })
			authorized.Any("/preview/:site/*path", sitePreviewProxyHandler)

			api := authorized.Group("/api")
			api.Use(handlers.RequestBodyLimit(config.MaxUploadSize + (1 << 20)))
			api.Use(handlers.CSRFProtection) // Apply CSRF protection to all API routes
			{
				api.GET("/csrf-token", handlers.GetCSRFToken) // Endpoint to get CSRF token
				api.POST("/build", handlers.HandleBuild)
				api.POST("/build/restart", handlers.HandleRestart)
				api.GET("/articles", handlers.ListArticles)
				api.GET("/article", handlers.GetArticle)
				api.POST("/article", handlers.SaveArticle)
				api.POST("/create", handlers.CreateArticle)
				api.POST("/delete", handlers.DeleteArticle)
				api.POST("/diff", handlers.GetDiff)
				api.GET("/config", handlers.GetConfig)
				api.GET("/sites", handlers.ListSites)
				api.GET("/snippets", handlers.GetSnippets)
				api.POST("/sync", handlers.HandleSync)
				api.POST("/publish", handlers.HandlePublish)
				api.GET("/media", handlers.ListMedia)
				api.POST("/media", handlers.UploadMedia)
				api.POST("/media/delete", handlers.DeleteMedia)
				api.GET("/media/raw", handlers.ServeMediaRaw)
			}
		}
	}

	// --- Root Proxy to Hugo ---
	previewProxyURL, _ := url.Parse("http://" + config.HugoServerBind + ":" + config.HugoServerPort)
	proxy := httputil.NewSingleHostReverseProxy(previewProxyURL)

	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		slog.Warn("Hugo preview proxy failed", "error", err)
		http.Error(w, "Preview unavailable", http.StatusBadGateway)
	}
	r.NoRoute(handlers.AuthRequired, handlers.TokenValidation, func(c *gin.Context) {
		if site, ok := previewSiteFromReferer(c.Request.Referer()); ok {
			c.Redirect(http.StatusTemporaryRedirect, previewRedirectTarget(c.Request, site))
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	})

	return r, nil
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + config.ServerPort,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Request bodies are size-limited by middleware. Global read and
		// write deadlines would make valid large uploads depend on connection
		// speed and can prevent the final response from being written.
		IdleTimeout:    2 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}
}

func main() {
	// Initialize config
	if err := config.Init(); err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}
	if err := config.ValidateSecurityConfig(); err != nil {
		slog.Error("Invalid security configuration", "error", err)
		os.Exit(1)
	}
	if err := services.ConfigureGeneratorAdapterFromConfig(); err != nil {
		slog.Error("Invalid generator configuration", "error", err)
		os.Exit(1)
	}

	appURL := config.GetAppURL()
	slog.Info("Starting server",
		"app_url", appURL,
		"redirect_url", config.OauthConf.RedirectURL,
		"port", config.ServerPort)

	r, err := SetupRouter()
	if err != nil {
		slog.Error("Failed to configure HTTP router", "error", err)
		os.Exit(1)
	}

	// Start default preview server
	if err := services.StartPreviewForSite(services.DefaultPreviewSite()); err != nil {
		slog.Error("Failed to start default preview server", "error", err)
	}

	srv := newHTTPServer(r)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server listen error", "error", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	// Clean up preview servers
	if err := services.StopAllPreviewServers(); err != nil {
		slog.Error("Failed to stop preview servers", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exiting")
}
