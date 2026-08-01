package services

import (
	"bytes"
	"errors"
	"fmt"
	stdhtml "html"
	"hugo-cms/pkg/config"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	goldmarktext "github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const MaxMarkdownPreviewSize = 1 << 20

var ErrMarkdownPreviewTooLarge = errors.New("markdown preview exceeds size limit")

var markdownPreviewRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

// RenderMarkdownPreview renders the editor body without executing raw HTML.
// The sanitizer is intentionally applied even though Goldmark escapes raw HTML,
// so future renderer options cannot accidentally widen the trust boundary.
func RenderMarkdownPreview(runtime config.SiteRuntime, articlePath, body string, frontmatter map[string]interface{}) (string, error) {
	if len(body) > MaxMarkdownPreviewSize {
		return "", ErrMarkdownPreviewTooLarge
	}

	var rendered bytes.Buffer
	if err := markdownPreviewRenderer.Convert([]byte(body), &rendered); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}

	policy := bluemonday.UGCPolicy()
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	safe := policy.SanitizeBytes(rendered.Bytes())
	rewritten, err := rewritePreviewImages(runtime, articlePath, safe)
	if err != nil {
		return "", err
	}

	return renderPreviewFrontmatter(frontmatter) + rewritten, nil
}

func rewritePreviewImages(runtime config.SiteRuntime, articlePath string, rendered []byte) (string, error) {
	contextNode := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(bytes.NewReader(rendered), contextNode)
	if err != nil {
		return "", fmt.Errorf("parse rendered markdown: %w", err)
	}

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "img" {
			for i := range node.Attr {
				if node.Attr[i].Key != "src" {
					continue
				}
				if rewritten, ok := previewImageURL(runtime, articlePath, node.Attr[i].Val); ok {
					node.Attr[i].Val = rewritten
				} else {
					node.Attr[i].Val = ""
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	var out bytes.Buffer
	for _, node := range nodes {
		walk(node)
		if err := html.Render(&out, node); err != nil {
			return "", fmt.Errorf("serialize rendered markdown: %w", err)
		}
	}
	return out.String(), nil
}

func previewImageURL(runtime config.SiteRuntime, articlePath, rawURL string) (string, bool) {
	repoPath, externalURL, ok := previewImageTarget(runtime, articlePath, rawURL)
	if !ok {
		return "", false
	}
	if externalURL != "" {
		return externalURL, true
	}

	query := url.Values{"path": {repoPath}}
	if runtime.ID != "" {
		query.Set("site", runtime.ID)
	}
	return "/admin/api/media/raw?" + query.Encode(), true
}

func previewImageTarget(runtime config.SiteRuntime, articlePath, rawURL string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", false
	}
	if parsed.IsAbs() {
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return "", parsed.String(), true
		}
		return "", "", false
	}
	if parsed.Host != "" || parsed.Path == "" || strings.Contains(parsed.Path, "\\") {
		return "", "", false
	}

	decodedPath, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", "", false
	}
	var repoPath string
	if strings.HasPrefix(decodedPath, "/") {
		repoPath = filepath.ToSlash(filepath.Join(runtime.StaticDir, filepath.FromSlash(strings.TrimPrefix(decodedPath, "/"))))
	} else {
		articleRepoPath := filepath.Join(runtime.ContentDir, filepath.FromSlash(articlePath))
		repoPath = filepath.ToSlash(filepath.Join(filepath.Dir(articleRepoPath), filepath.FromSlash(decodedPath)))
	}
	if !ValidateMediaRepoPathForRuntime(runtime, repoPath) {
		return "", "", false
	}
	fullPath := SafeJoin(runtime.RepoPath, "", repoPath)
	info, err := os.Stat(fullPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", false
	}
	return repoPath, "", true
}

// MarkdownPreviewMediaPaths returns only the local images referenced by the
// article. Deployment commits use this list instead of staging a site's whole
// static directory, which keeps independent drafts from collecting unrelated
// media changes.
func MarkdownPreviewMediaPaths(runtime config.SiteRuntime, articlePath, body string) []string {
	reader := goldmarktext.NewReader([]byte(body))
	document := markdownPreviewRenderer.Parser().Parse(reader)
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindImage {
			return ast.WalkContinue, nil
		}
		image, ok := node.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		repoPath, externalURL, ok := previewImageTarget(runtime, articlePath, string(image.Destination))
		if !ok || externalURL != "" {
			return ast.WalkContinue, nil
		}
		if _, exists := seen[repoPath]; !exists {
			seen[repoPath] = struct{}{}
			paths = append(paths, repoPath)
		}
		return ast.WalkContinue, nil
	})
	return paths
}

func renderPreviewFrontmatter(frontmatter map[string]interface{}) string {
	if len(frontmatter) == 0 {
		return ""
	}
	keys := []string{"title", "description", "date", "draft", "slug"}
	var out strings.Builder
	for _, key := range keys {
		value, ok := frontmatter[key]
		if !ok || value == nil {
			continue
		}
		if out.Len() == 0 {
			out.WriteString(`<dl class="markdown-preview-frontmatter">`)
		}
		out.WriteString("<dt>")
		out.WriteString(stdhtml.EscapeString(key))
		out.WriteString("</dt><dd>")
		out.WriteString(stdhtml.EscapeString(fmt.Sprint(value)))
		out.WriteString("</dd>")
	}
	if out.Len() == 0 {
		return ""
	}
	out.WriteString("</dl>")
	return out.String()
}
