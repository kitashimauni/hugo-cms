package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/models"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	articleCache []models.Article
	cacheMutex   sync.Mutex
	cacheLoaded  bool
)

func GetArticlesCache() ([]models.Article, error) {
	start := time.Now()
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if cacheLoaded {
		return articleCache, nil
	}
	
	defer func() {
		fmt.Printf("[Cache] Rebuild All Duration: %v, Count: %d\n", time.Since(start), len(articleCache))
	}()

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
		
		// Verify if semantically dirty (ignore formatting changes)
		if isSemanticallyDirty, _ := CheckSemanticDiff(path); isSemanticallyDirty {
			dirty[path] = true
		}
	}
	return dirty, nil
}

func InvalidateCache() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	cacheLoaded = false
	articleCache = nil
}

func UpdateCache(relPath string) {
	start := time.Now()
	defer func() {
		fmt.Printf("[Cache] Update Single: %s, Duration: %v\n", relPath, time.Since(start))
	}()

	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if !cacheLoaded {
		return // Next Get will rebuild
	}

	fullPath := filepath.Join(config.RepoPath, "content", relPath)
	
	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		// Remove from cache
		for i, art := range articleCache {
			if art.Path == relPath {
				articleCache = append(articleCache[:i], articleCache[i+1:]...)
				break
			}
		}
		return
	}

	// For now assuming update/create means it exists or we handle error
	content, err := readHead(fullPath, 4096)
	if err != nil {
		return // Ignore error, maybe remove from cache if not found?
	}

	title := relPath
	fm, _, _, err := ParseFrontMatter(content)
	if err == nil {
		if t, ok := fm["title"].(string); ok {
			title = t
		}
	}

	isDirty, _ := getGitFileStatus(relPath)

	newArt := models.Article{
		Path:    relPath,
		Title:   title,
		IsDirty: isDirty,
	}

	found := false
	for i, art := range articleCache {
		if art.Path == relPath {
			articleCache[i] = newArt
			found = true
			break
		}
	}
	if !found {
		articleCache = append(articleCache, newArt)
	}
}

func getGitFileStatus(relPath string) (bool, error) {
	// git status --porcelain content/posts/xxx.md
	// Note: relPath is relative to content/, but git needs relative to RepoPath
	target := filepath.Join("content", relPath)
	cmd := exec.Command("git", "status", "--porcelain", target)
	cmd.Dir = config.RepoPath
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		// Verify semantically
		return CheckSemanticDiff(target)
	}
	return false, nil
}
