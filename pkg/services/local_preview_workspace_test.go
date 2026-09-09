package services

import (
	"context"
	"hugo-cms/pkg/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalPreviewWorkspaceMirrorsAndUpdatesContent(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "content", "two.md"), []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	workspace, created, applied, err := manager.Update(runtime, "one.md", 1, []byte("new"))
	if err != nil || !created || !applied {
		t.Fatalf("Update() created=%v applied=%v err=%v", created, applied, err)
	}
	assertWorkspaceFileContent(t, filepath.Join(workspace.ContentDir, "one.md"), "new")
	assertWorkspaceFileContent(t, filepath.Join(workspace.ContentDir, "two.md"), "second")
	assertWorkspaceFileContent(t, filepath.Join(repo, "content", "one.md"), "original")
}

func TestLocalPreviewWorkspaceAllowsIndependentTabsAndUsesServerRevision(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	first, created, _, err := manager.Update(runtime, "one.md", 100, []byte("tab A"))
	if err != nil || !created || first.Revision != 1 {
		t.Fatalf("first update = %#v created=%v err=%v", first, created, err)
	}
	second, created, _, err := manager.Update(runtime, "two.md", 1, []byte("tab B"))
	if err != nil || created || second.Revision != 2 || second.ArticlePath != "two.md" {
		t.Fatalf("second update = %#v created=%v err=%v", second, created, err)
	}
	third, _, _, err := manager.Update(runtime, "one.md", 1, []byte("tab C"))
	if err != nil || third.Revision != 3 || third.ArticlePath != "one.md" {
		t.Fatalf("third update = %#v err=%v", third, err)
	}
	assertFileContent(t, filepath.Join(third.ContentDir, "one.md"), "tab C")
}

