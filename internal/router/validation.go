package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// SchemaValidator validates data against JSON schemas
type SchemaValidator struct {
	schemas map[string]*Schema
}

// NewSchemaValidator creates a new schema validator
func NewSchemaValidator() *SchemaValidator {
	return &SchemaValidator{
		schemas: make(map[string]*Schema),
	}
}

// RegisterSchema registers a schema with a name
func (v *SchemaValidator) RegisterSchema(name string, schema *Schema) {
	v.schemas[name] = schema
}

// ValidateRequest validates request data against a schema
func (v *SchemaValidator) ValidateRequest(schema *Schema, data interface{}) error {
	errors := NewValidationErrors()
	v.validateValue(schema, data, "", errors)

	if errors.HasErrors() {
		return errors
	}
	return nil
}

// ValidateResponse validates response data against a schema
func (v *SchemaValidator) ValidateResponse(schema *Schema, data interface{}) error {
	return v.ValidateRequest(schema, data)
}

// validateValue validates a value against a schema
func (v *SchemaValidator) validateValue(schema *Schema, value interface{}, path string, errors *ValidationErrors) {
	if schema == nil {
		return
	}

	// Handle $ref
	if schema.Ref != "" {
		// For now, skip ref validation (would need component resolution)
		return
	}

	// Check type
	actualType := getJSONType(value)
	if schema.Type != "" && actualType != schema.Type {
		// Allow null for nullable fields
		if actualType == "null" && schema.Nullable {
			return
		}
		errors.AddWithCode(path, fmt.Sprintf("Expected type %s, got %s", schema.Type, actualType), ErrCodeInvalidType, value)
		return
	}

	// Type-specific validation
	switch schema.Type {
	case "string":
		v.validateString(schema, value, path, errors)
	case "number", "integer":
		v.validateNumber(schema, value, path, errors)
	case "array":
		v.validateArray(schema, value, path, errors)
	case "object":
		v.validateObject(schema, value, path, errors)
	case "boolean":
		// Boolean doesn't need additional validation
	}

	// Enum validation
	if len(schema.Enum) > 0 {
		v.validateEnum(schema, value, path, errors)
	}
}

// validateString validates string values
func (v *SchemaValidator) validateString(schema *Schema, value interface{}, path string, errors *ValidationErrors) {
	str, ok := value.(string)
	if !ok {
		return
	}

	// Length validation
	if schema.MinLength > 0 && len(str) < schema.MinLength {
		errors.AddWithCode(path, fmt.Sprintf("String length must be at least %d", schema.MinLength), ErrCodeMinLength, value)
	}
	if schema.MaxLength > 0 && len(str) > schema.MaxLength {
		errors.AddWithCode(path, fmt.Sprintf("String length must be at most %d", schema.MaxLength), ErrCodeMaxLength, value)
	}

	// Pattern validation
	if schema.Pattern != "" {
		matched, err := regexp.MatchString(schema.Pattern, str)
		if err == nil && !matched {
			errors.AddWithCode(path, fmt.Sprintf("String does not match pattern: %s", schema.Pattern), ErrCodePattern, value)
		}
	}

	// Format validation (basic)
	if schema.Format != "" {
		v.validateFormat(schema.Format, str, path, errors)
	}
}

// validateNumber validates numeric values
func (v *SchemaValidator) validateNumber(schema *Schema, value interface{}, path string, errors *ValidationErrors) {
	var num float64

	switch val := value.(type) {
	case float64:
		num = val
	case float32:
		num = float64(val)
	case int:
		num = float64(val)
	case int64:
		num = float64(val)
	default:
		return
	}

	// Minimum/maximum validation
	if schema.Minimum > 0 {
		if schema.ExclusiveMinimum {
			if num <= schema.Minimum {
				errors.AddWithCode(path, fmt.Sprintf("Value must be greater than %v", schema.Minimum), ErrCodeMinValue, value)
			}
		} else {
			if num < schema.Minimum {
				errors.AddWithCode(path, fmt.Sprintf("Value must be at least %v", schema.Minimum), ErrCodeMinValue, value)
			}
		}
	}

	if schema.Maximum > 0 {
		if schema.ExclusiveMaximum {
			if num >= schema.Maximum {
				errors.AddWithCode(path, fmt.Sprintf("Value must be less than %v", schema.Maximum), ErrCodeMaxValue, value)
			}
		} else {
			if num > schema.Maximum {
				errors.AddWithCode(path, fmt.Sprintf("Value must be at most %v", schema.Maximum), ErrCodeMaxValue, value)
			}
		}
	}

	// MultipleOf validation
	if schema.MultipleOf > 0 {
		if remainder := num / schema.MultipleOf; remainder != float64(int(remainder)) {
			errors.AddWithCode(path, fmt.Sprintf("Value must be a multiple of %v", schema.MultipleOf), ErrCodeInvalidFormat, value)
		}
	}
}

