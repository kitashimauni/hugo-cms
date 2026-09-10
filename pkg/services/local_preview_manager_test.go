package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"hugo-cms/pkg/config"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHugoLocalPreviewArgs(t *testing.T) {
	runtime := config.SiteRuntime{ContentDir: "content"}
	got, err := hugoLocalPreviewArgs(runtime, 14123, "https://tech.preview.example.com/")
	if err != nil {
		t.Fatalf("hugoLocalPreviewArgs() error = %v", err)
	}
	want := []string{
		"server",
		"--source", ".",
		"--environment", localPreviewHugoEnvironment,
		"--contentDir", "content",
		"--bind", "127.0.0.1",
		"--port", "14123",
		"--baseURL", "https://tech.preview.example.com/",
		"--appendPort=false",
		"--liveReloadPort", "443",
		"--renderToMemory",
		"--buildDrafts",
		"--buildFuture",
		"--buildExpired",
		"--watch",
		"--noHTTPCache",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hugoLocalPreviewArgs() = %#v, want %#v", got, want)
	}
}

func TestHugoLocalPreviewArgsUsesHTTPReloadPort(t *testing.T) {
	args, err := hugoLocalPreviewArgs(config.SiteRuntime{ContentDir: "content"}, 14123, "http://tech.preview.example.com/")
	if err != nil {
		t.Fatalf("hugoLocalPreviewArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--liveReloadPort 80") {
		t.Fatalf("args = %q, want live reload port 80", joined)
	}
}

func TestHugoLocalPreviewArgsRejectsURLPort(t *testing.T) {
	if _, err := hugoLocalPreviewArgs(config.SiteRuntime{ContentDir: "content"}, 14123, "https://tech.preview.example.com:8443/"); err == nil {
		t.Fatal("hugoLocalPreviewArgs() should reject an external URL port")
	}
}

func TestEleventyLocalPreviewArgs(t *testing.T) {
	repo := t.TempDir()
	runtime := config.SiteRuntime{
		RepoPath:             repo,
		ContentDir:           filepath.Join(t.TempDir(), "shadow", "content"),
		ProductionContentDir: "content",
	}
	outputDir := filepath.Join(t.TempDir(), "hugo-cms-local-preview", "output")
	got, err := eleventyLocalPreviewArgs(runtime, 14123, outputDir)
	if err != nil {
		t.Fatalf("eleventyLocalPreviewArgs() error = %v", err)
	}
	scriptPath, err := eleventyLocalPreviewScriptPath()
	if err != nil {
		t.Fatalf("eleventyLocalPreviewScriptPath() error = %v", err)
	}
	want := []string{
		"node", scriptPath,
		"--serve",
		"--input", "content",
		"--output", outputDir,
		"--port", "14123",
		"--host", LocalPreviewBindAddress,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eleventyLocalPreviewArgs() = %#v, want %#v", got, want)
	}
}

func TestEleventyLocalPreviewArgsRejectsRelativeOutput(t *testing.T) {
	if _, err := eleventyLocalPreviewArgs(config.SiteRuntime{ContentDir: "content"}, 14123, "_site"); err == nil {
		t.Fatal("eleventyLocalPreviewArgs() should reject a relative output directory")
	}
}

func TestEleventyNodeCommandArgsUsesDetectedPackageManager(t *testing.T) {
	for _, testCase := range []struct {
		name string
		pm   eleventyPackageManager
		want []string
	}{
		{name: "npm", pm: eleventyPackageManager{Name: "npm"}, want: []string{"exec", "--", "node", "helper.cjs", "--json"}},
		{name: "pnpm", pm: eleventyPackageManager{Name: "pnpm"}, want: []string{"exec", "node", "helper.cjs", "--json"}},
		{name: "yarn", pm: eleventyPackageManager{Name: "yarn"}, want: []string{"exec", "node", "helper.cjs", "--json"}},
		{name: "bun", pm: eleventyPackageManager{Name: "bun"}, want: []string{"run", "node", "helper.cjs", "--json"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := eleventyNodeCommandArgs(testCase.pm, "helper.cjs", "--json"); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("eleventyNodeCommandArgs() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestEleventyLocalPreviewCommandUsesDetectedPackageManagerAndLoopbackServer(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "content"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte(`{"devDependencies":{"@11ty/eleventy":"^3.0.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0644); err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{
		ID:                   "daily-blog",
		RepoPath:             repo,
		Generator:            "eleventy",
		ContentDir:           "content",
		ProductionContentDir: "content",
	}
	cmd, err := eleventyLocalPreviewCommand(context.Background(), runtime, 14123)
	if err != nil {
		t.Fatalf("eleventyLocalPreviewCommand() error = %v", err)
	}
	cleanup := localPreviewProcessCleanup(runtime, cmd.Dir)
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Errorf("cleanup() error = %v", err)
		}
	})
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{"npm", "exec", "--", "node", "eleventy-local-preview.cjs", "--serve", "--input", "content", "--output", "--port", "14123", "--host", LocalPreviewBindAddress} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command args = %q, missing %q", joined, want)
		}
	}
	if cmd.Dir == repo || !strings.Contains(cmd.Dir, "hugo-cms-local-preview") {
		t.Fatalf("command dir = %q, want an isolated Eleventy project root", cmd.Dir)
	}
}

