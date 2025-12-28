package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"gopkg.in/yaml.v3"
)

// 設定
const (
	RepoPath   = "./repo"
	PublicPath = "./repo/public"
	PreviewURL = "/preview/"
)

var (
	oauthConf    *oauth2.Config
	articleCache []Article
	cacheMutex   sync.Mutex
	cacheLoaded  bool
)

// 記事構造体
type Article struct {
	Path        string                 `json:"path"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content,omitempty"` // Raw content (backward compatibility)
	FrontMatter map[string]interface{} `json:"frontmatter,omitempty"`
	Body        string                 `json:"body,omitempty"`
	Format      string                 `json:"format,omitempty"` // yaml, toml, json
	IsDirty     bool                   `json:"is_dirty"`
}

func main() {
	// .env 読み込み
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found or error loading it.")
	}

	// OAuth設定
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}

	redirectURL := os.Getenv("GITHUB_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = appURL + "/auth/callback"
	}

	oauthConf = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		Scopes:       []string{"repo"}, // リポジトリ操作権限が必要
		Endpoint:     github.Endpoint,
		RedirectURL:  redirectURL,
	}

	r := gin.Default()

	// セッション設定
	store := cookie.NewStore([]byte(os.Getenv("SESSION_SECRET")))
	r.Use(sessions.Sessions("mysession", store))

	// 静的ファイルとテンプレート
	r.LoadHTMLGlob("templates/*")
	r.Static(PreviewURL, PublicPath)

	// --- 認証ルート ---
	r.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", nil)
	})

	r.GET("/login/github", func(c *gin.Context) {
		url := oauthConf.AuthCodeURL("state", oauth2.AccessTypeOffline)
		c.Redirect(http.StatusTemporaryRedirect, url)
	})

	r.GET("/auth/callback", func(c *gin.Context) {
		code := c.Query("code")
		token, err := oauthConf.Exchange(context.Background(), code)
		if err != nil {
			c.String(http.StatusInternalServerError, "OAuth Exchange Failed")
			return
		}

		session := sessions.Default(c)
		session.Set("access_token", token.AccessToken)
		session.Save()

		c.Redirect(http.StatusFound, "/")
	})

	r.GET("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
		session.Save()
		c.Redirect(http.StatusFound, "/login")
	})

	// --- メインアプリ (要認証) ---
	authorized := r.Group("/")
	authorized.Use(authRequired)
	{
		authorized.GET("/", func(c *gin.Context) { c.HTML(http.StatusOK, "index.html", nil) })

		api := authorized.Group("/api")
		{
			api.POST("/build", handleBuild)

			api.GET("/articles", func(c *gin.Context) {
				articles, err := getArticlesCache()
				if err != nil {
					c.JSON(500, gin.H{"error": "Failed to fetch articles"})
					return
				}
				c.JSON(http.StatusOK, articles)
			})

			api.GET("/article", func(c *gin.Context) {
				targetPath := c.Query("path")
				fullPath := safeJoin(RepoPath, "content", targetPath)
				content, err := os.ReadFile(fullPath)
				if err != nil {
					c.JSON(404, gin.H{"error": "File not found"})
					return
				}

				// フロントマター分離とパース
				fm, body, format, err := parseFrontMatter(content)
				if err != nil {
					// パース失敗時はそのままテキストとして返す
					c.JSON(http.StatusOK, gin.H{"content": string(content)})
					return
				}

				c.JSON(http.StatusOK, Article{
					Path:        targetPath,
					FrontMatter: fm,
					Body:        body,
					Format:      format,
				})
			})

			api.POST("/article", func(c *gin.Context) {
				var art Article
				if err := c.BindJSON(&art); err != nil {
					c.JSON(400, gin.H{"error": "Invalid JSON"})
					return
				}

				fullPath := safeJoin(RepoPath, "content", art.Path)
				var finalContent []byte
				var err error

				// FrontMatterがある場合は構築、なければContentをそのまま使用
				if art.FrontMatter != nil {
					finalContent, err = constructFileContent(art.FrontMatter, art.Body, art.Format)
					if err != nil {
						c.JSON(500, gin.H{"error": "Failed to construct file content: " + err.Error()})
						return
					}
				} else {
					finalContent = []byte(art.Content)
				}

				if err := os.WriteFile(fullPath, finalContent, 0644); err != nil {
					c.JSON(500, gin.H{"error": "Save failed"})
					return
				}
				
				invalidateCache() // Cache invalidation
				c.JSON(200, gin.H{"status": "saved"})
			})

			api.POST("/create", func(c *gin.Context) {
				var req struct {
					Path    string `json:"path"`
					Content string `json:"content"` // Content is ignored when using hugo new
				}
				if err := c.BindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid JSON"})
					return
				}

				// Basic validation
				if req.Path == "" || strings.Contains(req.Path, "..") {
					c.JSON(400, gin.H{"error": "Invalid path"})
					return
				}

				// Check if file already exists (Hugo might overwrite or fail, safer to check)
				fullPath := safeJoin(RepoPath, "content", req.Path)
				if _, err := os.Stat(fullPath); err == nil {
					c.JSON(409, gin.H{"error": "File already exists"})
					return
				}

				// Run hugo new content
				cmd := exec.Command("hugo", "new", "content", req.Path)
				cmd.Dir = RepoPath
				output, err := cmd.CombinedOutput()
				
				if err != nil {
					c.JSON(500, gin.H{"error": "Hugo new failed", "log": string(output)})
					return
				}

				invalidateCache()
				c.JSON(200, gin.H{"status": "created", "log": string(output)})
			})

						api.POST("/diff", func(c *gin.Context) {
							var art Article
							if err := c.BindJSON(&art); err != nil {
								c.JSON(400, gin.H{"error": "Invalid JSON"})
								return
							}
			
							fullPath := safeJoin(RepoPath, "content", art.Path)
							
							// 1. Get current content from file
							currentContent, err := os.ReadFile(fullPath)
							if err != nil {
								currentContent = []byte("")
							}
			
							// Normalize current content to avoid formatting noise
							// (Parse -> Construct)
							if len(currentContent) > 0 {
								fm, body, format, err := parseFrontMatter(currentContent)
								if err == nil {
									normalized, err := constructFileContent(fm, body, format)
									if err == nil {
										currentContent = normalized
									}
								}
							}
			
							// 2. Construct new content from editor
							var newContent []byte
							if art.FrontMatter != nil {
								newContent, err = constructFileContent(art.FrontMatter, art.Body, art.Format)
								if err != nil {
									c.JSON(500, gin.H{"error": "Construction failed"})
									return
								}
							} else {
								newContent = []byte(art.Content)
							}
			
							// 3. Create temp files for comparison
							tmpDir := os.TempDir()
							f1, _ := os.CreateTemp(tmpDir, "diff_old_*")
							f2, _ := os.CreateTemp(tmpDir, "diff_new_*")
							defer os.Remove(f1.Name())
							defer os.Remove(f2.Name())
			
							f1.Write(currentContent)
							f2.Write(newContent)
							f1.Close()
							f2.Close()
			
							// 4. Check Unsaved Diff (Editor vs Saved-Normalized)
							cmd := exec.Command("git", "diff", "--no-index", f1.Name(), f2.Name())
							output, err := cmd.CombinedOutput()
							
							// Exit code 1 means diff found
							if err != nil && cmd.ProcessState.ExitCode() == 1 {
								diffStr := string(output)
								diffStr = strings.ReplaceAll(diffStr, f1.Name(), "Saved (Normalized)")
								diffStr = strings.ReplaceAll(diffStr, f2.Name(), "Editor")
								c.JSON(200, gin.H{"diff": diffStr, "type": "unsaved"})
								return
							}
			
							// 5. Check Git Diff (Saved vs HEAD)
							// Note: This still compares Raw Saved vs HEAD, which is correct for checking "what will be committed"
							// But user asked to "reset" formatting diffs.
							// If we normalize comparison above, "Diff" button will show No Diff.
							// But "Reset" just reloads raw file.
							// If user Saves, the file WILL change (normalized).
							// This is acceptable behavior for a CMS.
							
							relPath := filepath.Join("content", art.Path)
							cmdGit := exec.Command("git", "diff", "HEAD", "--", relPath)
							cmdGit.Dir = RepoPath
							outGit, _ := cmdGit.CombinedOutput()
							
							if len(outGit) > 0 {
								c.JSON(200, gin.H{"diff": string(outGit), "type": "git"})
							} else {
								c.JSON(200, gin.H{"diff": "", "type": "none"})
							}
						})
			// 7. Config取得
			api.GET("/config", func(c *gin.Context) {
				configPath := filepath.Join(RepoPath, "static/admin/config.yml")
				content, err := os.ReadFile(configPath)
				if err != nil {
					c.JSON(404, gin.H{"error": "Config not found"})
					return
				}
				
				var config map[string]interface{}
				if err := yaml.Unmarshal(content, &config); err != nil {
					c.JSON(500, gin.H{"error": "Failed to parse config"})
					return
				}
				c.JSON(http.StatusOK, config)
			})

			api.POST("/sync", handleSync)
			api.POST("/publish", handlePublish)
		}
	}

	r.Run(":8080")
}

