package handlers

import (
	"bytes"
	"encoding/json"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"hugo-cms/pkg/services"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func restoreArticleCreateConfig(t *testing.T, repoPath, contentDir, staticDir string) {
	t.Helper()

	originalRepoPath := config.RepoPath
	originalContentDir := config.ContentDir
	originalStaticDir := config.StaticDir
	t.Cleanup(func() {
		config.RepoPath = originalRepoPath
		config.ContentDir = originalContentDir
		config.StaticDir = originalStaticDir
	})

	config.RepoPath = repoPath
	config.ContentDir = contentDir
	config.StaticDir = staticDir
}

func TestCreateArticleNormalizesLegacyCollectionFolderToContentDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repoPath := t.TempDir()
	restoreArticleCreateConfig(t, repoPath, "src", "static")

	configPath := filepath.Join(repoPath, "static", "admin", "config.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`
collections:
  - name: posts
    folder: content/posts
    path: "{{slug}}"
    format: yaml-frontmatter
    fields:
      - name: title
        widget: string
      - name: body
        widget: markdown
`), 0644); err != nil {
		t.Fatalf("write CMS config: %v", err)
	}

	body := []byte(`{"collection":"posts","fields":{"slug":"new","title":"New Post","body":"Hello"}}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/create", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	CreateArticle(c)

	if w.Code != http.StatusOK {
		t.Fatalf("CreateArticle() status = %d, body = %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(repoPath, "src", "posts", "new.md")); err != nil {
		t.Fatalf("created article should be under configured content dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "content", "posts", "new.md")); !os.IsNotExist(err) {
		t.Fatalf("article should not be created under legacy content dir, stat err = %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["path"] != "posts/new.md" {
		t.Fatalf("response path = %q, want posts/new.md", resp["path"])
	}
}

func TestCollectionFolderWithinContentRejectsOutsideFolder(t *testing.T) {
	restoreArticleCreateConfig(t, t.TempDir(), "src", "static")

	runtime := config.CurrentSiteRuntime()
	if _, err := services.CollectionFolderWithinContentForRuntime(runtime, models.Collection{Folder: "assets/posts"}); err == nil {
		t.Fatal("collectionFolderWithinContent() should reject folders outside configured content dir")
	}
}