func TestLocalPreviewWorkspaceReusesWorkspaceWhenArticleChanges(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	first, _, _, err := manager.Update(runtime, "one.md", 1, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := manager.Update(runtime, "two.md", 1, []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentDir != second.ContentDir || first.ProjectDir != second.ProjectDir {
		t.Fatalf("article switch recreated workspace: first=%#v second=%#v", first, second)
	}
	assertWorkspaceFileContent(t, filepath.Join(second.ContentDir, "one.md"), "first")
	assertWorkspaceFileContent(t, filepath.Join(second.ContentDir, "two.md"), "second")
}

func TestLocalPreviewIdleRuntimeDetachesIdleWorkspace(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return base }
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	workspace, _, _, err := manager.Update(runtime, "one.md", 1, []byte("draft"))
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return base.Add(DefaultLocalPreviewIdleTimeout + time.Second) }
	previewManager := NewLocalPreviewManager(nil)
	previewManager.idleTimeout = DefaultLocalPreviewIdleTimeout
	if err := previewManager.StopIdle(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	if _, active := manager.Status(runtime.ID); active {
		t.Fatal("idle workspace is still active")
	}
	if _, err := os.Stat(workspace.ContentDir); !os.IsNotExist(err) {
		t.Fatalf("workspace was not detached: %v", err)
	}
}

func TestLocalPreviewWorkspaceCleanupGateBlocksIngressAndUpdates(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	if _, _, _, err := manager.Update(runtime, "one.md", 1, []byte("draft")); err != nil {
		t.Fatal(err)
	}
	ingress, _, active, transitioning := manager.AcquireIngress(runtime.ID)
	if !active || transitioning {
		ingress.Release()
		t.Fatalf("ingress active=%v transitioning=%v", active, transitioning)
	}
	cleanupReady := make(chan *LocalPreviewCleanupLease, 1)
	go func() {
		lease, claimed, err := manager.BeginCleanup(runtime.ID)
		if err != nil || !claimed {
			cleanupReady <- nil
			return
		}
		cleanupReady <- &lease
	}()
	select {
	case <-cleanupReady:
		t.Fatal("cleanup crossed an active ingress lease")
	case <-time.After(50 * time.Millisecond):
	}
	ingress.Release()
	var cleanup *LocalPreviewCleanupLease
	select {
	case cleanup = <-cleanupReady:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not acquire the site gate")
	}
	if cleanup == nil || cleanup.gate == nil {
		t.Fatal("cleanup lease was not acquired")
	}
	if _, err := manager.FinishCleanup(cleanup); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPreviewWorkspaceTracksPreviewActivity(t *testing.T) {
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return base }
	manager.Touch("tech")
	manager.now = func() time.Time { return base.Add(DefaultLocalPreviewIdleTimeout - time.Second) }
	if got := manager.IdleSites(DefaultLocalPreviewIdleTimeout); len(got) != 0 {
		t.Fatalf("site became idle too early: %v", got)
	}
	manager.now = func() time.Time { return base.Add(DefaultLocalPreviewIdleTimeout + time.Second) }
	got := manager.IdleSites(DefaultLocalPreviewIdleTimeout)
	if len(got) != 1 || got[0] != "tech" {
		t.Fatalf("idle sites = %v, want [tech]", got)
	}
}

func TestLocalPreviewWorkspaceRunsBeforeWriteHook(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	hookCalled := false
	workspace, _, _, err := manager.UpdateWithBeforeWrite(runtime, "one.md", 1, []byte("updated"), func() error {
		hookCalled = true
		return nil
	})
	if err != nil || !hookCalled {
		t.Fatalf("hookCalled=%v err=%v", hookCalled, err)
	}
	assertWorkspaceFileContent(t, filepath.Join(workspace.ContentDir, "one.md"), "updated")
}

func TestLocalPreviewWorkspaceSyncsContentResource(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace, _, _, err := manager.Update(runtime, "one.md", 1, []byte("draft"))
	if err != nil {
		t.Fatal(err)
	}
	resourcePath := filepath.Join(repo, "content", "images", "new.png")
	if err := os.MkdirAll(filepath.Dir(resourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resourcePath, []byte("image-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	synced, err := manager.SyncContentResource(runtime, filepath.ToSlash(filepath.Join("content", "images", "new.png")), false)
	if err != nil || !synced {
		t.Fatalf("SyncContentResource() synced=%v err=%v", synced, err)
	}
	assertWorkspaceFileContent(t, filepath.Join(workspace.ContentDir, "images", "new.png"), "image-bytes")
	if err := os.Remove(resourcePath); err != nil {
		t.Fatal(err)
	}
	synced, err = manager.SyncContentResource(runtime, filepath.ToSlash(filepath.Join("content", "images", "new.png")), true)
	if err != nil || !synced {
		t.Fatalf("delete SyncContentResource() synced=%v err=%v", synced, err)
	}
	if _, err := os.Stat(filepath.Join(workspace.ContentDir, "images", "new.png")); !os.IsNotExist(err) {
		t.Fatalf("shadow resource still exists: %v", err)
	}
}

func TestLocalPreviewWorkspaceIgnoresStaticResourceSync(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "static", "logo.png"), []byte("logo"), 0644); err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content", StaticDir: "static"}
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := manager.Update(runtime, "one.md", 1, []byte("draft")); err != nil {
		t.Fatal(err)
	}
	synced, err := manager.SyncContentResource(runtime, "static/logo.png", false)
	if err != nil || synced {
		t.Fatalf("static resource synced=%v err=%v, want false/nil", synced, err)
	}
}

func TestLocalPreviewWorkspaceRejectsContentSymlink(t *testing.T) {
	repo := t.TempDir()
	contentDir := filepath.Join(repo, "content")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, "outside.md")
	if err := os.WriteFile(target, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(contentDir, "one.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := manager.Update(config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}, "one.md", 1, []byte("draft")); err == nil {
		t.Fatal("Update() should reject content symlinks")
	}
}

func assertWorkspaceFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func makeLocalPreviewWorkspaceRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "content"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "content", "one.md"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "content", "two.md"), []byte("two original"), 0644); err != nil {
		t.Fatal(err)
	}
	return repo
}
