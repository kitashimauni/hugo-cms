package models

type CMSConfig struct {
	Collections []Collection `yaml:"collections"`
}

type Collection struct {
	Name   string  `yaml:"name"`
	Folder string  `yaml:"folder"`
	Fields []Field `yaml:"fields"`
}

type Field struct {
	Name    string      `yaml:"name"`
	Widget  string      `yaml:"widget"`
	Default interface{} `yaml:"default,omitempty"`
}
