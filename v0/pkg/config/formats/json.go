package formats

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xraph/forge/pkg/common"
)

// JSONProcessor implements JSON format processing
type JSONProcessor struct {
	options JSONOptions
}

// JSONOptions contains options for JSON processing
type JSONOptions struct {
	Strict              bool              // Strict parsing mode
	AllowComments       bool              // Allow comments in JSON (JSON5 style)
	AllowTrailingCommas bool              // Allow trailing commas
	ValidateSchema      bool              // Validate against JSON schema
	RequiredFields      []string          // List of required fields
	DeprecatedFields    map[string]string // Deprecated field mappings
	NumberAsString      bool              // Parse numbers as strings to preserve precision
	DisallowUnknown     bool              // Disallow unknown fields
}

// NewJSONProcessor creates a new JSON processor
func NewJSONProcessor() FormatProcessor {
	return &JSONProcessor{
		options: JSONOptions{
			Strict:              false,
			AllowComments:       false,
			AllowTrailingCommas: false,
			ValidateSchema:      false,
			NumberAsString:      false,
			DisallowUnknown:     false,
		},
	}
}

// NewJSONProcessorWithOptions creates a new JSON processor with options
func NewJSONProcessorWithOptions(options JSONOptions) FormatProcessor {
	return &JSONProcessor{
		options: options,
	}
}

// Name returns the processor name
func (p *JSONProcessor) Name() string {
	return "json"
}

// Extensions returns supported file extensions
func (p *JSONProcessor) Extensions() []string {
	return []string{".json", ".json5"}
}

// Parse parses JSON data into a configuration map
func (p *JSONProcessor) Parse(data []byte) (map[string]interface{}, error) {
	if len(data) == 0 {
		return make(map[string]interface{}), nil
	}

	// Preprocess for JSON5 features if enabled
	if p.options.AllowComments || p.options.AllowTrailingCommas {
		processed, err := p.preprocessJSON5(data)
		if err != nil {
			return nil, common.ErrConfigError("failed to preprocess JSON5", err)
		}
		data = processed
	}

	var result map[string]interface{}

	// Create decoder
	decoder := json.NewDecoder(strings.NewReader(string(data)))

	// Configure decoder options
	if p.options.DisallowUnknown {
		decoder.DisallowUnknownFields()
	}

	if p.options.NumberAsString {
		decoder.UseNumber()
	}

	// Decode JSON
	if err := decoder.Decode(&result); err != nil {
		return nil, common.ErrConfigError("failed to parse JSON", err)
	}

	// Handle null result
	if result == nil {
		result = make(map[string]interface{})
	}

	// Process the result
	processedResult, err := p.processJSONData(result)
	if err != nil {
		return nil, err
	}

	// Validate if required
	if p.options.ValidateSchema {
		if err := p.validateJSONStructure(processedResult); err != nil {
			return nil, err
		}
	}

	// Check required fields
	if err := p.checkRequiredFields(processedResult); err != nil {
		return nil, err
	}

	// Check for deprecated fields
	if err := p.checkDeprecatedFields(processedResult); err != nil {
		return nil, err
	}

	return processedResult, nil
}

// Validate validates a configuration map against JSON rules
func (p *JSONProcessor) Validate(data map[string]interface{}) error {
	if data == nil {
		return common.ErrValidationError("data", fmt.Errorf("configuration data is nil"))
	}

	// Check required fields
	if err := p.checkRequiredFields(data); err != nil {
		return err
	}

	// Check for deprecated fields
	if err := p.checkDeprecatedFields(data); err != nil {
		return err
	}

	// Validate structure if enabled
	if p.options.ValidateSchema {
		if err := p.validateJSONStructure(data); err != nil {
			return err
		}
	}

	return nil
}

// preprocessJSON5 preprocesses JSON5 features
func (p *JSONProcessor) preprocessJSON5(data []byte) ([]byte, error) {
	content := string(data)

	// Remove comments if allowed
	if p.options.AllowComments {
		content = p.removeComments(content)
	}

	// Handle trailing commas if allowed
	if p.options.AllowTrailingCommas {
		content = p.removeTrailingCommas(content)
	}

	return []byte(content), nil
}

// removeComments removes JavaScript-style comments from JSON
func (p *JSONProcessor) removeComments(content string) string {
	var result strings.Builder
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		// Remove single-line comments
		if idx := strings.Index(line, "//"); idx >= 0 {
			// Check if it's inside a string
			if !p.isInsideString(line, idx) {
				line = line[:idx]
			}
		}
		result.WriteString(line)
		result.WriteString("\n")
	}

	content = result.String()

	// Remove multi-line comments
	content = p.removeMultiLineComments(content)

	return content
}

