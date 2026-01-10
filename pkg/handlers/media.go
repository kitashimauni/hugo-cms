package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func ListMedia(c *gin.Context) {
	mode := c.Query("mode")
	articlePath := c.Query("path")
	files, err := services.ListMediaFiles(mode, articlePath)
	if err != nil {
		ErrorInternal(c, "Failed to list media: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, files)
}

func UploadMedia(c *gin.Context) {
	mode := c.PostForm("mode")
	articlePath := c.PostForm("path")
	file, err := c.FormFile("file")
	if err != nil {
		ErrorBadRequest(c, "No file uploaded")
		return
	}

	// Check file size
	if file.Size > config.MaxUploadSize {
		maxMB := config.MaxUploadSize / 1024 / 1024
		ErrorBadRequest(c, "File too large. Maximum size is "+strconv.FormatInt(maxMB, 10)+"MB")
		return
	}

	info, err := services.SaveMediaFile(file, mode, articlePath)
	if err != nil {
		ErrorInternal(c, "Failed to save file: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, info)
}

func DeleteMedia(c *gin.Context) {
	var req struct {
		RepoPath string `json:"repo_path"`
	}
	if err := c.BindJSON(&req); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}

	// Validate path is not empty and doesn't contain path traversal
	if req.RepoPath == "" {
		ErrorBadRequest(c, "repo_path is required")
		return
	}

	// Additional validation: must be within allowed directories (static or content)
	normalizedPath := filepath.ToSlash(filepath.Clean(req.RepoPath))
	if !strings.HasPrefix(normalizedPath, "static/") && !strings.HasPrefix(normalizedPath, "content/") {
		ErrorBadRequest(c, "Invalid media path: must be in static/ or content/")
		return
	}

	if err := services.DeleteMediaFile(req.RepoPath); err != nil {
		ErrorInternal(c, "Failed to delete: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func ServeMediaRaw(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		ErrorBadRequest(c, "Path parameter required")
		return
	}

	fullPath := services.SafeJoin(config.RepoPath, "", targetPath)
	if fullPath == "" {
		ErrorNotFound(c, "Invalid path")
		return
	}

	c.File(fullPath)
}
