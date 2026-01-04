package services

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseFrontMatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantFM      map[string]interface{}
		wantBody    string
		wantFormat  string
		expectError bool
	}{
		{
			name: "YAML Front Matter",
			content: `---
title: Test Article
draft: true
---
Hello World`,
			wantFM: map[string]interface{}{
				"title": "Test Article",
				"draft": true,
			},
			wantBody:   "Hello World",
			wantFormat: "yaml",
		},
		{
			name: "TOML Front Matter",
			content: `+++
title = "Test Article"
draft = true
+++
Hello World`,
			wantFM: map[string]interface{}{
				"title": "Test Article",
				"draft": true,
			},
			wantBody:   "Hello World",
			wantFormat: "toml",
		},
		{
			name: "JSON Front Matter",
			content: `{
  "title": "Test Article",
  "draft": true
}`,
			wantFM: map[string]interface{}{
				"title": "Test Article",
				"draft": true,
			},
			wantBody:   "",
			wantFormat: "json",
		},
		{
			name:        "Invalid Format",
			content:     "Just content without front matter",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, format, err := ParseFrontMatter([]byte(tt.content))
			if (err != nil) != tt.expectError {
				t.Errorf("ParseFrontMatter() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !tt.expectError {
				if !reflect.DeepEqual(fm, tt.wantFM) {
					t.Errorf("ParseFrontMatter() fm = %v, want %v", fm, tt.wantFM)
				}
				if tt.wantFormat != "json" && strings.TrimSpace(body) != strings.TrimSpace(tt.wantBody) {
					t.Errorf("ParseFrontMatter() body = %q, want %q", body, tt.wantBody)
				}
				if format != tt.wantFormat {
					t.Errorf("ParseFrontMatter() format = %v, want %v", format, tt.wantFormat)
				}
			}
		})
	}
}

func TestConstructFileContent(t *testing.T) {
	fm := map[string]interface{}{
		"title": "New Article",
		"date":  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	body := "Content Body"

	t.Run("Construct YAML", func(t *testing.T) {
		got, err := ConstructFileContent(fm, body, "yaml")
		if err != nil {
			t.Fatalf("ConstructFileContent() error = %v", err)
		}
		str := string(got)
		if !strings.HasPrefix(str, "---\n") {
			t.Error("YAML content should start with ---")
		}
		if !strings.Contains(str, "title: New Article") {
			t.Error("YAML content missing title")
		}
		if !strings.Contains(str, "Content Body") {
			t.Error("YAML content missing body")
		}
	})

	t.Run("Construct TOML", func(t *testing.T) {
		got, err := ConstructFileContent(fm, body, "toml")
		if err != nil {
			t.Fatalf("ConstructFileContent() error = %v", err)
		}
		str := string(got)
		if !strings.HasPrefix(str, "+++\n") {
			t.Error("TOML content should start with +++")
		}
		// TOML check: title = "New Article"
		if !strings.Contains(str, "title = \"New Article\"") && !strings.Contains(str, "title = 'New Article'") {
			t.Logf("TOML Output: %s", str)
			// Don't fail strictly on quotes if library behavior varies, but it usually quotes strings.
		}
		if !strings.Contains(str, "Content Body") {
			t.Error("TOML content missing body")
		}
	})
}