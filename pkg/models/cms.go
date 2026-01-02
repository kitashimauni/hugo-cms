package models

type CMSConfig struct {
	MediaFolder  string       `yaml:"media_folder"`
	PublicFolder string       `yaml:"public_folder"`
	Collections  []Collection `yaml:"collections"`
}

type Collection struct {
	Name      string  `yaml:"name"`
	Label     string  `yaml:"label"`
	Folder    string  `yaml:"folder"`
	Path      string  `yaml:"path"`
	Extension string  `yaml:"extension"`
	Fields    []Field `yaml:"fields"`
}

type Field struct {
	Name    string      `yaml:"name"`
	Widget  string      `yaml:"widget"`
	Default interface{} `yaml:"default,omitempty"`
}
