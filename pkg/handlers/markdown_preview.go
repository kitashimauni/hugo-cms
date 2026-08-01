package handlers

import (
	"errors"
	"hugo-cms/pkg/services"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type markdownPreviewRequest struct {
	Path        string                 `json:"path"`
	Body        string                 `json:"body"`
	Frontmatter map[string]interface{} `json:"frontmatter"`
}

func RenderMarkdownPreview(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if !runtime.MarkdownPreviewEnabled {
		ErrorConflict(c, "Markdown preview is disabled for this site")
		return
	}

	var req markdownPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}
	if strings.ToLower(filepath.Ext(req.Path)) != ".md" || services.SafeJoin(runtime.RepoPath, runtime.ContentDir, req.Path) == "" {
		ErrorBadRequest(c, "Invalid article path")
		return
	}

	rendered, err := services.RenderMarkdownPreview(runtime, req.Path, req.Body, req.Frontmatter)
	if errors.Is(err, services.ErrMarkdownPreviewTooLarge) {
		RespondError(c, http.StatusRequestEntityTooLarge, ErrCodeBadRequest, "Markdown document is too large to preview")
		return
	}
	if err != nil {
		ErrorInternal(c, "Failed to render Markdown preview")
		return
	}
	c.JSON(http.StatusOK, gin.H{"html": rendered})
}
