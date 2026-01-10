package services

import (
	"context"
	"fmt"
	"hugo-cms/pkg/config"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	hugoServerCmd *exec.Cmd
	hugoServerMu  sync.Mutex
)

func StartHugoServer() error {
	hugoServerMu.Lock()
	defer hugoServerMu.Unlock()

	if hugoServerCmd != nil && hugoServerCmd.Process != nil {
		// Check if process is still alive?
		// For simplicity assume if variable is set, it's running.
		// The goroutine below clears it on exit.
		return nil
	}

	slog.Info("Starting Hugo server", "port", config.HugoServerPort)

	cmd := exec.Command("hugo", "server",
		"--source", config.RepoPath,
		"--bind", config.HugoServerBind,
		"--port", config.HugoServerPort,
		"--baseURL", config.GetAppURL()+config.PreviewURL,
		"--appendPort=false",
		"--disableLiveReload", // Disable WS to avoid timeouts on mobile/proxy
		"-D",                  // Include drafts
		"-F",                  // Include future
	)

	// Pipe output to stdout for debugging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start hugo server: %w", err)
	}

	hugoServerCmd = cmd

	// Wait in goroutine
	go func() {
		state, err := cmd.Process.Wait()
		slog.Info("Hugo server stopped", "state", state, "error", err)
		hugoServerMu.Lock()
		hugoServerCmd = nil
		hugoServerMu.Unlock()
	}()

	return nil
}

func StopHugoServer() error {
	hugoServerMu.Lock()
	defer hugoServerMu.Unlock()

	if hugoServerCmd != nil && hugoServerCmd.Process != nil {
		slog.Info("Stopping Hugo server")
		if err := hugoServerCmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill hugo server: %w", err)
		}
		// Wait for process to exit
		time.Sleep(500 * time.Millisecond)
		hugoServerCmd = nil
	}
	return nil
}

func RestartHugoServer() error {
	if err := StopHugoServer(); err != nil {
		return err
	}
	return StartHugoServer()
}

// IsHugoServerRunning checks if the Hugo server process is currently running
func IsHugoServerRunning() bool {
	hugoServerMu.Lock()
	defer hugoServerMu.Unlock()
	return hugoServerCmd != nil && hugoServerCmd.Process != nil
}
func BuildSite() (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Hugo build completed", "duration", time.Since(start))
	}()

	// Use generous timeout for large sites (5 minutes)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "hugo",
		"--source", config.RepoPath,
		"--destination", "public",
		"--baseURL", config.GetAppURL()+config.PreviewURL,
		"--cleanDestinationDir",
		"-D",
		"-F",
	)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("hugo build timed out after 5 minutes")
	}
	return string(output), err
}

func CreateContent(path string) (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Hugo new content", "path", path, "duration", time.Since(start))
	}()

	// Check if file already exists
	fullPath := SafeJoin(config.RepoPath, "content", path)
	if _, err := os.Stat(fullPath); err == nil {
		return "File already exists", os.ErrExist
	}

	// Try to use CMS config first
	cmsConfig, err := GetCMSConfig()
	if err == nil {
		// path is like "posts/my-post.md"
		// collection.Folder is like "content/posts"
		relContentPath := filepath.Join("content", path)

		for _, collection := range cmsConfig.Collections {
			// Normalize paths for comparison
			collFolder := filepath.Clean(collection.Folder)
			targetFolder := filepath.Dir(relContentPath)

			// Check if target folder matches collection folder
			// or if target folder is inside collection folder (for nested structures)
			if strings.HasPrefix(targetFolder, collFolder) {
				content, err := GenerateContentFromCollection(collection, nil)
				if err == nil {
					// Ensure directory exists
					if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
						return "Failed to create directory", err
					}

					if err := os.WriteFile(fullPath, content, 0644); err != nil {
						return "Failed to write file", err
					}
					return "Created using CMS config", nil
				}
				// If generation fails, fall through to hugo new
				slog.Warn("Failed to generate content from CMS config", "error", err)
			}
		}
	}

	// Use timeout for hugo new command (60 seconds should be plenty)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "hugo", "new", "content", path)
	cmd.Dir = config.RepoPath
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("hugo new timed out")
	}
	return string(output), err
}
