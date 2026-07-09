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

func (adapter *HugoAdapter) StartPreview() error {
	slog.Info("Starting Hugo server", "port", config.HugoServerPort)

	err := adapter.preview.Start(func() managedProcess {
		cmd := exec.Command("hugo", hugoServerArgs()...)
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

func (adapter *HugoAdapter) RestartPreview() error {
	if err := adapter.StopPreview(); err != nil {
		return err
	}
	return adapter.StartPreview()
}

func (adapter *HugoAdapter) IsPreviewRunning() bool {
	return adapter.preview.Running()
}

func (*HugoAdapter) Build() (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Hugo build completed", "duration", time.Since(start))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "hugo", hugoBuildArgs()...)
	cmd.Env = generatorProcessEnvironment()
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("hugo build timed out after 5 minutes")
	}
	return string(output), err
}

func hugoServerArgs() []string {
	return []string{
		"server",
		"--source", config.RepoPath,
		"--contentDir", config.ContentDir,
		"--bind", config.HugoServerBind,
		"--port", config.HugoServerPort,
		"--baseURL", config.GetAppURL() + config.PreviewURL,
		"--appendPort=false",
		"--disableLiveReload",
		"-D",
		"-F",
	}
}

func hugoBuildArgs() []string {
	return []string{
		"--source", config.RepoPath,
		"--contentDir", config.ContentDir,
		"--destination", config.PublicDir,
		"--baseURL", config.GetAppURL() + config.PreviewURL,
		"--cleanDestinationDir",
		"-D",
		"-F",
	}
}

func (*HugoAdapter) CreateContent(path string) (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Hugo new content", "path", path, "duration", time.Since(start))
	}()

	fullPath := SafeJoin(config.RepoPath, config.ContentDir, path)
	if fullPath == "" {
		return "Invalid path", fmt.Errorf("invalid path: %s", path)
	}

	if _, err := os.Stat(fullPath); err == nil {
		return "File already exists", os.ErrExist
	}

	cmsConfig, err := GetCMSConfig()
	if err == nil {
		relContentPath := filepath.Join(config.ContentDir, path)

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

	cmd := exec.CommandContext(ctx, "hugo", hugoNewContentArgs(path)...)
	cmd.Dir = config.RepoPath
	cmd.Env = generatorProcessEnvironment()
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("hugo new timed out")
	}
	return string(output), err
}

func hugoNewContentArgs(path string) []string {
	return []string{
		"new",
		"content",
		"--contentDir", config.ContentDir,
		filepath.ToSlash(path),
	}
}

var _ GeneratorAdapter = (*HugoAdapter)(nil)
