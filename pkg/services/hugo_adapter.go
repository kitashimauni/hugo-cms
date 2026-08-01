package services

import (
	"context"
	"fmt"
	"hugo-cms/pkg/config"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type HugoAdapter struct{}

func NewHugoAdapter() *HugoAdapter {
	return &HugoAdapter{}
}

func (*HugoAdapter) Name() string {
	return "hugo"
}

func (*HugoAdapter) Build(runtime config.SiteRuntime) (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Hugo build completed", "site", runtime.ID, "duration", time.Since(start))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := generatorCommandContext(ctx, runtime, "hugo", hugoBuildArgs(runtime)...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("hugo build timed out after 5 minutes")
	}
	return string(output), err
}

func hugoBuildArgs(runtime config.SiteRuntime) []string {
	return []string{
		"--source", ".",
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
			collFolder, folderErr := CollectionFolderWithinContentForRuntime(runtime, collection)
			if folderErr != nil {
				continue
			}
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

	cmd := generatorCommandContext(ctx, runtime, "hugo", hugoNewContentArgs(runtime, path)...)
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
