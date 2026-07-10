package services

import (
	"context"
	"fmt"
	"hugo-cms/pkg/config"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type HugoAdapter struct {
	preview *ProcessManager
}

func NewHugoAdapter() *HugoAdapter {
	return &HugoAdapter{preview: &ProcessManager{}}
}

func (*HugoAdapter) Name() string {
	return "hugo"
}

func (adapter *HugoAdapter) StartPreview(runtime config.SiteRuntime) error {
	slog.Info("Starting Hugo server", "site", runtime.ID, "port", runtime.HugoServerPort)

	err := adapter.preview.Start(func() managedProcess {
		cmd := exec.Command("hugo", hugoServerArgs(runtime)...)
		cmd.Env = generatorProcessEnvironment()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return newExecManagedProcess(cmd)
	}, func(err error) {
		slog.Info("Hugo server stopped", "error", err)
	})
	if err != nil {
		return fmt.Errorf("failed to start hugo server: %w", err)
	}
	return nil
}

func (adapter *HugoAdapter) StopPreview() error {
	slog.Info("Stopping Hugo server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := adapter.preview.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop hugo server: %w", err)
	}
	return nil
}

func (adapter *HugoAdapter) IsPreviewRunning() bool {
	return adapter.preview.Running()
}

func (*HugoAdapter) Build(runtime config.SiteRuntime) (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Hugo build completed", "site", runtime.ID, "duration", time.Since(start))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "hugo", hugoBuildArgs(runtime)...)
	cmd.Env = generatorProcessEnvironment()
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("hugo build timed out after 5 minutes")
	}
	return string(output), err
}

func hugoServerArgs(runtime config.SiteRuntime) []string {
	return []string{
		"server",
		"--source", runtime.RepoPath,
		"--contentDir", runtime.ContentDir,
		"--bind", runtime.HugoServerBind,
		"--port", runtime.HugoServerPort,
		"--baseURL", runtime.AppURL + runtime.PreviewURL,
		"--appendPort=false",
		"--disableLiveReload",
		"-D",
		"-F",
	}
}

func hugoBuildArgs(runtime config.SiteRuntime) []string {
	return []string{
		"--source", runtime.RepoPath,
		"--contentDir", runtime.ContentDir,
		"--destination", runtime.PublicDir,
		"--baseURL", runtime.AppURL + runtime.PreviewURL,
		"--cleanDestinationDir",
		"-D",
		"-F",
	}
}

func (*HugoAdapter) CreateContent(runtime config.SiteRuntime, path string) (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Hugo new content", "site", runtime.ID, "path", path, "duration", time.Since(start))
	}()

	fullPath := SafeJoin(runtime.RepoPath, runtime.ContentDir, path)
	if fullPath == "" {
		return "Invalid path", fmt.Errorf("invalid path: %s", path)
	}

	if _, err := os.Stat(fullPath); err == nil {
		return "File already exists", os.ErrExist
	}

	cmsConfig, err := GetCMSConfigForRuntime(runtime)
	if err == nil {
		relContentPath := filepath.Join(runtime.ContentDir, path)

		for _, collection := range cmsConfig.Collections {
			collFolder := filepath.Clean(collection.Folder)
			targetFolder := filepath.Dir(relContentPath)

			if isPathWithin(collFolder, targetFolder) {
				content, generateErr := GenerateContentFromCollection(collection, nil)
				if generateErr != nil {
					return "Failed to generate content from CMS config", generateErr
				}
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					return "Failed to create directory", err
				}
				if err := os.WriteFile(fullPath, content, 0644); err != nil {
					return "Failed to write file", err
				}
				return "Created using CMS config", nil
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "hugo", hugoNewContentArgs(runtime, path)...)
	cmd.Dir = runtime.RepoPath
	cmd.Env = generatorProcessEnvironment()
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("hugo new timed out")
	}
	return string(output), err
}

func hugoNewContentArgs(runtime config.SiteRuntime, path string) []string {
	return []string{
		"new",
		"content",
		"--contentDir", runtime.ContentDir,
		filepath.ToSlash(path),
	}
}

var _ GeneratorAdapter = (*HugoAdapter)(nil)
