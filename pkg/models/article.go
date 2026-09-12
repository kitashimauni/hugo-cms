package models

// Article represents a content file in the CMS.
type Article struct {
	Path         string                 `json:"path"`
	Title        string                 `json:"title"`
	Content      string                 `json:"content,omitempty"`     // Raw content (backward compatibility)
	RawContent   string                 `json:"raw_content,omitempty"` // Original bytes for no-op preview synchronization
	FrontMatter  map[string]interface{} `json:"frontmatter,omitempty"`
	Body         string                 `json:"body,omitempty"`
	Format       string                 `json:"format,omitempty"` // yaml, toml, json
	IsDirty      bool                   `json:"is_dirty"`
	Revision     string                 `json:"revision,omitempty"`
	BaseRevision string                 `json:"base_revision,omitempty"`
}
