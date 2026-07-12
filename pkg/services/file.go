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

const (
	homeCMSConfigFile   = ".homecms.yml"
	legacyCMSConfigFile = "config.yml"
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

func DeleteFileForRuntime(runtime config.SiteRuntime, targetPath string) error {
	fullPath := SafeJoin(runtime.RepoPath, runtime.ContentDir, targetPath)
	if fullPath == "" {
		return fmt.Errorf("invalid path")
	}
	if err := os.Remove(fullPath); err != nil {
		return err
	}

	// Try to remove empty parent directories (e.g. bundle folders)
	// But ensure we don't remove top-level collection folders (e.g. content/posts)
	dir := filepath.Dir(fullPath)
	contentRoot := filepath.Join(runtime.RepoPath, runtime.ContentDir)

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

func GetConfigForRuntime(runtime config.SiteRuntime) (map[string]interface{}, error) {
	rawConfig, source, err := LoadConfigMapForRuntime(runtime)
	if err != nil {
		return nil, err
	}

	if rawConfig == nil {
		rawConfig = map[string]interface{}{}
	}
	rawConfig["_config_source"] = source
	return rawConfig, nil
}

func LoadConfigMapForRuntime(runtime config.SiteRuntime) (map[string]interface{}, string, error) {
	homePath := filepath.Join(runtime.RepoPath, homeCMSConfigFile)
	if _, err := os.Stat(homePath); err == nil {
		cmsConfig, err := readHomeCMSConfig(homePath)
		if err != nil {
			return nil, homeCMSConfigFile, err
		}
		rawConfig, err := cmsConfigToMap(cmsConfig)
		return rawConfig, homeCMSConfigFile, err
	} else if !os.IsNotExist(err) {
		return nil, homeCMSConfigFile, err
	}

	configPath := legacyCMSConfigPath(runtime)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, legacyCMSConfigFile, err
	}
	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(content, &rawConfig); err != nil {
		return nil, legacyCMSConfigFile, err
	}
	return rawConfig, legacyCMSConfigFile, nil
}

func GetCMSConfigForRuntime(runtime config.SiteRuntime) (*models.CMSConfig, error) {
	cfg, _, err := LoadCMSConfigForRuntime(runtime)
	return cfg, err
}

func LoadCMSConfigForRuntime(runtime config.SiteRuntime) (*models.CMSConfig, string, error) {
	homePath := filepath.Join(runtime.RepoPath, homeCMSConfigFile)
	if _, err := os.Stat(homePath); err == nil {
		cfg, err := readHomeCMSConfig(homePath)
		return cfg, homeCMSConfigFile, err
	} else if !os.IsNotExist(err) {
		return nil, homeCMSConfigFile, err
	}

	configPath := legacyCMSConfigPath(runtime)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, legacyCMSConfigFile, err
	}

	var cfg models.CMSConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, legacyCMSConfigFile, err
	}
	return &cfg, legacyCMSConfigFile, nil
}

func legacyCMSConfigPath(runtime config.SiteRuntime) string {
	return filepath.Join(runtime.RepoPath, runtime.StaticDir, "admin", legacyCMSConfigFile)
}

func cmsConfigToMap(cmsConfig *models.CMSConfig) (map[string]interface{}, error) {
	content, err := yaml.Marshal(cmsConfig)
	if err != nil {
		return nil, err
	}
	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(content, &rawConfig); err != nil {
		return nil, err
	}
	return rawConfig, nil
}

func readHomeCMSConfig(path string) (*models.CMSConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var home models.HomeCMSConfig
	if err := yaml.Unmarshal(content, &home); err != nil {
		return nil, err
	}
	if home.Version != 0 && home.Version != 1 {
		return nil, fmt.Errorf("unsupported .homecms.yml version %d", home.Version)
	}

	cfg := models.CMSConfig{
		MediaFolder:  cleanConfigPath(home.Media.Folder),
		PublicFolder: cleanPublicPath(home.Media.PublicPath),
		Preview: models.CMSPreview{
			URLField: strings.TrimSpace(home.Preview.URLField),
		},
		Collections: make([]models.Collection, 0, len(home.Content.Collections)),
	}
	for _, collection := range home.Content.Collections {
		cfg.Collections = append(cfg.Collections, models.Collection{
			Name:         collection.Name,
			Label:        collection.Label,
			Folder:       cleanConfigPath(collection.Folder),
			Path:         collection.Path,
			Extension:    collection.Extension,
			Format:       homeFrontMatterFormat(collection.FrontMatter),
			MediaFolder:  cleanConfigPath(collection.MediaFolder),
			PublicFolder: cleanPublicPath(collection.PublicFolder),
			Fields:       collection.Fields,
		})
	}
	return &cfg, nil
}

func homeFrontMatterFormat(frontMatter string) string {
	switch strings.ToLower(strings.TrimSpace(frontMatter)) {
	case "", "yaml", "yml":
		return "yaml-frontmatter"
	case "toml":
		return "toml-frontmatter"
	case "json":
		return "json-frontmatter"
	default:
		return frontMatter
	}
}

func cleanConfigPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func cleanPublicPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = "/" + strings.Trim(path, "/")
	if path == "/" {
		return ""
	}
	return path
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

func GetCollectionForPathForRuntime(runtime config.SiteRuntime, relPath string) (*models.Collection, error) {
	cfg, err := GetCMSConfigForRuntime(runtime)
	if err != nil {
		return nil, err
	}

	relPath = filepath.ToSlash(filepath.Clean(relPath))

	for i := range cfg.Collections {
		col := &cfg.Collections[i]
		colFolder, err := CollectionFolderWithinContentForRuntime(runtime, *col)
		if err != nil {
			continue
		}
		if relPath == colFolder || strings.HasPrefix(relPath, colFolder+"/") {
			return col, nil
		}
	}
	return nil, fmt.Errorf("no collection found")
}

func CollectionFolderWithinContentForRuntime(runtime config.SiteRuntime, collection models.Collection) (string, error) {
	folder := filepath.ToSlash(filepath.Clean(collection.Folder))
	contentDir := filepath.ToSlash(filepath.Clean(runtime.ContentDir))
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

	return "", fmt.Errorf("Collection folder must be under %s", runtime.ContentDir)
}
