package services

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"io"
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

	contentDir := filepath.Join(config.RepoPath, "content")
	dirtyFiles, _ := getGitDirtyFiles(config.RepoPath)

	var paths []string
	err := filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			paths = append(paths, path)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	articles := make([]models.Article, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 20) // Limit concurrency

	for i, path := range paths {
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			relPath, _ := filepath.Rel(contentDir, path)

			repoRelPath, _ := filepath.Rel(config.RepoPath, path)
			repoRelPath = filepath.ToSlash(repoRelPath)
			isDirty := dirtyFiles[repoRelPath]

			// Read file to get title (Limit to 4KB for performance)
			content, err := readHead(path, 4096)
			title := relPath // Default to path
			if err == nil {
				fm, _, _, err := ParseFrontMatter(content)
				if err == nil {
					if t, ok := fm["title"].(string); ok {
						title = t
					}
				}
			}

			articles[i] = models.Article{
				Path:    relPath,
				Title:   title,
				IsDirty: isDirty,
			}
		}(i, path)
	}

	wg.Wait()

	articleCache = articles
	cacheLoaded = true
	return articleCache, nil
}

func readHead(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, limit)
	n, err := f.Read(buf)
	// io.EOF is acceptable if file is smaller than limit
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func getGitDirtyFiles(dir string) (map[string]bool, error) {
	cmd := exec.Command("git", "status", "--porcelain", "--", "content")
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
