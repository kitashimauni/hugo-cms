package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"hugo-cms/pkg/config"

	"github.com/gin-gonic/gin"
)

var startTime = time.Now()

// HealthCheck returns basic health status of the server
// This endpoint is always available, even if dependencies are unhealthy
func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    time.Since(startTime).String(),
		"version":   "1.0.0",
	})
}

// ReadinessCheck performs deeper health checks on dependencies
// Returns 503 if any critical dependency is unhealthy
func ReadinessCheck(c *gin.Context) {
	checks := make(map[string]interface{})
	allHealthy := true
	runtime := config.CurrentSiteRuntime()

	// Check 1: Content directory is accessible
	contentDir := filepath.Join(runtime.RepoPath, runtime.ContentDir)
	contentAccessible := isDirAccessible(contentDir)
	checks["content_dir"] = gin.H{
		"healthy": contentAccessible,
	}
	if !contentAccessible {
		allHealthy = false
	}

	// Check 2: Git repository status
	gitHealthy := isGitRepoHealthy(runtime.RepoPath)
	checks["git_repo"] = gin.H{
		"healthy": gitHealthy,
	}
	if !gitHealthy {
		allHealthy = false
	}

	response := gin.H{
		"status":    getOverallStatus(allHealthy),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    time.Since(startTime).String(),
		"checks":    checks,
	}

	if allHealthy {
		c.JSON(http.StatusOK, response)
	} else {
		c.JSON(http.StatusServiceUnavailable, response)
	}
}

func getOverallStatus(healthy bool) string {
	if healthy {
		return "ok"
	}
	return "degraded"
}

func isDirAccessible(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func isGitRepoHealthy(repoPath string) bool {
	gitDir := repoPath + "/.git"
	info, err := os.Stat(gitDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}
