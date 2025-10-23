package formats

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAMLProcessor implements YAML format processing
type YAMLProcessor struct {
	options YAMLOptions
}

// YAMLOptions contains options for YAML processing
type YAMLOptions struct {
	Strict           bool              // Strict parsing mode
	AllowDuplicates  bool              // Allow duplicate keys
	ValidateSchema   bool              // Validate against JSON schema
	KnownFields      bool              // Only allow known fields
	RequiredFields   []string          // List of required fields
	DeprecatedFields map[string]string // Deprecated field mappings
}

// NewYAMLProcessor creates a new YAML processor
func NewYAMLProcessor() FormatProcessor {
	return &YAMLProcessor{
		options: YAMLOptions{
			Strict:          false,
			AllowDuplicates: false,
			ValidateSchema:  false,
			KnownFields:     false,
		},
	}
}

// NewYAMLProcessorWithOptions creates a new YAML processor with options
func NewYAMLProcessorWithOptions(options YAMLOptions) FormatProcessor {
	return &YAMLProcessor{
		options: options,
	}
}

// Name returns the processor name
func (p *YAMLProcessor) Name() string {
	return "yaml"
}

// Extensions returns supported file extensions
func (p *YAMLProcessor) Extensions() []string {
	return []string{".yaml", ".yml"}
}

