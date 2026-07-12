package services

import (
	"hugo-cms/pkg/config"
	"path/filepath"
	"testing"
)

func TestValidateConfigForRuntimeWarnsAboutHomeCMSCreateAndPreviewIssues(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: posts
      folder: content/posts
      path: "{{slug}}"
      frontmatter: yaml
      fields:
        - { name: title, widget: string }
media:
  folder: static/images
preview:
  url_field: permalink
`)

	warnings := ValidateConfigForRuntime(testRuntime(repoPath), ".homecms.yml")
	assertWarningCode(t, warnings, "missing_slug_field")
	assertWarningCode(t, warnings, "unknown_preview_url_field")
}

func TestValidateConfigForRuntimeWarnsAboutLegacyConfig(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, "static", "admin", "config.yml"), `
collections:
  - name: posts
    folder: content/posts
    path: "{{title}}"
    fields:
      - name: title
        widget: string
`)

	warnings := ValidateConfigForRuntime(testRuntime(repoPath), "config.yml")
	assertWarningCode(t, warnings, "legacy_config")
	assertNoWarningCode(t, warnings, "unknown_path_variable")
}

func TestValidateConfigForRuntimeWarnsAboutInvalidPaths(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: posts
      folder: ../posts
      path: "{{slug}}"
      fields:
        - { name: slug, widget: mystery }
media:
  folder: /absolute
`)

	warnings := ValidateConfigForRuntime(config.NewSiteRuntime(config.SiteConfig{
		ID:         "test",
		RepoPath:   repoPath,
		ContentDir: "content",
		StaticDir:  "static",
	}), ".homecms.yml")
	assertWarningCode(t, warnings, "invalid_collection_folder")
	assertWarningCode(t, warnings, "invalid_media_folder")
	assertWarningCode(t, warnings, "unsupported_widget")
}

func TestValidateConfigForRuntimeReturnsEmptySliceForCleanHomeCMSConfig(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: posts
      folder: content/posts
      path: "{{slug}}"
      frontmatter: yaml
      fields:
        - { name: slug, widget: string }
        - { name: title, widget: string }
        - { name: permalink, widget: string }
preview:
  url_field: permalink
`)

	warnings := ValidateConfigForRuntime(testRuntime(repoPath), ".homecms.yml")
	if warnings == nil {
		t.Fatal("ValidateConfigForRuntime() returned nil warnings, want empty slice")
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want empty slice", warnings)
	}
}

func assertWarningCode(t *testing.T, warnings []ConfigWarning, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code {
			return
		}
	}
	t.Fatalf("warning code %q not found in %#v", code, warnings)
}

func assertNoWarningCode(t *testing.T, warnings []ConfigWarning, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code {
			t.Fatalf("warning code %q unexpectedly found in %#v", code, warnings)
		}
	}
}
