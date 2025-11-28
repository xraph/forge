package shared

import (
	"fmt"
	"reflect"
)

// ResponseProcessor handles response struct processing.
// It extracts headers and unwraps body fields based on struct tags.
type ResponseProcessor struct {
	// HeaderSetter is called for each header:"..." tagged field with non-zero value.
	HeaderSetter func(name, value string)
}

// ProcessResponse handles response struct tags:
// - Calls HeaderSetter for header:"..." fields with non-zero values
// - Returns the unwrapped body if a body:"" tag is found
// - Falls back to original value if no special tags found.
func (p *ResponseProcessor) ProcessResponse(v any) any {
	if v == nil {
		return nil
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
