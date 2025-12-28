package models

// Article represents a content file in the CMS.
type Article struct {
	Path        string                 `json:"path"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content,omitempty"` // Raw content (backward compatibility)
	FrontMatter map[string]interface{} `json:"frontmatter,omitempty"`
	Body        string                 `json:"body,omitempty"`
	Format      string                 `json:"format,omitempty"` // yaml, toml, json
	IsDirty     bool                   `json:"is_dirty"`
}
