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
	// With Hugo Server running, explicit build is not needed for preview.
	// We just return OK so frontend logic continues.
	c.JSON(200, gin.H{"status": "ok", "log": "Preview managed by Hugo Server"})
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

	var req struct {
		Path string `json:"path"`
	}
	// Try to bind JSON. If it fails (e.g. empty body), we assume full publish (Path="")
	c.ShouldBindJSON(&req)

	gitPath := ""
	if req.Path != "" {
		// Convert content-relative path to repo-relative path
		// e.g. "posts/abc.md" -> "content/posts/abc.md"
		// We use Join to be OS agnostic, but git expects forward slashes.
		// git.go's PublishChanges might need to handle ToSlash, but let's do it here.
		gitPath = filepath.ToSlash(filepath.Join("content", req.Path))
	}

	err, log := services.PublishChanges(token, gitPath)
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

	services.UpdateCache(art.Path)
	c.JSON(200, gin.H{"status": "saved"})
}

func CreateArticle(c *gin.Context) {
	var req struct {
		Path       string                 `json:"path"`
		Content    string                 `json:"content"`
		Collection string                 `json:"collection"`
		Fields     map[string]interface{} `json:"fields"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	// New logic: Collection-based creation
	if req.Collection != "" {
		cmsConfig, err := services.GetCMSConfig()
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to load CMS config"})
			return
		}

		var targetCollection *models.Collection
		for _, col := range cmsConfig.Collections {
			if col.Name == req.Collection {
				targetCollection = &col
				break
			}
		}

		if targetCollection == nil {
			c.JSON(400, gin.H{"error": "Collection not found"})
			return
		}

		// Resolve Path
		relPath, err := services.ResolvePath(*targetCollection, req.Fields)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to resolve path: " + err.Error()})
			return
		}

		// Prepend collection folder if ResolvePath returned relative path without it?
		// ResolvePath returns path relative to collection folder? No, I implemented it to just return the filename/subpath based on pattern.
		// Wait, `GenerateContentFromCollection` returns content.
		// I need to join collection folder with resolved path.
		// `ResolvePath` implementation: just replaces {{...}} in `collection.Path`.
		// `collection.Path` in config example: `{{year}}.../index`.
		// `collection.Folder` is `content/posts`.
		// So full path is `content/posts/{{year}}.../index.md`.

		fullPath := services.SafeJoin(config.RepoPath, targetCollection.Folder, relPath)
		if fullPath == "" {
			c.JSON(400, gin.H{"error": "Invalid resolved path"})
			return
		}

		// Check if file exists
		if _, err := os.Stat(fullPath); err == nil {
			c.JSON(409, gin.H{"error": "File already exists"})
			return
		}

		// Generate Content
		content, err := services.GenerateContentFromCollection(*targetCollection, req.Fields)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to generate content: " + err.Error()})
			return
		}

		// Write File
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			c.JSON(500, gin.H{"error": "Failed to create directory"})
			return
		}

		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			c.JSON(500, gin.H{"error": "Failed to write file"})
			return
		}

		// Update Cache (we need the path relative to content dir for cache update usually?
		// services.UpdateCache takes "path". Existing CreateContent calls UpdateCache(req.Path).
		// CreateContent receives path relative to `content` usually?
		// `hugo new content path/to/file`.
		// Here `relPath` is relative to `collection.Folder`.
		// `collection.Folder` is e.g. `content/posts`.
		// So cache path should be `posts/` + `relPath`?
		// `SafeJoin` combined `config.RepoPath`, `targetCollection.Folder`, `relPath`.
		// `targetCollection.Folder` usually includes `content/`.
		// Let's deduce the content-relative path.

		contentRelPath, _ := filepath.Rel(filepath.Join(config.RepoPath, "content"), fullPath)
		// normalize slashes
		contentRelPath = filepath.ToSlash(contentRelPath)

		services.UpdateCache(contentRelPath)
		c.JSON(200, gin.H{"status": "created", "path": contentRelPath})
		return
	}

	// Legacy/Direct path logic
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

	services.UpdateCache(req.Path)
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

	// Apply defaults for normalization
	collectionPath := filepath.Join("content", art.Path)
	collection, _ := services.GetCollectionForPath(collectionPath)
	currentContent = services.NormalizeContent(currentContent, collection)

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

	newContent = services.NormalizeContent(newContent, collection)

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

func DeleteArticle(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	if req.Path == "" || strings.Contains(req.Path, "..") {
		c.JSON(400, gin.H{"error": "Invalid path"})
		return
	}

	if err := services.DeleteFile(req.Path); err != nil {
		c.JSON(500, gin.H{"error": "Delete failed: " + err.Error()})
		return
	}

	// Re-scan or remove from cache
	// Assuming UpdateCache handles re-scan or we'll fix it
	services.UpdateCache(req.Path)
	c.JSON(200, gin.H{"status": "deleted"})
}

func GetConfig(c *gin.Context) {
	cfg, err := services.GetConfig()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to parse config"})
		return
	}
	c.JSON(http.StatusOK, cfg)
}
