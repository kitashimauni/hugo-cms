package services

import (
	"testing"
)

func TestArticleRevisionIsContentBased(t *testing.T) {
	first := ArticleRevision([]byte("article"))
	second := ArticleRevision([]byte("article"))
	changed := ArticleRevision([]byte("article\n"))
	if first != second {
		t.Fatalf("same content revisions differ: %q != %q", first, second)
	}
	if first == changed {
		t.Fatalf("different content revisions match: %q", first)
	}
	if len(first) != len("sha256:")+64 {
		t.Fatalf("revision = %q, want sha256 prefix and 64 hex characters", first)
	}
}

func TestArticleRevisionForPathReturnsEmptyForMissingArticle(t *testing.T) {
	revision, err := ArticleRevisionForPath(t.TempDir() + "/missing.md")
	if err != nil {
		t.Fatalf("ArticleRevisionForPath() error = %v", err)
	}
	if revision != "" {
		t.Fatalf("missing article revision = %q, want empty", revision)
	}
}
