package output

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAMLFormatter formats data as YAML
type YAMLFormatter struct {
	config YAMLConfig
}

// YAMLConfig contains YAML formatting configuration
type YAMLConfig struct {
	Indent        int  `yaml:"indent" json:"indent"`
	FlowStyle     bool `yaml:"flow_style" json:"flow_style"`
	SortKeys      bool `yaml:"sort_keys" json:"sort_keys"`
	ColorOutput   bool `yaml:"color_output" json:"color_output"`
	SkipNilFields bool `yaml:"skip_nil_fields" json:"skip_nil_fields"`
	LineWidth     int  `yaml:"line_width" json:"line_width"`
}

// NewYAMLFormatter creates a new YAML formatter
func NewYAMLFormatter() OutputFormatter {
	return &YAMLFormatter{
		config: DefaultYAMLConfig(),
	}
}

// NewYAMLFormatterWithConfig creates a YAML formatter with custom config
func NewYAMLFormatterWithConfig(config YAMLConfig) OutputFormatter {
	return &YAMLFormatter{
		config: config,
	}
}

// Format formats data as YAML
func (yf *YAMLFormatter) Format(data interface{}) ([]byte, error) {
	// Prepare encoder
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)

	// Configure encoder
	encoder.SetIndent(yf.config.Indent)

	if yf.config.FlowStyle {
		// Note: yaml.v3 doesn't have a direct flow style option
		// This would need to be implemented differently
	}

	// Encode data
	if err := encoder.Encode(data); err != nil {
		return nil, fmt.Errorf("failed to marshal YAML: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	result := []byte(buf.String())

	// Remove trailing newline if present
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}

	// Add color formatting if enabled
	if yf.config.ColorOutput {
		return yf.colorizeYAML(result), nil
	}

	return result, nil
}

// MimeType returns the MIME type
func (yf *YAMLFormatter) MimeType() string {
	return "application/x-yaml"
}

// FileExtension returns the file extension
func (yf *YAMLFormatter) FileExtension() string {
	return ".yaml"
}

// colorizeYAML adds ANSI color codes to YAML output
func (yf *YAMLFormatter) colorizeYAML(data []byte) []byte {
	yamlStr := string(data)
	lines := strings.Split(yamlStr, "\n")

	// Color codes
	const (
		reset  = "\033[0m"
		red    = "\033[31m"
		green  = "\033[32m"
		yellow = "\033[33m"
		blue   = "\033[34m"
		purple = "\033[35m"
		cyan   = "\033[36m"
		gray   = "\033[90m"
	)

	for i, line := range lines {
		// Color comments
		if strings.Contains(line, "#") {
			parts := strings.SplitN(line, "#", 2)
			if len(parts) == 2 {
				line = parts[0] + gray + "#" + parts[1] + reset
			}
		}

		// Color keys (before :)
		if colonIndex := strings.Index(line, ":"); colonIndex != -1 {
			key := line[:colonIndex]
			rest := line[colonIndex:]

			// Color the key
			key = cyan + key + reset
			line = key + rest

			// Color values
			if valueStart := colonIndex + 1; valueStart < len(line) {
				value := strings.TrimSpace(line[valueStart:])
				if value != "" {
					coloredValue := yf.colorValue(value)
					line = line[:valueStart] + " " + coloredValue
				}
			}
		}

		// Color list items
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			indent := len(line) - len(strings.TrimLeft(line, " "))
			trimmed := strings.TrimSpace(line)
			value := strings.TrimSpace(trimmed[2:])
			line = strings.Repeat(" ", indent) + yellow + "-" + reset + " " + yf.colorValue(value)
		}

		lines[i] = line
	}

	return []byte(strings.Join(lines, "\n"))
}

// colorValue colors a YAML value based on its type
func (yf *YAMLFormatter) colorValue(value string) string {
	const (
		reset  = "\033[0m"
		red    = "\033[31m"
		green  = "\033[32m"
		yellow = "\033[33m"
		blue   = "\033[34m"
		purple = "\033[35m"
	)

	switch {
	case value == "true" || value == "false":
		if value == "true" {
			return green + value + reset
		}
		return red + value + reset
	case value == "null" || value == "~":
		return purple + value + reset
	case strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`):
		return blue + value + reset
	case strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`):
		return blue + value + reset
	case yf.isNumber(value):
		return yellow + value + reset
	default:
		return value
	}
}

// isNumber checks if a string represents a number
func (yf *YAMLFormatter) isNumber(s string) bool {
	// Simple number detection
	if s == "" {
		return false
	}

	// Handle negative numbers
	if s[0] == '-' {
		s = s[1:]
	}

	if s == "" {
		return false
	}

	hasDecimal := false
	for _, char := range s {
		if char == '.' {
			if hasDecimal {
				return false // Multiple decimals
			}
			hasDecimal = true
		} else if char < '0' || char > '9' {
			return false
		}
	}

	return true
}

// DefaultYAMLConfig returns default YAML configuration
func DefaultYAMLConfig() YAMLConfig {
	return YAMLConfig{
		Indent:        2,
		FlowStyle:     false,
		SortKeys:      false,
		ColorOutput:   false,
		SkipNilFields: false,
		LineWidth:     80,
	}
}

// PrettyYAMLConfig returns configuration for pretty YAML output
func PrettyYAMLConfig() YAMLConfig {
	return YAMLConfig{
		Indent:        2,
		FlowStyle:     false,
		SortKeys:      true,
		ColorOutput:   true,
		SkipNilFields: true,
		LineWidth:     120,
	}
}

// CompactYAMLConfig returns configuration for compact YAML output
func CompactYAMLConfig() YAMLConfig {
	return YAMLConfig{
		Indent:        1,
		FlowStyle:     true,
		SortKeys:      false,
		ColorOutput:   false,
		SkipNilFields: true,
		LineWidth:     200,
	}
}

// YAMLStreamFormatter formats multiple YAML documents
type YAMLStreamFormatter struct {
	*YAMLFormatter
	separator string
}

// NewYAMLStreamFormatter creates a formatter for YAML streams
func NewYAMLStreamFormatter(config YAMLConfig, separator string) *YAMLStreamFormatter {
	if separator == "" {
		separator = "---"
	}

	return &YAMLStreamFormatter{
		YAMLFormatter: &YAMLFormatter{config: config},
		separator:     separator,
	}
}

// FormatStream formats multiple documents as a YAML stream
func (ysf *YAMLStreamFormatter) FormatStream(documents []interface{}) ([]byte, error) {
	var result strings.Builder

	for i, doc := range documents {
		if i > 0 {
			result.WriteString("\n" + ysf.separator + "\n")
		}

		docBytes, err := ysf.Format(doc)
		if err != nil {
			return nil, fmt.Errorf("failed to format document %d: %w", i, err)
		}

		result.Write(docBytes)
	}

	return []byte(result.String()), nil
}
