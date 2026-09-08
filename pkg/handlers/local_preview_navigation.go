package handlers

import (
	"errors"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
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
	TouchArticle(runtime config.SiteRuntime, draftID, articlePath string) (services.LocalPreviewWorkspace, error)
}

type localPreviewNavigationDependencies struct {
	workspaceManager localPreviewNavigationWorkspaceManager
	ensureReady      func(config.SiteConfig) error
}

// NavigateLocalPreview starts Hugo when necessary and touches the selected
// shadow article after the process is ready. Hugo's --navigateToChanged then
// resolves the actual page URL, including slugs, permalinks and bundles.
func NavigateLocalPreview(c *gin.Context) {
	navigateLocalPreview(c, localPreviewNavigationDependencies{
		ensureReady: func(site config.SiteConfig) error {
			_, err := services.DefaultLocalPreviewManager().EnsureReady(site)
			return err
		},
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
	if generator := strings.TrimSpace(runtime.Generator); generator != "" && !strings.EqualFold(generator, "hugo") {
		ErrorConflict(c, "Local Live Preview currently supports Hugo only")
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

	site := runtime.SiteConfig()
	site.ContentDir = workspace.ContentDir
	if dependencies.ensureReady == nil {
		ErrorInternal(c, "Local preview manager is unavailable")
		return
	}
	if err := dependencies.ensureReady(site); err != nil {
		ErrorInternal(c, "Failed to start Local Live Preview")
		return
	}
	workspace, err = workspaceManager.TouchArticle(runtime, req.DraftID, req.Path)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrLocalPreviewSessionConflict),
			errors.Is(err, services.ErrLocalPreviewSessionMismatch),
			errors.Is(err, services.ErrLocalPreviewSessionNotFound),
			errors.Is(err, services.ErrLocalPreviewSessionExpired),
			errors.Is(err, services.ErrLocalPreviewSessionReclaiming):
			ErrorConflict(c, err.Error())
		default:
			ErrorBadRequest(c, err.Error())
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "navigated",
		"revision":     workspace.Revision,
		"preview_url":  runtime.LocalPreview.URL,
		"session_id":   req.DraftID,
		"article_path": workspace.ArticlePath,
	})
}
