package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
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

	fullMediaPath := filepath.Join(config.RepoPath, mediaFolder)
	
	// Create if not exists
	if _, err := os.Stat(fullMediaPath); os.IsNotExist(err) {
		os.MkdirAll(fullMediaPath, 0755)
	}

	entries, err := os.ReadDir(fullMediaPath)
	if err != nil {
		return nil, err
	}

	var files []MediaFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		info, err := entry.Info()
		if err != nil {
			continue
		}

		usagePath := ""
		if publicFolder != "" {
			usagePath = filepath.ToSlash(filepath.Join(publicFolder, entry.Name()))
		} else {
			// Fallback
			cleaned := filepath.ToSlash(mediaFolder)
			if strings.HasPrefix(cleaned, "static/") {
				usagePath = "/" + strings.TrimPrefix(cleaned, "static/") + "/" + entry.Name()
			} else if strings.HasPrefix(cleaned, "content/") {
				usagePath = entry.Name() 
			} else {
				usagePath = "/" + cleaned + "/" + entry.Name()
			}
		}
		if !strings.HasPrefix(usagePath, "/") && !strings.HasPrefix(usagePath, "http") {
			usagePath = "/" + usagePath
		}
		usagePath = strings.ReplaceAll(usagePath, "//", "/")

		files = append(files, MediaFile{
			Name: entry.Name(),
			Path: usagePath,
			Size: info.Size(),
			URL:  usagePath, 
		})
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