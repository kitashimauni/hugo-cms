package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/http"

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
