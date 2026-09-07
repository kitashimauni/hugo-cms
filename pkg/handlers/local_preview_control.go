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

type localPreviewControlRequest struct {
	DraftID string `json:"draft_id"`
}

func GetLocalPreviewStatus(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	enabled := runtime.LocalPreview.Enabled != nil && *runtime.LocalPreview.Enabled
	if !enabled {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false,
			"status":  "disabled",
		})
		return
	}

	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		ErrorInternal(c, "Local preview workspace is unavailable")
		return
	}
	workspace, active, stale := workspaceManager.Status(runtime.ID)
	draftID := strings.TrimSpace(c.Query("draft_id"))
	owned := active && draftID != "" && workspace.DraftID == draftID

	processState := services.LocalPreviewStopped
	processError := ""
	if slot, ok := services.DefaultLocalPreviewManager().Status(runtime.ID); ok {
		processState = slot.State
		processError = slot.Error
	}

	status := string(processState)
	if stale {
		status = "stale"
	} else if active && draftID != "" && !owned {
		status = "conflict"
	}

	response := gin.H{
		"enabled":           true,
		"status":            status,
		"process_state":     processState,
		"process_error":     processError,
		"preview_url":       runtime.LocalPreview.URL,
		"session_active":    active,
		"session_owned":     owned,
		"session_stale":     stale,
		"lease_seconds":     int(workspaceManager.LeaseTTL().Seconds()),
		"has_current_owner": active && !stale,
	}
	if active && !workspace.LastSeenAt.IsZero() {
		age := time.Since(workspace.LastSeenAt)
		if age < 0 {
			age = 0
		}
		response["last_seen_age_seconds"] = int(age.Seconds())
	}
	c.JSON(http.StatusOK, response)
}

func HeartbeatLocalPreviewContent(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	var req localPreviewControlRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.DraftID) == "" {
		ErrorBadRequest(c, "draft_id is required")
		return
	}
	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		ErrorInternal(c, "Local preview workspace is unavailable")
		return
	}
	workspace, err := workspaceManager.Heartbeat(runtime.ID, req.DraftID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrLocalPreviewSessionConflict),
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
		"status":   "active",
		"revision": workspace.Revision,
	})
}

func StopLocalPreview(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	var req localPreviewControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorBadRequest(c, "Invalid JSON")
		return
	}

	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		ErrorInternal(c, "Local preview workspace is unavailable")
		return
	}
	workspace, active := workspaceManager.Active(runtime.ID)
	if active {
		if strings.TrimSpace(req.DraftID) == "" || workspace.DraftID != req.DraftID {
			ErrorConflict(c, services.ErrLocalPreviewSessionConflict.Error())
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	stopErr := services.DefaultLocalPreviewManager().Stop(ctx, runtime.ID)
	cancel()
	if stopErr != nil {
		ErrorInternal(c, "Failed to stop Local Live Preview process")
		return
	}
	if active {
		if _, err := workspaceManager.Release(runtime.ID, req.DraftID); err != nil {
			if errors.Is(err, services.ErrLocalPreviewSessionConflict) || errors.Is(err, services.ErrLocalPreviewSessionReclaiming) {
				ErrorConflict(c, err.Error())
				return
			}
			ErrorBadRequest(c, err.Error())
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func ReclaimStaleLocalPreview(c *gin.Context) {
	runtime, err := requestedRuntime(c)
	if err != nil {
		ErrorBadRequest(c, err.Error())
		return
	}
	workspaceManager, err := services.DefaultLocalPreviewWorkspaceManager()
	if err != nil {
		ErrorInternal(c, "Local preview workspace is unavailable")
		return
	}

	claim, claimed, err := workspaceManager.ClaimStale(runtime.ID)
	if err != nil {
		if errors.Is(err, services.ErrLocalPreviewSessionNotStale) || errors.Is(err, services.ErrLocalPreviewSessionReclaiming) {
			ErrorConflict(c, err.Error())
			return
		}
		ErrorBadRequest(c, err.Error())
		return
	}
	if !claimed {
		c.JSON(http.StatusOK, gin.H{"status": "stopped", "reclaimed": false})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	stopErr := services.DefaultLocalPreviewManager().Stop(ctx, runtime.ID)
	cancel()
	if stopErr != nil {
		workspaceManager.CancelReclaim(claim)
		ErrorInternal(c, "Failed to stop stale Local Live Preview process")
		return
	}
	reclaimed, err := workspaceManager.FinishReclaim(claim)
	if err != nil {
		workspaceManager.CancelReclaim(claim)
		if errors.Is(err, services.ErrLocalPreviewSessionConflict) || errors.Is(err, services.ErrLocalPreviewSessionReclaiming) {
			ErrorConflict(c, err.Error())
			return
		}
		ErrorBadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped", "reclaimed": reclaimed})
}