func TestLocalPreviewManagerSupportsEleventy(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	site.Generator = "eleventy"
	defer shutdownTestLocalPreviewManager(t, manager)

	if _, err := manager.EnsureReady(site); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
}

func TestLocalPreviewManagerReusesSiteRuntimeForRepeatedPreparation(t *testing.T) {
	for _, generator := range []string{"hugo", "eleventy"} {
		t.Run(generator, func(t *testing.T) {
			manager, site := newTestLocalPreviewManager(t)
			site.Generator = generator
			defer shutdownTestLocalPreviewManager(t, manager)

			first, err := manager.EnsureReady(site)
			if err != nil {
				t.Fatalf("first EnsureReady() error = %v", err)
			}
			firstProcess := manager.process(site.ID)
			second, err := manager.EnsureReady(site)
			if err != nil {
				t.Fatalf("second EnsureReady() error = %v", err)
			}
			if first.Port != second.Port || firstProcess == nil || manager.process(site.ID) != firstProcess {
				t.Fatalf("site runtime was recreated: first=%#v second=%#v process_changed=%v", first, second, manager.process(site.ID) != firstProcess)
			}
		})
	}
}

func TestLocalPreviewManagerResolvesEleventyURLFromRunningProcess(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	site.Generator = "eleventy"
	defer shutdownTestLocalPreviewManager(t, manager)

	runtime := config.NewSiteRuntime(site)
	workspace := LocalPreviewWorkspace{
		SiteID:      site.ID,
		ArticlePath: "posts/one.md",
		ContentDir:  filepath.Join(t.TempDir(), "content"),
	}
	got, err := manager.ResolveArticleURL(context.Background(), runtime, workspace, workspace.ArticlePath)
	if err != nil {
		t.Fatalf("ResolveArticleURL() error = %v", err)
	}
	if got != "http://tech.preview.example.com/posts/one/" {
		t.Fatalf("resolved URL = %q", got)
	}
}

func TestLocalPreviewManagerInvalidatesEleventyMetadata(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	site.Generator = "eleventy"
	defer shutdownTestLocalPreviewManager(t, manager)

	if _, err := manager.EnsureReady(site); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	if err := manager.InvalidateArticleURL(config.NewSiteRuntime(site)); err != nil {
		t.Fatalf("InvalidateArticleURL() error = %v", err)
	}
}

func TestLocalPreviewManagerEnsureReadyAndProxy(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	defer shutdownTestLocalPreviewManager(t, manager)

	slot, err := manager.EnsureReady(site)
	if err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	if slot.State != LocalPreviewReady {
		t.Fatalf("slot.State = %q, want ready", slot.State)
	}

	request := httptest.NewRequest(http.MethodGet, "http://tech.preview.example.com/css/main.css", nil)
	request.Host = "tech.preview.example.com"
	response := httptest.NewRecorder()
	if err := manager.Proxy(response, request, site); err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "path=/css/main.css") {
		t.Fatalf("proxy changed root-relative path: %q", body)
	}
	if !strings.Contains(body, "host=tech.preview.example.com") {
		t.Fatalf("proxy did not preserve external host: %q", body)
	}
	if !strings.Contains(body, "proto=http") {
		t.Fatalf("proxy did not set external scheme: %q", body)
	}
}

