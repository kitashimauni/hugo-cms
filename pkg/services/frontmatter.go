package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hugo-cms/pkg/models"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func GenerateContentFromCollection(collection models.Collection, overrides map[string]interface{}) ([]byte, error) {
	fm := make(map[string]interface{})
	var bodyContent string

	for _, field := range collection.Fields {
		// Check override first
		if val, ok := overrides[field.Name]; ok {
			if field.Name == "body" {
				if strVal, ok := val.(string); ok {
					bodyContent = strVal
				}
				continue
			}
			fm[field.Name] = val
			continue
		}

		if field.Name == "body" {
			if field.Default != nil {
				if val, ok := field.Default.(string); ok {
					bodyContent = val
				}
			}
			continue
		}

		if field.Default != nil {
			fm[field.Name] = field.Default
		} else {
			switch field.Widget {
			case "datetime":
				fm[field.Name] = time.Now()
			case "boolean":
				fm[field.Name] = false
			case "list":
				fm[field.Name] = []string{}
			default:
				fm[field.Name] = ""
			}
		}
	}

	format, err := NormalizeFrontMatterFormat(collection.Format)
	if err != nil {
		return nil, err
	}
	if format == "" {
		format = "toml"
	}
	return ConstructFileContent(fm, bodyContent, format)
}

func NormalizeContent(content []byte, collection *models.Collection) []byte {
	if len(content) == 0 {
		return content
	}
	fm, body, format, err := ParseFrontMatter(content)
	if err != nil {
		return append(bytes.TrimSpace(content), '\n')
	}

	preparedFM := sanitizeFrontMatter(fm)
	applyCollectionDefaultsInPlace(preparedFM, collection)

	normalized, err := ConstructFileContent(preparedFM, body, format)
	if err != nil {
		return append(bytes.TrimSpace(content), '\n')
	}
	return append(bytes.TrimSpace(normalized), '\n')
}

func sanitizeFrontMatter(fm map[string]interface{}) map[string]interface{} {
	if fm == nil {
		return nil
	}
	sanitized := make(map[string]interface{}, len(fm))
	for k, v := range fm {
		sanitized[k] = sanitizeFrontMatterValue(v)
	}
	return sanitized
}

func sanitizeFrontMatterValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return sanitizeFrontMatter(v)
	case map[interface{}]interface{}:
		normalized := make(map[string]interface{}, len(v))
		for key, inner := range v {
			normalized[fmt.Sprint(key)] = sanitizeFrontMatterValue(inner)
		}
		return normalized
	case []interface{}:
		slice := make([]interface{}, len(v))
		for i := range v {
			slice[i] = sanitizeFrontMatterValue(v[i])
		}
		return slice
	case int64:
		// Normalize int64 to float64 for consistent JSON comparison
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case time.Time:
		return v.Truncate(time.Second)
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.Truncate(time.Second)
		}
		// Also try common date-only format YYYY-MM-DD
		if t, err := time.Parse("2006-01-02", v); err == nil {
			return t.Truncate(time.Second)
		}
		return v
	default:
		return v
	}
}

func applyCollectionDefaultsInPlace(fm map[string]interface{}, collection *models.Collection) {
	if fm == nil || collection == nil {
		return
	}
	for _, field := range collection.Fields {
		if field.Name == "body" {
			continue
		}
		if _, exists := fm[field.Name]; !exists && field.Default != nil {
			fm[field.Name] = field.Default
		}
	}
}

func normalizeOptionalListFields(fm map[string]interface{}, collection *models.Collection) {
	if fm == nil || collection == nil {
		return
	}
	for _, field := range collection.Fields {
		if field.Widget != "list" {
			continue
		}

		val, exists := fm[field.Name]
		if !exists || val == nil {
			fm[field.Name] = []interface{}{}
			continue
		}

		switch list := val.(type) {
		case []interface{}:
			normalized := make([]interface{}, len(list))
			for i := range list {
				normalized[i] = sanitizeFrontMatterValue(list[i])
			}
			fm[field.Name] = normalized
		case []string:
			normalized := make([]interface{}, len(list))
			for i := range list {
				normalized[i] = list[i]
			}
			fm[field.Name] = normalized
		default:
			fm[field.Name] = []interface{}{sanitizeFrontMatterValue(list)}
		}
	}
}

