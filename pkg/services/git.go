package services

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
	"hugo-cms/pkg/config"
)

func ExecuteGitWithToken(dir, token string, args ...string) (error, string) {
	cmdGetUrl := exec.Command("git", "remote", "get-url", "origin")
	cmdGetUrl.Dir = dir
	outUrl, err := cmdGetUrl.Output()
	if err != nil {
		return err, "Failed to get remote url"
	}
	remoteUrl := strings.TrimSpace(string(outUrl))
	u, err := url.Parse(remoteUrl)
	if err != nil {
		return err, "Invalid remote url"
	}
	u.User = url.UserPassword("oauth2", token)
	authenticatedUrl := u.String()
	newArgs := make([]string, len(args))
	copy(newArgs, args)
	for i, v := range newArgs {
		if v == "origin" {
			newArgs[i] = authenticatedUrl
		}
	}
	cmd := exec.Command("git", newArgs...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	safeLog := strings.ReplaceAll(string(output), token, "***")
	safeLog = strings.ReplaceAll(safeLog, authenticatedUrl, remoteUrl)
	return err, safeLog
}

func SyncRepo(token string) (error, string) {
	err, log := ExecuteGitWithToken(config.RepoPath, token, "pull", "origin", "main")
	if err == nil {
		InvalidateCache()
	}
	return err, log
}

func PublishRepo(token string) (error, string) {
	addCmd := exec.Command("git", "add", ".")
	addCmd.Dir = config.RepoPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return err, string(out)
	}
	msg := fmt.Sprintf("Update via HomeCMS: %s", time.Now().Format("2006-01-02 15:04:05"))
	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = config.RepoPath
	commitCmd.Run()
	return ExecuteGitWithToken(config.RepoPath, token, "push", "origin", "main")
}

func Diff(f1Path, f2Path, relPath string) (string, string) {
	// Check Unsaved Diff
	cmd := exec.Command("git", "diff", "--no-index", f1Path, f2Path)
	output, err := cmd.CombinedOutput()
	
	if err != nil && cmd.ProcessState.ExitCode() == 1 {
		diffStr := string(output)
		diffStr = strings.ReplaceAll(diffStr, f1Path, "Saved (Normalized)")
		diffStr = strings.ReplaceAll(diffStr, f2Path, "Editor")
		return diffStr, "unsaved"
	}

	cmdGit := exec.Command("git", "diff", "HEAD", "--", relPath)
	cmdGit.Dir = config.RepoPath
	outGit, _ := cmdGit.CombinedOutput()
	
	if len(outGit) > 0 {
		return string(outGit), "git"
	}
	return "", "none"
}
