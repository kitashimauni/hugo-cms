package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"net/url"
	"path/filepath"
	"sync"
)

type GeneratorAdapter interface {
	Name() string
	StartPreview(runtime config.SiteRuntime) error
	StopPreview() error
	IsPreviewRunning() bool
	Build(runtime config.SiteRuntime) (string, error)
	CreateContent(runtime config.SiteRuntime, path string) (string, error)
}

var (
	generatorAdapterMu sync.RWMutex
	generatorAdapter   GeneratorAdapter = NewHugoAdapter()
	previewAdaptersMu  sync.Mutex
	previewAdapters    = map[string]GeneratorAdapter{}
)

func CurrentGeneratorAdapter() GeneratorAdapter {
	generatorAdapterMu.RLock()
	defer generatorAdapterMu.RUnlock()
	return generatorAdapter
}

func SetGeneratorAdapter(adapter GeneratorAdapter) error {
	if adapter == nil {
		return fmt.Errorf("generator adapter cannot be nil")
	}

	generatorAdapterMu.Lock()
	defer generatorAdapterMu.Unlock()
	if generatorAdapter != nil && generatorAdapter.IsPreviewRunning() {
		return fmt.Errorf("cannot replace generator adapter while preview is running")
	}
	generatorAdapter = adapter
	return nil
}

func ConfigureGeneratorAdapterFromConfig() error {
	adapter, err := NewGeneratorAdapter(config.SiteGenerator)
	if err != nil {
		return err
	}
	return SetGeneratorAdapter(adapter)
}

func NewGeneratorAdapter(name string) (GeneratorAdapter, error) {
	switch name {
	case "", "hugo":
		return NewHugoAdapter(), nil
	case "eleventy", "11ty":
		return NewEleventyAdapter(), nil
	default:
		return nil, fmt.Errorf("unsupported site generator %q", name)
	}
}

// Compatibility wrappers keep the existing API stable while generator-specific
// behavior lives behind GeneratorAdapter.
func StartHugoServer() error {
	return StartPreviewForSite(defaultPreviewSite())
}

func StopHugoServer() error {
	return StopPreviewForSite(defaultPreviewSite())
}

func RestartHugoServer() error {
	return RestartPreviewForSite(defaultPreviewSite())
}

func IsHugoServerRunning() bool {
	return IsPreviewRunningForSite(defaultPreviewSite())
}

func BuildSite() (string, error) {
	return BuildSiteForRuntime(config.CurrentSiteRuntime())
}

func BuildSiteForRuntime(runtime config.SiteRuntime) (string, error) {
	adapter, err := NewGeneratorAdapter(runtime.Generator)
	if err != nil {
		return "", err
	}
	return adapter.Build(runtime)
}

func CreateContent(path string) (string, error) {
	return CreateContentForRuntime(config.CurrentSiteRuntime(), path)
}

func CreateContentForRuntime(runtime config.SiteRuntime, path string) (string, error) {
	adapter, err := NewGeneratorAdapter(runtime.Generator)
	if err != nil {
		return "", err
	}
	return adapter.CreateContent(runtime, path)
}

func StartPreviewForSite(site config.SiteConfig) error {
	return StartPreviewForRuntime(previewRuntime(site))
}

func StartPreviewForRuntime(runtime config.SiteRuntime) error {
	adapter, err := previewAdapterForRuntime(runtime)
	if err != nil {
		return err
	}
	return adapter.StartPreview(runtime)
}

func StopPreviewForSite(site config.SiteConfig) error {
	return StopPreviewForRuntime(config.NewSiteRuntime(site))
}

func StopPreviewForRuntime(runtime config.SiteRuntime) error {
	adapter, ok := previewAdapterForRuntimeIfExists(runtime)
	if !ok {
		return nil
	}
	return adapter.StopPreview()
}

func RestartPreviewForSite(site config.SiteConfig) error {
	return RestartPreviewForRuntime(previewRuntime(site))
}

func RestartPreviewForRuntime(runtime config.SiteRuntime) error {
	adapter, err := previewAdapterForRuntime(runtime)
	if err != nil {
		return err
	}
	if err := adapter.StopPreview(); err != nil {
		return err
	}
	return adapter.StartPreview(runtime)
}

func IsPreviewRunningForSite(site config.SiteConfig) bool {
	return IsPreviewRunningForRuntime(config.NewSiteRuntime(site))
}

func IsPreviewRunningForRuntime(runtime config.SiteRuntime) bool {
	adapter, ok := previewAdapterForRuntimeIfExists(runtime)
	return ok && adapter.IsPreviewRunning()
}

func StopAllPreviewServers() error {
	previewAdaptersMu.Lock()
	adapters := make([]GeneratorAdapter, 0, len(previewAdapters))
	for _, adapter := range previewAdapters {
		adapters = append(adapters, adapter)
	}
	previewAdapters = map[string]GeneratorAdapter{}
	previewAdaptersMu.Unlock()

	var stopErr error
	for _, adapter := range adapters {
		if err := adapter.StopPreview(); err != nil && stopErr == nil {
			stopErr = err
		}
	}
	return stopErr
}

func previewAdapterForRuntime(runtime config.SiteRuntime) (GeneratorAdapter, error) {
	previewAdaptersMu.Lock()
	defer previewAdaptersMu.Unlock()
	return previewAdapterForRuntimeLocked(runtime)
}

func previewAdapterForRuntimeIfExists(runtime config.SiteRuntime) (GeneratorAdapter, bool) {
	previewAdaptersMu.Lock()
	defer previewAdaptersMu.Unlock()
	adapter, ok := previewAdapters[sitePreviewKey(runtime)]
	return adapter, ok
}

func previewAdapterForRuntimeLocked(runtime config.SiteRuntime) (GeneratorAdapter, error) {
	key := sitePreviewKey(runtime)
	if adapter, ok := previewAdapters[key]; ok {
		return adapter, nil
	}

	adapter, err := NewGeneratorAdapter(runtime.Generator)
	if err != nil {
		return nil, err
	}
	previewAdapters[key] = adapter
	return adapter, nil
}

func sitePreviewKey(runtime config.SiteRuntime) string {
	if runtime.ID != "" {
		return runtime.ID
	}
	return filepath.ToSlash(filepath.Clean(runtime.RepoPath)) + "\x00" + runtime.HugoServerBind + "\x00" + runtime.HugoServerPort
}

func defaultPreviewSite() config.SiteConfig {
	if site, ok := config.GetSite(config.DefaultSiteID); ok {
		return site
	}
	return config.RuntimeSiteConfig()
}

func previewRuntimeSite(site config.SiteConfig) config.SiteConfig {
	return previewRuntime(site).SiteConfig()
}

func previewRuntime(site config.SiteConfig) config.SiteRuntime {
	if site.ID == "" {
		return config.NewSiteRuntime(site)
	}
	site.PreviewURL = "/admin/preview/" + url.PathEscape(site.ID) + "/"
	return config.NewSiteRuntime(site)
}
