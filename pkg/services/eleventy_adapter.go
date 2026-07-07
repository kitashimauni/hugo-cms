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

type EleventyAdapter struct {
	preview *ProcessManager
}

func NewEleventyAdapter() *EleventyAdapter {
	return &EleventyAdapter{preview: &ProcessManager{}}
}

func (*EleventyAdapter) Name() string {
	return "eleventy"
}

func (adapter *EleventyAdapter) StartPreview() error {
	if err := validateEleventyProject(config.RepoPath); err != nil {
		return err
	}
	slog.Info("Starting Eleventy server", "port", config.HugoServerPort)

	err := adapter.preview.Start(func() managedProcess {
		cmd := exec.Command("npm", "exec", "--", "eleventy",
			"--serve",
			"--port", config.HugoServerPort,
			"--input", config.ContentDir,
			"--output", config.PublicDir,
		)
		cmd.Dir = config.RepoPath
		cmd.Env = generatorProcessEnvironment("NODE_ENV=development")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return &execManagedProcess{cmd: cmd}
	}, func(err error) {
		slog.Info("Eleventy server stopped", "error", err)
	})
	if err != nil {
		return fmt.Errorf("failed to start eleventy server: %w", err)
	}
	return nil
}

func (adapter *EleventyAdapter) StopPreview() error {
	slog.Info("Stopping Eleventy server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := adapter.preview.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop eleventy server: %w", err)
	}
	return nil
}

func (adapter *EleventyAdapter) RestartPreview() error {
	if err := adapter.StopPreview(); err != nil {
		return err
	}
	return adapter.StartPreview()
}

func (adapter *EleventyAdapter) IsPreviewRunning() bool {
	return adapter.preview.Running()
}

func (*EleventyAdapter) Build() (string, error) {
	if err := validateEleventyProject(config.RepoPath); err != nil {
		return "", err
	}

	start := time.Now()
	defer func() {
		slog.Info("Eleventy build completed", "duration", time.Since(start))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npm", "exec", "--", "eleventy",
		"--input", config.ContentDir,
		"--output", config.PublicDir,
	)
	cmd.Dir = config.RepoPath
	cmd.Env = generatorProcessEnvironment("NODE_ENV=production")
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("eleventy build timed out after 5 minutes")
	}
	return string(output), err
}

func (*EleventyAdapter) CreateContent(path string) (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Eleventy new content", "path", path, "duration", time.Since(start))
	}()

	fullPath := SafeJoin(config.RepoPath, config.ContentDir, path)
	if fullPath == "" {
		return "Invalid path", fmt.Errorf("invalid path: %s", path)
	}
	if _, err := os.Stat(fullPath); err == nil {
		return "File already exists", os.ErrExist
	}

	cmsConfig, err := GetCMSConfig()
	if err != nil {
		return "Eleventy content creation requires CMS config", err
	}

	relContentPath := filepath.Join(config.ContentDir, path)
	for _, collection := range cmsConfig.Collections {
		collFolder := filepath.Clean(collection.Folder)
		targetFolder := filepath.Dir(relContentPath)
		if !isPathWithin(collFolder, targetFolder) {
			continue
		}

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

	return "No matching CMS collection for Eleventy content path", fmt.Errorf("no matching CMS collection for %s", path)
}

func validateEleventyProject(repoPath string) error {
	if _, err := os.Stat(filepath.Join(repoPath, "package.json")); err != nil {
		return fmt.Errorf("eleventy project requires package.json: %w", err)
	}
	for _, name := range []string{"package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb"} {
		if _, err := os.Stat(filepath.Join(repoPath, name)); err == nil {
			return nil
		}
	}
	return fmt.Errorf("eleventy project requires a committed lock file")
}

var _ GeneratorAdapter = (*EleventyAdapter)(nil)
