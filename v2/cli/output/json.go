package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// JSONFormatter formats data as JSON
type JSONFormatter struct {
	config JSONConfig
}

// JSONConfig contains JSON formatting configuration
type JSONConfig struct {
	Indent        string `yaml:"indent" json:"indent"`
	EscapeHTML    bool   `yaml:"escape_html" json:"escape_html"`
	SortKeys      bool   `yaml:"sort_keys" json:"sort_keys"`
	CompactOutput bool   `yaml:"compact_output" json:"compact_output"`
	ColorOutput   bool   `yaml:"color_output" json:"color_output"`
}

// NewJSONFormatter creates a new JSON formatter
func NewJSONFormatter() OutputFormatter {
	return &JSONFormatter{
		config: DefaultJSONConfig(),
	}
}

// NewJSONFormatterWithConfig creates a JSON formatter with custom config
func NewJSONFormatterWithConfig(config JSONConfig) OutputFormatter {
	return &JSONFormatter{
		config: config,
	}
}

// Format formats data as JSON
func (jf *JSONFormatter) Format(data interface{}) ([]byte, error) {
	encoder := json.NewEncoder(&jsonBuffer{})

	// Configure encoder
	encoder.SetEscapeHTML(jf.config.EscapeHTML)

	if !jf.config.CompactOutput {
		if jf.config.Indent == "" {
			encoder.SetIndent("", "  ")
		} else {
			encoder.SetIndent("", jf.config.Indent)
		}
	}

	var result []byte
	var err error

	if jf.config.CompactOutput {
		result, err = json.Marshal(data)
	} else {
		if jf.config.SortKeys {
			// For sorted keys, we need to marshal to bytes first
			rawJSON, marshalErr := json.Marshal(data)
			if marshalErr != nil {
				return nil, marshalErr
			}

			// Parse back to interface{} to ensure consistent ordering
			var parsed interface{}
			if unmarshalErr := json.Unmarshal(rawJSON, &parsed); unmarshalErr != nil {
				return nil, unmarshalErr
			}

			result, err = json.MarshalIndent(jf.sortKeys(parsed), "", jf.getIndent())
		} else {
			result, err = json.MarshalIndent(data, "", jf.getIndent())
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Add color formatting if enabled
	if jf.config.ColorOutput {
		return jf.colorizeJSON(result), nil
	}

	return result, nil
}

// MimeType returns the MIME type
func (jf *JSONFormatter) MimeType() string {
	return "application/json"
}

// FileExtension returns the file extension
func (jf *JSONFormatter) FileExtension() string {
	return ".json"
}

// getIndent returns the indent string
func (jf *JSONFormatter) getIndent() string {
	if jf.config.Indent == "" {
		return "  "
	}
	return jf.config.Indent
}

// sortKeys recursively sorts object keys in JSON data
func (jf *JSONFormatter) sortKeys(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		// Convert to SortedMap for consistent key ordering
		return NewSortedMap(v, jf.sortKeys)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = jf.sortKeys(item)
		}
		return result
	default:
		return data
	}
}

// colorizeJSON adds ANSI color codes to JSON output
func (jf *JSONFormatter) colorizeJSON(data []byte) []byte {
	// This is a simplified colorization
	// In a real implementation, you'd parse and colorize properly
	jsonStr := string(data)

	// Color codes
	const (
		reset  = "\033[0m"
		red    = "\033[31m"
		green  = "\033[32m"
		yellow = "\033[33m"
		blue   = "\033[34m"
		purple = "\033[35m"
		cyan   = "\033[36m"
	)

	// Simple string-based colorization
	jsonStr = strings.ReplaceAll(jsonStr, `"`, cyan+`"`+reset)
	jsonStr = strings.ReplaceAll(jsonStr, `true`, green+`true`+reset)
	jsonStr = strings.ReplaceAll(jsonStr, `false`, red+`false`+reset)
	jsonStr = strings.ReplaceAll(jsonStr, `null`, purple+`null`+reset)

	// Color numbers (simple regex would be better)
	for i, char := range jsonStr {
		if char >= '0' && char <= '9' {
			// This is a simplified approach
			// Real implementation would use proper parsing
			start := i
			for i+1 < len(jsonStr) && ((jsonStr[i+1] >= '0' && jsonStr[i+1] <= '9') || jsonStr[i+1] == '.') {
				i++
			}
			if start != i {
				number := jsonStr[start : i+1]
				jsonStr = jsonStr[:start] + yellow + number + reset + jsonStr[i+1:]
			}
		}
	}

	return []byte(jsonStr)
}

// SortedMap ensures consistent key ordering in JSON output
type SortedMap struct {
	data     map[string]interface{}
	sortFunc func(interface{}) interface{}
	keyOrder []string
}

// NewSortedMap creates a new sorted map
func NewSortedMap(data map[string]interface{}, sortFunc func(interface{}) interface{}) *SortedMap {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return &SortedMap{
		data:     data,
		sortFunc: sortFunc,
		keyOrder: keys,
	}
}

// MarshalJSON implements json.Marshaler
func (sm *SortedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{")

	for i, key := range sm.keyOrder {
		if i > 0 {
			buf.WriteString(",")
		}

		// Marshal key
		keyBytes, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyBytes)
		buf.WriteString(":")

		// Marshal value
		value := sm.data[key]
		if sm.sortFunc != nil {
			value = sm.sortFunc(value)
		}

		valueBytes, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		buf.Write(valueBytes)
	}

	buf.WriteString("}")
	return buf.Bytes(), nil
}

// jsonBuffer is a simple buffer for JSON encoding
type jsonBuffer struct {
	bytes.Buffer
}

// DefaultJSONConfig returns default JSON configuration
func DefaultJSONConfig() JSONConfig {
	return JSONConfig{
		Indent:        "  ",
		EscapeHTML:    false,
		SortKeys:      false,
		CompactOutput: false,
		ColorOutput:   false,
	}
}

// PrettyJSONConfig returns configuration for pretty JSON output
func PrettyJSONConfig() JSONConfig {
	return JSONConfig{
		Indent:        "  ",
		EscapeHTML:    false,
		SortKeys:      true,
		CompactOutput: false,
		ColorOutput:   true,
	}
}

// CompactJSONConfig returns configuration for compact JSON output
func CompactJSONConfig() JSONConfig {
	return JSONConfig{
		Indent:        "",
		EscapeHTML:    false,
		SortKeys:      false,
		CompactOutput: true,
		ColorOutput:   false,
	}
}
