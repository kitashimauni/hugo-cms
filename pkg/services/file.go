package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func SafeJoin(root, sub, target string) string {
	cleanTarget := filepath.Clean(target)
	if strings.Contains(cleanTarget, "..") {
		return ""
	}
	return filepath.Join(root, sub, cleanTarget)
}

func ParseFrontMatter(content []byte) (map[string]interface{}, string, string, error) {
	str := string(content)
	// Check for YAML (---)
	if strings.HasPrefix(str, "---\n") || strings.HasPrefix(str, "---\r\n") {
		parts := strings.SplitN(str, "---", 3) // "", FM, Body
		if len(parts) == 3 {
			var fm map[string]interface{}
			if err := yaml.Unmarshal([]byte(parts[1]), &fm); err == nil {
				return fm, strings.TrimSpace(parts[2]), "yaml", nil
			}
		}
	}
	// Check for TOML (+++)
	if strings.HasPrefix(str, "+++\n") || strings.HasPrefix(str, "+++\r\n") {
		parts := strings.SplitN(str, "+++", 3)
		if len(parts) == 3 {
			var fm map[string]interface{}
			if err := toml.Unmarshal([]byte(parts[1]), &fm); err == nil {
				return fm, strings.TrimSpace(parts[2]), "toml", nil
			}
		}
	}
	// Check for JSON ({)
	if strings.HasPrefix(strings.TrimSpace(str), "{") {
		var fm map[string]interface{}
		if err := json.Unmarshal(content, &fm); err == nil {
			return fm, "", "json", nil
		}
	}

	return nil, "", "", fmt.Errorf("unknown format")
}

func ConstructFileContent(fm map[string]interface{}, body string, format string) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case "yaml":
		buf.WriteString("---\n")
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(fm); err != nil {
			return nil, err
		}
		buf.WriteString("---\n")
	case "toml":
		buf.WriteString("+++\n")
		enc := toml.NewEncoder(&buf)
		if err := enc.Encode(fm); err != nil {
			return nil, err
		}
		buf.WriteString("+++\n")
	case "json":
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(fm); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	if body != "" {
		buf.WriteString("\n")
		buf.WriteString(body)
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
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

func GenerateContentFromCollection(collection models.Collection, overrides map[string]interface{}) ([]byte, error) {
	fm := make(map[string]interface{})
	var bodyContent string

	for _, field := range collection.Fields {
		// Check override first
		if val, ok := overrides[field.Name]; ok {
			if field.Name == "body" {
				if strVal, ok := val.(string); ok {
					bodyContent = strVal
				}
				continue
			}
			fm[field.Name] = val
			continue
		}

		if field.Name == "body" {
			if field.Default != nil {
				if val, ok := field.Default.(string); ok {
					bodyContent = val
				}
			}
			continue
		}

		if field.Default != nil {
			fm[field.Name] = field.Default
		} else {
			switch field.Widget {
			case "datetime":
				fm[field.Name] = time.Now().Format(time.RFC3339)
			case "boolean":
				fm[field.Name] = false
			case "list":
				fm[field.Name] = []string{}
			default:
				fm[field.Name] = ""
			}
		}
	}
	
	// Use TOML as default for Hugo if not specified, but config says format: "toml-frontmatter"
	// We can check collection.Format if needed, but for now let's default to TOML or YAML based on standard practice or simple heuristic.
	// The provided config example has `format: "toml-frontmatter"`.
	// ConstructFileContent handles this if we pass "toml" or "yaml".
	// Let's assume toml for now as per config sample.
	
	return ConstructFileContent(fm, bodyContent, "toml")
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

func NormalizeContent(content []byte) []byte {
	if len(content) == 0 {
		return content
	}
	fm, body, format, err := ParseFrontMatter(content)
	if err != nil {
		return content
	}
	normalized, err := ConstructFileContent(fm, body, format)
	if err != nil {
		return content
	}
	return normalized
}