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

func BuildSite() (error, string) {
	start := time.Now()
	defer func() {
		fmt.Printf("[Hugo] Build Duration: %v\n", time.Since(start))
	}()

	cmd := exec.Command("hugo",
		"--source", config.RepoPath,
		"--destination", "public",
		"--baseURL", config.GetAppURL()+config.PreviewURL,
		"--cleanDestinationDir",
		"-D",
		"-F",
	)
	output, err := cmd.CombinedOutput()
	return err, string(output)
}

func CreateContent(path string) (error, string) {
	start := time.Now()
	defer func() {
		fmt.Printf("[Hugo] New Content: %s, Duration: %v\n", path, time.Since(start))
	}()

	// Check if file already exists
	fullPath := SafeJoin(config.RepoPath, "content", path)
	if _, err := os.Stat(fullPath); err == nil {
		return os.ErrExist, "File already exists"
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
						return err, "Failed to create directory"
					}

					if err := os.WriteFile(fullPath, content, 0644); err != nil {
						return err, "Failed to write file"
					}
					return nil, "Created using CMS config"
				}
				// If generation fails, fall through to hugo new
				fmt.Printf("Failed to generate content from config: %v\n", err)
			}
		}
	}

	cmd := exec.Command("hugo", "new", "content", path)
	cmd.Dir = config.RepoPath
	output, err := cmd.CombinedOutput()
	
	return err, string(output)
}