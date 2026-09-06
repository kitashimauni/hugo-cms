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
	"net/http"
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

func SetupRouter() (*gin.Engine, error) {
	if err := configureGinMode(); err != nil {
		return nil, err
	}

	appURL := config.GetAppURL()
	r := gin.Default()

	// Preview hostname traffic must be handled before the CMS session middleware.
	// Viewer authentication is enforced by the external preview ingress rather
	// than by sharing the CMS admin cookie with repository-generated JavaScript.
	r.Use(handlers.LocalPreviewIngress(services.DefaultLocalPreviewManager()))

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
			api := authorized.Group("/api")
			api.Use(handlers.RequestBodyLimit(config.MaxUploadSize + (1 << 20)))
			api.Use(handlers.CSRFProtection) // Apply CSRF protection to all API routes
			{
				api.GET("/csrf-token", handlers.GetCSRFToken) // Endpoint to get CSRF token
				api.POST("/preview/markdown", handlers.RenderMarkdownPreview)
				api.POST("/preview/local", handlers.UpdateLocalPreviewContent)
				api.POST("/preview/local/release", handlers.ReleaseLocalPreviewContent)
				api.POST("/preview/deployments", handlers.UpdateDeploymentPreview)
				api.GET("/preview/deployments/:draft_id", handlers.GetDeploymentPreview)
				api.POST("/preview/deployments/:draft_id/retry", handlers.RetryDeploymentPreview)
				api.POST("/preview/deployments/:draft_id/discard", handlers.DiscardDeploymentPreview)
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
				api.POST("/publish", handlers.PublishDeploymentPreview)
				api.GET("/media", handlers.ListMedia)
				api.POST("/media", handlers.UploadMedia)
				api.POST("/media/delete", handlers.DeleteMedia)
				api.GET("/media/raw", handlers.ServeMediaRaw)
			}
		}
	}

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
	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		slog.Error("Failed to initialize local preview workspace", "error", err)
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

	previewManager := services.DefaultLocalPreviewManager()
	// Reject any new lazy preview starts immediately. Existing preview requests
	// may finish while the HTTP server drains, then all child processes are
	// stopped after no new HTTP requests can be accepted.
	previewManager.BeginShutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}
	cancel()

	previewCtx, cancelPreview := context.WithTimeout(context.Background(), 3*time.Second)
	if err := previewManager.Shutdown(previewCtx); err != nil {
		slog.Error("Failed to stop local preview processes cleanly", "error", err)
	}
	cancelPreview()

	// Child Hugo processes are stopped before removing their contentDir.
	if err := workspaceManager.Shutdown(); err != nil {
		slog.Error("Failed to remove local preview shadow workspace", "error", err)
	}

	slog.Info("Server exiting")
}
