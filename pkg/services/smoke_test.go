package services

import (
	"hugo-cms/pkg/config"
	"path/filepath"
	"testing"
)

func TestSmokeHomeCMSHugoCustomContentDir(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: posts
      label: Posts
      folder: src/posts
      path: "{{slug}}"
      extension: md
      frontmatter: yaml
      fields:
        - { name: slug, label: Slug, widget: string }
        - { name: title, label: Title, widget: string }
        - { name: permalink, label: Permalink, widget: string }
        - { name: body, label: Body, widget: markdown }
media:
  folder: static
preview:
  url_field: permalink
`)
	writeTestFile(t, filepath.Join(repoPath, "src", "posts", "hello.md"), `---
title: Hello Smoke
permalink: /blog/hello/
---

Body
`)

	runtime := config.NewSiteRuntime(config.SiteConfig{
		ID:             "hugo-smoke",
		RepoPath:       repoPath,
		Generator:      "hugo",
		ContentDir:     "src",
		StaticDir:      "static",
		PublicDir:      "public",
		PreviewURL:     "/",
		HugoServerBind: "127.0.0.1",
		HugoServerPort: "1314",
	})

	rawConfig, err := GetConfigForRuntime(runtime)
	if err != nil {
		t.Fatalf("GetConfigForRuntime() error = %v", err)
	}
	if rawConfig["_config_source"] != ".homecms.yml" {
		t.Fatalf("_config_source = %q, want .homecms.yml", rawConfig["_config_source"])
	}
	preview, ok := rawConfig["preview"].(map[string]interface{})
	if !ok || preview["url_field"] != "permalink" {
		t.Fatalf("preview = %#v, want permalink url field", rawConfig["preview"])
	}

	cmsConfig, err := GetCMSConfigForRuntime(runtime)
	if err != nil {
		t.Fatalf("GetCMSConfigForRuntime() error = %v", err)
	}
	if got := cmsConfig.Preview.URLField; got != "permalink" {
		t.Fatalf("Preview.URLField = %q, want permalink", got)
	}
	if len(cmsConfig.Collections) != 1 {
		t.Fatalf("collections = %#v, want one collection", cmsConfig.Collections)
	}
	if got := cmsConfig.Collections[0].Folder; got != "src/posts" {
		t.Fatalf("collection folder = %q, want src/posts", got)
	}

	articles, err := GetArticlesCacheForRuntime(runtime)
	if err != nil {
		t.Fatalf("GetArticlesCacheForRuntime() error = %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("articles = %#v, want one article", articles)
	}
	if articles[0].Path != "posts/hello.md" {
		t.Fatalf("article path = %q, want posts/hello.md", articles[0].Path)
	}
	if articles[0].Title != "Hello Smoke" {
		t.Fatalf("article title = %q, want Hello Smoke", articles[0].Title)
	}

	collection, err := GetCollectionForPathForRuntime(runtime, "src/posts/hello.md")
	if err != nil {
		t.Fatalf("GetCollectionForPathForRuntime() error = %v", err)
	}
	relFolder, err := CollectionFolderWithinContentForRuntime(runtime, *collection)
	if err != nil {
		t.Fatalf("CollectionFolderWithinContentForRuntime() error = %v", err)
	}
	if relFolder != filepath.ToSlash(filepath.Join("src", "posts")) {
		t.Fatalf("collection content folder = %q, want src/posts", relFolder)
	}
}

func TestSmokeEleventyHomeCMSRuntimeMetadata(t *testing.T) {
	repoPath := t.TempDir()
	writeTestFile(t, filepath.Join(repoPath, "package.json"), `{
  "dependencies": {
    "@11ty/eleventy": "^3.0.0"
  }
}
`)
	writeTestFile(t, filepath.Join(repoPath, "pnpm-lock.yaml"), "lockfileVersion: '9.0'\n")
	writeTestFile(t, filepath.Join(repoPath, ".homecms.yml"), `
version: 1
content:
  collections:
    - name: notes
      label: Notes
      folder: src/notes
      path: "{{slug}}"
      extension: md
      frontmatter: yaml
      fields:
        - { name: slug, label: Slug, widget: string }
        - { name: title, label: Title, widget: string }
        - { name: body, label: Body, widget: markdown }
media:
  folder: public-assets/images
  public_path: /images
`)
	writeTestFile(t, filepath.Join(repoPath, "src", "notes", "first.md"), `---
title: First Note
---

Body
`)

	runtime := config.NewSiteRuntime(config.SiteConfig{
		ID:             "eleventy-smoke",
		RepoPath:       repoPath,
		Generator:      "eleventy",
		ContentDir:     "src",
		StaticDir:      "public-assets",
		PublicDir:      "_site",
		PreviewURL:     "/",
		HugoServerBind: "127.0.0.1",
		HugoServerPort: "1315",
	})

	pm, err := detectEleventyPackageManager(runtime.RepoPath)
	if err != nil {
		t.Fatalf("detectEleventyPackageManager() error = %v", err)
	}
	if pm.Name != "pnpm" {
		t.Fatalf("package manager = %q, want pnpm", pm.Name)
	}

	articles, err := GetArticlesCacheForRuntime(runtime)
	if err != nil {
		t.Fatalf("GetArticlesCacheForRuntime() error = %v", err)
	}
	if len(articles) != 1 || articles[0].Path != "notes/first.md" || articles[0].Title != "First Note" {
		t.Fatalf("articles = %#v, want notes/first.md titled First Note", articles)
	}

	mediaTarget := staticMediaTargetForRuntime(runtime)
	if mediaTarget.repoRelDir != "public-assets/images" {
		t.Fatalf("static media repo dir = %q, want public-assets/images", mediaTarget.repoRelDir)
	}
	if mediaTarget.publicBase != "/images" {
		t.Fatalf("static media public base = %q, want /images", mediaTarget.publicBase)
	}
}
