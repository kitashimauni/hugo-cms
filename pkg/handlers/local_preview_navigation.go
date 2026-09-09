package handlers

import (
	"context"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type localPreviewNavigateRequest struct {
	DraftID string `json:"draft_id"`
	Path    string `json:"path"`
}

type localPreviewNavigationWorkspaceManager interface {
	Status(siteID string) (services.LocalPreviewWorkspace, bool, bool)
}

type localPreviewNavigationDependencies struct {
	workspaceManager  localPreviewNavigationWorkspaceManager
	resolveArticleURL func(context.Context, config.SiteRuntime, services.LocalPreviewWorkspace, string) (string, error)
}

// NavigateLocalPreview resolves the selected shadow article through the
// configured generator and returns the URL to open in the local preview.
func NavigateLocalPreview(c *gin.Context) {
	navigateLocalPreview(c, localPreviewNavigationDependencies{
		resolveArticleURL: services.ResolvePreviewArticleURL,
	})
}

func navigateLocalPreview(c *gin.Context, dependencies localPreviewNavigationDependencies) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	if runtime.LocalPreview.Enabled == nil || !*runtime.LocalPreview.Enabled {
		ErrorConflict(c, "Local Live Preview is disabled for this site")
		return
	}
	var req localPreviewNavigateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}
	req.DraftID = strings.TrimSpace(req.DraftID)
	req.Path = filepath.Clean(strings.TrimSpace(req.Path))
	if req.DraftID == "" || req.Path == "." || filepath.IsAbs(req.Path) {
		ErrorBadRequest(c, "draft_id and a relative path are required")
		return
	}

	workspaceManager := dependencies.workspaceManager
	if workspaceManager == nil {
		workspaceManager, err = services.DefaultLocalPreviewWorkspaceManager()
		if err != nil {
			ErrorInternal(c, "Local preview workspace is unavailable")
			return
		}
	}
	workspace, active, stale := workspaceManager.Status(runtime.ID)
	if !active {
		ErrorConflict(c, services.ErrLocalPreviewSessionNotFound.Error())
		return
	}
	if stale {
		ErrorConflict(c, services.ErrLocalPreviewSessionExpired.Error())
		return
	}
	if workspace.DraftID != req.DraftID {
		ErrorConflict(c, services.ErrLocalPreviewSessionConflict.Error())
		return
	}
	if workspace.ArticlePath != filepath.ToSlash(req.Path) {
		ErrorConflict(c, services.ErrLocalPreviewSessionMismatch.Error())
		return
	}

	if dependencies.resolveArticleURL == nil {
		ErrorInternal(c, "Local preview URL resolver is unavailable")
		return
	}
	resolverRuntime := runtime
	resolverRuntime.ContentDir = workspace.ContentDir
	if workspace.ProjectDir != "" {
		resolverRuntime.LocalPreviewSourceRepoPath = resolverRuntime.RepoPath
		resolverRuntime.RepoPath = workspace.ProjectDir
		resolverRuntime.LocalPreviewProjectDir = workspace.ProjectDir
	}
	articleURL, err := dependencies.resolveArticleURL(c.Request.Context(), resolverRuntime, workspace, req.Path)
	if err != nil {
		slog.Error("Failed to resolve Local Live Preview article URL", "site", runtime.ID, "article_path", req.Path, "error", err)
		ErrorInternal(c, "Failed to resolve Local Live Preview article URL")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "resolved",
		"article_url":  articleURL,
		"revision":     workspace.Revision,
		"preview_url":  runtime.LocalPreview.URL,
		"session_id":   req.DraftID,
		"article_path": workspace.ArticlePath,
	})
}
