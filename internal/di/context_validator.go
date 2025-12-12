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

	return c.validateStructFields(rv, rt, errors)
}

// validateStructFields recursively validates struct fields, handling embedded structs
func (c *Ctx) validateStructFields(rv reflect.Value, rt reflect.Type, errors *shared.ValidationErrors) error {
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			// Check if the embedded field has explicit tags (would mean it's not truly flattened)
			hasExplicitTag := field.Tag.Get("path") != "" ||
				field.Tag.Get("query") != "" ||
				field.Tag.Get("header") != "" ||
				field.Tag.Get("json") != ""

			if !hasExplicitTag {
				// Get the embedded struct type
				embeddedType := field.Type
				embeddedValue := fieldValue

				// Handle pointer to struct
				if embeddedType.Kind() == reflect.Ptr {
					if embeddedValue.IsNil() {
						// Skip nil pointer to embedded struct
						continue
					}
					embeddedType = embeddedType.Elem()
					embeddedValue = embeddedValue.Elem()
				}

				// Only recurse if it's a struct
				if embeddedType.Kind() == reflect.Struct {
					if err := c.validateStructFields(embeddedValue, embeddedType, errors); err != nil {
						return err
					}
					continue
				}
			}
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
	// Determine if the field is required using proper precedence
	fieldRequired := isValidationFieldRequired(field)

	// Handle pointer fields
	if fieldValue.Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			// Check if required
			if fieldRequired {
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

	// Required validation (for non-pointer, zero values)
	// Only validate if the field is marked as required
	// Skip for query/header/path parameters - they're already validated during binding phase
	// where we can distinguish between missing and zero values (0, false, "")
	// Only applies to JSON body fields where unmarshaling handles this correctly
	if fieldRequired && !isParameterField(field) && isZeroValue(fieldValue) {
		errors.AddWithCode(fieldName, "field is required", shared.ErrCodeRequired, value)
	}

	// Enum validation - only validate non-empty values for optional fields
	if enumTag := field.Tag.Get("enum"); enumTag != "" {
		// Skip enum validation for optional empty values
		if !isZeroValue(fieldValue) || fieldRequired {
			c.validateEnum(field, fieldValue, fieldName, enumTag, errors)
		}
	}
}

// validateString validates string fields
func (c *Ctx) validateString(field reflect.StructField, value string, fieldName string, errors *shared.ValidationErrors) {
	// Check if field is optional and empty - skip validation for optional empty fields
	// except for maxLength which should still apply
	isOptional := !isValidationFieldRequired(field)
	isEmpty := value == ""

	// MinLength validation - skip for optional empty fields
	if minLengthStr := field.Tag.Get("minLength"); minLengthStr != "" {
		if minLength, err := strconv.Atoi(minLengthStr); err == nil {
			// Only validate minLength if field is required OR has a non-empty value
			if !isOptional || !isEmpty {
				if len(value) < minLength {
					errors.AddWithCode(fieldName, fmt.Sprintf("must be at least %d characters", minLength), shared.ErrCodeMinLength, value)
				}
			}
		}
	}

	// MaxLength validation - always apply (even empty strings pass maxLength)
	if maxLengthStr := field.Tag.Get("maxLength"); maxLengthStr != "" {
		if maxLength, err := strconv.Atoi(maxLengthStr); err == nil {
			if len(value) > maxLength {
				errors.AddWithCode(fieldName, fmt.Sprintf("must be at most %d characters", maxLength), shared.ErrCodeMaxLength, value)
			}
		}
	}

	// Pattern validation - skip for optional empty fields
	if pattern := field.Tag.Get("pattern"); pattern != "" {
		if !isOptional || !isEmpty {
			matched, err := regexp.MatchString(pattern, value)
			if err == nil && !matched {
				errors.AddWithCode(fieldName, "does not match required pattern", shared.ErrCodePattern, value)
			}
		}
	}

	// Format validation - skip for optional empty fields
	if format := field.Tag.Get("format"); format != "" {
		if !isOptional || !isEmpty {
			c.validateFormat(format, value, fieldName, errors)
		}
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

	// Check if field is optional and has zero value - skip range validation
	// for optional zero-value fields (they pass by being "not provided")
	isOptional := !isValidationFieldRequired(field)
	isZero := numValue == 0

	// Minimum validation - skip for optional zero-value fields unless minimum is explicitly 0
	if minStr := field.Tag.Get("minimum"); minStr != "" {
		if min, err := strconv.ParseFloat(minStr, 64); err == nil {
			// For optional fields with zero value, only validate if min > 0
			// (zero is a valid "not provided" state for optional numeric fields)
			if !isOptional || !isZero || min == 0 {
				if numValue < min {
					errors.AddWithCode(fieldName, fmt.Sprintf("must be at least %v", min), shared.ErrCodeMinValue, numValue)
				}
			}
		}
	}

	// Maximum validation - always apply (zero values always pass maximum checks)
	if maxStr := field.Tag.Get("maximum"); maxStr != "" {
		if max, err := strconv.ParseFloat(maxStr, 64); err == nil {
			if numValue > max {
				errors.AddWithCode(fieldName, fmt.Sprintf("must be at most %v", max), shared.ErrCodeMaxValue, numValue)
			}
		}
	}

	// MultipleOf validation - skip for optional zero-value fields
	if multipleOfStr := field.Tag.Get("multipleOf"); multipleOfStr != "" {
		if multipleOf, err := strconv.ParseFloat(multipleOfStr, 64); err == nil && multipleOf != 0 {
			if !isOptional || !isZero {
				if int(numValue)%int(multipleOf) != 0 {
					errors.AddWithCode(fieldName, fmt.Sprintf("must be a multiple of %v", multipleOf), shared.ErrCodeInvalidType, numValue)
				}
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

// isValidationFieldRequired determines if a field is required for validation.
// Uses consistent precedence order:
// 1. optional:"true" - explicitly optional (highest priority)
// 2. required:"true" - explicitly required
// 3. omitempty in json/query/header tags - optional
// 4. pointer type - optional
// 5. default: non-pointer types are required
func isValidationFieldRequired(field reflect.StructField) bool {
	// 1. Explicit optional tag takes precedence (opt-out)
	if field.Tag.Get("optional") == "true" {
		return false
	}

	// 2. Explicit required tag
	if field.Tag.Get("required") == "true" {
		return true
	}

	// 3. Check for omitempty in various tags
	// Check JSON tag
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		if strings.Contains(jsonTag, ",omitempty") {
			return false
		}
	}

	// Check query tag
	if queryTag := field.Tag.Get("query"); queryTag != "" {
		if strings.Contains(queryTag, ",omitempty") {
			return false
		}
	}

	// Check header tag
	if headerTag := field.Tag.Get("header"); headerTag != "" {
		if strings.Contains(headerTag, ",omitempty") {
			return false
		}
	}

	// Check body tag
	if bodyTag := field.Tag.Get("body"); bodyTag != "" {
		if strings.Contains(bodyTag, ",omitempty") {
			return false
		}
	}

	// 4. Pointer types are optional by default
	if field.Type.Kind() == reflect.Ptr {
		return false
	}

	// 5. Non-pointer types without above markers are required
	return true
}

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

// isParameterField checks if a field is bound from query, header, or path parameters.
// These parameter types are validated during the binding phase where we can distinguish
// between missing values and explicit zero values (0, false, "").
func isParameterField(field reflect.StructField) bool {
	return field.Tag.Get("query") != "" ||
		field.Tag.Get("header") != "" ||
		field.Tag.Get("path") != ""
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
