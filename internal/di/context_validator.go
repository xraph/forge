package di

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/shared"
)

// validateStruct validates struct fields using validation tags
func (c *Ctx) validateStruct(v any, rt reflect.Type, errors *shared.ValidationErrors) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get field name for error messages
		fieldName := getFieldName(field)

		// Validate the field
		c.validateField(field, fieldValue, fieldName, errors)
	}

	return nil
}

// validateField validates a single field based on its tags
func (c *Ctx) validateField(field reflect.StructField, fieldValue reflect.Value, fieldName string, errors *shared.ValidationErrors) {
	// Handle pointer fields
	if fieldValue.Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			// Check if required
			if field.Tag.Get("required") == "true" {
				errors.AddWithCode(fieldName, "field is required", shared.ErrCodeRequired, nil)
			}
			return
		}
		fieldValue = fieldValue.Elem()
	}

	// Get the actual value
	value := fieldValue.Interface()

	// String validation
	if fieldValue.Kind() == reflect.String {
		c.validateString(field, fieldValue.String(), fieldName, errors)
	}

	// Numeric validation
	if isNumericKind(fieldValue.Kind()) {
		c.validateNumeric(field, fieldValue, fieldName, errors)
	}

	// Required validation (for non-pointer, non-zero values)
	if field.Tag.Get("required") == "true" {
		if isZeroValue(fieldValue) {
			errors.AddWithCode(fieldName, "field is required", shared.ErrCodeRequired, value)
		}
	}

	// Enum validation
	if enumTag := field.Tag.Get("enum"); enumTag != "" {
		c.validateEnum(field, fieldValue, fieldName, enumTag, errors)
	}
}

// validateString validates string fields
func (c *Ctx) validateString(field reflect.StructField, value string, fieldName string, errors *shared.ValidationErrors) {
	// MinLength validation
	if minLengthStr := field.Tag.Get("minLength"); minLengthStr != "" {
		if minLength, err := strconv.Atoi(minLengthStr); err == nil {
			if len(value) < minLength {
				errors.AddWithCode(fieldName, fmt.Sprintf("must be at least %d characters", minLength), shared.ErrCodeMinLength, value)
			}
		}
	}

	// MaxLength validation
	if maxLengthStr := field.Tag.Get("maxLength"); maxLengthStr != "" {
		if maxLength, err := strconv.Atoi(maxLengthStr); err == nil {
			if len(value) > maxLength {
				errors.AddWithCode(fieldName, fmt.Sprintf("must be at most %d characters", maxLength), shared.ErrCodeMaxLength, value)
			}
		}
	}

	// Pattern validation
	if pattern := field.Tag.Get("pattern"); pattern != "" {
		matched, err := regexp.MatchString(pattern, value)
		if err == nil && !matched {
			errors.AddWithCode(fieldName, "does not match required pattern", shared.ErrCodePattern, value)
		}
	}

	// Format validation
	if format := field.Tag.Get("format"); format != "" {
		c.validateFormat(format, value, fieldName, errors)
	}
}

// validateNumeric validates numeric fields
func (c *Ctx) validateNumeric(field reflect.StructField, fieldValue reflect.Value, fieldName string, errors *shared.ValidationErrors) {
	var numValue float64

	switch fieldValue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		numValue = float64(fieldValue.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		numValue = float64(fieldValue.Uint())
	case reflect.Float32, reflect.Float64:
		numValue = fieldValue.Float()
	default:
		return
	}

	// Minimum validation
	if minStr := field.Tag.Get("minimum"); minStr != "" {
		if min, err := strconv.ParseFloat(minStr, 64); err == nil {
			if numValue < min {
				errors.AddWithCode(fieldName, fmt.Sprintf("must be at least %v", min), shared.ErrCodeMinValue, numValue)
			}
		}
	}

	// Maximum validation
	if maxStr := field.Tag.Get("maximum"); maxStr != "" {
		if max, err := strconv.ParseFloat(maxStr, 64); err == nil {
			if numValue > max {
				errors.AddWithCode(fieldName, fmt.Sprintf("must be at most %v", max), shared.ErrCodeMaxValue, numValue)
			}
		}
	}

	// MultipleOf validation
	if multipleOfStr := field.Tag.Get("multipleOf"); multipleOfStr != "" {
		if multipleOf, err := strconv.ParseFloat(multipleOfStr, 64); err == nil && multipleOf != 0 {
			if int(numValue)%int(multipleOf) != 0 {
				errors.AddWithCode(fieldName, fmt.Sprintf("must be a multiple of %v", multipleOf), shared.ErrCodeInvalidType, numValue)
			}
		}
	}
}

