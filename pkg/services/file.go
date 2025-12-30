package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func SafeJoin(root, sub, target string) string {
	cleanTarget := filepath.Clean(target)
	if strings.Contains(cleanTarget, "..") {
		return ""
	}
	return filepath.Join(root, sub, cleanTarget)
}

func DeleteFile(targetPath string) error {
	fullPath := SafeJoin(config.RepoPath, "content", targetPath)
	if fullPath == "" {
		return fmt.Errorf("invalid path")
	}
	if err := os.Remove(fullPath); err != nil {
		return err
	}

	// Try to remove empty parent directories (e.g. bundle folders)
	// But ensure we don't remove top-level collection folders (e.g. content/posts)
	dir := filepath.Dir(fullPath)
	contentRoot := filepath.Join(config.RepoPath, "content")

	rel, err := filepath.Rel(contentRoot, dir)
	if err != nil {
		return nil // Should not happen if fullPath is inside contentRoot
	}

	// If it's root or top-level folder (e.g. "posts"), don't touch
	if rel == "." || !strings.Contains(rel, string(os.PathSeparator)) {
		return nil
	}

	// Check if empty
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		os.Remove(dir)
	}

	return nil
}

func GetConfig() (map[string]interface{}, error) {
	configPath := filepath.Join(config.RepoPath, "static/admin/config.yml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func GetCMSConfig() (*models.CMSConfig, error) {
	configPath := filepath.Join(config.RepoPath, "static/admin/config.yml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg models.CMSConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ResolvePath(collection models.Collection, fields map[string]interface{}) (string, error) {
	pathTmpl := collection.Path
	if pathTmpl == "" {
		// Default to {slug}.md or {title}.md
		pathTmpl = "{{slug}}"
	}

	// Prepare data for replacement
	data := make(map[string]string)

	// Helper to safely get string
	getString := func(key string) string {
		if v, ok := fields[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	// Date handling
	dateStr := getString("date")
	var t time.Time
	var err error
	if dateStr != "" {
		// Try parsing ISO format
		t, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			// Try other formats or fallback to Now
			t = time.Now()
		}
	} else {
		t = time.Now()
	}

	data["year"] = fmt.Sprintf("%04d", t.Year())
	data["month"] = fmt.Sprintf("%02d", t.Month())
	data["day"] = fmt.Sprintf("%02d", t.Day())
	data["hour"] = fmt.Sprintf("%02d", t.Hour())
	data["minute"] = fmt.Sprintf("%02d", t.Minute())
	data["second"] = fmt.Sprintf("%02d", t.Second())

	// Other fields
	for k, v := range fields {
		data[k] = fmt.Sprintf("%v", v)
	}

	// Regex to find {{...}}
	re := regexp.MustCompile(`{{([^}]+)}}`)

	resolvedPath := re.ReplaceAllStringFunc(pathTmpl, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-2])
		if val, ok := data[key]; ok {
			return val
		}
		// Special case: if key is "slug" but not in data, maybe derive from title?
		// For now, return empty or keep placeholder? Netlify CMS usually errors or requires it.
		// Let's return empty if not found.
		return ""
	})

	// Add extension
	ext := collection.Extension
	if ext == "" {
		ext = "md"
	}

	// If path doesn't end with extension, append it
	// But check if path is "folder/index" style
	if !strings.HasSuffix(resolvedPath, "."+ext) {
		resolvedPath = resolvedPath + "." + ext
	}

	return resolvedPath, nil
}

func GetCollectionForPath(relPath string) (*models.Collection, error) {
	cfg, err := GetCMSConfig()
	if err != nil {
		return nil, err
	}

	relPath = filepath.ToSlash(relPath)

	for _, col := range cfg.Collections {
		colFolder := filepath.ToSlash(filepath.Clean(col.Folder))
		if strings.HasPrefix(relPath, colFolder) {
			return &col, nil
		}
	}
	return nil, fmt.Errorf("no collection found")
}
