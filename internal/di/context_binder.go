package di

import (
	"fmt"
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
		return fmt.Errorf("BindRequest requires non-nil pointer")
	}

	rv = rv.Elem()
	rt := rv.Type()

	if rt.Kind() != reflect.Struct {
		// Not a struct, just bind body using regular Bind
		return c.Bind(v)
	}

	// Track validation errors
	validationErrors := shared.NewValidationErrors()

	// Iterate through struct fields and bind from path/query/header
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldValue := rv.Field(i)

		// Skip unexported fields
		if !field.IsExported() || !fieldValue.CanSet() {
			continue
		}

		// Bind based on tag priority: path -> query -> header -> form -> body/json
		if err := c.bindField(field, fieldValue, validationErrors); err != nil {
			return err
		}
	}

	// Bind body fields (if any) - this handles json/body tagged fields
	if err := c.bindBodyFields(v, rt); err != nil {
		// Don't fail on body binding for GET requests without body
		if c.request.Method != "GET" && c.request.Method != "HEAD" && c.request.Method != "DELETE" {
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

// bindField binds a single struct field from the appropriate source
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

// bindPathParam binds a path parameter
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

// bindQueryParam binds a query parameter
func (c *Ctx) bindQueryParam(field reflect.StructField, fieldValue reflect.Value, tag string, errors *shared.ValidationErrors) error {
	paramName := parseTagName(tag)
	if paramName == "" {
		paramName = field.Name
	}

	value := c.Query(paramName)

	// Check if required
	required := field.Tag.Get("required") == "true"
	omitempty := strings.Contains(tag, ",omitempty")

	// Determine if field is required (required tag OR non-pointer non-omitempty)
	if !omitempty && field.Type.Kind() != reflect.Ptr && !required {
		required = true
	}

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

// bindHeaderParam binds a header parameter
func (c *Ctx) bindHeaderParam(field reflect.StructField, fieldValue reflect.Value, tag string, errors *shared.ValidationErrors) error {
	headerName := parseTagName(tag)
	if headerName == "" {
		headerName = field.Name
	}

	value := c.Header(headerName)

	// Check if required
	required := field.Tag.Get("required") == "true"
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

// bindBodyFields binds body/json tagged fields
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
// Handles formats like: "paramName", "paramName,omitempty"
func parseTagName(tag string) string {
	if idx := strings.Index(tag, ","); idx != -1 {
		return strings.TrimSpace(tag[:idx])
	}
	return strings.TrimSpace(tag)
}

// setFieldValue sets a field value from a string, converting to the appropriate type
func setFieldValue(fieldValue reflect.Value, value string, fieldName string, errors *shared.ValidationErrors) error {
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

	case reflect.Ptr:
		// Handle pointer types
		if fieldValue.IsNil() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}
		return setFieldValue(fieldValue.Elem(), value, fieldName, errors)

	default:
		errors.AddWithCode(fieldName, fmt.Sprintf("unsupported field type: %s", fieldValue.Kind()), shared.ErrCodeInvalidType, value)
	}

	return nil
}