// isInsideString checks if a position is inside a JSON string
func (p *JSONProcessor) isInsideString(line string, pos int) bool {
	inString := false
	escaped := false

	for i, char := range line {
		if i >= pos {
			break
		}

		if escaped {
			escaped = false
			continue
		}

		if char == '\\' {
			escaped = true
			continue
		}

		if char == '"' {
			inString = !inString
		}
	}

	return inString
}

// removeMultiLineComments removes /* */ style comments
func (p *JSONProcessor) removeMultiLineComments(content string) string {
	var result strings.Builder
	i := 0

	for i < len(content) {
		if i < len(content)-1 && content[i] == '/' && content[i+1] == '*' {
			// Start of multi-line comment
			i += 2
			for i < len(content)-1 {
				if content[i] == '*' && content[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
		} else {
			result.WriteByte(content[i])
			i++
		}
	}

	return result.String()
}

// removeTrailingCommas removes trailing commas from JSON
func (p *JSONProcessor) removeTrailingCommas(content string) string {
	// Simple implementation - in production you might want a more sophisticated parser
	content = strings.ReplaceAll(content, ",\n}", "\n}")
	content = strings.ReplaceAll(content, ",\n]", "\n]")
	content = strings.ReplaceAll(content, ", }", " }")
	content = strings.ReplaceAll(content, ", ]", " ]")
	return content
}

// processJSONData processes JSON data to normalize types
func (p *JSONProcessor) processJSONData(data map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range data {
		processedValue, err := p.processJSONValue(value)
		if err != nil {
			return nil, common.ErrConfigError(fmt.Sprintf("failed to process key '%s'", key), err)
		}
		result[key] = processedValue
	}

	return result, nil
}

// processJSONValue processes a single JSON value
func (p *JSONProcessor) processJSONValue(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		// Recursively process nested maps
		return p.processJSONData(v)

	case []interface{}:
		// Process array elements
		result := make([]interface{}, len(v))
		for i, item := range v {
			processedItem, err := p.processJSONValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = processedItem
		}
		return result, nil

	case json.Number:
		// Handle json.Number type when UseNumber() is enabled
		return p.processNumberValue(v)

	case string:
		// Process string values
		return p.processStringValue(v)

	default:
		// Return other types as-is
		return value, nil
	}
}

func (p *JSONProcessor) processNumberValue(num json.Number) (interface{}, error) {
	if p.options.NumberAsString {
		return string(num), nil
	}

	// Try to convert to int64 first
	if intVal, err := num.Int64(); err == nil {
		// Check if it fits in a regular int
		// Use math constants for platform-independent int bounds
		const maxInt = int(^uint(0) >> 1)
		const minInt = -maxInt - 1

		if intVal >= int64(minInt) && intVal <= int64(maxInt) {
			return int(intVal), nil
		}
		return intVal, nil
	}

	// Convert to float64
	if floatVal, err := num.Float64(); err == nil {
		return floatVal, nil
	}

	// Fallback to string
	return string(num), nil
}

// processStringValue processes string values for special JSON constructs
func (p *JSONProcessor) processStringValue(value string) (interface{}, error) {
	// In JSON, strings are already properly escaped and typed
	return value, nil
}

// validateJSONStructure validates the JSON structure
func (p *JSONProcessor) validateJSONStructure(data map[string]interface{}) error {
	// Validate that the data can be marshaled back to valid JSON
	if _, err := json.Marshal(data); err != nil {
		return common.ErrValidationError("json_structure", err)
	}

	// Check for circular references (JSON doesn't support them)
	if err := p.checkCircularReferences(data, make(map[interface{}]bool)); err != nil {
		return err
	}

	// Validate nested structure
	return p.validateNestedStructure(data, "")
}

// checkCircularReferences checks for circular references in the data
func (p *JSONProcessor) checkCircularReferences(data interface{}, visited map[interface{}]bool) error {
	if data == nil {
		return nil
	}

	// Check if we've seen this exact object before
	if visited[data] {
		return common.ErrValidationError("circular_reference", fmt.Errorf("circular reference detected"))
	}

	// Mark as visited for objects and arrays
	switch v := data.(type) {
	case map[string]interface{}:
		visited[data] = true
		defer delete(visited, data)

		for _, value := range v {
			if err := p.checkCircularReferences(value, visited); err != nil {
				return err
			}
		}
	case []interface{}:
		visited[data] = true
		defer delete(visited, data)

		for _, item := range v {
			if err := p.checkCircularReferences(item, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateNestedStructure validates nested structure constraints
func (p *JSONProcessor) validateNestedStructure(data map[string]interface{}, path string) error {
	for key, value := range data {
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}

		// Validate key format
		if err := p.validateKeyFormat(key); err != nil {
			return common.ErrValidationError(currentPath, err)
		}

		// Recursively validate nested maps
		if nestedMap, ok := value.(map[string]interface{}); ok {
			if err := p.validateNestedStructure(nestedMap, currentPath); err != nil {
				return err
			}
		}

		// Validate arrays
		if array, ok := value.([]interface{}); ok {
			if err := p.validateArrayStructure(array, currentPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateKeyFormat validates configuration key format
func (p *JSONProcessor) validateKeyFormat(key string) error {
	// Check for empty keys
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("empty key not allowed")
	}

	// In strict mode, enforce additional key constraints
	if p.options.Strict {
		// Keys should not start with numbers (for JavaScript compatibility)
		if len(key) > 0 && key[0] >= '0' && key[0] <= '9' {
			return fmt.Errorf("key cannot start with a number: %s", key)
		}

		// Keys should be valid JavaScript identifiers
		if !p.isValidIdentifier(key) {
			return fmt.Errorf("key is not a valid identifier: %s", key)
		}
	}

	return nil
}

// isValidIdentifier checks if a string is a valid JavaScript identifier
func (p *JSONProcessor) isValidIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}

	// First character must be letter, underscore, or dollar sign
	first := s[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_' || first == '$') {
		return false
	}

	// Subsequent characters can be letters, digits, underscores, or dollar signs
	for i := 1; i < len(s); i++ {
		char := s[i]
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '$') {
			return false
		}
	}

	return true
}

// validateArrayStructure validates array structure
func (p *JSONProcessor) validateArrayStructure(array []interface{}, path string) error {
	for i, item := range array {
		itemPath := fmt.Sprintf("%s[%d]", path, i)

		// Recursively validate nested maps in arrays
		if nestedMap, ok := item.(map[string]interface{}); ok {
			if err := p.validateNestedStructure(nestedMap, itemPath); err != nil {
				return err
			}
		}

		// Recursively validate nested arrays
		if nestedArray, ok := item.([]interface{}); ok {
			if err := p.validateArrayStructure(nestedArray, itemPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkRequiredFields checks for required fields
func (p *JSONProcessor) checkRequiredFields(data map[string]interface{}) error {
	for _, required := range p.options.RequiredFields {
		if !p.hasNestedKey(data, required) {
			return common.ErrValidationError("required_field", fmt.Errorf("required field missing: %s", required))
		}
	}
	return nil
}

// checkDeprecatedFields checks for deprecated fields
func (p *JSONProcessor) checkDeprecatedFields(data map[string]interface{}) error {
	var warnings []string

	for deprecated, replacement := range p.options.DeprecatedFields {
		if p.hasNestedKey(data, deprecated) {
			warning := fmt.Sprintf("field '%s' is deprecated", deprecated)
			if replacement != "" {
				warning += fmt.Sprintf(", use '%s' instead", replacement)
			}
			warnings = append(warnings, warning)
		}
	}

	if len(warnings) > 0 && p.options.Strict {
		return common.ErrValidationError("deprecated_fields", errors.New(strings.Join(warnings, "; ")))
	}

	// In non-strict mode, just log warnings (would need logger passed in)
	return nil
}

// hasNestedKey checks if a nested key exists
func (p *JSONProcessor) hasNestedKey(data map[string]interface{}, key string) bool {
	keys := strings.Split(key, ".")
	current := interface{}(data)

	for _, k := range keys {
		if currentMap, ok := current.(map[string]interface{}); ok {
			if value, exists := currentMap[k]; exists {
				current = value
			} else {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

// Marshal marshals a configuration map to JSON
func (p *JSONProcessor) Marshal(data map[string]interface{}) ([]byte, error) {
	return json.Marshal(data)
}

// MarshalIndent marshals a configuration map to indented JSON
func (p *JSONProcessor) MarshalIndent(data map[string]interface{}, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(data, prefix, indent)
}

// Compact compacts JSON by removing whitespace
func (p *JSONProcessor) Compact(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// ValidateJSONSyntax validates JSON syntax without full parsing
func (p *JSONProcessor) ValidateJSONSyntax(data []byte) error {
	var temp interface{}
	return json.Unmarshal(data, &temp)
}

// FormatJSON formats JSON content with consistent indentation
func (p *JSONProcessor) FormatJSON(data []byte, indent string) ([]byte, error) {
	var content interface{}
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, err
	}

	return json.MarshalIndent(content, "", indent)
}

// ExtractPaths extracts all JSON paths from the data
func (p *JSONProcessor) ExtractPaths(data map[string]interface{}) []string {
	var paths []string
	p.extractPathsRecursive(data, "", &paths)
	return paths
}

// extractPathsRecursive recursively extracts paths
func (p *JSONProcessor) extractPathsRecursive(data interface{}, currentPath string, paths *[]string) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			newPath := key
			if currentPath != "" {
				newPath = currentPath + "." + key
			}
			*paths = append(*paths, newPath)
			p.extractPathsRecursive(value, newPath, paths)
		}
	case []interface{}:
		for i, item := range v {
			newPath := fmt.Sprintf("%s[%d]", currentPath, i)
			*paths = append(*paths, newPath)
			p.extractPathsRecursive(item, newPath, paths)
		}
	}
}

// GetValueByPath gets a value using a JSON path
func (p *JSONProcessor) GetValueByPath(data map[string]interface{}, path string) (interface{}, error) {
	// Simple implementation - in production you might want JSONPath support
	keys := strings.Split(path, ".")
	current := interface{}(data)

	for _, key := range keys {
		// Handle array indices
		if strings.Contains(key, "[") && strings.Contains(key, "]") {
			// Extract array key and index
			parts := strings.Split(key, "[")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid array path: %s", key)
			}

			arrayKey := parts[0]
			indexStr := strings.TrimSuffix(parts[1], "]")

			// Get array
			if currentMap, ok := current.(map[string]interface{}); ok {
				if arrayValue, exists := currentMap[arrayKey]; exists {
					if array, ok := arrayValue.([]interface{}); ok {
						// Parse index
						index := 0
						if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil {
							return nil, fmt.Errorf("invalid array index: %s", indexStr)
						}

						if index >= 0 && index < len(array) {
							current = array[index]
						} else {
							return nil, fmt.Errorf("array index out of bounds: %d", index)
						}
					} else {
						return nil, fmt.Errorf("expected array at %s", arrayKey)
					}
				} else {
					return nil, fmt.Errorf("key not found: %s", arrayKey)
				}
			} else {
				return nil, fmt.Errorf("expected object, got %T", current)
			}
		} else {
			// Regular object key
			if currentMap, ok := current.(map[string]interface{}); ok {
				if value, exists := currentMap[key]; exists {
					current = value
				} else {
					return nil, fmt.Errorf("key not found: %s", key)
				}
			} else {
				return nil, fmt.Errorf("expected object, got %T", current)
			}
		}
	}

	return current, nil
}

// GetMetadata returns metadata about the JSON processor
func (p *JSONProcessor) GetMetadata() map[string]interface{} {
	return map[string]interface{}{
		"name":                  p.Name(),
		"extensions":            p.Extensions(),
		"strict_mode":           p.options.Strict,
		"allow_comments":        p.options.AllowComments,
		"allow_trailing_commas": p.options.AllowTrailingCommas,
		"validate_schema":       p.options.ValidateSchema,
		"number_as_string":      p.options.NumberAsString,
		"disallow_unknown":      p.options.DisallowUnknown,
		"required_fields":       p.options.RequiredFields,
		"deprecated_fields":     p.options.DeprecatedFields,
	}
}

// SetOptions updates the processor options
func (p *JSONProcessor) SetOptions(options JSONOptions) {
	p.options = options
}

// GetOptions returns the current processor options
func (p *JSONProcessor) GetOptions() JSONOptions {
	return p.options
}

// JSON-specific utility functions

// ConvertToJSONSchema converts a configuration map to a JSON schema
func (p *JSONProcessor) ConvertToJSONSchema(data map[string]interface{}) (map[string]interface{}, error) {
	schema := map[string]interface{}{
		"$schema":    "http://json-schema.org/draft-07/schema#",
		"type":       "object",
		"properties": make(map[string]interface{}),
	}

	properties := schema["properties"].(map[string]interface{})

	for key, value := range data {
		propSchema, err := p.inferPropertySchema(value)
		if err != nil {
			return nil, err
		}
		properties[key] = propSchema
	}

	return schema, nil
}

// inferPropertySchema infers a JSON schema for a property
func (p *JSONProcessor) inferPropertySchema(value interface{}) (map[string]interface{}, error) {
	switch v := value.(type) {
	case string:
		return map[string]interface{}{"type": "string"}, nil
	case int, int32, int64:
		return map[string]interface{}{"type": "integer"}, nil
	case float32, float64:
		return map[string]interface{}{"type": "number"}, nil
	case bool:
		return map[string]interface{}{"type": "boolean"}, nil
	case []interface{}:
		schema := map[string]interface{}{
			"type": "array",
		}
		if len(v) > 0 {
			itemSchema, err := p.inferPropertySchema(v[0])
			if err != nil {
				return nil, err
			}
			schema["items"] = itemSchema
		}
		return schema, nil
	case map[string]interface{}:
		schema := map[string]interface{}{
			"type":       "object",
			"properties": make(map[string]interface{}),
		}
		properties := schema["properties"].(map[string]interface{})
		for key, val := range v {
			propSchema, err := p.inferPropertySchema(val)
			if err != nil {
				return nil, err
			}
			properties[key] = propSchema
		}
		return schema, nil
	case nil:
		return map[string]interface{}{"type": "null"}, nil
	default:
		return map[string]interface{}{"type": "string"}, nil
	}
}
