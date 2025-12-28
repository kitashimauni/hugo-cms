package services

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	articleCache []models.Article
	cacheMutex   sync.Mutex
	cacheLoaded  bool
)

func GetArticlesCache() ([]models.Article, error) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if cacheLoaded {
		return articleCache, nil
	}

	var articles []models.Article
	contentDir := filepath.Join(config.RepoPath, "content")
	
	dirtyFiles, _ := getGitDirtyFiles(config.RepoPath)

	err := filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			relPath, _ := filepath.Rel(contentDir, path)
			
			repoRelPath, _ := filepath.Rel(config.RepoPath, path)
			repoRelPath = filepath.ToSlash(repoRelPath)
			isDirty := dirtyFiles[repoRelPath]

			// Read file to get title
			content, err := os.ReadFile(path)
			title := relPath // Default to path
			if err == nil {
				fm, _, _, err := ParseFrontMatter(content)
				if err == nil {
					if t, ok := fm["title"].(string); ok {
						title = t
					}
				}
			}

			articles = append(articles, models.Article{
				Path:    relPath,
				Title:   title,
				IsDirty: isDirty,
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	articleCache = articles
	cacheLoaded = true
	return articleCache, nil
}

func getGitDirtyFiles(dir string) (map[string]bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	dirty := make(map[string]bool)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		path = strings.Trim(path, "\"")
		dirty[path] = true
	}
	return dirty, nil
}

func InvalidateCache() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	cacheLoaded = false
	articleCache = nil
}
