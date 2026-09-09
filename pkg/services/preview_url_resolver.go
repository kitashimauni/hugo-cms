package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PreviewURLResolver asks the configured generator to resolve a content path
// to the URL that should be opened in the local preview origin. The resolver
// receives the active shadow workspace so unsaved front matter participates in
// the same URL calculation as the preview server.
type PreviewURLResolver interface {
	ResolveArticleURL(context.Context, config.SiteRuntime, LocalPreviewWorkspace, string) (string, error)
}

func NewPreviewURLResolver(generator string) (PreviewURLResolver, error) {
	switch strings.ToLower(strings.TrimSpace(generator)) {
	case "", "hugo":
		return &hugoPreviewURLResolver{}, nil
	case "eleventy", "11ty":
		return &eleventyPreviewURLResolver{}, nil
	default:
		return nil, fmt.Errorf("preview URL resolution is not supported for generator %q", generator)
	}
}

func ResolvePreviewArticleURL(ctx context.Context, runtime config.SiteRuntime, workspace LocalPreviewWorkspace, articlePath string) (string, error) {
	resolver, err := NewPreviewURLResolver(runtime.Generator)
	if err != nil {
		return "", err
	}
	return resolver.ResolveArticleURL(ctx, runtime, workspace, articlePath)
}

type hugoPreviewURLResolver struct {
	run func(context.Context, config.SiteRuntime, string) ([]byte, error)
}

func (resolver *hugoPreviewURLResolver) ResolveArticleURL(ctx context.Context, runtime config.SiteRuntime, workspace LocalPreviewWorkspace, articlePath string) (string, error) {
	articlePath = filepath.Clean(strings.TrimSpace(articlePath))
	if articlePath == "." || filepath.IsAbs(articlePath) {
		return "", fmt.Errorf("invalid preview article path")
	}
	if workspace.ContentDir == "" {
		return "", fmt.Errorf("preview workspace content directory is required")
	}
	if workspace.ArticlePath != filepath.ToSlash(articlePath) {
		return "", fmt.Errorf("preview workspace article does not match request")
	}
	if strings.TrimSpace(runtime.ProductionContentDir) == "" {
		runtime.ProductionContentDir = runtime.ContentDir
	}
	runtime.ContentDir = workspace.ContentDir
	previewURL, err := localPreviewResolverURL(runtime)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := configuredLocalPreviewStartupTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	run := resolver.run
	if run == nil {
		run = runHugoListAll
	}
	output, err := run(ctx, runtime, previewURL)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("hugo list all timed out after %s", timeout)
		}
		return "", fmt.Errorf("hugo list all failed: %w", err)
	}
	permalink, err := parseHugoListAll(output, runtime, articlePath)
	if err != nil {
		return "", err
	}
	return localPreviewArticleURL(previewURL, permalink)
}

func runHugoListAll(ctx context.Context, runtime config.SiteRuntime, previewURL string) ([]byte, error) {
	cmd := generatorCommandContextWithEnv(
		ctx,
		runtime,
		hugoListAllEnvironment(runtime, previewURL),
		"hugo",
		hugoListAllArgs()...,
	)
	return cmd.CombinedOutput()
}

func hugoListAllArgs() []string {
	return []string{
		"list",
		"all",
		"--source", ".",
		"--environment", localPreviewHugoEnvironment,
		"--noBuildLock",
	}
}

func hugoListAllEnvironment(runtime config.SiteRuntime, previewURL string) []string {
	return []string{
		"HUGO_CONTENTDIR=" + runtime.ContentDir,
		"HUGO_BASEURL=" + previewURL,
	}
}

func localPreviewResolverURL(runtime config.SiteRuntime) (string, error) {
	previewURL := strings.TrimSpace(runtime.LocalPreview.URL)
	if previewURL == "" {
		var err error
		previewURL, err = config.LocalPreviewURL(runtime.ID)
		if err != nil {
			return "", err
		}
	}
	parsed, err := url.Parse(previewURL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid local preview URL %q", previewURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported local preview scheme %q", parsed.Scheme)
	}
	return parsed.String(), nil
}

