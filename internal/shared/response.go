package shared

import (
	"fmt"
	"reflect"
	"strings"
)

// sensitiveCleaningKey is a private type for context keys to avoid collisions.
type sensitiveCleaningKey struct{}

// ContextKeyForSensitiveCleaning is the key used in request context for sensitive field cleaning.
// This is exported so it can be used by both the router and context packages.
var ContextKeyForSensitiveCleaning = sensitiveCleaningKey{}

// SensitiveMode specifies how sensitive fields should be cleaned.
type SensitiveMode int

const (
	// SensitiveModeZero sets sensitive fields to their zero value.
	SensitiveModeZero SensitiveMode = iota
	// SensitiveModeRedact replaces sensitive fields with "[REDACTED]".
	SensitiveModeRedact
	// SensitiveModeMask replaces sensitive fields with a custom mask.
	SensitiveModeMask
)

// SensitiveFieldConfig holds configuration for a sensitive field.
type SensitiveFieldConfig struct {
	Mode SensitiveMode
	Mask string // Custom mask for SensitiveModeMask
}

// ParseSensitiveTag parses the sensitive tag value and returns the configuration.
// Supported formats:
//   - sensitive:"true"       -> zero value
//   - sensitive:"redact"     -> "[REDACTED]"
//   - sensitive:"mask:***"   -> custom mask "***"
func ParseSensitiveTag(tagValue string) *SensitiveFieldConfig {
	if tagValue == "" {
		return nil
	}

	tagValue = strings.TrimSpace(tagValue)

	switch {
	case tagValue == "true" || tagValue == "1":
		return &SensitiveFieldConfig{Mode: SensitiveModeZero}
	case tagValue == "redact":
		return &SensitiveFieldConfig{Mode: SensitiveModeRedact}
	case strings.HasPrefix(tagValue, "mask:"):
		mask := strings.TrimPrefix(tagValue, "mask:")
		return &SensitiveFieldConfig{Mode: SensitiveModeMask, Mask: mask}
	default:
		// Default to zero mode for any truthy value
		return &SensitiveFieldConfig{Mode: SensitiveModeZero}
	}
}

// CleanSensitiveFields creates a cleaned copy of the value with sensitive fields processed.
// It handles nested structs, slices, arrays, and maps recursively.
func CleanSensitiveFields(v any) any {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	cleaned := cleanSensitiveValue(rv)

	return cleaned.Interface()
}

// cleanSensitiveValue recursively cleans sensitive fields from a reflect.Value.
func cleanSensitiveValue(rv reflect.Value) reflect.Value {
	// Handle pointers
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return rv
		}
		// Create a new pointer and clean the element
		cleaned := cleanSensitiveValue(rv.Elem())
		result := reflect.New(rv.Elem().Type())
		result.Elem().Set(cleaned)

		return result
	}

	// Handle interfaces
	if rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return rv
		}

		return cleanSensitiveValue(rv.Elem())
	}

	switch rv.Kind() {
	case reflect.Struct:
		return cleanSensitiveStruct(rv)
	case reflect.Slice:
		return cleanSensitiveSlice(rv)
	case reflect.Array:
		return cleanSensitiveArray(rv)
	case reflect.Map:
		return cleanSensitiveMap(rv)
	default:
		return rv
	}
}

// cleanSensitiveStruct creates a cleaned copy of a struct with sensitive fields processed.
func cleanSensitiveStruct(rv reflect.Value) reflect.Value {
	rt := rv.Type()
	result := reflect.New(rt).Elem()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fieldVal := rv.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check for sensitive tag
		sensitiveTag := field.Tag.Get("sensitive")
		config := ParseSensitiveTag(sensitiveTag)

		if config != nil {
			// Apply sensitive field cleaning
			cleanedVal := applySensitiveCleaning(fieldVal, field.Type, config)
			result.Field(i).Set(cleanedVal)
		} else {
			// Recursively clean nested values
			cleanedVal := cleanSensitiveValue(fieldVal)
			result.Field(i).Set(cleanedVal)
		}
	}

	return result
}

// cleanSensitiveSlice creates a cleaned copy of a slice with sensitive fields processed.
func cleanSensitiveSlice(rv reflect.Value) reflect.Value {
	if rv.IsNil() {
		return rv
	}

	result := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Cap())

	for i := 0; i < rv.Len(); i++ {
		cleaned := cleanSensitiveValue(rv.Index(i))
		result.Index(i).Set(cleaned)
	}

	return result
}

// cleanSensitiveArray creates a cleaned copy of an array with sensitive fields processed.
func cleanSensitiveArray(rv reflect.Value) reflect.Value {
	result := reflect.New(rv.Type()).Elem()

	for i := 0; i < rv.Len(); i++ {
		cleaned := cleanSensitiveValue(rv.Index(i))
		result.Index(i).Set(cleaned)
	}

	return result
}

