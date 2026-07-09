package services

import (
	"fmt"
	"hugo-cms/pkg/config"
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
	return CurrentGeneratorAdapter().StartPreview()
}

func StopHugoServer() error {
	return CurrentGeneratorAdapter().StopPreview()
}

func RestartHugoServer() error {
	return CurrentGeneratorAdapter().RestartPreview()
}

func IsHugoServerRunning() bool {
	return CurrentGeneratorAdapter().IsPreviewRunning()
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
