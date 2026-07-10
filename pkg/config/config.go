package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"gopkg.in/yaml.v3"
)

var (
	RepoPath        = "./repo"
	PublicPath      = "./repo/public"
	PreviewURL      = "/"
	ContentDir      = "content"
	StaticDir       = "static"
	PublicDir       = "public"
	SiteGenerator   = "hugo"
	DefaultSiteID   = "default"
	SitesConfigPath = ""
	Sites           = []SiteConfig{}

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
	MaxUploadSize   = int64(10 * 1024 * 1024) // 10MB default

	// Git settings
	GitUserEmail = "bot@hugo-cms.local"
	GitUserName  = "Hugo CMS Bot"
	GitBranch    = "main"
	GitRemote    = "origin"

	// Git timeout settings (generous for large repos/slow networks)
	GitCommandTimeout = 60 * time.Second // Local git commands (status, diff, etc.)
	GitNetworkTimeout = 5 * time.Minute  // Network operations (push, pull)

	// Security settings
	AllowedGitHubUsers  = []string{}
	AllowAllGitHubUsers = false
	GitHubOAuthScopes   = []string{"public_repo"} // Default to public_repo only

	// Snippet settings
	SnippetPaths = []string{}
)

var OauthConf *oauth2.Config

type SiteConfig struct {
	ID              string   `yaml:"id" json:"id"`
	Name            string   `yaml:"name" json:"name"`
	RepoPath        string   `yaml:"repo_path" json:"repo_path"`
	Generator       string   `yaml:"generator" json:"generator"`
	ContentDir      string   `yaml:"content_dir" json:"content_dir"`
	StaticDir       string   `yaml:"static_dir" json:"static_dir"`
	PublicDir       string   `yaml:"public_dir" json:"public_dir"`
	PreviewURL      string   `yaml:"preview_url" json:"preview_url"`
	HugoServerPort  string   `yaml:"hugo_server_port" json:"hugo_server_port"`
	HugoServerBind  string   `yaml:"hugo_server_bind" json:"hugo_server_bind"`
	ArticleMediaDir string   `yaml:"article_media_dir" json:"article_media_dir"`
	StaticMediaDir  string   `yaml:"static_media_dir" json:"static_media_dir"`
	SnippetPaths    []string `yaml:"snippet_paths" json:"snippet_paths"`
}

type SiteRegistryConfig struct {
	DefaultSite string       `yaml:"default_site"`
	Sites       []SiteConfig `yaml:"sites"`
}

