package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// 設定
const (
	RepoPath   = "./repo"
	PublicPath = "./repo/public"
	PreviewURL = "/preview/"
)

var oauthConf *oauth2.Config

// 記事構造体
type Article struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content,omitempty"`
}

func main() {
	// .env 読み込み
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found or error loading it.")
	}

	// OAuth設定
	oauthConf = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		Scopes:       []string{"repo"}, // リポジトリ操作権限が必要
		Endpoint:     github.Endpoint,
		RedirectURL:  "https://cms.n-island.dev/auth/callback",
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
				var articles []Article
				contentDir := filepath.Join(RepoPath, "content")
				filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
						relPath, _ := filepath.Rel(contentDir, path)
						articles = append(articles, Article{Path: relPath, Title: relPath})
					}
					return nil
				})
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
				c.JSON(http.StatusOK, gin.H{"content": string(content)})
			})

			api.POST("/article", func(c *gin.Context) {
				var art Article
				if err := c.BindJSON(&art); err != nil {
					c.JSON(400, gin.H{"error": "Invalid JSON"})
					return
				}
				fullPath := safeJoin(RepoPath, "content", art.Path)
				if err := os.WriteFile(fullPath, []byte(art.Content), 0644); err != nil {
					c.JSON(500, gin.H{"error": "Save failed"})
					return
				}
				c.JSON(200, gin.H{"status": "saved"})
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
		// APIリクエストの場合は401、ブラウザならリダイレクト
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
	cmd := exec.Command("hugo",
		"--source", RepoPath,
		"--destination", "public",
		"--baseURL", "https://cms.n-island.dev"+PreviewURL,
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

	// git pull origin main
	// トークンを含めたURLでpullするのではなく、HTTPヘッダーにトークンを仕込む方式で実行
	err, log := executeGitWithToken(RepoPath, token, "pull", "origin", "main")
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": log})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": log})
}

func handlePublish(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token").(string)

	// 1. git add
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = RepoPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		c.JSON(500, gin.H{"error": "git add failed", "details": string(out)})
		return
	}

	// 2. git commit
	msg := fmt.Sprintf("Update via HomeCMS: %s", time.Now().Format("2006-01-02 15:04:05"))
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = RepoPath
	// Commitは変更がないとエラーになるが、続行してよい
	commitCmd.Run()

	// 3. git push
	err, log := executeGitWithToken(RepoPath, token, "push", "origin", "main")
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": log})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": log})
}

// Gitコマンドを認証付きで実行するヘルパー
// リモートURLにトークンを埋め込むとログに残るリスクがあるため、
// 現在のリモートURLを取得し、そこに認証情報を付加して一時的に使用するアプローチをとる
func executeGitWithToken(dir, token string, args ...string) (error, string) {
	// 1. リモートURLの取得 (origin)
	cmdGetUrl := exec.Command("git", "remote", "get-url", "origin")
	cmdGetUrl.Dir = dir
	outUrl, err := cmdGetUrl.Output()
	if err != nil {
		return err, "Failed to get remote url"
	}
	remoteUrl := strings.TrimSpace(string(outUrl))

	// 2. URLの解析とトークンの注入
	u, err := url.Parse(remoteUrl)
	if err != nil {
		return err, "Invalid remote url"
	}
	// https://github.com/user/repo.git -> https://oauth2:TOKEN@github.com/user/repo.git
	u.User = url.UserPassword("oauth2", token)
	authenticatedUrl := u.String()

	// 3. 引数の書き換え
	// argsの中に "origin" があれば、認証付きURLに置き換える
	// 例: "pull", "origin", "main" -> "pull", "https://...", "main"
	newArgs := make([]string, len(args))
	copy(newArgs, args)
	for i, v := range newArgs {
		if v == "origin" {
			newArgs[i] = authenticatedUrl
		}
	}

	// 4. 実行
	cmd := exec.Command("git", newArgs...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	
	// 5. ログのサニタイズ (トークンを隠す)
	safeLog := strings.ReplaceAll(string(output), token, "***")
	safeLog = strings.ReplaceAll(safeLog, authenticatedUrl, remoteUrl) // URL全体も元のものに戻して表示

	return err, safeLog
}

func safeJoin(root, sub, target string) string {
	cleanTarget := filepath.Clean(target)
	if strings.Contains(cleanTarget, "..") {
		return ""
	}
	return filepath.Join(root, sub, cleanTarget)
}