// validateArray validates array values
func (v *SchemaValidator) validateArray(schema *Schema, value interface{}, path string, errors *ValidationErrors) {
	arr, ok := toSlice(value)
	if !ok {
		return
	}

	// Length validation
	if schema.MinItems > 0 && len(arr) < schema.MinItems {
		errors.AddWithCode(path, fmt.Sprintf("Array must have at least %d items", schema.MinItems), ErrCodeMinItems, value)
	}
	if schema.MaxItems > 0 && len(arr) > schema.MaxItems {
		errors.AddWithCode(path, fmt.Sprintf("Array must have at most %d items", schema.MaxItems), ErrCodeMaxItems, value)
	}

	// Unique items validation
	if schema.UniqueItems {
		seen := make(map[string]bool)
		for i, item := range arr {
			key := fmt.Sprintf("%v", item)
			if seen[key] {
				errors.AddWithCode(fmt.Sprintf("%s[%d]", path, i), "Array items must be unique", ErrCodeUniqueItems, item)
			}
			seen[key] = true
		}
	}

	// Validate each item
	if schema.Items != nil {
		for i, item := range arr {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			v.validateValue(schema.Items, item, itemPath, errors)
		}
	}
}

// validateObject validates object values
func (v *SchemaValidator) validateObject(schema *Schema, value interface{}, path string, errors *ValidationErrors) {
	obj, ok := toMap(value)
	if !ok {
		return
	}

	// Required fields validation
	for _, required := range schema.Required {
		if _, exists := obj[required]; !exists {
			fieldPath := path
			if fieldPath != "" {
				fieldPath += "."
			}
			fieldPath += required
			errors.AddWithCode(fieldPath, "Field is required", ErrCodeRequired, nil)
		}
	}

	// Validate each property
	for propName, propSchema := range schema.Properties {
		if propValue, exists := obj[propName]; exists {
			propPath := path
			if propPath != "" {
				propPath += "."
			}
			propPath += propName
			v.validateValue(propSchema, propValue, propPath, errors)
		}
	}

	// Property count validation
	if schema.MinProperties > 0 && len(obj) < schema.MinProperties {
		errors.Add(path, fmt.Sprintf("Object must have at least %d properties", schema.MinProperties), value)
	}
	if schema.MaxProperties > 0 && len(obj) > schema.MaxProperties {
		errors.Add(path, fmt.Sprintf("Object must have at most %d properties", schema.MaxProperties), value)
	}
}

// validateEnum validates enum values
func (v *SchemaValidator) validateEnum(schema *Schema, value interface{}, path string, errors *ValidationErrors) {
	found := false
	for _, enumVal := range schema.Enum {
		if fmt.Sprintf("%v", value) == fmt.Sprintf("%v", enumVal) {
			found = true
			break
		}
	}

	if !found {
		errors.AddWithCode(path, fmt.Sprintf("Value must be one of: %v", schema.Enum), ErrCodeEnum, value)
	}
}

// validateFormat validates string formats
func (v *SchemaValidator) validateFormat(format, value, path string, errors *ValidationErrors) {
	switch format {
	case "email":
		if !isValidEmail(value) {
			errors.AddWithCode(path, "Invalid email format", ErrCodeInvalidFormat, value)
		}
	case "uri", "url":
		if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
			errors.AddWithCode(path, "Invalid URL format", ErrCodeInvalidFormat, value)
		}
	case "uuid":
		if !isValidUUID(value) {
			errors.AddWithCode(path, "Invalid UUID format", ErrCodeInvalidFormat, value)
		}
		// Add more format validations as needed
	}
}

// WithValidation adds validation middleware to a route
func WithValidation(enabled bool) RouteOption {
	return &validationOpt{enabled: enabled}
}

type validationOpt struct {
	enabled bool
}

func (o *validationOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["validation.enabled"] = o.enabled
}

// WithStrictValidation enables strict validation (validates both request and response)
func WithStrictValidation() RouteOption {
	return &strictValidationOpt{}
}

type strictValidationOpt struct{}

func (o *strictValidationOpt) Apply(config *RouteConfig) {
	if config.Metadata == nil {
		config.Metadata = make(map[string]interface{})
	}
	config.Metadata["validation.enabled"] = true
	config.Metadata["validation.strict"] = true
}

// CreateValidationMiddleware creates a middleware that validates requests against schemas
func CreateValidationMiddleware(validator *SchemaValidator) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract validation metadata from context if available
			// For now, just pass through
			// Real implementation would extract schema from route metadata
			next.ServeHTTP(w, r)
		})
	}
}

// Helper functions

func getJSONType(value interface{}) string {
	if value == nil {
		return "null"
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "unknown"
	}
}

