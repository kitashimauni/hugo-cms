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
	if root == "" || filepath.IsAbs(sub) || filepath.IsAbs(target) {
		return ""
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	intendedRoot := filepath.Join(rootAbs, sub)
	if !isPathWithin(rootAbs, intendedRoot) {
		return ""
	}

	finalPath := filepath.Join(intendedRoot, target)
	if !isPathWithin(intendedRoot, finalPath) {
		return ""
	}

	if !isResolvedPathWithin(rootAbs, finalPath) {
		return ""
	}
	return filepath.Join(root, sub, target)
}

func isPathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

// isResolvedPathWithin prevents existing symlinks below a target root from
// escaping it. Missing suffixes are allowed for new files.
func isResolvedPathWithin(root, target string) bool {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return os.IsNotExist(err)
	}

	rootBoundary := root
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		rootBoundary, err = filepath.EvalSymlinks(root)
		if err != nil {
			return false
		}
	}

	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	current := root
	for _, component := range strings.Split(rel, string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return true
		}
		if statErr != nil {
			return false
		}

		if info.Mode()&os.ModeSymlink != 0 {
			if rootBoundary == root {
				rootBoundary, err = filepath.EvalSymlinks(root)
				if err != nil {
					return false
				}
			}
			current, err = filepath.EvalSymlinks(current)
			if err != nil || !isPathWithin(rootBoundary, current) {
				return false
			}
		}
	}
	return true
}

func DeleteFile(targetPath string) error {
	fullPath := SafeJoin(config.RepoPath, config.ContentDir, targetPath)
	if fullPath == "" {
		return fmt.Errorf("invalid path")
	}
	if err := os.Remove(fullPath); err != nil {
		return err
	}

	// Try to remove empty parent directories (e.g. bundle folders)
	// But ensure we don't remove top-level collection folders (e.g. content/posts)
	dir := filepath.Dir(fullPath)
	contentRoot := filepath.Join(config.RepoPath, config.ContentDir)

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
	return GetConfigForRuntime(config.CurrentSiteRuntime())
}

func GetConfigForRuntime(runtime config.SiteRuntime) (map[string]interface{}, error) {
	configPath := filepath.Join(runtime.RepoPath, runtime.StaticDir, "admin", "config.yml")
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
	return GetCMSConfigForRuntime(config.CurrentSiteRuntime())
}

func GetCMSConfigForRuntime(runtime config.SiteRuntime) (*models.CMSConfig, error) {
	configPath := filepath.Join(runtime.RepoPath, runtime.StaticDir, "admin", "config.yml")
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

	relPath = filepath.ToSlash(filepath.Clean(relPath))

	for i := range cfg.Collections {
		col := &cfg.Collections[i]
		colFolder, err := CollectionFolderWithinContent(*col)
		if err != nil {
			continue
		}
		if relPath == colFolder || strings.HasPrefix(relPath, colFolder+"/") {
			return col, nil
		}
	}
	return nil, fmt.Errorf("no collection found")
}

func CollectionFolderWithinContent(collection models.Collection) (string, error) {
	folder := filepath.ToSlash(filepath.Clean(collection.Folder))
	contentDir := filepath.ToSlash(filepath.Clean(config.ContentDir))
	if folder == "." || filepath.IsAbs(folder) || strings.HasPrefix(folder, "/") || strings.HasPrefix(folder, "../") || folder == ".." {
		return "", fmt.Errorf("Invalid collection folder")
	}
	if contentDir == "." || filepath.IsAbs(contentDir) || strings.HasPrefix(contentDir, "/") || strings.HasPrefix(contentDir, "../") || contentDir == ".." {
		return "", fmt.Errorf("Invalid content directory")
	}

	if folder == contentDir || strings.HasPrefix(folder, contentDir+"/") {
		return folder, nil
	}

	const legacyContentDir = "content"
	if contentDir != legacyContentDir {
		if folder == legacyContentDir {
			return contentDir, nil
		}
		if strings.HasPrefix(folder, legacyContentDir+"/") {
			return filepath.ToSlash(filepath.Join(contentDir, strings.TrimPrefix(folder, legacyContentDir+"/"))), nil
		}
	}

	return "", fmt.Errorf("Collection folder must be under %s", config.ContentDir)
}
