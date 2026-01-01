package services

import (
	"bytes"
	"fmt"
	"hugo-cms/pkg/config"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func CheckSemanticDiff(relPath string) (bool, error) {
	gitPath := filepath.ToSlash(relPath)

	cmdHead := exec.Command("git", "show", "HEAD:"+gitPath)
	cmdHead.Dir = config.RepoPath
	headContent, _ := cmdHead.Output()

	diskPath := filepath.Join(config.RepoPath, filepath.FromSlash(gitPath))
	diskContent, _ := os.ReadFile(diskPath)

	collection, _ := GetCollectionForPath(gitPath)

	headFM, headBody, headErr := canonicalizeContentForDiff(headContent, collection)
	diskFM, diskBody, diskErr := canonicalizeContentForDiff(diskContent, collection)

	if headErr != nil || diskErr != nil {
		headTrimmed := strings.TrimSpace(normalizeLineEndings(string(headContent)))
		diskTrimmed := strings.TrimSpace(normalizeLineEndings(string(diskContent)))
		return headTrimmed != diskTrimmed, nil
	}

	if !bytes.Equal(headFM, diskFM) {
		return true, nil
	}

	return headBody != diskBody, nil
}

func ExecuteGitWithToken(dir, token string, args ...string) (string, error) {
	start := time.Now()
	defer func() {
		fmt.Printf("[Git] Cmd: %v, Duration: %v\n", args, time.Since(start))
	}()

	// 1. Prepare secure remote URL (username only, no password)
	// We want to use the token for auth, but via ASKPASS.
	// We need to ensure the remote URL in the command triggers ASKPASS.
	// Typically, https://username@host/repo... works, asking for password.
	
	cmdGetUrl := exec.Command("git", "remote", "get-url", "origin")
	cmdGetUrl.Dir = dir
	outUrl, err := cmdGetUrl.Output()
	if err != nil {
		return "Failed to get remote url", err
	}
	remoteUrl := strings.TrimSpace(string(outUrl))
	u, err := url.Parse(remoteUrl)
	if err != nil {
		return "Invalid remote url", err
	}
	
	// Set generic username "oauth2" and remove password to force prompt
	u.User = url.User("oauth2")
	authenticatedUrl := u.String()

	// 2. Prepare Arguments
	newArgs := make([]string, len(args))
	copy(newArgs, args)
	for i, v := range newArgs {
		if v == "origin" {
			newArgs[i] = authenticatedUrl
		}
	}

	// 3. Setup ASKPASS
	scriptPath, err := createAskPassScript()
	if err != nil {
		return "Failed to setup auth helper", err
	}
	defer os.Remove(scriptPath)

	cmd := exec.Command("git", newArgs...)
	cmd.Dir = dir

	// 4. Set Environment
	env := os.Environ()
	env = append(env,
		"GIT_ASKPASS="+scriptPath,
		"GIT_TOKEN="+token,
		"GIT_TERMINAL_PROMPT=0", // Disable interactive prompt fallback
	)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	
	// 5. Sanitize Log
	// The token is not in args, but might be in verbose output if any.
	safeLog := strings.ReplaceAll(string(output), token, "***")
	// Also hide the URL with username just in case user considers it sensitive, though it's generic
	safeLog = strings.ReplaceAll(safeLog, authenticatedUrl, remoteUrl)

	return safeLog, err
}

func createAskPassScript() (string, error) {
	var scriptContent string
	var pattern string

	if runtime.GOOS == "windows" {
		scriptContent = "@echo %GIT_TOKEN%"
		pattern = "git-askpass-*.bat"
	} else {
		scriptContent = "#!/bin/sh\necho \"$GIT_TOKEN\""
		pattern = "git-askpass-*.sh"
	}

	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.WriteString(scriptContent); err != nil {
		return "", err
	}

	if runtime.GOOS != "windows" {
		if err := f.Chmod(0700); err != nil {
			return "", err
		}
	}
	return f.Name(), nil
}

func SyncRepo(token string) (string, error) {
	log, err := ExecuteGitWithToken(config.RepoPath, token, "pull", config.GitRemote, config.GitBranch)
	if err == nil {
		InvalidateCache()
	}
	return log, err
}

func PublishChanges(token, path string) (string, error) {
	// Ensure Git Identity
	// We set this locally for the repo so it doesn't affect global config

	cmdConfigEmail := exec.Command("git", "config", "user.email", config.GitUserEmail)
	cmdConfigEmail.Dir = config.RepoPath
	if err := cmdConfigEmail.Run(); err != nil {
		fmt.Printf("[Git] Warning: failed to set user.email: %v\n", err)
	}

	cmdConfigName := exec.Command("git", "config", "user.name", config.GitUserName)
	cmdConfigName.Dir = config.RepoPath
	if err := cmdConfigName.Run(); err != nil {
		fmt.Printf("[Git] Warning: failed to set user.name: %v\n", err)
	}

	var addCmd *exec.Cmd
	var msg string

	if path != "" {
		// Single file publish
		addCmd = exec.Command("git", "add", path)
		msg = fmt.Sprintf("Update %s via HomeCMS", path)
	} else {
		// Publish all
		addCmd = exec.Command("git", "add", ".")
		msg = fmt.Sprintf("Update via HomeCMS: %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	addCmd.Dir = config.RepoPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("Git Add Failed: %s\nOutput: %s", err.Error(), string(out)), err
	}

	commitCmd := exec.Command("git", "commit", "-m", msg)
	commitCmd.Dir = config.RepoPath
	commitOut, commitErr := commitCmd.CombinedOutput()

	commitLog := string(commitOut)
	if commitErr != nil {
		commitLog = fmt.Sprintf("Commit Warning/Error: %s\nOutput: %s", commitErr.Error(), commitLog)
	}

	pushLog, err := ExecuteGitWithToken(config.RepoPath, token, "push", config.GitRemote, config.GitBranch)

	// Invalidate cache after successful publish to refresh dirty status
	if err == nil {
		InvalidateCache()
	}

	fullLog := fmt.Sprintf("--- Git Add ---\n(Success)\n\n--- Git Commit ---\n%s\n\n--- Git Push ---\n%s", commitLog, pushLog)
	return fullLog, err
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
	fHead, err := os.CreateTemp("", "diff_head_*")
	if err != nil {
		fmt.Printf("[Diff] Warning: failed to create temp file: %v\n", err)
		return "", "none"
	}
	defer os.Remove(fHead.Name())
	if _, err := fHead.Write(normalizedHead); err != nil {
		fmt.Printf("[Diff] Warning: failed to write temp file: %v\n", err)
		return "", "none"
	}
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