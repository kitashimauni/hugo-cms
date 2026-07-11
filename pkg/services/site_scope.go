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
	return WithRuntime(config.NewSiteRuntime(site), fn)
}

// WithRuntime temporarily applies a resolved runtime while executing fn.
// Prefer passing config.SiteRuntime directly into services for new code; this
// bridge exists for legacy services that still read config package globals.
func WithRuntime(runtime config.SiteRuntime, fn func() error) error {
	siteScopeMu.Lock()
	defer siteScopeMu.Unlock()

	previous := config.CurrentSiteRuntime()
	config.ApplyRuntime(runtime)
	defer func() {
		config.ApplyRuntime(previous)
	}()

	return fn()
}