func parseHugoListAll(output []byte, runtime config.SiteRuntime, articlePath string) (string, error) {
	reader := csv.NewReader(bytes.NewReader(output))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	pathColumn := -1
	permalinkColumn := -1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse hugo list all output: %w", err)
		}
		if pathColumn < 0 {
			pathColumn = findHugoListColumn(record, "path")
			permalinkColumn = findHugoListColumn(record, "permalink")
			if permalinkColumn < 0 {
				permalinkColumn = findHugoListColumn(record, "url")
			}
			if pathColumn < 0 || permalinkColumn < 0 {
				continue
			}
			continue
		}
		if pathColumn >= len(record) || permalinkColumn >= len(record) {
			continue
		}
		if !hugoContentPathMatches(runtime, articlePath, record[pathColumn]) {
			continue
		}
		permalink := strings.TrimSpace(record[permalinkColumn])
		if permalink == "" {
			return "", fmt.Errorf("hugo did not provide a URL for article %q", articlePath)
		}
		return permalink, nil
	}
	return "", fmt.Errorf("hugo did not resolve article %q", articlePath)
}

func findHugoListColumn(record []string, name string) int {
	for index, value := range record {
		value = strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))
		if strings.EqualFold(value, name) {
			return index
		}
	}
	return -1
}

func hugoContentPathMatches(runtime config.SiteRuntime, articlePath, listedPath string) bool {
	target := normalizeHugoContentPath(articlePath)
	candidate := normalizeHugoContentPath(listedPath)
	if target == "" || candidate == "" {
		return false
	}
	if candidate == target {
		return true
	}
	contentDir := normalizeHugoContentPath(runtime.ContentDir)
	if contentDir != "" && candidate == contentDir+"/"+target {
		return true
	}
	contentBase := path.Base(strings.TrimSuffix(strings.ReplaceAll(runtime.ContentDir, "\\", "/"), "/"))
	contentBase = normalizeHugoContentPath(contentBase)
	return contentBase != "" && (candidate == contentBase+"/"+target || strings.HasSuffix(candidate, "/"+contentBase+"/"+target))
}

func normalizeHugoContentPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	value = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(value)), "/")
	if value == "." {
		return ""
	}
	return value
}

func localPreviewArticleURL(previewURL, resolvedURL string) (string, error) {
	base, err := url.Parse(previewURL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return "", fmt.Errorf("invalid local preview URL %q", previewURL)
	}
	resolved, err := url.Parse(strings.TrimSpace(resolvedURL))
	if err != nil || strings.TrimSpace(resolvedURL) == "" {
		return "", fmt.Errorf("invalid resolved preview URL %q", resolvedURL)
	}
	if resolved.Scheme != "" && resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf("unsupported resolved preview URL scheme %q", resolved.Scheme)
	}
	if !resolved.IsAbs() {
		resolved = base.ResolveReference(resolved)
	}
	resolved.Scheme = base.Scheme
	resolved.Host = base.Host
	resolved.User = nil
	return resolved.String(), nil
}

var _ PreviewURLResolver = (*hugoPreviewURLResolver)(nil)

type eleventyPreviewURLResolver struct {
	run func(context.Context, config.SiteRuntime) ([]byte, error)
}