// --- Middleware & Handlers ---

func authRequired(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token")
	if token == nil {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		} else {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
		}
		return
	}
	c.Next()
}

func handleBuild(c *gin.Context) {
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}

	cmd := exec.Command("hugo",
		"--source", RepoPath,
		"--destination", "public",
		"--baseURL", appURL+PreviewURL,
		"--cleanDestinationDir",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": string(output)})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": string(output)})
}

func handleSync(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token").(string)
	err, log := executeGitWithToken(RepoPath, token, "pull", "origin", "main")
	
	if err == nil {
		invalidateCache() // Cache invalidation on sync
	}

	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": log})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": log})
}

func handlePublish(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token").(string)
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = RepoPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "git add failed", "details": string(out)})
		return
	}
	msg := fmt.Sprintf("Update via HomeCMS: %s", time.Now().Format("2006-01-02 15:04:05"))
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = RepoPath
	commitCmd.Run()
	err, log := executeGitWithToken(RepoPath, token, "push", "origin", "main")
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": log})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": log})
}

func executeGitWithToken(dir, token string, args ...string) (error, string) {
	cmdGetUrl := exec.Command("git", "remote", "get-url", "origin")
	cmdGetUrl.Dir = dir
	outUrl, err := cmdGetUrl.Output()
	if err != nil {
		return err, "Failed to get remote url"
	}
	remoteUrl := strings.TrimSpace(string(outUrl))
	u, err := url.Parse(remoteUrl)
	if err != nil {
		return err, "Invalid remote url"
	}
	u.User = url.UserPassword("oauth2", token)
	authenticatedUrl := u.String()
	newArgs := make([]string, len(args))
	copy(newArgs, args)
	for i, v := range newArgs {
		if v == "origin" {
			newArgs[i] = authenticatedUrl
		}
	}
	cmd := exec.Command("git", newArgs...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	safeLog := strings.ReplaceAll(string(output), token, "***")
	safeLog = strings.ReplaceAll(safeLog, authenticatedUrl, remoteUrl)
	return err, safeLog
}

func safeJoin(root, sub, target string) string {
	cleanTarget := filepath.Clean(target)
	if strings.Contains(cleanTarget, "..") {
		return ""
	}
	return filepath.Join(root, sub, cleanTarget)
}

// --- Cache Logic ---

func getArticlesCache() ([]Article, error) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if cacheLoaded {
		return articleCache, nil
	}

	var articles []Article
	contentDir := filepath.Join(RepoPath, "content")
	
dirtyFiles, _ := getGitDirtyFiles(RepoPath)

	err := filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			relPath, _ := filepath.Rel(contentDir, path)
			
			repoRelPath, _ := filepath.Rel(RepoPath, path)
			repoRelPath = filepath.ToSlash(repoRelPath)
			isDirty := dirtyFiles[repoRelPath]

			// Read file to get title
			content, err := os.ReadFile(path)
			title := relPath // Default to path
			if err == nil {
				fm, _, _, err := parseFrontMatter(content)
				if err == nil {
					if t, ok := fm["title"].(string); ok {
						title = t
					}
				}
			}

			articles = append(articles, Article{
				Path:    relPath,
				Title:   title,
				IsDirty: isDirty,
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	articleCache = articles
	cacheLoaded = true
	return articleCache, nil
}

func getGitDirtyFiles(dir string) (map[string]bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	dirty := make(map[string]bool)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		path = strings.Trim(path, "\"")
		dirty[path] = true
	}
	return dirty, nil
}

func invalidateCache() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	cacheLoaded = false
	articleCache = nil
}

