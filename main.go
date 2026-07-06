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

func SetupRouter() (*gin.Engine, error) {
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
	config.Init()
	if err := config.ValidateSecurityConfig(); err != nil {
		slog.Error("Invalid security configuration", "error", err)
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

	// Start Hugo Server
	if err := services.StartHugoServer(); err != nil {
		slog.Error("Failed to start Hugo Server", "error", err)
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

	// Clean up Hugo Server
	if err := services.StopHugoServer(); err != nil {
		slog.Error("Failed to stop Hugo Server", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exiting")
}
