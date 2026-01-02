package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var hugoServerCmd *exec.Cmd

func StartHugoServer() error {
	if hugoServerCmd != nil && hugoServerCmd.Process != nil {
		// Check if process is still alive?
		// For simplicity assume if variable is set, it's running.
		// The goroutine below clears it on exit.
		return nil
	}

	fmt.Printf("[Hugo] Starting server on :%s...\n", config.HugoServerPort)

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
		fmt.Printf("[Hugo] Server stopped. State: %v, Err: %v\n", state, err)
		hugoServerCmd = nil
	}()

	return nil
}

func RestartHugoServer() error {
	if hugoServerCmd != nil && hugoServerCmd.Process != nil {
		fmt.Println("[Hugo] Stopping existing server...")
		if err := hugoServerCmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill hugo server: %w", err)
		}
		// Give it a moment to release ports
		time.Sleep(1 * time.Second)
	}
	return StartHugoServer()
}

func BuildSite() (string, error) {
	start := time.Now()
	defer func() {
		fmt.Printf("[Hugo] Build Duration: %v\n", time.Since(start))
	}()

	cmd := exec.Command("hugo",
		"--source", config.RepoPath,
		"--destination", "public",
		"--baseURL", config.GetAppURL(),
		"--cleanDestinationDir",
		"-D",
		"-F",
	)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func CreateContent(path string) (string, error) {
	start := time.Now()
	defer func() {
		fmt.Printf("[Hugo] New Content: %s, Duration: %v\n", path, time.Since(start))
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
				fmt.Printf("Failed to generate content from config: %v\n", err)
			}
		}
	}

	cmd := exec.Command("hugo", "new", "content", path)
	cmd.Dir = config.RepoPath
	output, err := cmd.CombinedOutput()

	return string(output), err
}
