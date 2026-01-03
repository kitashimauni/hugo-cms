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
		Path:     "/", // Cookie valid for whole domain
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

	// CMS Static Assets
	r.Static("/admin/static", "./static")

	// --- Admin Routes ---
	admin := r.Group("/admin")
	{
		// Public Auth
		admin.GET("/login", handlers.LoginPage)
		admin.GET("/login/github", handlers.GithubLogin)
		admin.GET("/auth/callback", handlers.AuthCallback)
		admin.GET("/logout", handlers.Logout)

		// Protected Admin
		authorized := admin.Group("/")
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
	}

	// --- Root Proxy to Hugo ---
	previewProxyURL, _ := url.Parse("http://" + config.HugoServerBind + ":" + config.HugoServerPort)
	proxy := httputil.NewSingleHostReverseProxy(previewProxyURL)

	r.NoRoute(func(c *gin.Context) {
		// Protect Proxy
		session := sessions.Default(c)
		if session.Get("access_token") == nil {
			// If not authenticated, redirect to admin login
			c.Redirect(http.StatusFound, "/admin/login")
			return
		}

		// Proxy to Hugo
		proxy.ServeHTTP(c.Writer, c.Request)
	})

	r.Run(":8080")
}