// cleanSensitiveMap creates a cleaned copy of a map with sensitive fields processed.
func cleanSensitiveMap(rv reflect.Value) reflect.Value {
	if rv.IsNil() {
		return rv
	}

	result := reflect.MakeMap(rv.Type())

	iter := rv.MapRange()
	for iter.Next() {
		key := iter.Key()
		val := iter.Value()
		cleanedVal := cleanSensitiveValue(val)
		result.SetMapIndex(key, cleanedVal)
	}

	return result
}

// applySensitiveCleaning applies the appropriate cleaning based on the config.
func applySensitiveCleaning(fieldVal reflect.Value, fieldType reflect.Type, config *SensitiveFieldConfig) reflect.Value {
	switch config.Mode {
	case SensitiveModeZero:
		return reflect.Zero(fieldType)

	case SensitiveModeRedact:
		return getRedactedValue(fieldType)

	case SensitiveModeMask:
		return getMaskedValue(fieldType, config.Mask)

	default:
		return reflect.Zero(fieldType)
	}
}

// getRedactedValue returns the "[REDACTED]" value for supported types.
func getRedactedValue(fieldType reflect.Type) reflect.Value {
	// Handle pointers
	if fieldType.Kind() == reflect.Ptr {
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.String {
			s := "[REDACTED]"
			result := reflect.New(elemType)
			result.Elem().SetString(s)

			return result
		}
		// For non-string pointers, return nil
		return reflect.Zero(fieldType)
	}

	// Handle direct string type
	if fieldType.Kind() == reflect.String {
		return reflect.ValueOf("[REDACTED]")
	}

	// For non-string types, return zero value
	return reflect.Zero(fieldType)
}

// getMaskedValue returns the custom masked value for supported types.
func getMaskedValue(fieldType reflect.Type, mask string) reflect.Value {
	// Handle pointers
	if fieldType.Kind() == reflect.Ptr {
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.String {
			result := reflect.New(elemType)
			result.Elem().SetString(mask)

			return result
		}
		// For non-string pointers, return nil
		return reflect.Zero(fieldType)
	}

	// Handle direct string type
	if fieldType.Kind() == reflect.String {
		return reflect.ValueOf(mask)
	}

	// For non-string types, return zero value
	return reflect.Zero(fieldType)
}

// ResponseProcessor handles response struct processing.
// It extracts headers and unwraps body fields based on struct tags.
type ResponseProcessor struct {
	// HeaderSetter is called for each header:"..." tagged field with non-zero value.
	HeaderSetter func(name, value string)
	// CleanSensitive when true, processes sensitive fields.
	CleanSensitive bool
}

// ProcessResponse handles response struct tags:
// - Calls HeaderSetter for header:"..." fields with non-zero values
// - Returns the unwrapped body if a body:"" tag is found
// - Cleans sensitive fields if CleanSensitive is true
// - Falls back to original value if no special tags found.
func (p *ResponseProcessor) ProcessResponse(v any) any {
	if v == nil {
		return nil
	}

	// Clean sensitive fields first if enabled
	if p.CleanSensitive {
		v = CleanSensitiveFields(v)
	}

	rv := reflect.ValueOf(v)

	// Handle pointer
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return v
	}

	rt := rv.Type()
	var bodyValue any
	hasBodyUnwrap := false

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		// Set headers via callback
		if headerName := field.Tag.Get("header"); headerName != "" && headerName != "-" {
			fieldVal := rv.Field(i)
			if !fieldVal.IsZero() && p.HeaderSetter != nil {
				p.HeaderSetter(headerName, fmt.Sprint(fieldVal.Interface()))
			}
		}

		// Check for body:"" unwrap marker
		if bodyTag, hasTag := field.Tag.Lookup("body"); hasTag && bodyTag == "" {
			bodyValue = rv.Field(i).Interface()
			hasBodyUnwrap = true
		}
	}

	if hasBodyUnwrap {
		return bodyValue
	}

	return v
}

// ProcessResponseValue is a convenience function that processes a response value
// with the given header setter callback.
func ProcessResponseValue(v any, headerSetter func(name, value string)) any {
	processor := &ResponseProcessor{
		HeaderSetter: headerSetter,
	}
	return processor.ProcessResponse(v)
}

// ProcessResponseValueWithSensitive is a convenience function that processes a response value
// with the given header setter callback and sensitive field cleaning.
func ProcessResponseValueWithSensitive(v any, headerSetter func(name, value string), cleanSensitive bool) any {
	processor := &ResponseProcessor{
		HeaderSetter:   headerSetter,
		CleanSensitive: cleanSensitive,
	}
	return processor.ProcessResponse(v)
}
