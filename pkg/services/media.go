package services

import (
	"bytes"
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
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
	URL      string `json:"url"` // URL for preview
	RepoPath string `json:"repo_path"`
}

var ErrInvalidMedia = errors.New("invalid media")

var allowedMediaTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".pdf":  "application/pdf",
}

func isAllowedMediaExtension(path string) bool {
	_, ok := allowedMediaTypes[strings.ToLower(filepath.Ext(path))]
	return ok
}

func isAllowedImageExtension(path string) bool {
	mediaType, ok := allowedMediaTypes[strings.ToLower(filepath.Ext(path))]
	return ok && strings.HasPrefix(mediaType, "image/")
}

func ValidateMediaRepoPath(repoPath string) bool {
	return ValidateMediaRepoPathForRuntime(config.CurrentSiteRuntime(), repoPath)
}

func ValidateMediaRepoPathForRuntime(runtime config.SiteRuntime, repoPath string) bool {
	if repoPath == "" || !isAllowedMediaExtension(repoPath) {
		return false
	}
	normalized := filepath.ToSlash(filepath.Clean(repoPath))
	staticPrefix := filepath.ToSlash(filepath.Clean(runtime.StaticDir)) + "/"
	contentPrefix := filepath.ToSlash(filepath.Clean(runtime.ContentDir)) + "/"
	if normalized == "." ||
		(!strings.HasPrefix(normalized, staticPrefix) && !strings.HasPrefix(normalized, contentPrefix)) {
		return false
	}
	return SafeJoin(runtime.RepoPath, "", normalized) != ""
}

func ListMediaFiles(mode, articlePath string) ([]MediaFile, error) {
	return ListMediaFilesForRuntime(config.CurrentSiteRuntime(), mode, articlePath)
}

func ListMediaFilesForRuntime(runtime config.SiteRuntime, mode, articlePath string) ([]MediaFile, error) {
	var searchDirs []string
	if mode == "" {
		mode = "static"
	}

	// Determine search roots based on mode
	if mode == "static" {
		// List all files in repo/static/{StaticMediaDir}
		staticDir := SafeJoin(runtime.RepoPath, runtime.StaticDir, runtime.StaticMediaDir)
		if staticDir == "" {
			return nil, fmt.Errorf("%w: invalid static media directory", ErrInvalidMedia)
		}
		if _, err := os.Stat(staticDir); err == nil {
			searchDirs = append(searchDirs, staticDir)
		}
	} else if mode == "content" {
		if articlePath == "" {
			return nil, nil // No article context, return empty
		}
		fullArticlePath := SafeJoin(runtime.RepoPath, runtime.ContentDir, articlePath)
		if fullArticlePath == "" || strings.ToLower(filepath.Ext(fullArticlePath)) != ".md" {
			return nil, fmt.Errorf("%w: invalid article path", ErrInvalidMedia)
		}
		// Assuming articlePath is "posts/2024/slug/index.md" (relative to content)
		// We want to list files in "repo/content/posts/2024/slug"
		fullBundlePath := filepath.Dir(fullArticlePath)
		if _, err := os.Stat(fullBundlePath); err == nil {
			searchDirs = append(searchDirs, fullBundlePath)
		}
	} else {
		return nil, fmt.Errorf("%w: invalid media mode", ErrInvalidMedia)
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
			// The current media picker renders thumbnails with <img> and
			// inserts image Markdown, so only expose image files here.
			if isAllowedImageExtension(path) {
				// Found image
				relPath, relErr := filepath.Rel(runtime.RepoPath, path)
				if relErr != nil {
					return relErr
				}
				relPath = filepath.ToSlash(relPath)
				if !ValidateMediaRepoPathForRuntime(runtime, relPath) {
					return nil
				}

				// Determine Usage Path
				usagePath := ""
				if mode == "static" {
					// repo/static/sub/img.png -> /sub/img.png (relative to static)
					// BUT usually Hugo static files are served at root.
					// So if StaticMediaDir is "uploads", path is repo/static/uploads/img.png
					// Usage path: /uploads/img.png
					staticRel, relErr := filepath.Rel(filepath.Join(runtime.RepoPath, runtime.StaticDir), path)
					if relErr != nil {
						return relErr
					}
					usagePath = "/" + filepath.ToSlash(staticRel)
				} else {
					// content/posts/slug/img.png -> img.png (Page Bundle)
					// Or if in subfolder src/img.png -> src/img.png
					bundleRel, relErr := filepath.Rel(root, path)
					if relErr != nil {
						return relErr
					}
					usagePath = filepath.ToSlash(bundleRel)
				}

				info, infoErr := d.Info()
				if infoErr != nil {
					return infoErr
				}
				files = append(files, MediaFile{
					Name:     d.Name(), // Or relative path from root?
					Path:     usagePath,
					Size:     info.Size(),
					URL:      "/admin/api/media/raw?path=" + url.QueryEscape(relPath),
					RepoPath: relPath,
				})
			}
			return nil
		})
		if err != nil {
			slog.Warn("Walk error during media listing", "root", root, "error", err)
		}
	}
	return files, nil
}

func SaveMediaFile(header *multipart.FileHeader, mode, articlePath string) (*MediaFile, error) {
	return SaveMediaFileForRuntime(config.CurrentSiteRuntime(), header, mode, articlePath)
}

