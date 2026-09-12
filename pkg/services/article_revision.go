package services

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// ArticleRevision returns the stable, content-based revision used by the
// production article optimistic-concurrency contract.
func ArticleRevision(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// ArticleRevisionForPath returns an empty revision when the article does not
// exist. This makes the empty revision the explicit "article absent" version
// for create/delete conflict checks.
func ArticleRevisionForPath(path string) (string, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ArticleRevision(content), nil
}
