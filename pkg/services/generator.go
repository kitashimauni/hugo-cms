package services

import (
	"fmt"
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
	return CurrentGeneratorAdapter().Build()
}

func CreateContent(path string) (string, error) {
	return CurrentGeneratorAdapter().CreateContent(path)
}