func canonicalizeFrontMatterForJSON(fm map[string]interface{}) map[string]interface{} {
	if fm == nil {
		return nil
	}
	canonical := make(map[string]interface{}, len(fm))
	for k, v := range fm {
		canonical[k] = canonicalizeValueForJSON(v)
	}
	return canonical
}

// formatTimeForComparison formats time consistently for comparison.
// Uses RFC3339 without nanoseconds if they are zero, otherwise RFC3339Nano.
// Hugo expects format like: 2025-12-10T00:00:00+09:00
func formatTimeForComparison(t time.Time) string {
	utc := t.UTC()
	if utc.Nanosecond() == 0 {
		return utc.Format(time.RFC3339)
	}
	return utc.Format(time.RFC3339Nano)
}

func canonicalizeValueForJSON(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		canonical := make(map[string]interface{}, len(v))
		for key, inner := range v {
			canonical[key] = canonicalizeValueForJSON(inner)
		}
		return canonical
	case map[interface{}]interface{}:
		normalized := make(map[string]interface{}, len(v))
		for key, inner := range v {
			normalized[fmt.Sprint(key)] = canonicalizeValueForJSON(inner)
		}
		return normalized
	case []interface{}:
		slice := make([]interface{}, len(v))
		for i := range v {
			slice[i] = canonicalizeValueForJSON(v[i])
		}
		return slice
	case time.Time:
		return formatTimeForComparison(v)
	case toml.LocalDateTime:
		return formatTimeForComparison(v.AsTime(time.UTC))
	case toml.LocalDate:
		return v.AsTime(time.UTC).UTC().Format("2006-01-02")
	case toml.LocalTime:
		return v.String()
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case string:
		// Try to parse string as date/time for normalization
		// Hugo uses RFC3339 format: 2025-12-10T00:00:00+09:00
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return formatTimeForComparison(parsed)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return formatTimeForComparison(parsed)
		}
		// Also try common date-only format
		if parsed, err := time.Parse("2006-01-02", v); err == nil {
			return parsed.UTC().Format("2006-01-02")
		}
		return v
	default:
		return v
	}
}

func normalizeLineEndings(input string) string {
	return strings.ReplaceAll(input, "\r\n", "\n")
}

func pruneEmptyFields(val interface{}) interface{} {
	switch v := val.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{})
		for k, elem := range v {
			pruned := pruneEmptyFields(elem)
			// 値が nil (削除対象) でなければマップに追加
			if pruned != nil {
				out[k] = pruned
			}
		}
		// マップ自体が空になった場合も残すかどうかは要件によりますが、
		// フロントマター全体が消えるのを防ぐため、トップレベル呼び出しでは注意が必要。
		// ここでは再帰的な削除としてそのまま返します。
		return out

	case []interface{}:
		// 空のリストは削除 (nilを返すことで親マップからキーが消える)
		if len(v) == 0 {
			return nil
		}
		// 必要に応じてリストの中身も再帰的にチェック可能
		return v

	case []string:
		// sanitizeFrontMatterを通していれば []interface{} になっているはずですが念のため
		if len(v) == 0 {
			return nil
		}
		return v

	case string:
		// 空文字は削除
		if v == "" {
			return nil
		}
		return v

	default:
		// bool (false) や数値 (0)、日付などはそのまま返す
		return v
	}
}

func canonicalizeContentForDiff(content []byte, collection *models.Collection) ([]byte, string, error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return nil, "", nil
	}

	fm, body, _, err := ParseFrontMatter(trimmed)
	if err != nil {
		return nil, strings.TrimSpace(normalizeLineEndings(string(trimmed))), err
	}

	sanitized := sanitizeFrontMatter(fm)
	applyCollectionDefaultsInPlace(sanitized, collection)
	normalizeOptionalListFields(sanitized, collection)

	// ▼▼▼ 追加: 比較用に空の値を削除して構造を統一する ▼▼▼
	prunedFM := pruneEmptyFields(sanitized)
	// マップならキャストしてJSON化へ渡す（nilチェックを入れるとより安全です）
	var fmMap map[string]interface{}
	if m, ok := prunedFM.(map[string]interface{}); ok {
		fmMap = m
	} else {
		fmMap = make(map[string]interface{})
	}

	canonicalFM, err := json.Marshal(canonicalizeFrontMatterForJSON(fmMap))
	if err != nil {
		return nil, "", err
	}

	normalizedBody := strings.Trim(normalizeLineEndings(body), "\n")
	return canonicalFM, normalizedBody, nil
}