func (resolver *eleventyPreviewURLResolver) ResolveArticleURL(ctx context.Context, runtime config.SiteRuntime, workspace LocalPreviewWorkspace, articlePath string) (string, error) {
	articlePath = filepath.Clean(strings.TrimSpace(articlePath))
	if articlePath == "." || filepath.IsAbs(articlePath) {
		return "", fmt.Errorf("invalid preview article path")
	}
	if workspace.ContentDir == "" {
		return "", fmt.Errorf("preview workspace content directory is required")
	}
	if workspace.ArticlePath != filepath.ToSlash(articlePath) {
		return "", fmt.Errorf("preview workspace article does not match request")
	}
	if strings.TrimSpace(runtime.ProductionContentDir) == "" {
		runtime.ProductionContentDir = runtime.ContentDir
	}
	runtime.ContentDir = workspace.ContentDir
	previewURL, err := localPreviewResolverURL(runtime)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := configuredLocalPreviewStartupTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	run := resolver.run
	if run == nil {
		run = runEleventyJSON
	}
	output, err := run(ctx, runtime)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("eleventy URL resolution timed out after %s", timeout)
		}
		return "", fmt.Errorf("eleventy URL resolution failed: %w", err)
	}
	resolvedURL, err := parseEleventyJSON(output, runtime, articlePath)
	if err != nil {
		return "", err
	}
	return localPreviewArticleURL(previewURL, resolvedURL)
}

type eleventyRunningMetadata struct {
	URL string `json:"url"`
}

// resolveRunningEleventyArticleURL reads the URL map exposed by the running
// Eleventy helper. A 503 means the watcher is still building (or rebuilding),
// so the request waits for the next completed build instead of starting a
// second Eleventy process.
func resolveRunningEleventyArticleURL(ctx context.Context, runtime config.SiteRuntime, port int, articlePath string) (string, error) {
	previewURL, err := localPreviewResolverURL(runtime)
	if err != nil {
		return "", err
	}
	articlePath = filepath.Clean(strings.TrimSpace(articlePath))
	if articlePath == "." || filepath.IsAbs(articlePath) {
		return "", fmt.Errorf("invalid preview article path")
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid local preview port %d", port)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	timeout := configuredLocalPreviewStartupTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(LocalPreviewBindAddress, strconv.Itoa(port)),
		Path:   eleventyLocalPreviewMetadataPath,
	}
	query := endpoint.Query()
	query.Set("path", filepath.ToSlash(articlePath))
	endpoint.RawQuery = query.Encode()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(defaultLocalPreviewProbeInterval)
	defer ticker.Stop()

	for {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if requestErr == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				switch response.StatusCode {
				case http.StatusOK:
					var metadata eleventyRunningMetadata
					decodeErr := json.NewDecoder(response.Body).Decode(&metadata)
					_ = response.Body.Close()
					if decodeErr != nil {
						return "", fmt.Errorf("decode Eleventy metadata: %w", decodeErr)
					}
					if strings.TrimSpace(metadata.URL) == "" {
						return "", fmt.Errorf("Eleventy metadata did not provide a URL for article %q", articlePath)
					}
					return localPreviewArticleURL(previewURL, metadata.URL)
				case http.StatusNotFound:
					_ = response.Body.Close()
					return "", fmt.Errorf("eleventy did not resolve article %q", articlePath)
				case http.StatusServiceUnavailable:
					_ = response.Body.Close()
				default:
					status := response.Status
					_ = response.Body.Close()
					return "", fmt.Errorf("Eleventy metadata endpoint returned %s", status)
				}
			}
		}

		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("eleventy running preview URL resolution timed out after %s", timeout)
			}
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

func runEleventyJSON(ctx context.Context, runtime config.SiteRuntime) ([]byte, error) {
	projectDir, outputDir, cleanup, err := prepareEleventyResolverProject(runtime)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	sourceRepoPath := strings.TrimSpace(runtime.LocalPreviewSourceRepoPath)
	if sourceRepoPath == "" {
		sourceRepoPath = runtime.RepoPath
	}
	pm, err := detectEleventyPackageManager(sourceRepoPath)
	if err != nil {
		return nil, err
	}
	scriptPath, err := eleventyLocalPreviewScriptPath()
	if err != nil {
		return nil, err
	}
	inputRuntime := runtime
	inputRuntime.RepoPath = sourceRepoPath
	inputDir, err := eleventyLocalPreviewInputDir(inputRuntime)
	if err != nil {
		return nil, err
	}
	commandRuntime := runtime
	commandRuntime.RepoPath = projectDir
	commandRuntime.ContentDir = inputDir
	commandRuntime.LocalPreviewProjectDir = projectDir
	args := eleventyNodeCommandArgs(pm, scriptPath,
		"--json",
		"--input", inputDir,
		"--output", outputDir,
	)
	cmd := generatorCommandContextWithEnv(
		ctx,
		commandRuntime,
		[]string{"NODE_ENV=development", "ELEVENTY_ENV=development"},
		pm.Bin,
		args...,
	)
	return cmd.Output()
}

