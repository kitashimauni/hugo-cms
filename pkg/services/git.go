package services

import (
	"bytes"
	"fmt"
	"hugo-cms/pkg/config"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	return safeLog, err
}

func SyncRepo(token string) (string, error) {
	log, err := ExecuteGitWithToken(config.RepoPath, token, "pull", "origin", "main")
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
		// We need the full path relative to repo root.
		// Usually path coming from frontend is like "posts/my-article.md" (content relative)
		// But let's assume the handler passes the correct relative path for git add.
		// Wait, Handler receives "path" which is usually relative to "content" dir for articles.
		// git add needs path relative to repo root.
		// So we should prepend "content/" if it's not there?
		// Actually, let's rely on the caller to provide the correct repo-relative path,
		// OR we handle it here.
		// In CreateArticle, we saw logic: services.SafeJoin(config.RepoPath, "content", art.Path)
		// So the frontend usually sends "posts/foo.md".
		// Git add needs "content/posts/foo.md".

		// To be safe, let's just take the path as is from argument.
		// The Handler should ensure it is correct.

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

	// If commit fails, we should check if it's because there were no changes.
	// However, for explicit publish, we usually expect changes.
	// But let's log it regardless.
	commitLog := string(commitOut)
	if commitErr != nil {
		// If "nothing to commit" is in output, it might not be a fatal error for the flow,
		// but for a "Publish this file" action, it's suspicious if we expected a change.
		// We'll verify this by appending to the log.
		commitLog = fmt.Sprintf("Commit Warning/Error: %s\nOutput: %s", commitErr.Error(), commitLog)
	}

	pushLog, err := ExecuteGitWithToken(config.RepoPath, token, "push", "origin", "main")

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
