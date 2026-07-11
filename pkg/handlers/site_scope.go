package handlers

import (
	"hugo-cms/pkg/config"
	"hugo-cms/pkg/services"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const siteContextKey = "cms_site_id"

func SiteScoped(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		site, err := requestedSite(c)
		if err != nil {
			ErrorBadRequest(c, err.Error())
			return
		}
		c.Set(siteContextKey, site.ID)

		if err := services.WithSiteRuntime(site, func() error {
			handler(c)
			return nil
		}); err != nil {
			ErrorInternal(c, "Failed to apply site configuration: "+err.Error())
			return
		}
	}
}

func RuntimeLocked(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := services.WithSiteRuntimeLock(func() error {
			handler(c)
			return nil
		}); err != nil {
			ErrorInternal(c, "Failed to acquire site runtime lock: "+err.Error())
			return
		}
	}
}

func requestedSite(c *gin.Context) (config.SiteConfig, error) {
	siteID := ""
	if c.Request != nil {
		siteID = strings.TrimSpace(c.Query("site"))
		if siteID == "" {
			siteID = strings.TrimSpace(c.GetHeader("X-CMS-Site"))
		}
	}
	if siteID == "" {
		siteID = config.DefaultSiteID
	}

	site, ok := config.GetSite(siteID)
	if !ok {
		if siteID == config.DefaultSiteID {
			return config.RuntimeSiteConfig(), nil
		}
		return config.SiteConfig{}, errInvalidSite(siteID)
	}
	return site, nil
}

func requestedRuntime(c *gin.Context) (config.SiteRuntime, error) {
	site, err := requestedSite(c)
	if err != nil {
		return config.SiteRuntime{}, err
	}
	c.Set(siteContextKey, site.ID)
	return config.NewSiteRuntime(site), nil
}

func requestedPreviewRuntime(c *gin.Context) (config.SiteRuntime, error) {
	site, err := requestedSite(c)
	if err != nil {
		return config.SiteRuntime{}, err
	}
	c.Set(siteContextKey, site.ID)
	return services.PreviewRuntimeForSite(site), nil
}

func currentSiteID(c *gin.Context) string {
	if v, ok := c.Get(siteContextKey); ok {
		if siteID, ok := v.(string); ok {
			return siteID
		}
	}
	return config.DefaultSiteID
}

func addSiteQuery(rawURL, siteID string) string {
	if siteID == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("site", siteID)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

type invalidSiteError string

func errInvalidSite(siteID string) error {
	return invalidSiteError(siteID)
}

func (err invalidSiteError) Error() string {
	return "Unknown site: " + string(err)
}
