package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"os"
	"os/exec"
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

	cmd := exec.Command("hugo", "new", "content", path)
	cmd.Dir = config.RepoPath
	output, err := cmd.CombinedOutput()
	
	return err, string(output)
}