// validateEnum validates enum fields
func (c *Ctx) validateEnum(field reflect.StructField, fieldValue reflect.Value, fieldName string, enumTag string, errors *shared.ValidationErrors) {
	// Parse enum values
	enumValues := strings.Split(enumTag, ",")
	for i, v := range enumValues {
		enumValues[i] = strings.TrimSpace(v)
	}

	// Get string representation of value
	var strValue string
	switch fieldValue.Kind() {
	case reflect.String:
		strValue = fieldValue.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		strValue = strconv.FormatInt(fieldValue.Int(), 10)
	default:
		strValue = fmt.Sprintf("%v", fieldValue.Interface())
	}

	// Check if value is in enum
	found := false
	for _, enumVal := range enumValues {
		if strValue == enumVal {
			found = true
			break
		}
	}

	if !found {
		errors.AddWithCode(fieldName, fmt.Sprintf("must be one of: %s", strings.Join(enumValues, ", ")), shared.ErrCodeEnum, strValue)
	}
}

// validateFormat validates format constraints
func (c *Ctx) validateFormat(format string, value string, fieldName string, errors *shared.ValidationErrors) {
	switch format {
	case "email":
		if !isValidEmail(value) {
			errors.AddWithCode(fieldName, "must be a valid email address", shared.ErrCodeInvalidFormat, value)
		}
	case "uuid":
		if !isValidUUID(value) {
			errors.AddWithCode(fieldName, "must be a valid UUID", shared.ErrCodeInvalidFormat, value)
		}
	case "uri", "url":
		if !isValidURL(value) {
			errors.AddWithCode(fieldName, "must be a valid URL", shared.ErrCodeInvalidFormat, value)
		}
	case "date":
		// Simple date format validation (YYYY-MM-DD)
		datePattern := `^\d{4}-\d{2}-\d{2}$`
		if matched, _ := regexp.MatchString(datePattern, value); !matched {
			errors.AddWithCode(fieldName, "must be a valid date (YYYY-MM-DD)", shared.ErrCodeInvalidFormat, value)
		}
	case "date-time":
		// Basic ISO 8601 validation
		if !isValidISO8601(value) {
			errors.AddWithCode(fieldName, "must be a valid ISO 8601 date-time", shared.ErrCodeInvalidFormat, value)
		}
	}
}

// Helper functions

func getFieldName(field reflect.StructField) string {
	// Try to get name from path, query, or header tag
	if pathTag := field.Tag.Get("path"); pathTag != "" {
		return parseTagName(pathTag)
	}
	if queryTag := field.Tag.Get("query"); queryTag != "" {
		return parseTagName(queryTag)
	}
	if headerTag := field.Tag.Get("header"); headerTag != "" {
		return parseTagName(headerTag)
	}
	// Try json tag
	if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
		return parseTagName(jsonTag)
	}
	// Fallback to field name
	return field.Name
}

func isNumericKind(kind reflect.Kind) bool {
	return kind == reflect.Int || kind == reflect.Int8 || kind == reflect.Int16 ||
		kind == reflect.Int32 || kind == reflect.Int64 ||
		kind == reflect.Uint || kind == reflect.Uint8 || kind == reflect.Uint16 ||
		kind == reflect.Uint32 || kind == reflect.Uint64 ||
		kind == reflect.Float32 || kind == reflect.Float64
}

func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

func isValidEmail(email string) bool {
	// Simple email regex
	emailRegex := `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`
	matched, _ := regexp.MatchString(emailRegex, email)
	return matched
}

func isValidUUID(uuid string) bool {
	// UUID v4 regex
	uuidRegex := `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`
	matched, _ := regexp.MatchString(uuidRegex, uuid)
	return matched
}

func isValidURL(url string) bool {
	// Basic URL regex
	urlRegex := `^https?://[^\s/$.?#].[^\s]*$`
	matched, _ := regexp.MatchString(urlRegex, url)
	return matched
}

func isValidISO8601(datetime string) bool {
	// Basic ISO 8601 pattern
	iso8601Regex := `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})?$`
	matched, _ := regexp.MatchString(iso8601Regex, datetime)
	return matched
}
