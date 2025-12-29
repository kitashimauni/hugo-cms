package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func ExecuteGitWithToken(dir, token string, args ...string) (error, string) {
	start := time.Now()
	defer func() {
		fmt.Printf("[Git] Cmd: %v, Duration: %v\n", args, time.Since(start))
	}()

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
	// 1. Check Unsaved Diff (Saved/Disk Normalized vs Editor Normalized)
	cmd := exec.Command("git", "diff", "--no-index", "--", f1Path, f2Path)
	output, err := cmd.CombinedOutput()
	
	if err != nil && cmd.ProcessState.ExitCode() == 1 {
		diffStr := string(output)
		// Fix labels
		// git diff --no-index usually shows path
		diffStr = strings.ReplaceAll(diffStr, f1Path, "Saved (Normalized)")
		diffStr = strings.ReplaceAll(diffStr, f2Path, "Editor")
		return diffStr, "unsaved"
	}

	// 2. Check Git Diff (HEAD Normalized vs Editor Normalized)
	// We use f2Path (Editor Normalized) as the "New" content because f1==f2 here.
	
	// Get HEAD content
	// Use filepath.ToSlash to ensure forward slashes for git
	gitPath := filepath.ToSlash(relPath)
	cmdHead := exec.Command("git", "show", "HEAD:"+gitPath)
	cmdHead.Dir = config.RepoPath
	outHead, _ := cmdHead.Output()
	// err is expected for new files, we treat it as empty
	
	// Normalize HEAD content with defaults
	collection, _ := GetCollectionForPath(relPath)
	normalizedHead := NormalizeContent(outHead, collection)
	
	// Write to temp file
	fHead, _ := os.CreateTemp("", "diff_head_*")
	defer os.Remove(fHead.Name())
	fHead.Write(normalizedHead)
	fHead.Close()
	
	cmdGit := exec.Command("git", "diff", "--no-index", "--", fHead.Name(), f2Path)
	outGit, err := cmdGit.CombinedOutput()
	
	if err != nil && cmdGit.ProcessState.ExitCode() == 1 {
		diffStr := string(outGit)
		diffStr = strings.ReplaceAll(diffStr, fHead.Name(), "HEAD (Normalized)")
		diffStr = strings.ReplaceAll(diffStr, f2Path, "Current (Normalized)")
		return diffStr, "git"
	}
	
	return "", "none"
}
