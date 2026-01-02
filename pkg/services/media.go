package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"io/fs"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MediaFile struct {
	Name     string `json:"name"`
	Path     string `json:"path"` // Relative path for usage in markdown
	Size     int64  `json:"size"`
	URL      string `json:"url"`  // URL for preview
	RepoPath string `json:"repo_path"`
}

func ListMediaFiles(mode, articlePath string) ([]MediaFile, error) {
	var searchDirs []string
	
	// Determine search roots based on mode
	if mode == "static" {
		// List all files in repo/static/{StaticMediaDir}
		staticDir := filepath.Join(config.RepoPath, "static", config.StaticMediaDir)
		if _, err := os.Stat(staticDir); err == nil {
			searchDirs = append(searchDirs, staticDir)
		}
	} else if mode == "content" {
		if articlePath == "" {
			return nil, nil // No article context, return empty
		}
		// Assuming articlePath is "posts/2024/slug/index.md" (relative to content)
		// We want to list files in "repo/content/posts/2024/slug"
		bundleDir := filepath.Dir(articlePath)
		fullBundlePath := filepath.Join(config.RepoPath, "content", bundleDir)
		if _, err := os.Stat(fullBundlePath); err == nil {
			searchDirs = append(searchDirs, fullBundlePath)
		}
	} else {
		// Default/Fallback: maybe show static?
		staticDir := filepath.Join(config.RepoPath, "static", config.StaticMediaDir)
		if _, err := os.Stat(staticDir); err == nil {
			searchDirs = append(searchDirs, staticDir)
		}
	}

	var files []MediaFile
	for _, root := range searchDirs {
		// Walk directory to find images
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			// Simple filter for images
			ext := strings.ToLower(filepath.Ext(path))
			switch ext {
			case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
				// Found image
				relPath, _ := filepath.Rel(config.RepoPath, path)
				relPath = filepath.ToSlash(relPath)

				// Determine Usage Path
				usagePath := ""
				if mode == "static" {
					// repo/static/sub/img.png -> /sub/img.png (relative to static)
					// BUT usually Hugo static files are served at root.
					// So if StaticMediaDir is "uploads", path is repo/static/uploads/img.png
					// Usage path: /uploads/img.png
					staticRel, _ := filepath.Rel(filepath.Join(config.RepoPath, "static"), path)
					usagePath = "/" + filepath.ToSlash(staticRel)
				} else {
					// content/posts/slug/img.png -> img.png (Page Bundle)
					// Or if in subfolder src/img.png -> src/img.png
					bundleRel, _ := filepath.Rel(root, path)
					usagePath = filepath.ToSlash(bundleRel)
				}

				files = append(files, MediaFile{
					Name:     d.Name(), // Or relative path from root?
					Path:     usagePath,
					Size:     0, // d.Info() needed
					URL:      "/api/media/raw?path=" + url.QueryEscape(relPath),
					RepoPath: relPath,
				})
			}
			return nil
		})
		if err != nil {
			fmt.Printf("Walk error: %v\n", err)
		}
	}
	return files, nil
}

func SaveMediaFile(header *multipart.FileHeader, mode, articlePath string) (*MediaFile, error) {
	src, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	filename := filepath.Base(header.Filename)
	filename = strings.ReplaceAll(filename, " ", "_")
	
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	filename = fmt.Sprintf("%s_%d%s", name, time.Now().Unix(), ext)

	var targetDir string

	if mode == "static" {
		targetDir = filepath.Join(config.RepoPath, "static", config.StaticMediaDir)
	} else {
		// Content mode
		if articlePath == "" {
			return nil, fmt.Errorf("article path required for content upload")
		}
		bundleDir := filepath.Dir(articlePath)
		// Use ARTICLE_MEDIA_DIR config
		subDir := config.ArticleMediaDir
		targetDir = filepath.Join(config.RepoPath, "content", bundleDir, subDir)
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, err
	}

	fullMediaPath := filepath.Join(targetDir, filename)
	dst, err := os.Create(fullMediaPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return nil, err
	}

	// Calculate Result
	relPath, _ := filepath.Rel(config.RepoPath, fullMediaPath)
	relPath = filepath.ToSlash(relPath)

	usagePath := ""
	if mode == "static" {
		staticRel, _ := filepath.Rel(filepath.Join(config.RepoPath, "static"), fullMediaPath)
		usagePath = "/" + filepath.ToSlash(staticRel)
	} else {
		// Relative to bundle root
		// targetDir is bundle + subDir
		// We need path relative to bundle root.
		// Bundle root is targetDir without subDir (if subDir is relative)
		// Actually simpler:
		bundleRoot := filepath.Join(config.RepoPath, "content", filepath.Dir(articlePath))
		bundleRel, _ := filepath.Rel(bundleRoot, fullMediaPath)
		usagePath = filepath.ToSlash(bundleRel)
	}

	return &MediaFile{
		Name:     filename,
		Path:     usagePath,
		Size:     header.Size,
		URL:      "/api/media/raw?path=" + url.QueryEscape(relPath),
		RepoPath: relPath,
	}, nil
}

func DeleteMediaFile(repoPath string) error {
	fullMediaPath := SafeJoin(config.RepoPath, "", repoPath)
	if fullMediaPath == "" {
		return fmt.Errorf("invalid media path")
	}
	return os.Remove(fullMediaPath)
}