func Init() error {
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found or error loading", "error", err)
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
	ContentDir = cleanRelativeDir(getEnv("CONTENT_DIR", "content"), "content")
	StaticDir = cleanRelativeDir(getEnv("STATIC_DIR", "static"), "static")
	PublicDir = cleanRelativeDir(getEnv("PUBLIC_DIR", "public"), "public")
	PublicPath = getEnv("PUBLIC_PATH", filepath.Join(RepoPath, PublicDir))
	PreviewURL = getEnv("PREVIEW_URL", "/")
	SiteGenerator = strings.ToLower(getEnv("SITE_GENERATOR", "hugo"))
	DefaultSiteID = getEnv("DEFAULT_SITE_ID", "default")
	SitesConfigPath = getEnv("SITES_CONFIG_PATH", "")

	HugoServerPort = getEnv("HUGO_SERVER_PORT", "1314")
	HugoServerBind = getEnv("HUGO_SERVER_BIND", "127.0.0.1")

	ServerPort = getEnv("PORT", "8080")

	ArticleMediaDir = cleanOptionalRelativeDir(getEnv("ARTICLE_MEDIA_DIR", ""))
	StaticMediaDir = cleanOptionalRelativeDir(getEnv("STATIC_MEDIA_DIR", ""))

	// Max upload size (default 10MB)
	if maxSize := os.Getenv("MAX_UPLOAD_SIZE_MB"); maxSize != "" {
		if val, err := strconv.ParseInt(maxSize, 10, 64); err == nil && val > 0 && val <= 1024 {
			MaxUploadSize = val * 1024 * 1024
		} else {
			slog.Warn("Invalid MAX_UPLOAD_SIZE_MB value; using existing default", "value", maxSize)
		}
	}

	GitUserEmail = getEnv("GIT_USER_EMAIL", "bot@hugo-cms.local")
	GitUserName = getEnv("GIT_USER_NAME", "Hugo CMS Bot")
	GitBranch = getEnv("GIT_BRANCH", "main")
	GitRemote = getEnv("GIT_REMOTE", "origin")

	// Security settings
	AllowedGitHubUsers = nil
	AllowAllGitHubUsers = false
	if users := os.Getenv("ALLOWED_GITHUB_USERS"); users != "" {
		AllowedGitHubUsers = splitAndTrim(users, ",")
	}
	if allowAll := os.Getenv("ALLOW_ALL_GITHUB_USERS"); allowAll != "" {
		val, err := strconv.ParseBool(allowAll)
		if err != nil {
			slog.Warn("Invalid ALLOW_ALL_GITHUB_USERS value; defaulting to false", "value", allowAll)
		} else {
			AllowAllGitHubUsers = val
		}
	}
	if cc := os.Getenv("CACHE_CONCURRENCY"); cc != "" {
		if val, err := strconv.Atoi(cc); err == nil && val >= 1 && val <= 256 {
			CacheConcurrency = val
		} else {
			slog.Warn("Invalid CACHE_CONCURRENCY value; using existing default", "value", cc)
		}
	}

	if snippets := os.Getenv("SNIPPET_PATHS"); snippets != "" {
		SnippetPaths = splitAndTrim(snippets, ",")
	} else {
		SnippetPaths = defaultSnippetPaths(RepoPath)
	}
	if err := loadSiteRegistry(); err != nil {
		return err
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
	return nil
}

func loadSiteRegistry() error {
	Sites = []SiteConfig{defaultSiteFromGlobals()}
	if SitesConfigPath == "" {
		return nil
	}

	content, err := os.ReadFile(SitesConfigPath)
	if err != nil {
		return fmt.Errorf("read site registry config %q: %w", SitesConfigPath, err)
	}

	var registry SiteRegistryConfig
	if err := yaml.Unmarshal(content, &registry); err != nil {
		return fmt.Errorf("parse site registry config %q: %w", SitesConfigPath, err)
	}

	normalized := make([]SiteConfig, 0, len(registry.Sites))
	for _, site := range registry.Sites {
		site = normalizeSiteConfig(site)
		if site.ID == "" {
			slog.Warn("Skipping site registry entry without id")
			continue
		}
		normalized = append(normalized, site)
	}
	if len(normalized) == 0 {
		return fmt.Errorf("site registry config %q contains no usable sites", SitesConfigPath)
	}
	if err := validateUniquePreviewAddresses(normalized); err != nil {
		return err
	}

	Sites = normalized
	if strings.TrimSpace(registry.DefaultSite) != "" {
		DefaultSiteID = strings.TrimSpace(registry.DefaultSite)
	}

	defaultSite, ok := GetSite(DefaultSiteID)
	if !ok {
		return fmt.Errorf("default site %q is not defined in site registry %q", DefaultSiteID, SitesConfigPath)
	}
	applyDefaultSite(defaultSite)
	return nil
}

func defaultSiteFromGlobals() SiteConfig {
	return normalizeSiteConfig(SiteConfig{
		ID:              DefaultSiteID,
		Name:            "Default",
		RepoPath:        RepoPath,
		Generator:       SiteGenerator,
		ContentDir:      ContentDir,
		StaticDir:       StaticDir,
		PublicDir:       PublicDir,
		PreviewURL:      PreviewURL,
		HugoServerPort:  HugoServerPort,
		HugoServerBind:  HugoServerBind,
		ArticleMediaDir: ArticleMediaDir,
		StaticMediaDir:  StaticMediaDir,
		SnippetPaths:    SnippetPaths,
	})
}

func normalizeSiteConfig(site SiteConfig) SiteConfig {
	site.ID = strings.TrimSpace(site.ID)
	site.Name = strings.TrimSpace(site.Name)
	site.RepoPath = strings.TrimSpace(site.RepoPath)
	site.Generator = strings.ToLower(strings.TrimSpace(site.Generator))
	site.ContentDir = cleanRelativeDir(site.ContentDir, "content")
	site.StaticDir = cleanRelativeDir(site.StaticDir, "static")
	site.PublicDir = cleanRelativeDir(site.PublicDir, "public")
	site.PreviewURL = strings.TrimSpace(site.PreviewURL)
	site.HugoServerPort = strings.TrimSpace(site.HugoServerPort)
	site.HugoServerBind = strings.TrimSpace(site.HugoServerBind)
	site.ArticleMediaDir = cleanOptionalRelativeDir(site.ArticleMediaDir)
	site.StaticMediaDir = cleanOptionalRelativeDir(site.StaticMediaDir)

	if site.Name == "" {
		site.Name = site.ID
	}
	if site.RepoPath == "" {
		site.RepoPath = "./repo"
	}
	if site.Generator == "" {
		site.Generator = "hugo"
	}
	if site.PreviewURL == "" {
		site.PreviewURL = "/"
	}
	if site.HugoServerPort == "" {
		site.HugoServerPort = "1314"
	}
	if site.HugoServerBind == "" {
		site.HugoServerBind = "127.0.0.1"
	}
	site.SnippetPaths = normalizeSnippetPaths(site.SnippetPaths, site.RepoPath)
	return site
}

func validateUniquePreviewAddresses(sites []SiteConfig) error {
	seen := map[string]string{}
	for _, site := range sites {
		key := site.HugoServerBind + ":" + site.HugoServerPort
		if existingID, ok := seen[key]; ok {
			return fmt.Errorf("preview address %s is used by both site %q and site %q", key, existingID, site.ID)
		}
		seen[key] = site.ID
	}
	return nil
}

func defaultSnippetPaths(repoPath string) []string {
	return []string{filepath.Clean(filepath.Join(repoPath, ".vscode", "md.code-snippets"))}
}

func normalizeSnippetPaths(paths []string, repoPath string) []string {
	if len(paths) == 0 {
		return defaultSnippetPaths(repoPath)
	}

	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if filepath.IsAbs(path) {
			normalized = append(normalized, path)
			continue
		}
		if relativePathWithinRepo(path, repoPath) {
			normalized = append(normalized, path)
			continue
		}
		normalized = append(normalized, filepath.Join(repoPath, path))
	}
	if len(normalized) == 0 {
		return defaultSnippetPaths(repoPath)
	}
	return normalized
}

