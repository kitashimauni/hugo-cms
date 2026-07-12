package models

type CMSConfig struct {
	MediaFolder  string       `yaml:"media_folder"`
	PublicFolder string       `yaml:"public_folder"`
	Preview      CMSPreview   `yaml:"preview"`
	Collections  []Collection `yaml:"collections"`
}

type CMSPreview struct {
	URLField string `yaml:"url_field"`
}

type HomeCMSConfig struct {
	Version int            `yaml:"version" json:"version"`
	Content HomeCMSContent `yaml:"content" json:"content"`
	Media   HomeCMSMedia   `yaml:"media" json:"media"`
	Preview HomeCMSPreview `yaml:"preview" json:"preview"`
}

type HomeCMSContent struct {
	Collections []HomeCMSCollection `yaml:"collections" json:"collections"`
}

type HomeCMSMedia struct {
	Folder     string `yaml:"folder" json:"folder"`
	PublicPath string `yaml:"public_path" json:"public_path"`
}

type HomeCMSPreview struct {
	URLField string `yaml:"url_field" json:"url_field"`
}

type HomeCMSCollection struct {
	Name         string  `yaml:"name" json:"name"`
	Label        string  `yaml:"label" json:"label"`
	Folder       string  `yaml:"folder" json:"folder"`
	Path         string  `yaml:"path" json:"path"`
	Extension    string  `yaml:"extension" json:"extension"`
	FrontMatter  string  `yaml:"frontmatter" json:"frontmatter"`
	MediaFolder  string  `yaml:"media_folder" json:"media_folder"`
	PublicFolder string  `yaml:"public_path" json:"public_path"`
	Fields       []Field `yaml:"fields" json:"fields"`
}

type Collection struct {
	Name         string  `yaml:"name"`
	Label        string  `yaml:"label"`
	Folder       string  `yaml:"folder"`
	Path         string  `yaml:"path"`
	Extension    string  `yaml:"extension"`
	Format       string  `yaml:"format"`
	MediaFolder  string  `yaml:"media_folder"`
	PublicFolder string  `yaml:"public_folder"`
	Fields       []Field `yaml:"fields"`
}

type Field struct {
	Name    string      `yaml:"name"`
	Label   string      `yaml:"label,omitempty"`
	Widget  string      `yaml:"widget"`
	Default interface{} `yaml:"default,omitempty"`
}
