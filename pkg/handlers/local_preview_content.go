package handlers

import (
	"context"
	"errors"
	"hugo-cms/pkg/services"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type localPreviewContentRequest struct {
	DraftID     string                 `json:"draft_id"`
	Revision    uint64                 `json:"revision"`
	Path        string                 `json:"path"`
	Content     string                 `json:"content,omitempty"`
	FrontMatter map[string]interface{} `json:"frontmatter,omitempty"`
	Body        string                 `json:"body,omitempty"`
	Format      string                 `json:"format,omitempty"`
}

type localPreviewReleaseRequest struct {
	DraftID string `json:"draft_id"`
}

func UpdateLocalPreviewContent(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if runtime.LocalPreview.Enabled == nil || !*runtime.LocalPreview.Enabled {
		ErrorConflict(c, "Local Live Preview is disabled for this site")
		return
	}
	generator := strings.TrimSpace(runtime.Generator)
	if generator != "" && !strings.EqualFold(generator, "hugo") {
		ErrorConflict(c, "Local Live Preview currently supports Hugo only")
		return
	}

	var req localPreviewContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.Path) == "" || strings.TrimSpace(req.DraftID) == "" || req.Revision == 0 {
		ErrorBadRequest(c, "path, draft_id, and positive revision are required")
		return
	}

	var finalContent []byte
	if req.FrontMatter != nil {
		finalContent, err = services.ConstructFileContent(req.FrontMatter, req.Body, req.Format)
		if err != nil {
			ErrorBadRequest(c, "Failed to construct preview content: "+err.Error())
			return
		}
	} else {
		finalContent = []byte(req.Content)
	}

	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		ErrorInternal(c, "Local preview workspace is unavailable")
		return
	}
	workspace, created, applied, err := workspaceManager.Update(runtime, req.DraftID, req.Path, req.Revision, finalContent)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrLocalPreviewSessionConflict),
			errors.Is(err, services.ErrLocalPreviewSessionMismatch),
			errors.Is(err, services.ErrLocalPreviewSessionExpired),
			errors.Is(err, services.ErrLocalPreviewSessionReclaiming):
			ErrorConflict(c, err.Error())
		default:
			ErrorBadRequest(c, err.Error())
		}
		return
	}

	if created {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		stopErr := services.DefaultLocalPreviewManager().Stop(ctx, runtime.ID)
		cancel()
		if stopErr != nil {
			_, _ = workspaceManager.Release(runtime.ID, req.DraftID)
			ErrorInternal(c, "Failed to switch Local Live Preview to shadow content")
			return
		}
	}

	// session_id is returned only to the authenticated document that supplied
	// it. Status/recovery endpoints never disclose another tab's owner ID.
	c.JSON(http.StatusOK, gin.H{
		"status":      "updated",
		"applied":     applied,
		"revision":    workspace.Revision,
		"preview_url": runtime.LocalPreview.URL,
		"session_id":  req.DraftID,
	})
}

func ReleaseLocalPreviewContent(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	var req localPreviewReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.DraftID) == "" {
		ErrorBadRequest(c, "draft_id is required")
		return
	}

	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		ErrorInternal(c, "Local preview workspace is unavailable")
		return
	}
	workspace, active := workspaceManager.Active(runtime.ID)
	if !active {
		c.JSON(http.StatusOK, gin.H{"status": "released", "released": false})
		return
	}
	if workspace.DraftID != req.DraftID {
		ErrorConflict(c, services.ErrLocalPreviewSessionConflict.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	stopErr := services.DefaultLocalPreviewManager().Stop(ctx, runtime.ID)
	cancel()
	if stopErr != nil {
		ErrorInternal(c, "Failed to stop Local Live Preview process")
		return
	}

	released, err := workspaceManager.Release(runtime.ID, req.DraftID)
	if err != nil {
		if errors.Is(err, services.ErrLocalPreviewSessionConflict) || errors.Is(err, services.ErrLocalPreviewSessionReclaiming) {
			ErrorConflict(c, err.Error())
			return
		}
		ErrorBadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "released", "released": released})
}
