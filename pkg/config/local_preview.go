package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func normalizePreviewDomain(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func normalizePreviewScheme(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateLocalPreviewBaseSettings(enabled bool) error {
	if !enabled && PreviewDomain == "" {
		return nil
	}
	if PreviewScheme != "http" && PreviewScheme != "https" {
		return fmt.Errorf("PREVIEW_SCHEME must be http or https")
	}
	if PreviewDomain == "" {
		return fmt.Errorf("PREVIEW_DOMAIN is required when local live preview is enabled")
	}
	if !validDNSName(PreviewDomain) {
		return fmt.Errorf("PREVIEW_DOMAIN %q must be a DNS name without scheme, wildcard, path, or port", PreviewDomain)
	}
	return nil
}

func validateLocalPreviewSite(site SiteConfig) error {
	if err := validateLocalPreviewBaseSettings(true); err != nil {
		return err
	}
	if site.ID != strings.ToLower(site.ID) || !validDNSLabel(site.ID) {
		return fmt.Errorf("local preview site id %q must be a lowercase DNS label", site.ID)
	}
	return nil
}

func LocalPreviewURL(siteID string) (string, error) {
	if err := validateLocalPreviewBaseSettings(true); err != nil {
		return "", err
	}
	if siteID != strings.ToLower(siteID) || !validDNSLabel(siteID) {
		return "", fmt.Errorf("local preview site id %q must be a lowercase DNS label", siteID)
	}
	return fmt.Sprintf("%s://%s.%s/", PreviewScheme, siteID, PreviewDomain), nil
}

// ResolveLocalPreviewHost maps an HTTP Host header to an enabled site. Only a
// single DNS label directly below PREVIEW_DOMAIN is accepted. The Host header
// never controls repository paths, commands, or internal preview ports.
func ResolveLocalPreviewHost(host string) (SiteConfig, error) {
	hostname, err := normalizePreviewHost(host)
	if err != nil {
		return SiteConfig{}, err
	}
	if err := validateLocalPreviewBaseSettings(true); err != nil {
		return SiteConfig{}, err
	}

	suffix := "." + PreviewDomain
	if !strings.HasSuffix(hostname, suffix) {
		return SiteConfig{}, fmt.Errorf("host %q is outside preview domain %q", hostname, PreviewDomain)
	}

	siteID := strings.TrimSuffix(hostname, suffix)
	if siteID == "" || strings.Contains(siteID, ".") || !validDNSLabel(siteID) {
		return SiteConfig{}, fmt.Errorf("host %q does not contain exactly one valid site label", hostname)
	}

	site, ok := GetSite(siteID)
	if !ok {
		return SiteConfig{}, fmt.Errorf("unknown local preview site %q", siteID)
	}
	if site.Preview.LocalPreview.Enabled == nil || !*site.Preview.LocalPreview.Enabled {
		return SiteConfig{}, fmt.Errorf("local preview is disabled for site %q", siteID)
	}
	return site, nil
}

func normalizePreviewHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("preview host is empty")
	}
	if strings.ContainsAny(host, "/\\@ \t\r\n") {
		return "", fmt.Errorf("invalid preview host %q", host)
	}

	hostname := host
	if strings.Contains(host, ":") {
		parsedHost, portText, err := net.SplitHostPort(host)
		if err != nil || parsedHost == "" || portText == "" {
			return "", fmt.Errorf("invalid preview host %q", host)
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("invalid preview host port %q", portText)
		}
		hostname = parsedHost
	}

	hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
	if !validDNSName(hostname) {
		return "", fmt.Errorf("invalid preview hostname %q", hostname)
	}
	return hostname, nil
}

func validDNSName(name string) bool {
	if len(name) == 0 || len(name) > 253 || strings.Contains(name, "*") {
		return false
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

func validDNSLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}