func relativePathWithinRepo(path, repoPath string) bool {
	if filepath.IsAbs(path) || filepath.IsAbs(repoPath) {
		return false
	}
	cleanPath := filepath.Clean(path)
	cleanRepo := filepath.Clean(repoPath)
	return cleanPath == cleanRepo || strings.HasPrefix(cleanPath, cleanRepo+string(os.PathSeparator))
}

func cleanRelativeDir(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
		return fallback
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return fallback
	}
	return cleaned
}

func cleanOptionalRelativeDir(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return ""
	}
	return cleaned
}

func GetSite(id string) (SiteConfig, bool) {
	id = strings.TrimSpace(id)
	for _, site := range Sites {
		if site.ID == id {
			return site, true
		}
	}
	return SiteConfig{}, false
}

func applyDefaultSite(site SiteConfig) {
	ApplySiteRuntime(site)
}

func RuntimeSiteConfig() SiteConfig {
	return SiteConfig{
		ID:              DefaultSiteID,
		Name:            "Current",
		RepoPath:        RepoPath,
		Generator:       SiteGenerator,
		ContentDir:      ContentDir,
		StaticDir:       StaticDir,
		PublicDir:       PublicDir,
		PreviewURL:      PreviewURL,
		HugoServerPort:  HugoServerPort,
		HugoServerBind:  HugoServerBind,
		ArticleMediaDir: ArticleMediaDir,
		StaticMediaDir:  StaticMediaDir,
		SnippetPaths:    SnippetPaths,
	}
}

func ApplySiteRuntime(site SiteConfig) {
	RepoPath = site.RepoPath
	SiteGenerator = site.Generator
	ContentDir = site.ContentDir
	StaticDir = site.StaticDir
	PublicDir = site.PublicDir
	PublicPath = filepath.Join(site.RepoPath, site.PublicDir)
	PreviewURL = site.PreviewURL
	HugoServerPort = site.HugoServerPort
	HugoServerBind = site.HugoServerBind
	ArticleMediaDir = site.ArticleMediaDir
	StaticMediaDir = site.StaticMediaDir
	SnippetPaths = site.SnippetPaths
}

// ValidateSecurityConfig rejects insecure authorization defaults.
func ValidateSecurityConfig() error {
	if AllowAllGitHubUsers && strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "release") {
		return fmt.Errorf("ALLOW_ALL_GITHUB_USERS cannot be enabled when GIN_MODE=release")
	}
	if len(AllowedGitHubUsers) == 0 && !AllowAllGitHubUsers {
		return fmt.Errorf("ALLOWED_GITHUB_USERS must contain at least one user; set ALLOW_ALL_GITHUB_USERS=true only for explicit development use")
	}
	return nil
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
		return AllowAllGitHubUsers
	}
	for _, u := range AllowedGitHubUsers {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}
