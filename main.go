package main

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/handlers"
	"net/http"
	"os"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize config
	config.Init()

	r := gin.Default()

	// Session Setup
	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	r.Use(sessions.Sessions("mysession", store))

	// Static Files & Templates
	r.LoadHTMLGlob("templates/*")
	r.Static(config.PreviewURL, config.PublicPath)
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
			api.POST("/diff", handlers.GetDiff)
			api.GET("/config", handlers.GetConfig)
			api.POST("/sync", handlers.HandleSync)
			api.POST("/publish", handlers.HandlePublish)
		}
	}

	r.Run(":8080")
}