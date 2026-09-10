package handlers

import (
	"context"
	"hugo-cms/pkg/services"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

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
	workspace, active := workspaceManager.Status(runtime.ID)

	processState := services.LocalPreviewStopped
	processError := ""
	if slot, ok := services.DefaultLocalPreviewManager().Status(runtime.ID); ok {
		processState = slot.State
		processError = slot.Error
	}

	response := gin.H{
		"enabled":          true,
		"status":           string(processState),
		"process_state":    processState,
		"process_error":    processError,
		"preview_url":      runtime.LocalPreview.URL,
		"workspace_active": active,
	}
	if active && !workspace.LastActivityAt.IsZero() {
		age := time.Since(workspace.LastActivityAt)
		if age < 0 {
			age = 0
		}
		response["last_activity_age_seconds"] = int(age.Seconds())
	}
	c.JSON(http.StatusOK, response)
}

func StopLocalPreview(c *gin.Context) {
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
	cleanup, claimed, err := workspaceManager.BeginCleanup(runtime.ID)
	if err != nil {
		ErrorInternal(c, "Failed to prepare Local Live Preview cleanup")
		return
	}
	if !claimed {
		ErrorInternal(c, "Failed to prepare Local Live Preview cleanup")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), services.DefaultLocalPreviewStopTimeout)
	stopErr := services.DefaultLocalPreviewManager().Stop(ctx, runtime.ID)
	cancel()
	if stopErr != nil {
		slog.Error("Failed to stop Local Live Preview process", "site", runtime.ID, "error", stopErr)
		workspaceManager.CancelCleanup(&cleanup)
		ErrorInternal(c, "Failed to stop Local Live Preview process")
		return
	}
	if _, err := workspaceManager.FinishCleanup(&cleanup); err != nil {
		workspaceManager.CancelCleanup(&cleanup)
		slog.Error("Failed to detach Local Live Preview workspace", "site", runtime.ID, "error", err)
		ErrorInternal(c, "Failed to clean up Local Live Preview workspace")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}
