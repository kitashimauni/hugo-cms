package handlers

import (
	"encoding/json"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"hugo-cms/pkg/services"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func HandleBuild(c *gin.Context) {
	// With Hugo Server running, explicit build is not needed for preview.
	// We just return OK so frontend logic continues.
	RespondOK(c, "Preview managed by Hugo Server")
}

func HandleRestart(c *gin.Context) {
	if err := services.RestartHugoServer(); err != nil {
		ErrorInternal(c, "Failed to restart Hugo server: "+err.Error())
		return
	}
	RespondOK(c, "Hugo Server Restarted")
}

func HandleSync(c *gin.Context) {
	session := sessions.Default(c)
	token, ok := session.Get("access_token").(string)
	if !ok {
		ErrorUnauthorized(c, "Invalid session token")
		return
	}
	log, err := services.SyncRepo(token)

	if err != nil {
		ErrorInternal(c, "Sync failed: "+log)
		return
	}
	RespondOK(c, log)
}

func HandlePublish(c *gin.Context) {
	session := sessions.Default(c)
	token, ok := session.Get("access_token").(string)
	if !ok {
		ErrorUnauthorized(c, "Invalid session token")
		return
	}

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

		// Verify file exists before passing to git
		fullPath := services.SafeJoin(config.RepoPath, "content", req.Path)
		if fullPath == "" {
			ErrorBadRequest(c, "Invalid path")
			return
		}
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			ErrorNotFound(c, "File does not exist")
			return
		}
	}

	log, err := services.PublishChanges(token, gitPath)
	if err != nil {
		ErrorInternal(c, "Publish failed: "+log)
		return
	}
	RespondOK(c, log)
}

func ListArticles(c *gin.Context) {
	articles, err := services.GetArticlesCache()
	if err != nil {
		ErrorInternal(c, "Failed to fetch articles")
		return
	}
	c.JSON(http.StatusOK, articles)
}

func GetArticle(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		ErrorBadRequest(c, "Path parameter is required")
		return
	}

	fullPath := services.SafeJoin(config.RepoPath, "content", targetPath)
	if fullPath == "" {
		ErrorBadRequest(c, "Invalid path")
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		ErrorNotFound(c, "File not found")
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
		ErrorBadRequest(c, "Invalid JSON")
		return
	}

	if art.Path == "" {
		ErrorBadRequest(c, "Path is required")
		return
	}

	fullPath := services.SafeJoin(config.RepoPath, "content", art.Path)
	if fullPath == "" {
		ErrorBadRequest(c, "Invalid path")
		return
	}

	unlock := services.LockRepositoryOperation()
	defer unlock()

	var finalContent []byte
	var err error

	if art.FrontMatter != nil {
		finalContent, err = services.ConstructFileContent(art.FrontMatter, art.Body, art.Format)
		if err != nil {
			ErrorInternal(c, "Failed to construct file content: "+err.Error())
			return
		}
	} else {
		finalContent = []byte(art.Content)
	}

	if err := os.WriteFile(fullPath, finalContent, 0644); err != nil {
		ErrorInternal(c, "Save failed")
		return
	}

	services.UpdateCache(art.Path)
	c.JSON(http.StatusOK, gin.H{"status": "saved"})
}

func CreateArticle(c *gin.Context) {
	var req struct {
		Path       string                 `json:"path"`
		Content    string                 `json:"content"`
		Collection string                 `json:"collection"`
		Fields     map[string]interface{} `json:"fields"`
	}
	if err := c.BindJSON(&req); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}

	unlock := services.LockRepositoryOperation()
	defer unlock()

	// New logic: Collection-based creation
	if req.Collection != "" {
		cmsConfig, err := services.GetCMSConfig()
		if err != nil {
			ErrorInternal(c, "Failed to load CMS config")
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
			ErrorNotFound(c, "Collection not found: "+req.Collection)
			return
		}

		// Resolve Path
		relPath, err := services.ResolvePath(*targetCollection, req.Fields)
		if err != nil {
			ErrorInternal(c, "Failed to resolve path: "+err.Error())
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
			ErrorBadRequest(c, "Invalid resolved path")
			return
		}

		// Check if file exists
		if _, err := os.Stat(fullPath); err == nil {
			ErrorConflict(c, "File already exists")
			return
		}

		// Generate Content
		content, err := services.GenerateContentFromCollection(*targetCollection, req.Fields)
		if err != nil {
			ErrorInternal(c, "Failed to generate content: "+err.Error())
			return
		}

		// Write File
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			ErrorInternal(c, "Failed to create directory")
			return
		}

		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			ErrorInternal(c, "Failed to write file")
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
		c.JSON(http.StatusOK, gin.H{"status": "created", "path": contentRelPath})
		return
	}

	// Legacy/Direct path logic
	if req.Path == "" || strings.Contains(req.Path, "..") {
		ErrorBadRequest(c, "Invalid path")
		return
	}

	log, err := services.CreateContent(req.Path)
	if err != nil {
		if os.IsExist(err) {
			ErrorConflict(c, log)
		} else {
			ErrorInternal(c, "Hugo new failed: "+log)
		}
		return
	}

	services.UpdateCache(req.Path)
	c.JSON(http.StatusOK, gin.H{"status": "created", "log": log})
}

