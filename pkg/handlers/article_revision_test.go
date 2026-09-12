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

func setupArticleRevisionTest(t *testing.T) string {
	t.Helper()
	restoreSiteScopeConfig(t)
	repoPath := t.TempDir()
	config.DefaultSiteID = "default"
	config.Sites = nil
	config.RepoPath = repoPath
	config.ContentDir = "content"
	contentPath := filepath.Join(repoPath, "content", "posts", "article.md")
	if err := os.MkdirAll(filepath.Dir(contentPath), 0755); err != nil {
		t.Fatalf("create content directory: %v", err)
	}
	if err := os.WriteFile(contentPath, []byte("original"), 0644); err != nil {
		t.Fatalf("write article: %v", err)
	}
	return contentPath
}

func getArticleRevision(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/article?path=posts/article.md", nil)
	GetArticle(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GetArticle() status = %d, body = %s", w.Code, w.Body.String())
	}
	var article models.Article
	if err := json.Unmarshal(w.Body.Bytes(), &article); err != nil {
		t.Fatalf("decode article: %v", err)
	}
	return article.Revision
}

func saveArticleForRevisionTest(t *testing.T, article models.Article) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(article)
	if err != nil {
		t.Fatalf("encode article: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/article", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SaveArticle(c)
	return w
}

func TestArticleSaveRejectsStaleRevisionAndReturnsLatestRevision(t *testing.T) {
	contentPath := setupArticleRevisionTest(t)
	baseRevision := getArticleRevision(t)

	if err := os.WriteFile(contentPath, []byte("remote update"), 0644); err != nil {
		t.Fatalf("write remote update: %v", err)
	}
	staleResponse := saveArticleForRevisionTest(t, models.Article{
		Path:         "posts/article.md",
		Content:      "stale local update",
		BaseRevision: baseRevision,
	})
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale save status = %d, body = %s", staleResponse.Code, staleResponse.Body.String())
	}
	content, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("read article after stale save: %v", err)
	}
	if string(content) != "remote update" {
		t.Fatalf("stale save overwrote article with %q", content)
	}

	latestRevision := services.ArticleRevision([]byte("remote update"))
	successResponse := saveArticleForRevisionTest(t, models.Article{
		Path:         "posts/article.md",
		Content:      "explicitly reloaded update",
		BaseRevision: latestRevision,
	})
	if successResponse.Code != http.StatusOK {
		t.Fatalf("current save status = %d, body = %s", successResponse.Code, successResponse.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(successResponse.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	wantRevision := services.ArticleRevision([]byte("explicitly reloaded update"))
	if response["revision"] != wantRevision {
		t.Fatalf("save revision = %q, want %q", response["revision"], wantRevision)
	}
}

func TestArticleRevisionIsScopedToEachArticle(t *testing.T) {
	contentPath := setupArticleRevisionTest(t)
	otherPath := filepath.Join(filepath.Dir(contentPath), "other.md")
	if err := os.WriteFile(otherPath, []byte("other original"), 0644); err != nil {
		t.Fatalf("write other article: %v", err)
	}
	baseRevision := getArticleRevision(t)
	if err := os.WriteFile(otherPath, []byte("other remote update"), 0644); err != nil {
		t.Fatalf("write other remote update: %v", err)
	}

	response := saveArticleForRevisionTest(t, models.Article{
		Path:         "posts/article.md",
		Content:      "article update",
		BaseRevision: baseRevision,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("save after unrelated article update status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestArticleDeleteRejectsStaleRevision(t *testing.T) {
	contentPath := setupArticleRevisionTest(t)
	baseRevision := getArticleRevision(t)
	if err := os.WriteFile(contentPath, []byte("remote update"), 0644); err != nil {
		t.Fatalf("write remote update: %v", err)
	}

	body, err := json.Marshal(map[string]string{
		"path":          "posts/article.md",
		"base_revision": baseRevision,
	})
	if err != nil {
		t.Fatalf("encode delete request: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/delete", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	DeleteArticle(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("stale delete status = %d, body = %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(contentPath); err != nil {
		t.Fatalf("stale delete removed article: %v", err)
	}
}
