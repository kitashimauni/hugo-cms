package services

import (
	"context"
	"encoding/json"
	"fmt"
	"hugo-cms/pkg/config"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type EleventyAdapter struct{}

type eleventyPackageManager struct {
	Name string
	Bin  string
	Args []string
}

func NewEleventyAdapter() *EleventyAdapter {
	return &EleventyAdapter{}
}

func (*EleventyAdapter) Name() string {
	return "eleventy"
}

func (*EleventyAdapter) Build(runtime config.SiteRuntime) (string, error) {
	pm, err := detectEleventyPackageManager(runtime.RepoPath)
	if err != nil {
		return "", err
	}

	start := time.Now()
	defer func() {
		slog.Info("Eleventy build completed", "site", runtime.ID, "duration", time.Since(start), "package_manager", pm.Name)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	args := append([]string{}, pm.Args...)
	args = append(args,
		"--input", runtime.ContentDir,
		"--output", runtime.PublicDir,
	)
	cmd := generatorCommandContextWithEnv(ctx, runtime, []string{"NODE_ENV=production"}, pm.Bin, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("eleventy build timed out after 5 minutes")
	}
	return string(output), err
}

func (*EleventyAdapter) CreateContent(runtime config.SiteRuntime, path string) (string, error) {
	start := time.Now()
	defer func() {
		slog.Info("Eleventy new content", "site", runtime.ID, "path", path, "duration", time.Since(start))
	}()

	fullPath := SafeJoin(runtime.RepoPath, runtime.ContentDir, path)
	if fullPath == "" {
		return "Invalid path", fmt.Errorf("invalid path: %s", path)
	}
	if _, err := os.Stat(fullPath); err == nil {
		return "File already exists", os.ErrExist
	}

	cmsConfig, err := GetCMSConfigForRuntime(runtime)
	if err != nil {
		return "Eleventy content creation requires CMS config", err
	}

	relContentPath := filepath.Join(runtime.ContentDir, path)
	for _, collection := range cmsConfig.Collections {
		collFolder, folderErr := CollectionFolderWithinContentForRuntime(runtime, collection)
		if folderErr != nil {
			continue
		}
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
	_, err := detectEleventyPackageManager(repoPath)
	return err
}

func detectEleventyPackageManager(repoPath string) (eleventyPackageManager, error) {
	packageJSONPath := filepath.Join(repoPath, "package.json")
	content, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return eleventyPackageManager{}, fmt.Errorf("eleventy project requires package.json: %w", err)
	}
	if !packageJSONDeclaresEleventy(content) {
		return eleventyPackageManager{}, fmt.Errorf("eleventy project must declare @11ty/eleventy in package.json dependencies")
	}

	lockFiles := []struct {
		File string
		PM   eleventyPackageManager
	}{
		{File: "package-lock.json", PM: eleventyPackageManager{Name: "npm", Bin: "npm", Args: []string{"exec", "--", "eleventy"}}},
		{File: "npm-shrinkwrap.json", PM: eleventyPackageManager{Name: "npm", Bin: "npm", Args: []string{"exec", "--", "eleventy"}}},
		{File: "pnpm-lock.yaml", PM: eleventyPackageManager{Name: "pnpm", Bin: "pnpm", Args: []string{"exec", "eleventy"}}},
		{File: "yarn.lock", PM: eleventyPackageManager{Name: "yarn", Bin: "yarn", Args: []string{"exec", "eleventy"}}},
		{File: "bun.lock", PM: eleventyPackageManager{Name: "bun", Bin: "bunx", Args: []string{"eleventy"}}},
		{File: "bun.lockb", PM: eleventyPackageManager{Name: "bun", Bin: "bunx", Args: []string{"eleventy"}}},
	}
	for _, lock := range lockFiles {
		if _, err := os.Stat(filepath.Join(repoPath, lock.File)); err == nil {
			return lock.PM, nil
		}
	}
	return eleventyPackageManager{}, fmt.Errorf("eleventy project requires a committed lock file")
}

func packageJSONDeclaresEleventy(content []byte) bool {
	var pkg struct {
		Dependencies         map[string]interface{} `json:"dependencies"`
		DevDependencies      map[string]interface{} `json:"devDependencies"`
		OptionalDependencies map[string]interface{} `json:"optionalDependencies"`
	}
	if err := json.Unmarshal(content, &pkg); err != nil {
		return false
	}
	for _, deps := range []map[string]interface{}{pkg.Dependencies, pkg.DevDependencies, pkg.OptionalDependencies} {
		if _, ok := deps["@11ty/eleventy"]; ok {
			return true
		}
	}
	return false
}

var _ GeneratorAdapter = (*EleventyAdapter)(nil)
