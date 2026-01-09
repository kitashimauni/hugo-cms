package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var (
	RepoPath   = "./repo"
	PublicPath = "./repo/public"
	PreviewURL = "/"

	// Hugo Server settings
	HugoServerPort = "1314"
	HugoServerBind = "127.0.0.1"

	ServerPort = "8080"

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

	// Git timeout settings (generous for large repos/slow networks)
	GitCommandTimeout = 60 * time.Second // Local git commands (status, diff, etc.)
	GitNetworkTimeout = 5 * time.Minute  // Network operations (push, pull)

	// Security settings
	AllowedGitHubUsers = []string{} // Empty means allow all authenticated users
	CSRFSecret         = ""
	GitHubOAuthScopes  = []string{"public_repo"} // Default to public_repo only
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
	redirectURL := getEnv("GITHUB_REDIRECT_URL", appURL+"/admin/auth/callback")

	// Load Configs
	RepoPath = getEnv("REPO_PATH", "./repo")
	PublicPath = getEnv("PUBLIC_PATH", RepoPath+"/public")

	HugoServerPort = getEnv("HUGO_SERVER_PORT", "1314")
	HugoServerBind = getEnv("HUGO_SERVER_BIND", "127.0.0.1")

	ServerPort = getEnv("PORT", "8080")

	ArticleMediaDir = getEnv("ARTICLE_MEDIA_DIR", "")
	StaticMediaDir = getEnv("STATIC_MEDIA_DIR", "")

	GitUserEmail = getEnv("GIT_USER_EMAIL", "bot@hugo-cms.local")
	GitUserName = getEnv("GIT_USER_NAME", "Hugo CMS Bot")
	GitBranch = getEnv("GIT_BRANCH", "main")
	GitRemote = getEnv("GIT_REMOTE", "origin")

	// Security settings
	if users := os.Getenv("ALLOWED_GITHUB_USERS"); users != "" {
		AllowedGitHubUsers = splitAndTrim(users, ",")
	}
	CSRFSecret = getEnv("CSRF_SECRET", "")

	if cc := os.Getenv("CACHE_CONCURRENCY"); cc != "" {
		if val, err := strconv.Atoi(cc); err == nil {
			CacheConcurrency = val
		}
	}

	// OAuth scopes configuration
	// Use GITHUB_OAUTH_SCOPES env var to override (comma-separated)
	// Options: public_repo (public only), repo (public + private)
	if scopes := os.Getenv("GITHUB_OAUTH_SCOPES"); scopes != "" {
		GitHubOAuthScopes = splitAndTrim(scopes, ",")
	}

	OauthConf = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		Scopes:       GitHubOAuthScopes,
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

// splitAndTrim splits a string by separator and trims whitespace from each element
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// IsUserAllowed checks if a GitHub username is in the allowed list
func IsUserAllowed(username string) bool {
	if len(AllowedGitHubUsers) == 0 {
		return true // No restriction if list is empty
	}
	for _, u := range AllowedGitHubUsers {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}
