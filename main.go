package main

import (
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
	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
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
	previewProxyURL, _ := url.Parse("http://127.0.0.1:1314")
	proxy := httputil.NewSingleHostReverseProxy(previewProxyURL)
	
	r.Any(config.PreviewURL+"*path", func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	})
	
	r.Static("/static", "./static") // Serve static assets (css/js)

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
			api.GET("/articles", handlers.ListArticles)
			api.GET("/article", handlers.GetArticle)
			api.POST("/article", handlers.SaveArticle)
			api.POST("/create", handlers.CreateArticle)
			api.POST("/delete", handlers.DeleteArticle)
			api.POST("/diff", handlers.GetDiff)
			api.GET("/config", handlers.GetConfig)
			api.POST("/sync", handlers.HandleSync)
			api.POST("/publish", handlers.HandlePublish)
		}
	}

	r.Run(":8080")
}