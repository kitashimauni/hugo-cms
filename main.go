package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/handlers"
	"hugo-cms/pkg/services"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize config
	config.Init()

	appURL := config.GetAppURL()
	fmt.Printf("Starting server...\n")
	fmt.Printf("APP_URL: %s\n", appURL)
	fmt.Printf("Redirect URL: %s\n", config.OauthConf.RedirectURL)

	r := gin.Default()

	// Determine if we are running on HTTPS
	isSecure := strings.HasPrefix(appURL, "https://")

	// Session Setup
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		fmt.Println("WARNING: SESSION_SECRET is not set. Using a temporary random secret.")
		b := make([]byte, 32)
		rand.Read(b)
		secret = base64.StdEncoding.EncodeToString(b)
	}
	store := cookie.NewStore([]byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("mysession", store))

	// Static Files & Templates
	r.LoadHTMLGlob("templates/*")
	// Start Hugo Server
	if err := services.StartHugoServer(); err != nil {
		fmt.Printf("Failed to start Hugo Server: %v\n", err)
	}

	// Proxy /preview/ to Hugo Server
	previewProxyURL, _ := url.Parse("http://" + config.HugoServerBind + ":" + config.HugoServerPort)
	proxy := httputil.NewSingleHostReverseProxy(previewProxyURL)

	r.Any(config.PreviewURL+"*path", func(c *gin.Context) {
		prefix := strings.TrimRight(config.PreviewURL, "/")
		if strings.HasPrefix(c.Request.URL.Path, prefix) {
			c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, prefix)
			if c.Request.URL.Path == "" {
				c.Request.URL.Path = "/"
			}
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	})

	r.Static("/static", "./static") // Serve static assets (css/js)

	// Fallback proxy for site assets (e.g. /images/...) protected by auth
	r.NoRoute(handlers.AuthRequired, func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	})

	// --- Auth Routes ---
	r.GET("/login", handlers.LoginPage)
	r.GET("/login/github", handlers.GithubLogin)
	r.GET("/auth/callback", handlers.AuthCallback)
	r.GET("/logout", handlers.Logout)

	// --- Main App (Authorized) ---
	authorized := r.Group("/")
	authorized.Use(handlers.AuthRequired)
	{
		authorized.GET("/", func(c *gin.Context) { c.HTML(http.StatusOK, "index.html", nil) })

		api := authorized.Group("/api")
		{
			api.POST("/build", handlers.HandleBuild)
			api.POST("/build/restart", handlers.HandleRestart)
			api.GET("/articles", handlers.ListArticles)
			api.GET("/article", handlers.GetArticle)
			api.POST("/article", handlers.SaveArticle)
			api.POST("/create", handlers.CreateArticle)
			api.POST("/delete", handlers.DeleteArticle)
			api.POST("/diff", handlers.GetDiff)
			api.GET("/config", handlers.GetConfig)
			api.POST("/sync", handlers.HandleSync)
			api.POST("/publish", handlers.HandlePublish)
			api.GET("/media", handlers.ListMedia)
			api.POST("/media", handlers.UploadMedia)
			api.POST("/media/delete", handlers.DeleteMedia)
			api.GET("/media/raw", handlers.ServeMediaRaw)
		}
	}

	r.Run(":8080")
}