func prepareEleventyResolverProject(runtime config.SiteRuntime) (string, string, func(), error) {
	sourceRepoPath := strings.TrimSpace(runtime.LocalPreviewSourceRepoPath)
	if sourceRepoPath == "" {
		sourceRepoPath = runtime.RepoPath
	}
	inputRuntime := runtime
	inputRuntime.RepoPath = sourceRepoPath
	inputDir, err := eleventyLocalPreviewInputDir(inputRuntime)
	if err != nil {
		return "", "", func() {}, err
	}
	publicDir, err := eleventyLocalPreviewPublicDir(runtime)
	if err != nil {
		return "", "", func() {}, err
	}

	projectDir, err := os.MkdirTemp("", "hugo-cms-eleventy-resolver-*")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("create Eleventy resolver project: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(projectDir) }

	contentSource := strings.TrimSpace(runtime.ContentDir)
	if !filepath.IsAbs(contentSource) {
		contentSource = filepath.Join(sourceRepoPath, inputDir)
	}
	if err := createEleventyLocalPreviewProjectOverlay(sourceRepoPath, projectDir, inputDir, publicDir, contentSource); err != nil {
		cleanup()
		return "", "", func() {}, err
	}
	return projectDir, filepath.Join(projectDir, publicDir), cleanup, nil
}

type eleventyPreviewJSONEntry struct {
	InputPath  string          `json:"inputPath"`
	URL        json.RawMessage `json:"url"`
	OutputPath string          `json:"outputPath"`
	Data       struct {
		Page struct {
			InputPath string          `json:"inputPath"`
			URL       json.RawMessage `json:"url"`
		} `json:"page"`
	} `json:"data"`
}

func parseEleventyJSON(output []byte, runtime config.SiteRuntime, articlePath string) (string, error) {
	var entries []eleventyPreviewJSONEntry
	if err := json.Unmarshal(output, &entries); err != nil {
		return "", fmt.Errorf("parse eleventy JSON output: %w", err)
	}
	for _, entry := range entries {
		inputPath := entry.InputPath
		if strings.TrimSpace(inputPath) == "" {
			inputPath = entry.Data.Page.InputPath
		}
		if !eleventyInputPathMatches(runtime.ContentDir, articlePath, inputPath) {
			continue
		}
		resolvedURL := jsonStringValue(entry.URL)
		if resolvedURL == "" {
			resolvedURL = jsonStringValue(entry.Data.Page.URL)
		}
		if resolvedURL == "" {
			continue
		}
		return resolvedURL, nil
	}
	return "", fmt.Errorf("eleventy did not resolve article %q", articlePath)
}

func jsonStringValue(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func eleventyInputPathMatches(inputDir, articlePath, listedPath string) bool {
	target := normalizePreviewPath(articlePath)
	candidate := normalizePreviewPath(listedPath)
	if target == "" || candidate == "" {
		return false
	}
	if candidate == target {
		return true
	}
	inputDir = strings.TrimSpace(inputDir)
	if inputDir == "" {
		return false
	}
	normalizedInputDir := normalizePreviewPath(inputDir)
	inputBase := path.Base(normalizedInputDir)
	if inputBase != "" && (candidate == inputBase+"/"+target || strings.HasSuffix(candidate, "/"+inputBase+"/"+target)) {
		return true
	}
	expected := path.Join(normalizedInputDir, target)
	return expected != "" && candidate == expected
}

func normalizePreviewPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimSuffix(path.Clean(value), "/")
	if value == "." {
		return ""
	}
	return value
}

var _ PreviewURLResolver = (*eleventyPreviewURLResolver)(nil)
