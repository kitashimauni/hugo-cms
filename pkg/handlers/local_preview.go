package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// LocalPreviewIngress routes requests in the configured preview hostname
// namespace before the CMS session/auth middleware runs. Viewer authentication
// belongs to the external preview ingress (for example Tailscale or Cloudflare
// Access); the CMS admin session cookie is deliberately not reused here.
func LocalPreviewIngress(manager *services.LocalPreviewManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.IsLocalPreviewHostCandidate(c.Request.Host) {
			return
		}

		site, err := config.ResolveLocalPreviewHost(c.Request.Host)
		if err != nil {
			slog.Warn("Rejected local preview host", "host", c.Request.Host, "error", err)
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if manager == nil {
			slog.Error("Local preview manager is not configured", "site", site.ID)
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}

		// Phase 3 keeps unsaved editor content outside the production working
		// tree. If this site has an active shadow workspace, point Hugo's
		// contentDir at it while leaving configuration/theme/layout/static files
		// rooted in the original repository.
		if workspaceManager, workspaceErr := services.DefaultLocalPreviewWorkspaceManager(); workspaceErr == nil {
			if workspace, ok := workspaceManager.Active(site.ID); ok {
				site.ContentDir = workspace.ContentDir
			}
		} else {
			slog.Warn("Local preview shadow workspace unavailable; serving saved content", "site", site.ID, "error", workspaceErr)
		}

		if err := manager.Proxy(c.Writer, c.Request, site); err != nil {
			slog.Error("Local preview proxy failed", "site", site.ID, "error", err)
			if !c.Writer.Written() {
				c.AbortWithStatus(http.StatusBadGateway)
			} else {
				c.Abort()
			}
			return
		}
		c.Abort()
	}
}
