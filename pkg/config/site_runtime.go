package config

import "path/filepath"

// SiteRuntime is the resolved, process-local view of a site used by services.
//
// SiteConfig is the persisted/admin-facing registry entry. SiteRuntime adds
// process settings that are currently global, so services can gradually accept
// explicit runtime data instead of reading mutable package globals.
type SiteRuntime struct {
	ID              string
	Name            string
	RepoPath        string
	Generator       string
	Runtime         string
	ContentDir      string
	StaticDir       string
	PublicDir       string
	PublicPath      string
	PreviewURL      string
	HugoServerPort  string
	HugoServerBind  string
	ArticleMediaDir string
	StaticMediaDir  string
	SnippetPaths    []string
	AppURL          string
	GitUserEmail    string
	GitUserName     string
	GitBranch       string
	GitRemote       string
}

func NewSiteRuntime(site SiteConfig) SiteRuntime {
	return SiteRuntime{
		ID:              site.ID,
		Name:            site.Name,
		RepoPath:        site.RepoPath,
		Generator:       site.Generator,
		Runtime:         site.Runtime,
		ContentDir:      site.ContentDir,
		StaticDir:       site.StaticDir,
		PublicDir:       site.PublicDir,
		PublicPath:      filepath.Join(site.RepoPath, site.PublicDir),
		PreviewURL:      site.PreviewURL,
		HugoServerPort:  site.HugoServerPort,
		HugoServerBind:  site.HugoServerBind,
		ArticleMediaDir: site.ArticleMediaDir,
		StaticMediaDir:  site.StaticMediaDir,
		SnippetPaths:    append([]string(nil), site.SnippetPaths...),
		AppURL:          GetAppURL(),
		GitUserEmail:    GitUserEmail,
		GitUserName:     GitUserName,
		GitBranch:       GitBranch,
		GitRemote:       GitRemote,
	}
}

func CurrentSiteRuntime() SiteRuntime {
	return SiteRuntime{
		ID:              DefaultSiteID,
		Name:            "Current",
		RepoPath:        RepoPath,
		Generator:       SiteGenerator,
		Runtime:         GeneratorRuntime,
		ContentDir:      ContentDir,
		StaticDir:       StaticDir,
		PublicDir:       PublicDir,
		PublicPath:      PublicPath,
		PreviewURL:      PreviewURL,
		HugoServerPort:  HugoServerPort,
		HugoServerBind:  HugoServerBind,
		ArticleMediaDir: ArticleMediaDir,
		StaticMediaDir:  StaticMediaDir,
		SnippetPaths:    append([]string(nil), SnippetPaths...),
		AppURL:          GetAppURL(),
		GitUserEmail:    GitUserEmail,
		GitUserName:     GitUserName,
		GitBranch:       GitBranch,
		GitRemote:       GitRemote,
	}
}

func (runtime SiteRuntime) SiteConfig() SiteConfig {
	return SiteConfig{
		ID:              runtime.ID,
		Name:            runtime.Name,
		RepoPath:        runtime.RepoPath,
		Generator:       runtime.Generator,
		Runtime:         runtime.Runtime,
		ContentDir:      runtime.ContentDir,
		StaticDir:       runtime.StaticDir,
		PublicDir:       runtime.PublicDir,
		PreviewURL:      runtime.PreviewURL,
		HugoServerPort:  runtime.HugoServerPort,
		HugoServerBind:  runtime.HugoServerBind,
		ArticleMediaDir: runtime.ArticleMediaDir,
		StaticMediaDir:  runtime.StaticMediaDir,
		SnippetPaths:    append([]string(nil), runtime.SnippetPaths...),
	}
}
