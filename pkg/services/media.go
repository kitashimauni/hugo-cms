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

func GetMediaConfig() (string, string, error) {
	cfg, err := GetCMSConfig()
	if err != nil {
		return "", "", err
	}
	if cfg.MediaFolder == "" {
		return "", "", fmt.Errorf("media_folder not configured")
	}
	return cfg.MediaFolder, cfg.PublicFolder, nil
}

func ListMediaFiles() ([]MediaFile, error) {
	mediaFolder, publicFolder, err := GetMediaConfig()
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

		// Filter images? For now, list all.
		// Construct public URL
		// If public_folder is set (e.g. /img), use it.
		// If media_folder is "static/img", and public_folder is "/img".
		// usage path: /img/filename.ext
		
		usagePath := ""
		if publicFolder != "" {
			usagePath = filepath.ToSlash(filepath.Join(publicFolder, entry.Name()))
		} else {
			// Fallback: try to deduce from media_folder
			// If media_folder starts with "static/", usage path is "/" + rest
			// e.g. static/images -> /images
			cleaned := filepath.ToSlash(mediaFolder)
			if strings.HasPrefix(cleaned, "static/") {
				usagePath = "/" + strings.TrimPrefix(cleaned, "static/") + "/" + entry.Name()
			} else if strings.HasPrefix(cleaned, "content/") {
				// Page bundles? Complex.
				usagePath = entry.Name() // Relative to page
			} else {
				usagePath = "/" + cleaned + "/" + entry.Name()
			}
		}
		// Ensure leading slash for absolute paths if intended
		if !strings.HasPrefix(usagePath, "/") && !strings.HasPrefix(usagePath, "http") {
			usagePath = "/" + usagePath
		}
		// Remove double slashes
		usagePath = strings.ReplaceAll(usagePath, "//", "/")

		// URL for preview: same as usage path usually, assuming server serves static files
		// Our Go server serves /static from ./static.
		// If media is in repo/static/img, we need to proxy or serve it?
		// Current main.go: r.Static("/static", "./static") (Project root static, NOT repo static)
		// r.Any(config.PreviewURL+"*path", ...) proxies to Hugo Server.
		// Hugo Server serves `static/` content at root `/`.
		// So `http://localhost:1314/img/foo.jpg` works if `repo/static/img/foo.jpg` exists.
		// So preview URL = usagePath (relative to root).

		files = append(files, MediaFile{
			Name: entry.Name(),
			Path: usagePath,
			Size: info.Size(),
			URL:  usagePath, // Used for <img> src in CMS
		})
	}
	return files, nil
}

func SaveMediaFile(header *multipart.FileHeader) (*MediaFile, error) {
	mediaFolder, publicFolder, err := GetMediaConfig()
	if err != nil {
		return nil, err
	}

	src, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	// sanitize filename
	filename := filepath.Base(header.Filename)
	filename = strings.ReplaceAll(filename, " ", "_")
	
	// Prevent overwriting? Append timestamp if exists?
	// For now, let's append timestamp to ensure uniqueness and cache busting
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

	// Invalidate cache? No need for articles.
	// Return info
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

func DeleteMediaFile(filename string) error {
	mediaFolder, _, err := GetMediaConfig()
	if err != nil {
		return err
	}

	fullMediaPath := SafeJoin(config.RepoPath, mediaFolder, filename)
	if fullMediaPath == "" {
		return fmt.Errorf("invalid media path")
	}

	return os.Remove(fullMediaPath)
}
