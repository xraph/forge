package formats

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/xraph/forge/internal/logger"
)

// TOMLProcessor handles TOML format configuration files
type TOMLProcessor struct {
	logger       logger.Logger
	strictMode   bool
	allowUnknown bool
}

// TOMLProcessorOptions contains options for the TOML processor
type TOMLProcessorOptions struct {
	Logger       logger.Logger
	StrictMode   bool // Strict parsing mode
	AllowUnknown bool // Allow unknown fields during validation
}

// NewTOMLProcessor creates a new TOML format processor
func NewTOMLProcessor(options TOMLProcessorOptions) *TOMLProcessor {
	return &TOMLProcessor{
		logger:       options.Logger,
		strictMode:   options.StrictMode,
		allowUnknown: options.AllowUnknown,
	}
}

// Name returns the processor name
func (tp *TOMLProcessor) Name() string {
	return "toml"
}

// Extensions returns the file extensions handled by this processor
func (tp *TOMLProcessor) Extensions() []string {
	return []string{".toml", ".tml"}
}

// Parse parses TOML data into a configuration map
func (tp *TOMLProcessor) Parse(data []byte) (map[string]interface{}, error) {
	if len(data) == 0 {
		return make(map[string]interface{}), nil
	}

	var result map[string]interface{}

	// Create TOML decoder with custom options
	decoder := toml.NewDecoder(strings.NewReader(string(data)))

	// Decode the TOML data
	if _, err := decoder.Decode(&result); err != nil {
		return nil, ErrConfigError(fmt.Sprintf("failed to parse TOML: %v", err), err)
	}

	// Convert the result to ensure proper types
	converted, err := tp.convertValue(result)
	if err != nil {
		return nil, ErrConfigError(fmt.Sprintf("failed to convert TOML values: %v", err), err)
	}

	// In strict mode, perform additional validation after parsing
	if tp.strictMode {
		if err := tp.validateStrictMode(result); err != nil {
			return nil, ErrConfigError(fmt.Sprintf("strict mode validation failed: %v", err), err)
		}
	}

	if convertedMap, ok := converted.(map[string]interface{}); ok {
		if tp.logger != nil {
			tp.logger.Debug("TOML data parsed successfully",
				logger.Int("keys", len(convertedMap)),
				logger.Int("size", len(data)),
			)
		}

		return convertedMap, nil
	}

	return nil, ErrConfigError("TOML root must be an object", nil)
}

// validateStrictMode performs strict mode validation after parsing
func (tp *TOMLProcessor) validateStrictMode(data map[string]interface{}) error {
	// Implement strict mode validation logic here
	// This could include checking for unknown fields, required fields, etc.
	// The exact implementation depends on what "strict mode" means for your use case
	return nil
}

// Validate validates TOML configuration data
func (tp *TOMLProcessor) Validate(data map[string]interface{}) error {
	if tp.logger != nil {
		tp.logger.Debug("validating TOML configuration",
			logger.Int("keys", len(data)),
		)
	}

	// Basic validation - ensure no invalid types
	return tp.validateValue("", data)
}

// validateValue recursively validates configuration values
func (tp *TOMLProcessor) validateValue(path string, value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		// Validate nested objects
		for key, val := range v {
			keyPath := key
			if path != "" {
				keyPath = path + "." + key
			}
			if err := tp.validateValue(keyPath, val); err != nil {
				return err
			}
		}

	case []interface{}:
		// Validate arrays
		for i, val := range v {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			if err := tp.validateValue(itemPath, val); err != nil {
				return err
			}
		}

	case string, int, int64, float64, bool, time.Time:
		// Valid TOML types
		return nil

	case nil:
		// Null values are acceptable
		return nil

	default:
		// Unknown types
		if tp.strictMode && !tp.allowUnknown {
			return ErrValidationError(path, fmt.Errorf("unsupported value type: %T", value))
		}

		if tp.logger != nil {
			tp.logger.Warn("unknown value type in TOML configuration",
				logger.String("path", path),
				logger.String("type", fmt.Sprintf("%T", value)),
			)
		}
	}

	return nil
}

