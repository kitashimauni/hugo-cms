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
	StartPreview() error
	StopPreview() error
	RestartPreview() error
	IsPreviewRunning() bool
	Build() (string, error)
	CreateContent(path string) (string, error)
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
	adapter, err := NewGeneratorAdapter(config.SiteGenerator)
	if err != nil {
		return "", err
	}
	return adapter.Build()
}

func CreateContent(path string) (string, error) {
	adapter, err := NewGeneratorAdapter(config.SiteGenerator)
	if err != nil {
		return "", err
	}
	return adapter.CreateContent(path)
}

func StartPreviewForSite(site config.SiteConfig) error {
	adapter, err := previewAdapterForSite(site)
	if err != nil {
		return err
	}
	return WithSiteRuntime(previewRuntimeSite(site), adapter.StartPreview)
}

func StopPreviewForSite(site config.SiteConfig) error {
	adapter, ok := previewAdapterForSiteIfExists(site)
	if !ok {
		return nil
	}
	return adapter.StopPreview()
}

func RestartPreviewForSite(site config.SiteConfig) error {
	adapter, err := previewAdapterForSite(site)
	if err != nil {
		return err
	}
	if err := adapter.StopPreview(); err != nil {
		return err
	}
	return WithSiteRuntime(previewRuntimeSite(site), adapter.StartPreview)
}

func IsPreviewRunningForSite(site config.SiteConfig) bool {
	adapter, ok := previewAdapterForSiteIfExists(site)
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

func previewAdapterForSite(site config.SiteConfig) (GeneratorAdapter, error) {
	previewAdaptersMu.Lock()
	defer previewAdaptersMu.Unlock()
	return previewAdapterForSiteLocked(site)
}

func previewAdapterForSiteIfExists(site config.SiteConfig) (GeneratorAdapter, bool) {
	previewAdaptersMu.Lock()
	defer previewAdaptersMu.Unlock()
	adapter, ok := previewAdapters[sitePreviewKey(site)]
	return adapter, ok
}

func previewAdapterForSiteLocked(site config.SiteConfig) (GeneratorAdapter, error) {
	key := sitePreviewKey(site)
	if adapter, ok := previewAdapters[key]; ok {
		return adapter, nil
	}

	adapter, err := NewGeneratorAdapter(site.Generator)
	if err != nil {
		return nil, err
	}
	previewAdapters[key] = adapter
	return adapter, nil
}

func sitePreviewKey(site config.SiteConfig) string {
	if site.ID != "" {
		return site.ID
	}
	return filepath.ToSlash(filepath.Clean(site.RepoPath)) + "\x00" + site.HugoServerBind + "\x00" + site.HugoServerPort
}

func defaultPreviewSite() config.SiteConfig {
	if site, ok := config.GetSite(config.DefaultSiteID); ok {
		return site
	}
	return config.RuntimeSiteConfig()
}

func previewRuntimeSite(site config.SiteConfig) config.SiteConfig {
	if site.ID == "" {
		return site
	}
	site.PreviewURL = "/admin/preview/" + url.PathEscape(site.ID) + "/"
	return site
}
