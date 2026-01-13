package handlers

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"

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

	// Check 1: Hugo server is running
	hugoHealthy := services.IsHugoServerRunning()
	checks["hugo_server"] = gin.H{
		"healthy": hugoHealthy,
		"message": getHugoStatusMessage(hugoHealthy),
	}
	if !hugoHealthy {
		allHealthy = false
	}

	// Check 2: Content directory is accessible
	contentDir := config.RepoPath + "/content"
	contentAccessible := isDirAccessible(contentDir)
	checks["content_dir"] = gin.H{
		"healthy": contentAccessible,
		"path":    contentDir,
	}
	if !contentAccessible {
		allHealthy = false
	}

	// Check 3: Git repository status
	gitHealthy := isGitRepoHealthy(config.RepoPath)
	checks["git_repo"] = gin.H{
		"healthy": gitHealthy,
	}
	if !gitHealthy {
		allHealthy = false
	}

	// System info
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	response := gin.H{
		"status":    getOverallStatus(allHealthy),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    time.Since(startTime).String(),
		"checks":    checks,
		"system": gin.H{
			"goroutines":   runtime.NumGoroutine(),
			"memory_alloc": memStats.Alloc / 1024 / 1024, // MB
		},
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

func getHugoStatusMessage(running bool) string {
	if running {
		return "Hugo server is running"
	}
	return "Hugo server is not running"
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
