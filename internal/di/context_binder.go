package di

import (
	"encoding"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/shared"
)

// BindRequest binds and validates request data from all sources (path, query, header, body).
// This method provides comprehensive request binding that:
//   - Binds path parameters from URL path segments (path:"name")
//   - Binds query parameters from URL query string (query:"name")
//   - Binds headers from HTTP headers (header:"name")
//   - Binds body fields from request body (json:"name" or body:"")
//   - Validates all fields using validation tags (required, minLength, etc.)
//
// Example:
//
//	type CreateUserRequest struct {
//	    TenantID string `path:"tenantId" description:"Tenant ID"`
//	    DryRun   bool   `query:"dryRun" default:"false"`
//	    APIKey   string `header:"X-API-Key" required:"true"`
//	    Name     string `json:"name" minLength:"1" maxLength:"100"`
//	}
//
//	func handler(ctx forge.Context) error {
//	    var req CreateUserRequest
//	    if err := ctx.BindRequest(&req); err != nil {
//	        return err // Returns ValidationErrors if validation fails
//	    }
//	    // All fields are now bound and validated
//	}
func (c *Ctx) BindRequest(v any) error {
	// Get reflection value
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("BindRequest requires non-nil pointer")
	}

	rv = rv.Elem()
	rt := rv.Type()

	if rt.Kind() != reflect.Struct {
		// Not a struct, just bind body using regular Bind
		return c.Bind(v)
	}

	// Track validation errors
	validationErrors := shared.NewValidationErrors()

	// Bind struct fields recursively (handles embedded structs)
	if err := c.bindStructFields(rv, rt, validationErrors); err != nil {
		return err
	}

	// Bind body fields (if any) - this handles json/body tagged fields
	if err := c.bindBodyFields(v, rt); err != nil {
		// Don't fail on body binding for GET requests without body
		if c.request.Method != http.MethodGet && c.request.Method != http.MethodHead && c.request.Method != http.MethodDelete {
			return fmt.Errorf("failed to bind body: %w", err)
		}
	}

	// Validate all fields using their validation tags
	if err := c.validateStruct(v, rt, validationErrors); err != nil {
		return err
	}

	// Return validation errors if any
	if validationErrors.HasErrors() {
		return validationErrors
	}

	return nil
}

// bindStructFields recursively binds struct fields, handling embedded structs.
func (c *Ctx) bindStructFields(rv reflect.Value, rt reflect.Type, errors *shared.ValidationErrors) error {
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Skip unexported fields
		if !field.IsExported() || !fieldValue.CanSet() {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			// Check if the embedded field has explicit tags (would mean it's not truly flattened)
			hasExplicitTag := field.Tag.Get("path") != "" ||
				field.Tag.Get("query") != "" ||
				field.Tag.Get("header") != ""

			if !hasExplicitTag {
				// Get the embedded struct type
				embeddedType := field.Type
				embeddedValue := fieldValue

				// Handle pointer to struct
				if embeddedType.Kind() == reflect.Ptr {
					embeddedType = embeddedType.Elem()
					if embeddedValue.IsNil() {
						embeddedValue.Set(reflect.New(embeddedType))
					}

					embeddedValue = embeddedValue.Elem()
				}

				// Only recurse if it's a struct
				if embeddedType.Kind() == reflect.Struct {
					if err := c.bindStructFields(embeddedValue, embeddedType, errors); err != nil {
						return err
					}

					continue
				}
			}
		}

		// Bind based on tag priority: path -> query -> header -> form -> body/json
		if err := c.bindField(field, fieldValue, errors); err != nil {
			return err
		}
	}

	return nil
}

// bindField binds a single struct field from the appropriate source.
func (c *Ctx) bindField(field reflect.StructField, fieldValue reflect.Value, errors *shared.ValidationErrors) error {
	// Check tags in priority order
	if pathTag := field.Tag.Get("path"); pathTag != "" {
		return c.bindPathParam(field, fieldValue, pathTag, errors)
	}

	if queryTag := field.Tag.Get("query"); queryTag != "" {
		return c.bindQueryParam(field, fieldValue, queryTag, errors)
	}

	if headerTag := field.Tag.Get("header"); headerTag != "" {
		return c.bindHeaderParam(field, fieldValue, headerTag, errors)
	}

	// Form and body fields are handled separately in bindBodyFields
	return nil
}

