package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"os"
	"os/exec"
	"path/filepath"
	goRuntime "runtime"
	"strings"
)

func isEleventyLocalPreviewGenerator(generator string) bool {
	switch strings.ToLower(strings.TrimSpace(generator)) {
	case "eleventy", "11ty":
		return true
	default:
		return false
	}
}

func eleventyLocalPreviewInputDir(runtime config.SiteRuntime) (string, error) {
	inputDir := strings.TrimSpace(runtime.ProductionContentDir)
	if inputDir == "" {
		inputDir = strings.TrimSpace(runtime.ContentDir)
	}
	if inputDir == "" {
		return "", fmt.Errorf("Eleventy local preview content directory is required")
	}
	if filepath.IsAbs(inputDir) {
		repoPath, err := filepath.Abs(strings.TrimSpace(runtime.RepoPath))
		if err != nil {
			return "", fmt.Errorf("resolve Eleventy local preview repository: %w", err)
		}
		relative, err := filepath.Rel(repoPath, filepath.Clean(inputDir))
		if err != nil || !isSafeLocalPreviewRelativePath(relative) {
			return "", fmt.Errorf("Eleventy local preview content directory must be inside the project repository")
		}
		inputDir = relative
	}
	inputDir = filepath.Clean(inputDir)
	if !isSafeLocalPreviewRelativePath(inputDir) {
		return "", fmt.Errorf("invalid Eleventy local preview content directory %q", inputDir)
	}
	return inputDir, nil
}

func eleventyLocalPreviewPublicDir(runtime config.SiteRuntime) (string, error) {
	publicDir := strings.TrimSpace(runtime.PublicDir)
	if publicDir == "" {
		publicDir = "public"
	}
	publicDir = filepath.Clean(publicDir)
	if !isSafeLocalPreviewRelativePath(publicDir) {
		return "", fmt.Errorf("invalid Eleventy local preview public directory %q", publicDir)
	}
	return publicDir, nil
}

func isSafeLocalPreviewRelativePath(value string) bool {
	if value == "" || value == "." || filepath.IsAbs(value) {
		return false
	}
	return value != ".." && !strings.HasPrefix(value, ".."+string(filepath.Separator))
}

