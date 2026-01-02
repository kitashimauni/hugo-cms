package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var (
	RepoPath   = "./repo"
	PublicPath = "./repo/public"
	PreviewURL = "/preview/"

	// Hugo Server settings
	HugoServerPort = "1314"
	HugoServerBind = "127.0.0.1"

	// Cache settings
	CacheConcurrency  = 20
	FileReadHeadLimit = int64(4096)

	// Media settings
	ArticleMediaDir = ""
	StaticMediaDir  = ""

	// Git settings
	GitUserEmail = "bot@hugo-cms.local"
	GitUserName  = "Hugo CMS Bot"
	GitBranch    = "main"
	GitRemote    = "origin"
)

var OauthConf *oauth2.Config

func Init() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found or error loading it.")
	}

	// Helper to get env with default
	getEnv := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fallback
	}

	appURL := getEnv("APP_URL", "http://localhost:8080")
	redirectURL := getEnv("GITHUB_REDIRECT_URL", appURL+"/auth/callback")

	// Load Configs
	RepoPath = getEnv("REPO_PATH", "./repo")
	PublicPath = getEnv("PUBLIC_PATH", RepoPath+"/public")
	
	HugoServerPort = getEnv("HUGO_SERVER_PORT", "1314")
	HugoServerBind = getEnv("HUGO_SERVER_BIND", "127.0.0.1")

	ArticleMediaDir = getEnv("ARTICLE_MEDIA_DIR", "")
	StaticMediaDir = getEnv("STATIC_MEDIA_DIR", "")

	GitUserEmail = getEnv("GIT_USER_EMAIL", "bot@hugo-cms.local")
	GitUserName = getEnv("GIT_USER_NAME", "Hugo CMS Bot")
	GitBranch = getEnv("GIT_BRANCH", "main")
	GitRemote = getEnv("GIT_REMOTE", "origin")

	if cc := os.Getenv("CACHE_CONCURRENCY"); cc != "" {
		if val, err := strconv.Atoi(cc); err == nil {
			CacheConcurrency = val
		}
	}

	OauthConf = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		Scopes:       []string{"repo"},
		Endpoint:     github.Endpoint,
		RedirectURL:  redirectURL,
	}
}

func GetAppURL() string {
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}
	return appURL
}