// bindPathParam binds a path parameter.
func (c *Ctx) bindPathParam(field reflect.StructField, fieldValue reflect.Value, tag string, errors *shared.ValidationErrors) error {
	paramName := parseTagName(tag)
	if paramName == "" {
		paramName = field.Name
	}

	value := c.Param(paramName)

	// Path params are always required
	if value == "" {
		errors.AddWithCode(paramName, "path parameter is required", shared.ErrCodeRequired, nil)

		return nil
	}

	return setFieldValue(fieldValue, value, paramName, errors)
}

// bindQueryParam binds a query parameter.
func (c *Ctx) bindQueryParam(field reflect.StructField, fieldValue reflect.Value, tag string, errors *shared.ValidationErrors) error {
	paramName := parseTagName(tag)
	if paramName == "" {
		paramName = field.Name
	}

	value := c.Query(paramName)

	// Determine if field is required using consistent precedence:
	// 1. optional:"true" - explicitly optional (highest priority)
	// 2. required:"true" - explicitly required
	// 3. omitempty in tag - optional
	// 4. pointer type - optional
	// 5. default: non-pointer types are required
	required := isBindFieldRequired(field, tag)

	if required && value == "" {
		errors.AddWithCode(paramName, "query parameter is required", shared.ErrCodeRequired, nil)

		return nil
	}

	// Use default if provided and value is empty
	if value == "" {
		if defaultVal := field.Tag.Get("default"); defaultVal != "" {
			value = defaultVal
		}
	}

	if value != "" {
		return setFieldValue(fieldValue, value, paramName, errors)
	}

	return nil
}

// bindHeaderParam binds a header parameter.
func (c *Ctx) bindHeaderParam(field reflect.StructField, fieldValue reflect.Value, tag string, errors *shared.ValidationErrors) error {
	headerName := parseTagName(tag)
	if headerName == "" {
		headerName = field.Name
	}

	value := c.Header(headerName)

	// Determine if field is required using consistent precedence:
	// 1. optional:"true" - explicitly optional (highest priority)
	// 2. required:"true" - explicitly required
	// 3. omitempty in tag - optional
	// 4. pointer type - optional
	// 5. default: non-pointer types are required
	required := isBindFieldRequired(field, tag)

	if required && value == "" {
		errors.AddWithCode(headerName, "header is required", shared.ErrCodeRequired, nil)

		return nil
	}

	// Use default if provided
	if value == "" {
		if defaultVal := field.Tag.Get("default"); defaultVal != "" {
			value = defaultVal
		}
	}

	if value != "" {
		return setFieldValue(fieldValue, value, headerName, errors)
	}

	return nil
}

// bindBodyFields binds body/json tagged fields.
func (c *Ctx) bindBodyFields(v any, rt reflect.Type) error {
	// Check if struct has body fields
	hasBodyFields := false

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Tag.Get("path") == "" &&
			field.Tag.Get("query") == "" &&
			field.Tag.Get("header") == "" {
			// Check if has json or body tag
			if field.Tag.Get("json") != "" && field.Tag.Get("json") != "-" {
				hasBodyFields = true

				break
			}

			if field.Tag.Get("body") != "" && field.Tag.Get("body") != "-" {
				hasBodyFields = true

				break
			}
		}
	}

	if !hasBodyFields {
		return nil
	}

	// Bind body content using existing Bind method
	return c.Bind(v)
}

// parseTagName extracts the parameter name from a tag value
// Handles formats like: "paramName", "paramName,omitempty".
func parseTagName(tag string) string {
	if idx := strings.Index(tag, ","); idx != -1 {
		return strings.TrimSpace(tag[:idx])
	}

	return strings.TrimSpace(tag)
}