func GetDiff(c *gin.Context) {
	var art models.Article
	if err := c.BindJSON(&art); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}

	if art.Path == "" {
		ErrorBadRequest(c, "Path is required")
		return
	}

	fullPath := services.SafeJoin(config.RepoPath, "content", art.Path)
	if fullPath == "" {
		ErrorBadRequest(c, "Invalid path")
		return
	}

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
			ErrorInternal(c, "Construction failed")
			return
		}
	} else {
		newContent = []byte(art.Content)
	}

	newContent = services.NormalizeContent(newContent, collection)

	tmpDir := os.TempDir()
	f1, err := os.CreateTemp(tmpDir, "diff_old_*")
	if err != nil {
		ErrorInternal(c, "Failed to create temp file")
		return
	}
	defer os.Remove(f1.Name())

	f2, err := os.CreateTemp(tmpDir, "diff_new_*")
	if err != nil {
		ErrorInternal(c, "Failed to create temp file")
		return
	}
	defer os.Remove(f2.Name())

	if _, err := f1.Write(currentContent); err != nil {
		ErrorInternal(c, "Failed to write temp file")
		return
	}
	if _, err := f2.Write(newContent); err != nil {
		ErrorInternal(c, "Failed to write temp file")
		return
	}
	f1.Close()
	f2.Close()

	relPath := filepath.Join("content", art.Path)
	diffStr, diffType := services.Diff(f1.Name(), f2.Name(), relPath)

	c.JSON(http.StatusOK, gin.H{"diff": diffStr, "type": diffType})
}

func DeleteArticle(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.BindJSON(&req); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}

	if req.Path == "" || strings.Contains(req.Path, "..") {
		ErrorBadRequest(c, "Invalid path")
		return
	}

	unlock := services.LockRepositoryOperation()
	defer unlock()

	if err := services.DeleteFile(req.Path); err != nil {
		ErrorInternal(c, "Delete failed: "+err.Error())
		return
	}

	// Re-scan or remove from cache
	// Assuming UpdateCache handles re-scan or we'll fix it
	services.UpdateCache(req.Path)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func GetConfig(c *gin.Context) {
	cfg, err := services.GetConfig()
	if err != nil {
		ErrorInternal(c, "Failed to parse config")
		return
	}
	c.JSON(http.StatusOK, cfg)
}

type SnippetDef struct {
	Prefix      interface{} `json:"prefix"`
	Body        interface{} `json:"body"`
	Description string      `json:"description"`
	Scope       string      `json:"scope,omitempty"`
}

func GetSnippets(c *gin.Context) {
	allSnippets := make(map[string]SnippetDef)

	for _, path := range config.SnippetPaths {
		// Clean the path
		path = filepath.Clean(path)

		content, err := os.ReadFile(path)
		if err != nil {
			// Skip missing or unreadable files
			continue
		}

		// Simple sanitization for JSONC (VS Code snippets)
		sanitized := sanitizeJSONC(string(content))

		var fileSnippets map[string]SnippetDef
		if err := json.Unmarshal([]byte(sanitized), &fileSnippets); err != nil {
			// Skip malformed files
			continue
		}

		for name, snippet := range fileSnippets {
			// Scope check
			// If scope is defined, it must contain "markdown"
			if snippet.Scope != "" {
				scopes := strings.Split(snippet.Scope, ",")
				isMarkdown := false
				for _, s := range scopes {
					if strings.TrimSpace(s) == "markdown" {
						isMarkdown = true
						break
					}
				}
				if !isMarkdown {
					continue
				}
			}

			// Add to map (last one wins if duplicate names)
			allSnippets[name] = snippet
		}
	}

	c.JSON(http.StatusOK, allSnippets)
}

// sanitizeJSONC is a simple helper to remove comments and trailing commas.
func sanitizeJSONC(src string) string {
	var out []rune
	var inString bool
	var escaped bool

	// Convert to runes for iteration
	runes := []rune(src)
	length := len(runes)

	for i := 0; i < length; i++ {
		c := runes[i]

		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
			} else {
				if c == '\\' {
					escaped = true
				} else if c == '"' {
					inString = false
				}
			}
			continue
		}

		// Check for comments
		if c == '/' && i+1 < length {
			next := runes[i+1]
			if next == '/' {
				// Single line comment: skip until newline
				i += 2
				for i < length && runes[i] != '\n' {
					i++
				}
				// Keep the newline
				if i < length {
					out = append(out, '\n')
				}
				continue
			} else if next == '*' {
				// Block comment: skip until */
				i += 2
				for i+1 < length && !(runes[i] == '*' && runes[i+1] == '/') {
					i++
				}
				i++ // skip '/'
				continue
			}
		}

		out = append(out, c)
	}

	res := string(out)

	// Remove trailing commas using Regex
	// We matched comment-stripped text. Now we remove commas before closing braces.
	// We must avoid matching inside strings.
	// But we already preserved strings in `out`.
	// Since we are doing a quick fix and valid JSON rarely has `,\s*}` inside a string,
	// we will use regex. A robust parser is better but complex.

	// Regex to remove trailing commas: ,(?:\s*)([}\]])
	re := regexp.MustCompile(`,(\s*[}\]])`)
	return re.ReplaceAllString(res, "$1")
}