// convertValue converts TOML values to standard Go types
func (tp *TOMLProcessor) convertValue(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		// Convert nested maps
		result := make(map[string]interface{})
		for key, val := range v {
			converted, err := tp.convertValue(val)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil

	case []interface{}:
		// Convert arrays
		result := make([]interface{}, len(v))
		for i, val := range v {
			converted, err := tp.convertValue(val)
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil

	case time.Time:
		// Convert time.Time to RFC3339 string for consistency
		return v.Format(time.RFC3339), nil

	case int64:
		// Convert int64 to int if it fits
		const maxInt = int(^uint(0) >> 1)
		const minInt = -maxInt - 1

		if v >= int64(minInt) && v <= int64(maxInt) {
			return int(v), nil // Convert to int if it fits
		}
		return v, nil // Keep as int64 if too large

	default:
		// Return as-is for other types (string, int, float64, bool)
		return value, nil
	}
}

// ParseToStruct parses TOML data directly into a struct
func (tp *TOMLProcessor) ParseToStruct(data []byte, target interface{}) error {
	if len(data) == 0 {
		return nil
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return ErrConfigError("target must be a pointer to struct", nil)
	}

	// Create TOML decoder
	decoder := toml.NewDecoder(strings.NewReader(string(data)))

	// Decode directly into struct
	if _, err := decoder.Decode(target); err != nil {
		return ErrConfigError(fmt.Sprintf("failed to decode TOML into struct: %v", err), err)
	}

	if tp.logger != nil {
		tp.logger.Debug("TOML data parsed into struct successfully",
			logger.String("type", targetValue.Elem().Type().String()),
			logger.Int("size", len(data)),
		)
	}

	return nil
}

// FormatData formats configuration data as TOML
func (tp *TOMLProcessor) FormatData(data map[string]interface{}) ([]byte, error) {
	if data == nil {
		return []byte{}, nil
	}

	// Convert data to ensure TOML compatibility
	converted, err := tp.prepareTOMLData(data)
	if err != nil {
		return nil, ErrConfigError(fmt.Sprintf("failed to prepare data for TOML: %v", err), err)
	}

	// Use strings.Builder for efficient string building
	var buf bytes.Buffer

	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(converted); err != nil {
		return nil, ErrConfigError(fmt.Sprintf("failed to encode TOML: %v", err), err)
	}

	result := buf.Bytes()

	if tp.logger != nil {
		tp.logger.Debug("configuration data formatted as TOML",
			logger.Int("input_keys", len(data)),
			logger.Int("output_size", len(result)),
		)
	}

	return result, nil
}

// prepareTOMLData prepares data for TOML encoding by ensuring type compatibility
func (tp *TOMLProcessor) prepareTOMLData(data interface{}) (interface{}, error) {
	switch v := data.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			prepared, err := tp.prepareTOMLData(val)
			if err != nil {
				return nil, err
			}
			result[key] = prepared
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			prepared, err := tp.prepareTOMLData(val)
			if err != nil {
				return nil, err
			}
			result[i] = prepared
		}
		return result, nil

	case string, int, int32, int64, float32, float64, bool:
		return v, nil

	case time.Time:
		return v, nil

	case time.Duration:
		// Convert duration to string
		return v.String(), nil

	case nil:
		// TOML doesn't support null values, convert to empty string
		return "", nil

	default:
		// Try to convert unknown types to string
		return fmt.Sprintf("%v", v), nil
	}
}

// ValidateStructTags validates TOML struct tags in a target struct
func (tp *TOMLProcessor) ValidateStructTags(target interface{}) error {
	targetType := reflect.TypeOf(target)
	if targetType.Kind() == reflect.Ptr {
		targetType = targetType.Elem()
	}

	if targetType.Kind() != reflect.Struct {
		return ErrConfigError("target must be a struct", nil)
	}

	return tp.validateStructFields(targetType, "")
}

