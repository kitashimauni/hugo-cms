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
	Name     string `json:"name"`
	Path     string `json:"path"` // Relative path for usage in markdown
	Size     int64  `json:"size"`
	URL      string `json:"url"`  // URL for preview
	RepoPath string `json:"repo_path"`
}

func GetMediaConfig(collectionName string) (string, string, error) {
	cfg, err := GetCMSConfig()
	if err != nil {
		return "", "", err
	}
	
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

	var collectionFolder string
	if collectionName != "" {
		cfg, _ := GetCMSConfig()
		for _, col := range cfg.Collections {
			if col.Name == collectionName {
				collectionFolder = col.Folder
				break
			}
		}
	}

	var searchDirs []string
	
	// Strategies: 1. As configured (relative to repo), 2. Relative to collection folder
	strategies := []string{mediaFolder}
	if collectionFolder != "" {
		// Try appending mediaFolder to collectionFolder
		// Clean mediaFolder first to ensure it joins correctly
		cleanMF := strings.TrimLeft(mediaFolder, `/\`)
		strategies = append(strategies, filepath.Join(collectionFolder, cleanMF))
	}

	for _, pattern := range strategies {
		pattern = strings.TrimLeft(pattern, `/\`)
		
		if strings.Contains(pattern, "{{") {
			re := regexp.MustCompile(`\{\{[^}]+\}\}`)
			globPattern := re.ReplaceAllString(pattern, "*")
			fullGlob := filepath.Join(config.RepoPath, globPattern)
			
			// Debug
			fmt.Printf("[ListMedia] Trying Glob: %s\n", fullGlob)

			matches, err := filepath.Glob(fullGlob)
			if err == nil && len(matches) > 0 {
				searchDirs = matches
				break
			}
		} else {
			fullPath := filepath.Join(config.RepoPath, pattern)
			if info, err := os.Stat(fullPath); err == nil && info.IsDir() {
				searchDirs = []string{fullPath}
				break
			}
		}
	}

	var files []MediaFile
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
			// Usage path logic
			if publicFolder != "" && !strings.Contains(publicFolder, "{{") {
				usagePath = filepath.ToSlash(filepath.Join(publicFolder, entry.Name()))
			} else {
				if strings.HasPrefix(relPath, "static/") {
					usagePath = "/" + strings.TrimPrefix(relPath, "static/")
				} else if strings.HasPrefix(relPath, "content/") {
					usagePath = entry.Name()
				} else {
					usagePath = entry.Name()
				}
			}

			if !strings.HasPrefix(usagePath, "/") && !strings.HasPrefix(usagePath, "http") && strings.HasPrefix(relPath, "static/") {
				usagePath = "/" + usagePath
			}
			usagePath = strings.ReplaceAll(usagePath, "//", "/")

			displayName := entry.Name()
			if len(searchDirs) > 1 {
				displayName += " (" + filepath.Base(dir) + ")"
			}

			files = append(files, MediaFile{
				Name:     displayName,
				Path:     usagePath,
				Size:     0,
				URL:      "/api/media/raw?path=" + url.QueryEscape(relPath),
				RepoPath: relPath,
			})
		}
	}
	return files, nil
}

func SaveMediaFile(header *multipart.FileHeader, collectionName, articlePath string) (*MediaFile, error) {
	mediaFolder, publicFolder, err := GetMediaConfig(collectionName)
	if err != nil {
		return nil, err
	}

	mediaFolder = strings.TrimLeft(mediaFolder, `/\`)

	resolvedMediaFolder := mediaFolder
	if strings.Contains(mediaFolder, "{{") {
		if articlePath == "" {
			return nil, fmt.Errorf("cannot upload to dynamic folder without article context")
		}

		bundleDir := filepath.Dir(articlePath)
		suffix := ""
		lastBrace := strings.LastIndex(mediaFolder, "}}")
		if lastBrace != -1 && lastBrace < len(mediaFolder)-2 {
			suffix = mediaFolder[lastBrace+2:]
			suffix = strings.TrimLeft(suffix, `/\`)
		}

		fullTargetDir := filepath.Join(config.RepoPath, "content", bundleDir, suffix)
		if err := os.MkdirAll(fullTargetDir, 0755); err != nil {
			return nil, err
		}

		rel, err := filepath.Rel(config.RepoPath, fullTargetDir)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("resolved path outside repo")
		}
		resolvedMediaFolder = rel
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

	repoRelPath := filepath.Join(resolvedMediaFolder, filename)
	fullMediaPath := SafeJoin(config.RepoPath, "", repoRelPath)
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
	if publicFolder != "" && !strings.Contains(publicFolder, "{{") {
		usagePath = filepath.ToSlash(filepath.Join(publicFolder, filename))
	} else {
		relPath, _ := filepath.Rel(config.RepoPath, fullMediaPath)
		relPath = filepath.ToSlash(relPath)
		
		if strings.HasPrefix(relPath, "static/") {
			usagePath = "/" + strings.TrimPrefix(relPath, "static/")
		} else {
			usagePath = filename
		}
	}
	
	if !strings.HasPrefix(usagePath, "/") && !strings.HasPrefix(usagePath, "http") && strings.HasPrefix(resolvedMediaFolder, "static/") {
		usagePath = "/" + usagePath
	}
	usagePath = strings.ReplaceAll(usagePath, "//", "/")

	finalRepoPath, _ := filepath.Rel(config.RepoPath, fullMediaPath)
	finalRepoPath = filepath.ToSlash(finalRepoPath)

	return &MediaFile{
		Name:     filename,
		Path:     usagePath,
		Size:     header.Size,
		URL:      "/api/media/raw?path=" + url.QueryEscape(finalRepoPath),
		RepoPath: finalRepoPath,
	}, nil
}

func DeleteMediaFile(repoPath string) error {
	fullMediaPath := SafeJoin(config.RepoPath, "", repoPath)
	if fullMediaPath == "" {
		return fmt.Errorf("invalid media path")
	}
	return os.Remove(fullMediaPath)
}
