package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"os"
	"path/filepath"
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

func GenerateContentFromCollection(collection models.Collection) ([]byte, error) {
	fm := make(map[string]interface{})
	var bodyContent string

	for _, field := range collection.Fields {
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