// Parse parses YAML data into a configuration map
func (p *YAMLProcessor) Parse(data []byte) (map[string]interface{}, error) {
	if len(data) == 0 {
		return make(map[string]interface{}), nil
	}

	var result map[string]interface{}

	// Create decoder with options
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(p.options.KnownFields)

	if err := decoder.Decode(&result); err != nil {
		return nil, ErrConfigError("failed to parse YAML", err)
	}

	// Handle null result
	if result == nil {
		result = make(map[string]interface{})
	}

	// Process the result
	processedResult, err := p.processYAMLData(result)
	if err != nil {
		return nil, err
	}

	// Validate if required
	if p.options.ValidateSchema {
		if err := p.validateYAMLStructure(processedResult); err != nil {
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

// Validate validates a configuration map against YAML rules
func (p *YAMLProcessor) Validate(data map[string]interface{}) error {
	if data == nil {
		return ErrValidationError("data", fmt.Errorf("configuration data is nil"))
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
		if err := p.validateYAMLStructure(data); err != nil {
			return err
		}
	}

	return nil
}

// processYAMLData processes YAML data to normalize types
func (p *YAMLProcessor) processYAMLData(data map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range data {
		processedValue, err := p.processYAMLValue(value)
		if err != nil {
			return nil, ErrConfigError(fmt.Sprintf("failed to process key '%s'", key), err)
		}
		result[key] = processedValue
	}

	return result, nil
}

// processYAMLValue processes a single YAML value
func (p *YAMLProcessor) processYAMLValue(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		// Recursively process nested maps
		return p.processYAMLData(v)

	case map[interface{}]interface{}:
		// Convert map[interface{}]interface{} to map[string]interface{}
		result := make(map[string]interface{})
		for k, val := range v {
			keyStr, ok := k.(string)
			if !ok {
				keyStr = fmt.Sprintf("%v", k)
			}
			processedVal, err := p.processYAMLValue(val)
			if err != nil {
				return nil, err
			}
			result[keyStr] = processedVal
		}
		return result, nil

	case []interface{}:
		// Process array elements
		result := make([]interface{}, len(v))
		for i, item := range v {
			processedItem, err := p.processYAMLValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = processedItem
		}
		return result, nil

	case string:
		// Process string values (handle special YAML strings)
		return p.processStringValue(v)

	default:
		// Return other types as-is
		return value, nil
	}
}

// processStringValue processes string values for special YAML constructs
func (p *YAMLProcessor) processStringValue(value string) (interface{}, error) {
	// Handle YAML null values
	if value == "null" || value == "~" || value == "" {
		return nil, nil
	}

	// Handle YAML boolean values
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
		return false, nil
	}

	// Handle multiline strings
	value = strings.TrimSpace(value)

	return value, nil
}

// validateYAMLStructure validates the YAML structure
func (p *YAMLProcessor) validateYAMLStructure(data map[string]interface{}) error {
	// Check for circular references
	if err := p.checkCircularReferences(data, make(map[interface{}]bool)); err != nil {
		return err
	}

	// Validate nested structure
	return p.validateNestedStructure(data, "")
}

// checkCircularReferences checks for circular references in the data
func (p *YAMLProcessor) checkCircularReferences(data interface{}, visited map[interface{}]bool) error {
	if data == nil {
		return nil
	}

	// Use pointer for reference checking
	dataPtr := reflect.ValueOf(data)
	if !dataPtr.IsValid() {
		return nil
	}

	// Check if we've seen this exact object before
	if visited[dataPtr.Interface()] {
		return ErrValidationError("circular_reference", fmt.Errorf("circular reference detected"))
	}

	// Mark as visited
	visited[dataPtr.Interface()] = true
	defer delete(visited, dataPtr.Interface())

	switch v := data.(type) {
	case map[string]interface{}:
		for _, value := range v {
			if err := p.checkCircularReferences(value, visited); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, item := range v {
			if err := p.checkCircularReferences(item, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateNestedStructure validates nested structure constraints
func (p *YAMLProcessor) validateNestedStructure(data map[string]interface{}, path string) error {
	for key, value := range data {
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}

		// Validate key format
		if err := p.validateKeyFormat(key); err != nil {
			return ErrValidationError(currentPath, err)
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
func (p *YAMLProcessor) validateKeyFormat(key string) error {
	// Check for empty keys
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("empty key not allowed")
	}

	// Check for reserved characters in strict mode
	if p.options.Strict {
		invalidChars := []string{" ", "\t", "\n", "\r"}
		for _, char := range invalidChars {
			if strings.Contains(key, char) {
				return fmt.Errorf("key contains invalid character: %q", char)
			}
		}
	}

	return nil
}

// validateArrayStructure validates array structure
func (p *YAMLProcessor) validateArrayStructure(array []interface{}, path string) error {
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
func (p *YAMLProcessor) checkRequiredFields(data map[string]interface{}) error {
	for _, required := range p.options.RequiredFields {
		if !p.hasNestedKey(data, required) {
			return ErrValidationError("required_field", fmt.Errorf("required field missing: %s", required))
		}
	}
	return nil
}

// checkDeprecatedFields checks for deprecated fields
func (p *YAMLProcessor) checkDeprecatedFields(data map[string]interface{}) error {
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
		return ErrValidationError("deprecated_fields", errors.New(strings.Join(warnings, "; ")))
	}

	// In non-strict mode, just log warnings (would need logger passed in)
	return nil
}

// hasNestedKey checks if a nested key exists
func (p *YAMLProcessor) hasNestedKey(data map[string]interface{}, key string) bool {
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

// Marshal marshals a configuration map to YAML
func (p *YAMLProcessor) Marshal(data map[string]interface{}) ([]byte, error) {
	return yaml.Marshal(data)
}

// MarshalIndent marshals a configuration map to indented YAML
func (p *YAMLProcessor) MarshalIndent(data map[string]interface{}, indent string) ([]byte, error) {
	// YAML doesn't have a direct indent option like JSON, but we can configure the encoder
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(len(indent))

	if err := encoder.Encode(data); err != nil {
		return nil, err
	}

	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return []byte(buf.String()), nil
}

// ParseMultiDocument parses multi-document YAML
func (p *YAMLProcessor) ParseMultiDocument(data []byte) ([]map[string]interface{}, error) {
	if len(data) == 0 {
		return []map[string]interface{}{}, nil
	}

	var documents []map[string]interface{}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))

	for {
		var doc map[string]interface{}
		err := decoder.Decode(&doc)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, ErrConfigError("failed to parse YAML document", err)
		}

		if doc != nil {
			processedDoc, err := p.processYAMLData(doc)
			if err != nil {
				return nil, err
			}
			documents = append(documents, processedDoc)
		}
	}

	return documents, nil
}

// ValidateWithSchema validates YAML against a JSON schema
func (p *YAMLProcessor) ValidateWithSchema(data map[string]interface{}, schema interface{}) error {
	// This is a placeholder for JSON schema validation
	// In a real implementation, you would use a JSON schema validation library
	// like github.com/xeipuuv/gojsonschema

	// Convert data to JSON for schema validation
	jsonData, err := p.convertToJSONCompatible(data)
	if err != nil {
		return ErrValidationError("json_conversion", err)
	}

	// Perform schema validation (placeholder)
	_ = jsonData
	_ = schema

	return nil
}

// convertToJSONCompatible converts YAML data to JSON-compatible format
func (p *YAMLProcessor) convertToJSONCompatible(data interface{}) (interface{}, error) {
	switch v := data.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, value := range v {
			convertedValue, err := p.convertToJSONCompatible(value)
			if err != nil {
				return nil, err
			}
			result[key] = convertedValue
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			convertedItem, err := p.convertToJSONCompatible(item)
			if err != nil {
				return nil, err
			}
			result[i] = convertedItem
		}
		return result, nil

	case nil:
		return nil, nil

	default:
		return v, nil
	}
}

// GetMetadata returns metadata about the YAML processor
func (p *YAMLProcessor) GetMetadata() map[string]interface{} {
	return map[string]interface{}{
		"name":              p.Name(),
		"extensions":        p.Extensions(),
		"strict_mode":       p.options.Strict,
		"allow_duplicates":  p.options.AllowDuplicates,
		"validate_schema":   p.options.ValidateSchema,
		"known_fields":      p.options.KnownFields,
		"required_fields":   p.options.RequiredFields,
		"deprecated_fields": p.options.DeprecatedFields,
	}
}

// SetOptions updates the processor options
func (p *YAMLProcessor) SetOptions(options YAMLOptions) {
	p.options = options
}

// GetOptions returns the current processor options
func (p *YAMLProcessor) GetOptions() YAMLOptions {
	return p.options
}

// YAML-specific utility functions

// ExtractComments extracts comments from YAML content
func ExtractComments(data []byte) ([]string, error) {
	var comments []string
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			comment := strings.TrimPrefix(trimmed, "#")
			comment = strings.TrimSpace(comment)
			if comment != "" {
				comments = append(comments, comment)
			}
		}
	}

	return comments, nil
}

// ValidateYAMLSyntax validates YAML syntax without full parsing
func ValidateYAMLSyntax(data []byte) error {
	var temp interface{}
	return yaml.Unmarshal(data, &temp)
}

// FormatYAML formats YAML content with consistent indentation
func FormatYAML(data []byte) ([]byte, error) {
	var content interface{}
	if err := yaml.Unmarshal(data, &content); err != nil {
		return nil, err
	}

	return yaml.Marshal(content)
}

// MergeYAMLDocuments merges multiple YAML documents
func MergeYAMLDocuments(documents ...map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for _, doc := range documents {
		if err := mergeMap(result, doc); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// mergeMap recursively merges maps
func mergeMap(dst, src map[string]interface{}) error {
	for key, srcValue := range src {
		if dstValue, exists := dst[key]; exists {
			// Both values exist, try to merge if both are maps
			if dstMap, ok := dstValue.(map[string]interface{}); ok {
				if srcMap, ok := srcValue.(map[string]interface{}); ok {
					if err := mergeMap(dstMap, srcMap); err != nil {
						return err
					}
					continue
				}
			}
		}
		// Set or override the value
		dst[key] = srcValue
	}
	return nil
}