// validateStructFields recursively validates struct fields and their TOML tags
func (tp *TOMLProcessor) validateStructFields(structType reflect.Type, path string) error {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		fieldPath := field.Name
		if path != "" {
			fieldPath = path + "." + field.Name
		}

		// Check TOML tag
		tomlTag := field.Tag.Get("toml")
		if tomlTag == "-" {
			continue // Skip fields marked to ignore
		}

		// Validate tag format
		if tomlTag != "" {
			if err := tp.validateTOMLTag(tomlTag, fieldPath); err != nil {
				return err
			}
		}

		// Recursively validate nested structs
		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		if fieldType.Kind() == reflect.Struct {
			// Skip time.Time and other known types
			if fieldType != reflect.TypeOf(time.Time{}) {
				if err := tp.validateStructFields(fieldType, fieldPath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// validateTOMLTag validates the format of a TOML struct tag
func (tp *TOMLProcessor) validateTOMLTag(tag, fieldPath string) error {
	// Parse tag options
	parts := strings.Split(tag, ",")
	name := parts[0]

	// Validate field name
	if name != "" && !tp.isValidTOMLFieldName(name) {
		return ErrValidationError(fieldPath, fmt.Errorf("invalid TOML field name: %s", name))
	}

	// Validate options
	for i := 1; i < len(parts); i++ {
		option := strings.TrimSpace(parts[i])
		if !tp.isValidTOMLOption(option) {
			return ErrValidationError(fieldPath, fmt.Errorf("invalid TOML tag option: %s", option))
		}
	}

	return nil
}

// isValidTOMLFieldName checks if a field name is valid for TOML
func (tp *TOMLProcessor) isValidTOMLFieldName(name string) bool {
	if name == "" {
		return false
	}

	// TOML field names can contain letters, numbers, underscores, and hyphens
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}

	return true
}

// isValidTOMLOption checks if a tag option is valid
func (tp *TOMLProcessor) isValidTOMLOption(option string) bool {
	validOptions := []string{"omitempty", "inline"}
	for _, valid := range validOptions {
		if option == valid {
			return true
		}
	}
	return false
}

// GetProcessor returns a configured TOML processor
func GetProcessor(logger logger.Logger) FormatProcessor {
	return NewTOMLProcessor(TOMLProcessorOptions{
		Logger:       logger,
		StrictMode:   false,
		AllowUnknown: true,
	})
}

// GetStrictProcessor returns a strict TOML processor
func GetStrictProcessor(logger logger.Logger) FormatProcessor {
	return NewTOMLProcessor(TOMLProcessorOptions{
		Logger:       logger,
		StrictMode:   true,
		AllowUnknown: false,
	})
}

// DetectTOMLFormat attempts to detect if data is in TOML format
func DetectTOMLFormat(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	content := strings.TrimSpace(string(data))

	// Basic TOML format detection
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		// Look for TOML patterns
		if strings.Contains(line, " = ") || // Key-value pair
			strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") || // Table header
			strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") { // Array of tables
			return true
		}

		// If we encounter other patterns, it's probably not TOML
		break
	}

	return false
}

// TOMLFormatValidator provides additional validation for TOML-specific features
type TOMLFormatValidator struct {
	processor *TOMLProcessor
}

// NewTOMLFormatValidator creates a new TOML format validator
func NewTOMLFormatValidator(processor *TOMLProcessor) *TOMLFormatValidator {
	return &TOMLFormatValidator{
		processor: processor,
	}
}

// ValidateTableStructure validates TOML table structure
func (tv *TOMLFormatValidator) ValidateTableStructure(data map[string]interface{}) error {
	return tv.validateTables("", data)
}

// validateTables recursively validates TOML table structure
func (tv *TOMLFormatValidator) validateTables(path string, value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, val := range v {
			keyPath := key
			if path != "" {
				keyPath = path + "." + key
			}

			// Validate key name
			if !tv.processor.isValidTOMLFieldName(key) {
				return ErrValidationError(keyPath, fmt.Errorf("invalid TOML table key: %s", key))
			}

			// Recursively validate nested structures
			if err := tv.validateTables(keyPath, val); err != nil {
				return err
			}
		}

	case []interface{}:
		// Validate array of tables (if all elements are objects)
		allObjects := true
		for _, item := range v {
			if _, ok := item.(map[string]interface{}); !ok {
				allObjects = false
				break
			}
		}

		if allObjects {
			// This is an array of tables
			for i, item := range v {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				if err := tv.validateTables(itemPath, item); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
