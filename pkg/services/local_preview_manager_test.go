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
	"reflect"
	"strconv"
	"strings"
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

func testLocalPreviewCommand(ctx context.Context, _ config.SiteRuntime, port int, _ string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalPreviewHelperProcess$")
	cmd.Env = append(os.Environ(),
		"HUGO_CMS_LOCAL_PREVIEW_HELPER=1",
		"HUGO_CMS_LOCAL_PREVIEW_PORT="+strconv.Itoa(port),
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
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		os.Exit(3)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