// --- Front Matter Logic ---

func parseFrontMatter(content []byte) (map[string]interface{}, string, string, error) {
	str := string(content)
	// Check for YAML (---)
	if strings.HasPrefix(str, "---\n") || strings.HasPrefix(str, "---\r\n") {
		parts := strings.SplitN(str, "---", 3) // "", FM, Body
		if len(parts) == 3 {
			var fm map[string]interface{}
			if err := yaml.Unmarshal([]byte(parts[1]), &fm); err == nil {
				return fm, strings.TrimSpace(parts[2]), "yaml", nil
			}
		}
	}
	// Check for TOML (+++)
	if strings.HasPrefix(str, "+++\n") || strings.HasPrefix(str, "+++\r\n") {
		parts := strings.SplitN(str, "+++", 3)
		if len(parts) == 3 {
			var fm map[string]interface{}
			if err := toml.Unmarshal([]byte(parts[1]), &fm); err == nil {
				return fm, strings.TrimSpace(parts[2]), "toml", nil
			}
		}
	}
	// Check for JSON ({)
	if strings.HasPrefix(strings.TrimSpace(str), "{") {
		var fm map[string]interface{}
		if err := json.Unmarshal(content, &fm); err == nil {
			return fm, "", "json", nil
		}
	}

	return nil, "", "", fmt.Errorf("unknown format")
}

func constructFileContent(fm map[string]interface{}, body string, format string) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case "yaml":
		buf.WriteString("---\n")
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(fm); err != nil {
			return nil, err
		}
		buf.WriteString("---\n")
	case "toml":
		buf.WriteString("+++\n")
		enc := toml.NewEncoder(&buf)
		if err := enc.Encode(fm); err != nil {
			return nil, err
		}
		buf.WriteString("+++\n")
	case "json":
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(fm); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	if body != "" {
		buf.WriteString("\n")
		buf.WriteString(body)
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}