func SaveMediaFileForRuntime(runtime config.SiteRuntime, header *multipart.FileHeader, mode, articlePath string) (*MediaFile, error) {
	unlock := LockRepositoryOperation()
	defer unlock()

	src, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	if header.Size <= 0 || header.Size > config.MaxUploadSize {
		return nil, fmt.Errorf("%w: invalid file size", ErrInvalidMedia)
	}

	filename := filepath.Base(header.Filename)
	filename = strings.ReplaceAll(filename, " ", "_")

	ext := strings.ToLower(filepath.Ext(filename))
	expectedContentType, ok := allowedMediaTypes[ext]
	if !ok {
		return nil, fmt.Errorf("%w: file type not allowed: %s", ErrInvalidMedia, ext)
	}

	name := strings.TrimSuffix(filename, ext)
	filename = fmt.Sprintf("%s_%d%s", name, time.Now().UnixNano(), ext)

	var targetDir string

	if mode == "static" {
		targetDir = SafeJoin(runtime.RepoPath, runtime.StaticDir, runtime.StaticMediaDir)
	} else if mode == "content" {
		// Content mode
		if articlePath == "" {
			return nil, fmt.Errorf("%w: article path required for content upload", ErrInvalidMedia)
		}
		fullArticlePath := SafeJoin(runtime.RepoPath, runtime.ContentDir, articlePath)
		if fullArticlePath == "" || strings.ToLower(filepath.Ext(fullArticlePath)) != ".md" {
			return nil, fmt.Errorf("%w: invalid article path", ErrInvalidMedia)
		}
		// Use ARTICLE_MEDIA_DIR config, constrained to the article bundle.
		targetDir = SafeJoin(filepath.Dir(fullArticlePath), "", runtime.ArticleMediaDir)
	} else {
		return nil, fmt.Errorf("%w: invalid media mode", ErrInvalidMedia)
	}
	if targetDir == "" {
		return nil, fmt.Errorf("%w: invalid media target directory", ErrInvalidMedia)
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, err
	}

	fullMediaPath := filepath.Join(targetDir, filename)
	dst, err := os.CreateTemp(targetDir, ".upload-*")
	if err != nil {
		return nil, err
	}
	tempPath := dst.Name()
	defer os.Remove(tempPath)

	head := make([]byte, 512)
	n, readErr := io.ReadFull(src, head)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		dst.Close()
		return nil, fmt.Errorf("read upload: %w", readErr)
	}
	if detected := http.DetectContentType(head[:n]); detected != expectedContentType {
		dst.Close()
		return nil, fmt.Errorf("%w: file content does not match extension: got %s", ErrInvalidMedia, detected)
	}

	reader := io.MultiReader(bytes.NewReader(head[:n]), src)
	written, copyErr := io.Copy(dst, io.LimitReader(reader, config.MaxUploadSize+1))
	if copyErr != nil {
		dst.Close()
		return nil, copyErr
	}
	if written > config.MaxUploadSize {
		dst.Close()
		return nil, fmt.Errorf("%w: file exceeds maximum upload size", ErrInvalidMedia)
	}
	if err := dst.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, fullMediaPath); err != nil {
		return nil, err
	}
	if err := os.Chmod(fullMediaPath, 0644); err != nil {
		return nil, err
	}

	// Calculate Result
	relPath, err := filepath.Rel(runtime.RepoPath, fullMediaPath)
	if err != nil {
		return nil, err
	}
	relPath = filepath.ToSlash(relPath)

	usagePath := ""
	if mode == "static" {
		staticRel, relErr := filepath.Rel(filepath.Join(runtime.RepoPath, runtime.StaticDir), fullMediaPath)
		if relErr != nil {
			return nil, relErr
		}
		usagePath = "/" + filepath.ToSlash(staticRel)
	} else {
		// Relative to bundle root
		// targetDir is bundle + subDir
		// We need path relative to bundle root.
		// Bundle root is targetDir without subDir (if subDir is relative)
		// Actually simpler:
		bundleRoot := filepath.Dir(SafeJoin(runtime.RepoPath, runtime.ContentDir, articlePath))
		bundleRel, relErr := filepath.Rel(bundleRoot, fullMediaPath)
		if relErr != nil {
			return nil, relErr
		}
		usagePath = filepath.ToSlash(bundleRel)
	}

	return &MediaFile{
		Name:     filename,
		Path:     usagePath,
		Size:     header.Size,
		URL:      "/admin/api/media/raw?path=" + url.QueryEscape(relPath),
		RepoPath: relPath,
	}, nil
}

func DeleteMediaFile(repoPath string) error {
	return DeleteMediaFileForRuntime(config.CurrentSiteRuntime(), repoPath)
}

func DeleteMediaFileForRuntime(runtime config.SiteRuntime, repoPath string) error {
	unlock := LockRepositoryOperation()
	defer unlock()

	if !ValidateMediaRepoPathForRuntime(runtime, repoPath) {
		return fmt.Errorf("%w: invalid media path", ErrInvalidMedia)
	}
	fullMediaPath := SafeJoin(runtime.RepoPath, "", repoPath)
	if fullMediaPath == "" {
		return fmt.Errorf("%w: invalid media path", ErrInvalidMedia)
	}
	return os.Remove(fullMediaPath)
}