func TestLocalPreviewProxyRewritesInternalLocation(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	defer shutdownTestLocalPreviewManager(t, manager)

	request := httptest.NewRequest(http.MethodGet, "http://tech.preview.example.com/redirect", nil)
	request.Host = "tech.preview.example.com"
	response := httptest.NewRecorder()
	if err := manager.Proxy(response, request, site); err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	if got := response.Header().Get("Location"); got != "http://tech.preview.example.com/target" {
		t.Fatalf("Location = %q", got)
	}
}

func TestLocalPreviewProxyPassesUpgrade(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	defer shutdownTestLocalPreviewManager(t, manager)

	outer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := manager.Proxy(w, r, site); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	}))
	defer outer.Close()

	address := strings.TrimPrefix(outer.URL, "http://")
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "GET /_livereload HTTP/1.1\r\nHost: tech.preview.example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"); err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", response.StatusCode)
	}
}

func TestLocalPreviewManagerStopReleasesSlot(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)

	if _, err := manager.EnsureReady(site); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Stop(ctx, site.ID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, ok := manager.Status(site.ID); ok {
		t.Fatal("Stop() should release the lifecycle slot")
	}
}

func TestLocalPreviewManagerStopDoesNotWaitForCleanup(t *testing.T) {
	lifecycle, err := NewLocalPreviewLifecycle(14100, 14100)
	if err != nil {
		t.Fatalf("NewLocalPreviewLifecycle() error = %v", err)
	}
	manager := NewLocalPreviewManager(lifecycle)
	const siteID = "tech"
	if _, err := lifecycle.Reserve(siteID, nil); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if _, err := lifecycle.Transition(siteID, LocalPreviewStarting, nil); err != nil {
		t.Fatalf("Transition(starting) error = %v", err)
	}
	if _, err := lifecycle.Transition(siteID, LocalPreviewReady, nil); err != nil {
		t.Fatalf("Transition(ready) error = %v", err)
	}

	terminated := make(chan struct{})
	cleanupDone := make(chan struct{})
	cleanupStarted := make(chan struct{})
	releaseCleanup := make(chan struct{})
	var cancelOnce sync.Once
	process := &managedLocalPreviewProcess{
		cmd:         &exec.Cmd{},
		done:        terminated,
		cleanupDone: cleanupDone,
		cleanup: func() error {
			close(cleanupStarted)
			<-releaseCleanup
			return nil
		},
	}
	process.cancel = func() {
		cancelOnce.Do(func() {
			close(terminated)
			go process.finishCleanup(siteID)
		})
	}
	manager.setProcess(siteID, process)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := manager.Stop(ctx, siteID); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, ok := manager.Status(siteID); ok {
		t.Fatal("Stop() should release the lifecycle slot before cleanup completes")
	}
	select {
	case <-cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start after process termination")
	}

	close(releaseCleanup)
	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not complete")
	}
	if err := process.cleanupError(); err != nil {
		t.Fatalf("cleanup error = %v", err)
	}
}

func TestEleventyLocalPreviewCleanupLeavesWorkspaceOwnedProject(t *testing.T) {
	projectDir := t.TempDir()
	outputDir := filepath.Join(projectDir, "public")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	outputFile := filepath.Join(outputDir, "generated.html")
	if err := os.WriteFile(outputFile, []byte("preview"), 0644); err != nil {
		t.Fatal(err)
	}

	runtime := config.SiteRuntime{
		Generator:              "eleventy",
		LocalPreviewProjectDir: projectDir,
	}
	cleanup := localPreviewProcessCleanup(runtime, projectDir)
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if _, err := os.Stat(outputFile); err != nil {
		t.Fatalf("workspace-owned output was removed by process cleanup: %v", err)
	}
}

func TestEleventyLocalPreviewGenerationsDoNotShareCleanupPath(t *testing.T) {
	repo := t.TempDir()
	contentDir := filepath.Join(repo, "content")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "one.md"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{
		ID:         "tech",
		RepoPath:   repo,
		Generator:  "eleventy",
		ContentDir: "content",
	}

	firstProject, _, err := prepareEleventyLocalPreviewProject(runtime)
	if err != nil {
		t.Fatalf("prepare first project: %v", err)
	}
	secondProject, _, err := prepareEleventyLocalPreviewProject(runtime)
	if err != nil {
		t.Fatalf("prepare second project: %v", err)
	}
	if firstProject == secondProject {
		t.Fatalf("preview generations share project path %q", firstProject)
	}

	firstCleanup := localPreviewProcessCleanup(runtime, firstProject)
	if err := firstCleanup(); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	if _, err := os.Stat(secondProject); err != nil {
		t.Fatalf("old cleanup removed new preview project: %v", err)
	}
	secondCleanup := localPreviewProcessCleanup(runtime, secondProject)
	if err := secondCleanup(); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
}

