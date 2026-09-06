package handlers

import (
	"errors"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func ListMedia(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	mode := c.Query("mode")
	articlePath := c.Query("path")
	files, err := services.ListMediaFilesForRuntime(runtime, mode, articlePath)
	if err != nil {
		if errors.Is(err, services.ErrInvalidMedia) {
			ErrorBadRequest(c, "Invalid media request")
			return
		}
		ErrorInternal(c, "Failed to list media: "+err.Error())
		return
	}
	for i := range files {
		files[i].URL = addSiteQuery(files[i].URL, runtime.ID)
	}
	c.JSON(http.StatusOK, files)
}

func UploadMedia(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	mode := c.PostForm("mode")
	articlePath := c.PostForm("path")
	file, err := c.FormFile("file")
	if err != nil {
		if c.Request.Body != nil && strings.Contains(err.Error(), "request body too large") {
			RespondError(c, http.StatusRequestEntityTooLarge, ErrCodeBadRequest, "Upload request is too large")
			return
		}
		ErrorBadRequest(c, "No file uploaded")
		return
	}

	// Check file size
	if file.Size > config.MaxUploadSize {
		maxMB := config.MaxUploadSize / 1024 / 1024
		ErrorBadRequest(c, "File too large. Maximum size is "+strconv.FormatInt(maxMB, 10)+"MB")
		return
	}

	info, err := services.SaveMediaFileForRuntime(runtime, file, mode, articlePath)
	if err != nil {
		if errors.Is(err, services.ErrInvalidMedia) {
			ErrorBadRequest(c, err.Error())
			return
		}
		ErrorInternal(c, "Failed to save file: "+err.Error())
		return
	}
	syncLocalPreviewContentResource(runtime, info.RepoPath, false)
	info.URL = addSiteQuery(info.URL, runtime.ID)

	c.JSON(http.StatusOK, info)
}

func DeleteMedia(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
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

	if !services.ValidateMediaRepoPathForRuntime(runtime, req.RepoPath) {
		ErrorBadRequest(c, "Invalid media path")
		return
	}

	if err := services.DeleteMediaFileForRuntime(runtime, req.RepoPath); err != nil {
		ErrorInternal(c, "Failed to delete: "+err.Error())
		return
	}
	syncLocalPreviewContentResource(runtime, req.RepoPath, true)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func syncLocalPreviewContentResource(runtime config.SiteRuntime, repoPath string, deleted bool) {
	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		slog.Warn("Local preview workspace unavailable during media sync", "site", runtime.ID, "error", err)
		return
	}
	if _, err := workspaceManager.SyncContentResource(runtime, repoPath, deleted); err != nil {
		// The media operation already succeeded in the production workspace. Do
		// not turn a preview-only synchronization failure into a misleading media
		// retry; log it and let the next workspace rebuild recover the resource.
		slog.Warn("Failed to synchronize media into Local Live Preview", "site", runtime.ID, "path", repoPath, "error", err)
	}
}

func ServeMediaRaw(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	targetPath := c.Query("path")
	if targetPath == "" {
		ErrorBadRequest(c, "Path parameter required")
		return
	}
	if !services.ValidateMediaRepoPathForRuntime(runtime, targetPath) {
		ErrorNotFound(c, "Invalid media path")
		return
	}

	fullPath := services.SafeJoin(runtime.RepoPath, "", targetPath)
	if fullPath == "" {
		ErrorNotFound(c, "Invalid path")
		return
	}

	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, max-age=300")
	c.File(fullPath)
}
