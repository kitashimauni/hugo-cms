package services

import (
	"hugo-cms/pkg/config"
	"os"
	"os/exec"
)

func BuildSite() (error, string) {
	cmd := exec.Command("hugo",
		"--source", config.RepoPath,
		"--destination", "public",
		"--baseURL", config.GetAppURL()+config.PreviewURL,
		"--cleanDestinationDir",
		"-D",
	)
	output, err := cmd.CombinedOutput()
	return err, string(output)
}

func CreateContent(path string) (error, string) {
	// Check if file already exists
	fullPath := SafeJoin(config.RepoPath, "content", path)
	if _, err := os.Stat(fullPath); err == nil {
		return os.ErrExist, "File already exists"
	}

	cmd := exec.Command("hugo", "new", "content", path)
	cmd.Dir = config.RepoPath
	output, err := cmd.CombinedOutput()

	if err == nil {
		InvalidateCache()
	}
	return err, string(output)
}
