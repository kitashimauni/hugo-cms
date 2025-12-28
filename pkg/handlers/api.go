package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"hugo-cms/pkg/services"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func HandleBuild(c *gin.Context) {
	err, log := services.BuildSite()
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": log})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": log})
}

func HandleSync(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token").(string)
	err, log := services.SyncRepo(token)

	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": log})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": log})
}

func HandlePublish(c *gin.Context) {
	session := sessions.Default(c)
	token := session.Get("access_token").(string)
	err, log := services.PublishRepo(token)
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "log": log})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "log": log})
}

func ListArticles(c *gin.Context) {
	articles, err := services.GetArticlesCache()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to fetch articles"})
		return
	}
	c.JSON(http.StatusOK, articles)
}

func GetArticle(c *gin.Context) {
	targetPath := c.Query("path")
	fullPath := services.SafeJoin(config.RepoPath, "content", targetPath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		c.JSON(404, gin.H{"error": "File not found"})
		return
	}

	fm, body, format, err := services.ParseFrontMatter(content)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"content": string(content)})
		return
	}

	c.JSON(http.StatusOK, models.Article{
		Path:        targetPath,
		FrontMatter: fm,
		Body:        body,
		Format:      format,
	})
}

func SaveArticle(c *gin.Context) {
	var art models.Article
	if err := c.BindJSON(&art); err != nil {
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	fullPath := services.SafeJoin(config.RepoPath, "content", art.Path)
	var finalContent []byte
	var err error

	if art.FrontMatter != nil {
		finalContent, err = services.ConstructFileContent(art.FrontMatter, art.Body, art.Format)
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

	services.InvalidateCache()
	c.JSON(200, gin.H{"status": "saved"})
}

func CreateArticle(c *gin.Context) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	if req.Path == "" || strings.Contains(req.Path, "..") {
		c.JSON(400, gin.H{"error": "Invalid path"})
		return
	}

	err, log := services.CreateContent(req.Path)
	if err != nil {
		if os.IsExist(err) {
			c.JSON(409, gin.H{"error": log})
		} else {
			c.JSON(500, gin.H{"error": "Hugo new failed", "log": log})
		}
		return
	}

	c.JSON(200, gin.H{"status": "created", "log": log})
}

func GetDiff(c *gin.Context) {
	var art models.Article
	if err := c.BindJSON(&art); err != nil {
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	fullPath := services.SafeJoin(config.RepoPath, "content", art.Path)
	
	currentContent, err := os.ReadFile(fullPath)
	if err != nil {
		currentContent = []byte("")
	}

	if len(currentContent) > 0 {
		fm, body, format, err := services.ParseFrontMatter(currentContent)
		if err == nil {
			normalized, err := services.ConstructFileContent(fm, body, format)
			if err == nil {
				currentContent = normalized
			}
		}
	}

	var newContent []byte
	if art.FrontMatter != nil {
		newContent, err = services.ConstructFileContent(art.FrontMatter, art.Body, art.Format)
		if err != nil {
			c.JSON(500, gin.H{"error": "Construction failed"})
			return
		}
	} else {
		newContent = []byte(art.Content)
	}

	tmpDir := os.TempDir()
	f1, _ := os.CreateTemp(tmpDir, "diff_old_*")
	f2, _ := os.CreateTemp(tmpDir, "diff_new_*")
	defer os.Remove(f1.Name())
	defer os.Remove(f2.Name())

	f1.Write(currentContent)
	f2.Write(newContent)
	f1.Close()
	f2.Close()

	relPath := filepath.Join("content", art.Path)
	diffStr, diffType := services.Diff(f1.Name(), f2.Name(), relPath)
	
	c.JSON(200, gin.H{"diff": diffStr, "type": diffType})
}

func GetConfig(c *gin.Context) {
	cfg, err := services.GetConfig()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to parse config"})
		return
	}
	c.JSON(http.StatusOK, cfg)
}
