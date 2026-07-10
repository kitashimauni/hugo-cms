package services

import (
	"hugo-cms/pkg/config"
	"sync"
)

var siteScopeMu sync.Mutex

func WithSiteRuntimeLock(fn func() error) error {
	siteScopeMu.Lock()
	defer siteScopeMu.Unlock()
	return fn()
}

// WithSiteRuntime temporarily applies a site configuration while executing fn.
//
// Existing services read site settings from config globals. This scoped bridge
// keeps the current APIs backward-compatible while allowing handlers to target
// a selected site explicitly. Calls are serialized so concurrent requests do
// not observe another site's runtime settings.
func WithSiteRuntime(site config.SiteConfig, fn func() error) error {
	siteScopeMu.Lock()
	defer siteScopeMu.Unlock()

	previous := config.RuntimeSiteConfig()
	config.ApplySiteRuntime(site)
	defer func() {
		config.ApplySiteRuntime(previous)
	}()

	return fn()
}
