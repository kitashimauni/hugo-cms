package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type MediaFile struct {
	Name string `json:"name"`
	Path string `json:"path"` // Relative path for usage in markdown
	Size int64  `json:"size"`
	URL  string `json:"url"`  // URL for preview
}

func GetMediaConfig(collectionName string) (string, string, error) {
	cfg, err := GetCMSConfig()
	if err != nil {
		return "", "", err
	}
	
	// Check collection override
	if collectionName != "" {
		for _, col := range cfg.Collections {
			if col.Name == collectionName {
				if col.MediaFolder != "" {
					return col.MediaFolder, col.PublicFolder, nil
				}
				break
			}
		}
	}

	if cfg.MediaFolder == "" {
		return "", "", fmt.Errorf("media_folder not configured")
	}
	return cfg.MediaFolder, cfg.PublicFolder, nil
}

func ListMediaFiles(collectionName string) ([]MediaFile, error) {
	mediaFolder, publicFolder, err := GetMediaConfig(collectionName)
	if err != nil {
		return nil, err
	}

	var files []MediaFile
	var searchDirs []string

	// Check for dynamic patterns {{...}}
	if strings.Contains(mediaFolder, "{{") {
		re := regexp.MustCompile(`\{\{[^}]+\}\}`)
		globPattern := re.ReplaceAllString(mediaFolder, "*")
		fullGlob := filepath.Join(config.RepoPath, globPattern)

		matches, err := filepath.Glob(fullGlob)
		if err != nil {
			return nil, err
		}
		searchDirs = matches
	} else {
		fullMediaPath := filepath.Join(config.RepoPath, mediaFolder)
		if _, err := os.Stat(fullMediaPath); os.IsNotExist(err) {
			os.MkdirAll(fullMediaPath, 0755)
		}
		searchDirs = []string{fullMediaPath}
	}

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			fullPath := filepath.Join(dir, entry.Name())
			relPath, _ := filepath.Rel(config.RepoPath, fullPath)
			relPath = filepath.ToSlash(relPath)

			usagePath := ""
			// Determine usage path (for Markdown insertion)
			if publicFolder != "" && !strings.Contains(publicFolder, "{{") {
				usagePath = filepath.ToSlash(filepath.Join(publicFolder, entry.Name()))
			} else {
				// Fallback logic
				if strings.HasPrefix(relPath, "static/") {
					usagePath = "/" + strings.TrimPrefix(relPath, "static/")
				} else if strings.HasPrefix(relPath, "content/") {
					// For page bundles, usually just filename
					usagePath = entry.Name()
				} else {
					// Fallback to media folder relative
					// usagePath = "/" + mediaFolder + "/" + entry.Name() // But mediaFolder might be dynamic
					usagePath = entry.Name()
				}
			}

			if !strings.HasPrefix(usagePath, "/") && !strings.HasPrefix(usagePath, "http") && strings.HasPrefix(relPath, "static/") {
				usagePath = "/" + usagePath
			}
			usagePath = strings.ReplaceAll(usagePath, "//", "/")

			// Display Name: include parent folder name if dynamic to distinguish
			displayName := entry.Name()
			if len(searchDirs) > 1 {
				displayName += " (" + filepath.Base(dir) + ")"
			}

			files = append(files, MediaFile{
				Name: displayName,
				Path: usagePath,
				Size: 0,
				URL:  "/api/media/raw?path=" + url.QueryEscape(relPath),
			})
		}
	}
	return files, nil
}

func SaveMediaFile(header *multipart.FileHeader, collectionName string) (*MediaFile, error) {
	mediaFolder, publicFolder, err := GetMediaConfig(collectionName)
	if err != nil {
		return nil, err
	}

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

	fullMediaPath := SafeJoin(config.RepoPath, mediaFolder, filename)
	if fullMediaPath == "" {
		return nil, fmt.Errorf("invalid media path")
	}

	dst, err := os.Create(fullMediaPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return nil, err
	}

	usagePath := ""
	if publicFolder != "" {
		usagePath = filepath.ToSlash(filepath.Join(publicFolder, filename))
	} else {
		cleaned := filepath.ToSlash(mediaFolder)
		if strings.HasPrefix(cleaned, "static/") {
			usagePath = "/" + strings.TrimPrefix(cleaned, "static/") + "/" + filename
		} else {
			usagePath = "/" + cleaned + "/" + filename
		}
	}
	if !strings.HasPrefix(usagePath, "/") {
		usagePath = "/" + usagePath
	}
	usagePath = strings.ReplaceAll(usagePath, "//", "/")

	return &MediaFile{
		Name: filename,
		Path: usagePath,
		Size: header.Size,
		URL:  usagePath,
	}, nil
}

func DeleteMediaFile(filename, collectionName string) error {
	mediaFolder, _, err := GetMediaConfig(collectionName)
	if err != nil {
		return err
	}

	fullMediaPath := SafeJoin(config.RepoPath, mediaFolder, filename)
	if fullMediaPath == "" {
		return fmt.Errorf("invalid media path")
	}

	return os.Remove(fullMediaPath)
}