func toSlice(value interface{}) ([]interface{}, bool) {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil, false
	}

	result := make([]interface{}, v.Len())
	for i := 0; i < v.Len(); i++ {
		result[i] = v.Index(i).Interface()
	}
	return result, true
}

func toMap(value interface{}) (map[string]interface{}, bool) {
	// Try direct type assertion first
	if m, ok := value.(map[string]interface{}); ok {
		return m, true
	}

	// Try reflection for struct types
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() == reflect.Map {
		result := make(map[string]interface{})
		iter := v.MapRange()
		for iter.Next() {
			key := fmt.Sprintf("%v", iter.Key().Interface())
			result[key] = iter.Value().Interface()
		}
		return result, true
	}

	if v.Kind() == reflect.Struct {
		result := make(map[string]interface{})
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}

			// Get JSON tag name
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}

			name := field.Name
			if jsonTag != "" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] != "" {
					name = parts[0]
				}
			}

			result[name] = v.Field(i).Interface()
		}
		return result, true
	}

	return nil, false
}

func isValidEmail(email string) bool {
	// Basic email validation
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	matched, _ := regexp.MatchString(pattern, email)
	return matched
}

func isValidUUID(uuid string) bool {
	// UUID v4 pattern
	pattern := `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	matched, _ := regexp.MatchString(pattern, strings.ToLower(uuid))
	return matched
}

// ValidateRequestBody validates the request body against a schema
func ValidateRequestBody(r *http.Request, schema *Schema) error {
	if r.Body == nil {
		return NewValidationErrors()
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		verr := NewValidationErrors()
		verr.Add("body", "Invalid JSON", string(body))
		return verr
	}

	validator := NewSchemaValidator()
	return validator.ValidateRequest(schema, data)
}

// FormatValidationError formats validation errors for HTTP response
func FormatValidationError(err error) (int, []byte) {
	if verr, ok := err.(*ValidationErrors); ok {
		response := NewValidationErrorResponse(verr)
		data, _ := json.Marshal(response)
		return 422, data
	}

	// Generic error
	data, _ := json.Marshal(map[string]interface{}{
		"error": err.Error(),
		"code":  400,
	})
	return 400, data
}

// ValidateQueryParams validates query parameters against a struct schema
func ValidateQueryParams(r *http.Request, schemaType interface{}) error {
	// Extract query params
	queryParams := make(map[string]interface{})
	for key, values := range r.URL.Query() {
		if len(values) == 1 {
			queryParams[key] = values[0]
		} else {
			queryParams[key] = values
		}
	}

	// Generate schema from struct type
	rt := reflect.TypeOf(schemaType)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	validator := NewSchemaValidator()
	errors := NewValidationErrors()

	// Validate each field
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		queryTag := field.Tag.Get("query")
		if queryTag == "" || queryTag == "-" {
			continue
		}

		paramName, _ := parseTagWithOmitempty(queryTag)
		if paramName == "" {
			paramName = field.Name
		}

		// Check if required
		required := field.Tag.Get("required") == "true"
		value, exists := queryParams[paramName]

		if required && !exists {
			errors.AddWithCode(paramName, "Query parameter is required", ErrCodeRequired, nil)
			continue
		}

		if exists {
			// Validate the value
			// Convert string value to appropriate type based on field type
			convertedValue := convertQueryValue(value, field.Type)

			// Create a simple schema for validation
			schema := &Schema{
				Type: getSchemaTypeFromReflectType(field.Type),
			}

			// Apply validation tags
			if minStr := field.Tag.Get("minimum"); minStr != "" {
				if min, err := strconv.ParseFloat(minStr, 64); err == nil {
					schema.Minimum = min
				}
			}
			if maxStr := field.Tag.Get("maximum"); maxStr != "" {
				if max, err := strconv.ParseFloat(maxStr, 64); err == nil {
					schema.Maximum = max
				}
			}
			if enumStr := field.Tag.Get("enum"); enumStr != "" {
				enumValues := strings.Split(enumStr, ",")
				schema.Enum = make([]interface{}, len(enumValues))
				for i, v := range enumValues {
					schema.Enum[i] = strings.TrimSpace(v)
				}
			}

			validator.validateValue(schema, convertedValue, paramName, errors)
		}
	}

	if errors.HasErrors() {
		return errors
	}
	return nil
}

func convertQueryValue(value interface{}, targetType reflect.Type) interface{} {
	str, ok := value.(string)
	if !ok {
		return value
	}

	switch targetType.Kind() {
	case reflect.Int, reflect.Int64:
		if v, err := strconv.ParseInt(str, 10, 64); err == nil {
			return v
		}
	case reflect.Float64:
		if v, err := strconv.ParseFloat(str, 64); err == nil {
			return v
		}
	case reflect.Bool:
		if v, err := strconv.ParseBool(str); err == nil {
			return v
		}
	}

	return value
}

func getSchemaTypeFromReflectType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "string"
	}
}
