package services

import (
	"fmt"
	"hugo-cms/pkg/config"
	"sync"
)

// GeneratorAdapter supports explicit builds and content creation. Preview
// rendering and deployment are handled by their dedicated services.
type GeneratorAdapter interface {
	Name() string
	Build(runtime config.SiteRuntime) (string, error)
	CreateContent(runtime config.SiteRuntime, path string) (string, error)
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

func BuildSiteForRuntime(runtime config.SiteRuntime) (string, error) {
	adapter, err := NewGeneratorAdapter(runtime.Generator)
	if err != nil {
		return "", err
	}
	return adapter.Build(runtime)
}

func CreateContentForRuntime(runtime config.SiteRuntime, path string) (string, error) {
	adapter, err := NewGeneratorAdapter(runtime.Generator)
	if err != nil {
		return "", err
	}
	return adapter.CreateContent(runtime, path)
}