func TestLocalPreviewManagerBeginShutdownRejectsNewStarts(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	manager.BeginShutdown()

	if _, err := manager.EnsureReady(site); !errors.Is(err, errLocalPreviewShuttingDown) {
		t.Fatalf("EnsureReady() error = %v, want shutting down", err)
	}
	if _, ok := manager.Status(site.ID); ok {
		t.Fatal("EnsureReady() should not reserve a slot after BeginShutdown")
	}
}

func TestLocalPreviewManagerShutdownRejectsRestart(t *testing.T) {
	manager, site := newTestLocalPreviewManager(t)
	if _, err := manager.EnsureReady(site); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, ok := manager.Status(site.ID); ok {
		t.Fatal("Shutdown() should release the lifecycle slot")
	}
	if _, err := manager.EnsureReady(site); !errors.Is(err, errLocalPreviewShuttingDown) {
		t.Fatalf("EnsureReady() after Shutdown error = %v, want shutting down", err)
	}
}

func newTestLocalPreviewManager(t *testing.T) (*LocalPreviewManager, config.SiteConfig) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	lifecycle, err := NewLocalPreviewLifecycle(port, port)
	if err != nil {
		t.Fatalf("NewLocalPreviewLifecycle() error = %v", err)
	}
	manager := NewLocalPreviewManager(lifecycle)
	manager.commandFactory = testLocalPreviewCommand
	manager.startAttempts = 1
	manager.startupTimeout = 5 * time.Second
	manager.probeInterval = 10 * time.Millisecond

	enabled := true
	site := config.SiteConfig{
		ID:         "tech",
		Generator:  "hugo",
		RepoPath:   ".",
		ContentDir: "content",
		Preview: config.SitePreviewConfig{
			LocalPreview: config.LocalPreviewConfig{
				Enabled: &enabled,
				URL:     "http://tech.preview.example.com/",
			},
		},
	}
	return manager, site
}

func shutdownTestLocalPreviewManager(t *testing.T, manager *LocalPreviewManager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func testLocalPreviewCommand(ctx context.Context, runtime config.SiteRuntime, port int, _ string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalPreviewHelperProcess$")
	cmd.Env = append(os.Environ(),
		"HUGO_CMS_LOCAL_PREVIEW_HELPER=1",
		"HUGO_CMS_LOCAL_PREVIEW_PORT="+strconv.Itoa(port),
		"HUGO_CMS_LOCAL_PREVIEW_GENERATOR="+runtime.Generator,
	)
	return cmd, nil
}

func TestLocalPreviewHelperProcess(t *testing.T) {
	if os.Getenv("HUGO_CMS_LOCAL_PREVIEW_HELPER") != "1" {
		return
	}
	port, err := strconv.Atoi(os.Getenv("HUGO_CMS_LOCAL_PREVIEW_PORT"))
	if err != nil {
		os.Exit(2)
	}
	generator := strings.ToLower(strings.TrimSpace(os.Getenv("HUGO_CMS_LOCAL_PREVIEW_GENERATOR")))
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		os.Exit(3)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if generator == "eleventy" && r.URL.Path == eleventyLocalPreviewInvalidatePath {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if generator == "eleventy" && r.URL.Path == eleventyLocalPreviewReadyPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"ready","building":false}`)
			return
		}
		if generator == "eleventy" && r.URL.Path == eleventyLocalPreviewMetadataPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"resolved","url":"/posts/one/"}`)
			return
		}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				os.Exit(4)
			}
			conn, rw, err := hijacker.Hijack()
			if err != nil {
				os.Exit(5)
			}
			_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
			_ = rw.Flush()
			_, _ = io.Copy(conn, conn)
			_ = conn.Close()
			return
		}
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "http://127.0.0.1:"+strconv.Itoa(port)+"/target")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = fmt.Fprintf(w, "path=%s host=%s proto=%s forwarded-host=%s", r.URL.Path, r.Host, r.Header.Get("X-Forwarded-Proto"), r.Header.Get("X-Forwarded-Host"))
	})

	server := &http.Server{Handler: handler}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		os.Exit(6)
	}
	os.Exit(0)
}
