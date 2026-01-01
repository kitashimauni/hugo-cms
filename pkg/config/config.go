package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

const (
	RepoPath   = "./repo"
	PublicPath = "./repo/public"
	PreviewURL = "/preview/"

	// Hugo Server settings
	HugoServerPort = "1314"
	HugoServerBind = "127.0.0.1"

	// Cache settings
	CacheConcurrency  = 20   // Number of concurrent goroutines for cache operations
	FileReadHeadLimit = 4096 // Bytes to read from file head for front matter parsing

	// Git settings
	GitUserEmail = "bot@hugo-cms.local"
	GitUserName  = "Hugo CMS Bot"
)

var OauthConf *oauth2.Config

func Init() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found or error loading it.")
	}

	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}

	redirectURL := os.Getenv("GITHUB_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = appURL + "/auth/callback"
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
