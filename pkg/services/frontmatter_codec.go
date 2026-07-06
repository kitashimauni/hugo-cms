package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

type FrontMatterCodec interface {
	Format() string
	Parse(content []byte) (map[string]interface{}, string, error)
	Construct(frontMatter map[string]interface{}, body string) ([]byte, error)
}

type delimitedFrontMatterCodec struct {
	format    string
	delimiter string
	decode    func([]byte, interface{}) error
	encode    func(*bytes.Buffer, interface{}) error
}

type jsonFrontMatterCodec struct{}

var frontMatterCodecs = map[string]FrontMatterCodec{
	"yaml": delimitedFrontMatterCodec{
		format:    "yaml",
		delimiter: "---",
		decode:    yaml.Unmarshal,
		encode: func(buf *bytes.Buffer, value interface{}) error {
			encoder := yaml.NewEncoder(buf)
			defer encoder.Close()
			encoder.SetIndent(2)
			return encoder.Encode(value)
		},
	},
	"toml": delimitedFrontMatterCodec{
		format:    "toml",
		delimiter: "+++",
		decode:    toml.Unmarshal,
		encode: func(buf *bytes.Buffer, value interface{}) error {
			return toml.NewEncoder(buf).Encode(value)
		},
	},
	"json": jsonFrontMatterCodec{},
}

func NormalizeFrontMatterFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "":
		return "", nil
	case "yaml", "yml", "yaml-frontmatter":
		return "yaml", nil
	case "toml", "toml-frontmatter":
		return "toml", nil
	case "json", "json-frontmatter":
		return "json", nil
	default:
		return "", fmt.Errorf("unsupported front matter format: %s", format)
	}
}

func ParseFrontMatter(content []byte) (map[string]interface{}, string, string, error) {
	trimmedBOM := bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	var format string
	switch {
	case hasOpeningDelimiter(trimmedBOM, "---"):
		format = "yaml"
	case hasOpeningDelimiter(trimmedBOM, "+++"):
		format = "toml"
	case len(bytes.TrimSpace(trimmedBOM)) > 0 && bytes.TrimSpace(trimmedBOM)[0] == '{':
		format = "json"
	default:
		return nil, "", "", fmt.Errorf("unknown front matter format")
	}

	codec := frontMatterCodecs[format]
	frontMatter, body, err := codec.Parse(trimmedBOM)
	if err != nil {
		return nil, "", "", err
	}
	return frontMatter, body, format, nil
}

func ConstructFileContent(frontMatter map[string]interface{}, body, format string) ([]byte, error) {
	normalizedFormat, err := NormalizeFrontMatterFormat(format)
	if err != nil {
		return nil, err
	}
	codec, ok := frontMatterCodecs[normalizedFormat]
	if !ok {
		return nil, fmt.Errorf("unsupported front matter format: %s", format)
	}
	return codec.Construct(frontMatter, body)
}

func (codec delimitedFrontMatterCodec) Format() string {
	return codec.format
}

func (codec delimitedFrontMatterCodec) Parse(content []byte) (map[string]interface{}, string, error) {
	frontMatterBytes, bodyBytes, err := splitDelimitedFrontMatter(content, codec.delimiter)
	if err != nil {
		return nil, "", err
	}

	var frontMatter map[string]interface{}
	if err := codec.decode(frontMatterBytes, &frontMatter); err != nil {
		return nil, "", fmt.Errorf("parse %s front matter: %w", codec.format, err)
	}
	return frontMatter, trimBodyNewlines(bodyBytes), nil
}

func (codec delimitedFrontMatterCodec) Construct(frontMatter map[string]interface{}, body string) ([]byte, error) {
	normalized := sanitizeFrontMatter(frontMatter)
	if normalized == nil {
		normalized = map[string]interface{}{}
	}

	var buf bytes.Buffer
	buf.WriteString(codec.delimiter)
	buf.WriteByte('\n')
	if err := codec.encode(&buf, normalized); err != nil {
		return nil, fmt.Errorf("encode %s front matter: %w", codec.format, err)
	}
	buf.WriteString(codec.delimiter)
	buf.WriteByte('\n')
	appendMarkdownBody(&buf, body)
	return buf.Bytes(), nil
}

func (jsonFrontMatterCodec) Format() string {
	return "json"
}

func (jsonFrontMatterCodec) Parse(content []byte) (map[string]interface{}, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	var frontMatter map[string]interface{}
	if err := decoder.Decode(&frontMatter); err != nil {
		return nil, "", fmt.Errorf("parse json front matter: %w", err)
	}

	offset := decoder.InputOffset()
	if offset < 0 || offset > int64(len(content)) {
		return nil, "", fmt.Errorf("invalid json front matter boundary")
	}
	return frontMatter, trimBodyNewlines(content[offset:]), nil
}

func (jsonFrontMatterCodec) Construct(frontMatter map[string]interface{}, body string) ([]byte, error) {
	normalized := sanitizeFrontMatter(frontMatter)
	if normalized == nil {
		normalized = map[string]interface{}{}
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(normalized); err != nil {
		return nil, fmt.Errorf("encode json front matter: %w", err)
	}
	appendMarkdownBody(&buf, body)
	return buf.Bytes(), nil
}

func hasOpeningDelimiter(content []byte, delimiter string) bool {
	lineEnd := bytes.IndexByte(content, '\n')
	if lineEnd == -1 {
		lineEnd = len(content)
	}
	return strings.TrimSuffix(string(content[:lineEnd]), "\r") == delimiter
}

func splitDelimitedFrontMatter(content []byte, delimiter string) ([]byte, []byte, error) {
	firstLineEnd := bytes.IndexByte(content, '\n')
	if firstLineEnd == -1 || !hasOpeningDelimiter(content, delimiter) {
		return nil, nil, fmt.Errorf("missing opening %s delimiter", delimiter)
	}

	frontMatterStart := firstLineEnd + 1
	lineStart := frontMatterStart
	for lineStart <= len(content) {
		lineEnd := bytes.IndexByte(content[lineStart:], '\n')
		nextLineStart := len(content)
		if lineEnd >= 0 {
			lineEnd += lineStart
			nextLineStart = lineEnd + 1
		} else {
			lineEnd = len(content)
		}

		line := strings.TrimSuffix(string(content[lineStart:lineEnd]), "\r")
		if line == delimiter {
			return content[frontMatterStart:lineStart], content[nextLineStart:], nil
		}
		if nextLineStart == len(content) {
			break
		}
		lineStart = nextLineStart
	}
	return nil, nil, fmt.Errorf("missing closing %s delimiter", delimiter)
}

func appendMarkdownBody(buf *bytes.Buffer, body string) {
	body = strings.Trim(body, "\r\n")
	if body == "" {
		return
	}
	buf.WriteByte('\n')
	buf.WriteString(body)
	buf.WriteByte('\n')
}

func trimBodyNewlines(body []byte) string {
	return strings.Trim(string(body), "\r\n")
}