// isBindFieldRequired determines if a field is required for binding.
// Uses consistent precedence order:
// 1. optional:"true" - explicitly optional (highest priority)
// 2. required:"true" - explicitly required
// 3. omitempty in tag - optional
// 4. pointer type - optional
// 5. default: non-pointer types are required.
func isBindFieldRequired(field reflect.StructField, tag string) bool {
	// 1. Explicit optional tag takes precedence (opt-out)
	if field.Tag.Get("optional") == "true" {
		return false
	}

	// 2. Explicit required tag
	if field.Tag.Get("required") == "true" {
		return true
	}

	// 3. Check for omitempty in the parameter tag (query, header, etc.)
	if strings.Contains(tag, ",omitempty") {
		return false
	}

	// 4. Check JSON tag for omitempty (for body fields)
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		if strings.Contains(jsonTag, ",omitempty") {
			return false
		}
	}

	// 5. Pointer types are optional by default
	if field.Type.Kind() == reflect.Ptr {
		return false
	}

	// 6. Non-pointer types without above markers are required
	return true
}

// setFieldValue sets a field value from a string, converting to the appropriate type.
// Supports types that implement encoding.TextUnmarshaler (e.g., xid.ID, uuid.UUID).
func setFieldValue(fieldValue reflect.Value, value string, fieldName string, errors *shared.ValidationErrors) error {
	// Handle pointer types first - create the value if nil, then recurse
	if fieldValue.Kind() == reflect.Ptr {
		if fieldValue.IsNil() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}

		return setFieldValue(fieldValue.Elem(), value, fieldName, errors)
	}

	// Check if the type implements encoding.TextUnmarshaler
	// This handles types like xid.ID, uuid.UUID, time.Time, etc.
	if handled := tryTextUnmarshaler(fieldValue, value, fieldName, errors); handled {
		return nil
	}

	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intVal, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			errors.AddWithCode(fieldName, "invalid integer value", shared.ErrCodeInvalidType, value)

			return nil
		}

		fieldValue.SetInt(intVal)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			errors.AddWithCode(fieldName, "invalid unsigned integer value", shared.ErrCodeInvalidType, value)

			return nil
		}

		fieldValue.SetUint(uintVal)

	case reflect.Float32, reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			errors.AddWithCode(fieldName, "invalid float value", shared.ErrCodeInvalidType, value)

			return nil
		}

		fieldValue.SetFloat(floatVal)

	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			errors.AddWithCode(fieldName, "invalid boolean value", shared.ErrCodeInvalidType, value)

			return nil
		}

		fieldValue.SetBool(boolVal)

	default:
		errors.AddWithCode(fieldName, fmt.Sprintf("unsupported field type: %s", fieldValue.Kind()), shared.ErrCodeInvalidType, value)
	}

	return nil
}

// tryTextUnmarshaler attempts to use encoding.TextUnmarshaler if the type implements it.
// Returns true if the type was handled (either successfully or with an error added).
func tryTextUnmarshaler(fieldValue reflect.Value, value string, fieldName string, errors *shared.ValidationErrors) bool {
	// Get the interface for the field value
	// We need to check both pointer and non-pointer receivers

	// First, try getting a pointer to the value (for pointer receiver implementations)
	if fieldValue.CanAddr() {
		ptrVal := fieldValue.Addr()
		if unmarshaler, ok := ptrVal.Interface().(encoding.TextUnmarshaler); ok {
			if err := unmarshaler.UnmarshalText([]byte(value)); err != nil {
				errors.AddWithCode(fieldName, fmt.Sprintf("invalid value: %v", err), shared.ErrCodeInvalidType, value)
			}

			return true
		}
	}

	// Try the value directly (for value receiver implementations, though rare)
	if fieldValue.CanInterface() {
		if unmarshaler, ok := fieldValue.Interface().(encoding.TextUnmarshaler); ok {
			if err := unmarshaler.UnmarshalText([]byte(value)); err != nil {
				errors.AddWithCode(fieldName, fmt.Sprintf("invalid value: %v", err), shared.ErrCodeInvalidType, value)
			}

			return true
		}
	}

	return false
}
