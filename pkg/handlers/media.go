package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ListMedia(c *gin.Context) {
	collection := c.Query("collection")
	files, err := services.ListMediaFiles(collection)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list media: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, files)
}

func UploadMedia(c *gin.Context) {
	collection := c.PostForm("collection")
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	info, err := services.SaveMediaFile(file, collection)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

func DeleteMedia(c *gin.Context) {
	var req struct {
		Filename   string `json:"filename"`
		Collection string `json:"collection"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	if err := services.DeleteMediaFile(req.Filename, req.Collection); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func ServeMediaRaw(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	// Assuming path is relative to RepoPath.
	// We pass "" as sub-folder to allow access to static/ or content/ via SafeJoin logic
	fullPath := services.SafeJoin(config.RepoPath, "", targetPath)
	if fullPath == "" {
		c.Status(http.StatusNotFound)
		return
	}

	c.File(fullPath)
}