func isLocalPreviewPathAncestor(ancestor, target string) bool {
	relative, err := filepath.Rel(ancestor, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// createEleventyLocalPreviewProjectOverlay creates a project-root filesystem
// view. Configuration, package metadata and non-special directories remain
// production references; content and public are materialized in the supplied
// temporary project root so generators cannot write those trees back to the
// production repository.
func createEleventyLocalPreviewProjectOverlay(sourceRoot, projectRoot, inputDir, publicDir, contentSource string) error {
	sourceRoot, err := filepath.Abs(strings.TrimSpace(sourceRoot))
	if err != nil {
		return fmt.Errorf("resolve Eleventy project repository: %w", err)
	}
	projectRoot, err = filepath.Abs(strings.TrimSpace(projectRoot))
	if err != nil {
		return fmt.Errorf("resolve Eleventy preview project root: %w", err)
	}
	contentSource, err = filepath.Abs(strings.TrimSpace(contentSource))
	if err != nil {
		return fmt.Errorf("resolve Eleventy preview content source: %w", err)
	}
	if filepath.Clean(sourceRoot) == filepath.Clean(projectRoot) {
		return fmt.Errorf("Eleventy preview project root must differ from production repository")
	}
	if !isSafeLocalPreviewRelativePath(inputDir) || !isSafeLocalPreviewRelativePath(publicDir) {
		return fmt.Errorf("Eleventy preview project directories must be safe relative paths")
	}
	if inputDir == publicDir || isLocalPreviewPathAncestor(inputDir, publicDir) || isLocalPreviewPathAncestor(publicDir, inputDir) {
		return fmt.Errorf("Eleventy preview content and public directories must not overlap")
	}
	if info, err := os.Stat(sourceRoot); err != nil {
		return fmt.Errorf("stat Eleventy project repository: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("Eleventy project repository must be a directory")
	}
	if info, err := os.Stat(contentSource); err != nil {
		return fmt.Errorf("stat Eleventy preview content source: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("Eleventy preview content source must be a directory")
	}
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		return fmt.Errorf("create Eleventy preview project root: %w", err)
	}
	if err := materializeEleventyPreviewTree(sourceRoot, projectRoot, ".", inputDir, publicDir, contentSource); err != nil {
		return err
	}
	if err := ensureEleventyPreviewDirectory(projectRoot, inputDir, contentSource); err != nil {
		return err
	}
	if err := ensureEleventyPreviewDirectory(projectRoot, publicDir, ""); err != nil {
		return err
	}
	return nil
}

func materializeEleventyPreviewTree(sourceRoot, projectRoot, relative, inputDir, publicDir, contentSource string) error {
	if relative == inputDir {
		destination := filepath.Join(projectRoot, relative)
		return copyLocalPreviewContentTree(contentSource, destination)
	}
	if relative == publicDir {
		return os.MkdirAll(filepath.Join(projectRoot, relative), 0755)
	}

	sourcePath := sourceRoot
	destinationPath := projectRoot
	if relative != "." {
		sourcePath = filepath.Join(sourceRoot, relative)
		destinationPath = filepath.Join(projectRoot, relative)
	}

	if relative != "." && (isLocalPreviewPathAncestor(relative, inputDir) || isLocalPreviewPathAncestor(relative, publicDir)) {
		if err := os.MkdirAll(destinationPath, 0755); err != nil {
			return fmt.Errorf("create Eleventy preview directory %s: %w", relative, err)
		}
		entries, err := os.ReadDir(sourcePath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read Eleventy preview directory %s: %w", relative, err)
		}
		for _, entry := range entries {
			if err := materializeEleventyPreviewTree(sourceRoot, projectRoot, filepath.Join(relative, entry.Name()), inputDir, publicDir, contentSource); err != nil {
				return err
			}
		}
		return nil
	}

	if relative == "." {
		entries, err := os.ReadDir(sourcePath)
		if err != nil {
			return fmt.Errorf("read Eleventy project repository: %w", err)
		}
		for _, entry := range entries {
			if entry.Name() == ".git" {
				continue
			}
			if err := materializeEleventyPreviewTree(sourceRoot, projectRoot, entry.Name(), inputDir, publicDir, contentSource); err != nil {
				return err
			}
		}
		return nil
	}
	if entryInfo, err := os.Lstat(sourcePath); err != nil {
		return fmt.Errorf("stat Eleventy preview source %s: %w", relative, err)
	} else if entryInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Eleventy preview production symlinks are not supported: %s", relative)
	} else if entryInfo.IsDir() {
		return linkEleventyPreviewDirectory(sourcePath, destinationPath)
	} else if entryInfo.Mode().IsRegular() {
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0755); err != nil {
			return err
		}
		return copyLocalPreviewFile(sourcePath, destinationPath, entryInfo.Mode().Perm())
	}
	return fmt.Errorf("unsupported Eleventy preview production file type: %s", relative)
}

func ensureEleventyPreviewDirectory(projectRoot, relative, source string) error {
	destination := filepath.Join(projectRoot, relative)
	if info, err := os.Stat(destination); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("Eleventy preview path is not a directory: %s", relative)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return fmt.Errorf("create Eleventy preview directory %s: %w", relative, err)
	}
	if source != "" {
		return copyLocalPreviewContentTree(source, destination)
	}
	return nil
}

func linkEleventyPreviewDirectory(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	if err := os.Symlink(source, destination); err == nil {
		return nil
	} else if goRuntime.GOOS != "windows" {
		return fmt.Errorf("link Eleventy preview directory %s: %w", destination, err)
	}

	command := exec.Command("cmd.exe", "/d", "/s", "/c", fmt.Sprintf("mklink /J %q %q", destination, source))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("link Eleventy preview directory %s: %w (%s)", destination, err, strings.TrimSpace(string(output)))
	}
	return nil
}
