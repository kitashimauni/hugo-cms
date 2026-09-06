package services

import (
	"errors"
	"hugo-cms/pkg/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalPreviewWorkspaceMirrorsAndUpdatesContent(t *testing.T) {
	repo := t.TempDir()
	contentDir := filepath.Join(repo, "content")
	if err := os.MkdirAll(filepath.Join(contentDir, "posts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "posts", "one.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "posts", "two.md"), []byte("second"), 0644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	workspace, created, applied, err := manager.Update(runtime, "draft-1", "posts/one.md", 1, []byte("new"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !created || !applied {
		t.Fatalf("created=%v applied=%v, want true/true", created, applied)
	}

	got, err := os.ReadFile(filepath.Join(workspace.ContentDir, "posts", "one.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("updated content = %q", got)
	}
	mirrored, err := os.ReadFile(filepath.Join(workspace.ContentDir, "posts", "two.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(mirrored) != "second" {
		t.Fatalf("mirrored content = %q", mirrored)
	}
	original, err := os.ReadFile(filepath.Join(contentDir, "posts", "one.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != "old" {
		t.Fatalf("production content was modified: %q", original)
	}
}

func TestLocalPreviewWorkspaceRejectsStaleRevision(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	workspace, _, _, err := manager.Update(runtime, "draft-1", "one.md", 2, []byte("newer"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, created, applied, err := manager.Update(runtime, "draft-1", "one.md", 1, []byte("older"))
	if err != nil {
		t.Fatal(err)
	}
	if created || applied || workspace.Revision != 2 {
		t.Fatalf("created=%v applied=%v revision=%d", created, applied, workspace.Revision)
	}
	got, err := os.ReadFile(filepath.Join(workspace.ContentDir, "one.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "newer" {
		t.Fatalf("stale revision overwrote content: %q", got)
	}
}

func TestLocalPreviewWorkspaceRejectsOtherDraftSameSite(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	if _, _, _, err := manager.Update(runtime, "draft-1", "one.md", 1, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := manager.Update(runtime, "draft-2", "one.md", 1, []byte("two")); !errors.Is(err, ErrLocalPreviewSessionConflict) {
		t.Fatalf("error = %v, want session conflict", err)
	}
}

func TestLocalPreviewWorkspaceSeparatesSites(t *testing.T) {
	repoA := makeLocalPreviewWorkspaceRepo(t)
	repoB := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	workspaceA, _, _, err := manager.Update(config.SiteRuntime{ID: "tech", RepoPath: repoA, ContentDir: "content"}, "draft-a", "one.md", 1, []byte("tech"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceB, _, _, err := manager.Update(config.SiteRuntime{ID: "daily", RepoPath: repoB, ContentDir: "content"}, "draft-b", "one.md", 1, []byte("daily"))
	if err != nil {
		t.Fatal(err)
	}
	if workspaceA.ContentDir == workspaceB.ContentDir {
		t.Fatal("different sites share the same workspace")
	}
	gotA, _ := os.ReadFile(filepath.Join(workspaceA.ContentDir, "one.md"))
	gotB, _ := os.ReadFile(filepath.Join(workspaceB.ContentDir, "one.md"))
	if string(gotA) != "tech" || string(gotB) != "daily" {
		t.Fatalf("site contents mixed: tech=%q daily=%q", gotA, gotB)
	}
}

func TestLocalPreviewWorkspaceReleaseProtectsActiveDraft(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	workspace, _, _, err := manager.Update(runtime, "draft-1", "one.md", 1, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Release("tech", "draft-2"); !errors.Is(err, ErrLocalPreviewSessionConflict) {
		t.Fatalf("error = %v, want session conflict", err)
	}
	if _, err := os.Stat(workspace.ContentDir); err != nil {
		t.Fatalf("active workspace was removed: %v", err)
	}
	released, err := manager.Release("tech", "draft-1")
	if err != nil || !released {
		t.Fatalf("Release() released=%v err=%v", released, err)
	}
	if _, err := os.Stat(workspace.ContentDir); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists after release: %v", err)
	}
}

func TestLocalPreviewWorkspaceLeaseAndHeartbeat(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager.leaseTTL = 2 * time.Minute
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	workspace, _, _, err := manager.Update(runtime, "draft-1", "one.md", 1, []byte("draft"))
	if err != nil {
		t.Fatal(err)
	}
	if !workspace.LastSeenAt.Equal(now) {
		t.Fatalf("last seen = %s, want %s", workspace.LastSeenAt, now)
	}

	now = now.Add(3 * time.Minute)
	_, active, stale := manager.Status("tech")
	if !active || !stale {
		t.Fatalf("active=%v stale=%v, want true/true", active, stale)
	}
	workspace, err = manager.Heartbeat("tech", "draft-1")
	if err != nil {
		t.Fatal(err)
	}
	if !workspace.LastSeenAt.Equal(now) {
		t.Fatalf("heartbeat last seen = %s, want %s", workspace.LastSeenAt, now)
	}
	_, _, stale = manager.Status("tech")
	if stale {
		t.Fatal("heartbeat did not renew lease")
	}
	if _, err := manager.Heartbeat("tech", "draft-2"); !errors.Is(err, ErrLocalPreviewSessionConflict) {
		t.Fatalf("heartbeat error = %v, want conflict", err)
	}
}

func TestLocalPreviewWorkspaceReleaseStaleRequiresExpiredLease(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager.leaseTTL = time.Minute
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	workspace, _, _, err := manager.Update(runtime, "draft-1", "one.md", 1, []byte("draft"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReleaseStale("tech"); !errors.Is(err, ErrLocalPreviewSessionNotStale) {
		t.Fatalf("ReleaseStale error = %v, want not stale", err)
	}
	now = now.Add(2 * time.Minute)
	released, err := manager.ReleaseStale("tech")
	if err != nil || !released {
		t.Fatalf("ReleaseStale released=%v err=%v", released, err)
	}
	if _, err := os.Stat(workspace.ContentDir); !os.IsNotExist(err) {
		t.Fatalf("stale workspace still exists: %v", err)
	}
}

func TestLocalPreviewWorkspaceSyncsContentResource(t *testing.T) {
	repo := makeLocalPreviewWorkspaceRepo(t)
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace, _, _, err := manager.Update(runtime, "draft-1", "one.md", 1, []byte("draft"))
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
	got, err := os.ReadFile(filepath.Join(workspace.ContentDir, "images", "new.png"))
	if err != nil || string(got) != "image-bytes" {
		t.Fatalf("shadow resource = %q err=%v", got, err)
	}

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
	staticPath := filepath.Join(repo, "static", "logo.png")
	if err := os.WriteFile(staticPath, []byte("logo"), 0644); err != nil {
		t.Fatal(err)
	}
	runtime := config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content", StaticDir: "static"}
	manager, err := NewLocalPreviewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := manager.Update(runtime, "draft-1", "one.md", 1, []byte("draft")); err != nil {
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
	_, _, _, err = manager.Update(config.SiteRuntime{ID: "tech", RepoPath: repo, ContentDir: "content"}, "draft-1", "one.md", 1, []byte("draft"))
	if err == nil {
		t.Fatal("Update() should reject content symlinks")
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
	return repo
}
