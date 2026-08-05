package router

import (
	"reflect"
	"slices"
	"strings"
)

// RequestComponents holds all components of a request extracted from a unified schema.
type RequestComponents struct {
	PathParams   []Parameter
	QueryParams  []Parameter
	HeaderParams []Parameter
	BodySchema   *Schema
	HasBody      bool
	IsMultipart  bool // true if the schema contains file uploads or form:"" tags
}

// extractUnifiedRequestComponents extracts all request components from a unified struct
// Fields are classified based on struct tags in priority order:
// 1. path:"name" - Path parameter
// 2. query:"name" - Query parameter
// 3. header:"name" - Header parameter
// 4. body:"" or json:"name" - Body field.
func extractUnifiedRequestComponents(schemaGen *schemaGenerator, schemaType any) (*RequestComponents, error) {
	components := &RequestComponents{
		PathParams:   []Parameter{},
		QueryParams:  []Parameter{},
		HeaderParams: []Parameter{},
	}

	rt := reflect.TypeOf(schemaType)
	if rt == nil {
		return components, nil
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		// Not a struct, treat as body schema
		schema, err := schemaGen.GenerateSchema(schemaType)
		if err != nil {
			return nil, err
		}

		components.BodySchema = schema
		components.HasBody = true

		return components, nil
	}

	// Classify fields by tags
	bodyProperties := make(map[string]*Schema)

	var bodyRequired []string

	for i := range rt.NumField() {
		field := rt.Field(i)

		// Skip unexported fields, but descend into anonymous ones so an embedded
		// lowercase-named struct still promotes its exported fields.
		if skipStructField(field) {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			// Check if any tag is explicitly set (would mean it's not truly flattened)
			hasExplicitTag := field.Tag.Get("path") != "" ||
				field.Tag.Get("query") != "" ||
				field.Tag.Get("header") != "" ||
				field.Tag.Get("form") != "" ||
				field.Tag.Get("body") != "" ||
				field.Tag.Get("json") != ""

			if !hasExplicitTag {
				// Recursively extract components from embedded struct
				embeddedComponents, err := extractEmbeddedComponents(schemaGen, field, bodyProperties, &bodyRequired)
				if err != nil {
					return nil, err
				}

				components.PathParams = append(components.PathParams, embeddedComponents.PathParams...)
				components.QueryParams = append(components.QueryParams, embeddedComponents.QueryParams...)

				components.HeaderParams = append(components.HeaderParams, embeddedComponents.HeaderParams...)
				if embeddedComponents.HasBody {
					components.HasBody = true
				}

				if embeddedComponents.IsMultipart {
					components.IsMultipart = true
				}

				continue
			}
		}

		// Check tags in priority order
		if pathTag := field.Tag.Get("path"); pathTag != "" {
			// Path parameter
			param := generatePathParamFromField(schemaGen, field, pathTag)
			components.PathParams = append(components.PathParams, param)
		} else if queryTag := field.Tag.Get("query"); queryTag != "" {
			// Query parameter
			param := generateQueryParamFromField(schemaGen, field, queryTag)
			components.QueryParams = append(components.QueryParams, param)
		} else if headerTag := field.Tag.Get("header"); headerTag != "" {
			// Header parameter
			param := generateHeaderParamFromField(schemaGen, field, headerTag)
			components.HeaderParams = append(components.HeaderParams, param)
		} else if formTag := field.Tag.Get("form"); formTag != "" {
			// Multipart form field - used for multipart/form-data requests
			components.HasBody = true
			components.IsMultipart = true

			// Get field name for form
			formName := field.Name

			name, _ := parseTagWithOmitempty(formTag)
			if name != "" && name != "-" {
				formName = name
			}

			// Generate field schema
			fieldSchema, err := schemaGen.generateSchemaFromType(field.Type)
			if err != nil {
				return nil, err // Return error on collision
			}

			schemaGen.applyStructTags(fieldSchema, field)
			bodyProperties[formName] = fieldSchema

			// Check if required
			if isFieldRequired(field) {
				bodyRequired = append(bodyRequired, formName)
			}
		} else if bodyTag := field.Tag.Get("body"); bodyTag != "" || field.Tag.Get("json") != "" {
			// Body field - explicit body tag or json tag
			components.HasBody = true

			// Get field name for body
			jsonName := field.Name
			if jsonTag := field.Tag.Get("json"); jsonTag != "" {
				name, _ := parseJSONTag(jsonTag)
				if name != "" && name != "-" {
					jsonName = name
				}
			} else if bodyTag != "" && bodyTag != "-" {
				jsonName = bodyTag
			}

			// Generate field schema
			fieldSchema, err := schemaGen.generateSchemaFromType(field.Type)
			if err != nil {
				return nil, err // Return error on collision
			}

			schemaGen.applyStructTags(fieldSchema, field)

			// Check if field is a file (binary format)
			if fieldSchema.Format == "binary" {
				components.IsMultipart = true
			}

			bodyProperties[jsonName] = fieldSchema

			// Check if required
			if isFieldRequired(field) {
				bodyRequired = append(bodyRequired, jsonName)
			}
		}
	}

	// Create body schema if we have body fields
	if components.HasBody && len(bodyProperties) > 0 {
		components.BodySchema = &Schema{
			Type:       "object",
			Properties: bodyProperties,
		}
		if len(bodyRequired) > 0 {
			components.BodySchema.Required = bodyRequired
		}
	}

	return components, nil
}

// extractEmbeddedComponents recursively extracts request components from an embedded struct field.
func extractEmbeddedComponents(schemaGen *schemaGenerator, field reflect.StructField, bodyProperties map[string]*Schema, bodyRequired *[]string) (*RequestComponents, error) {
	components := &RequestComponents{
		PathParams:   []Parameter{},
		QueryParams:  []Parameter{},
		HeaderParams: []Parameter{},
	}

	fieldType := field.Type

	// Handle pointer types
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// If it's not a struct, return empty
	if fieldType.Kind() != reflect.Struct {
		return components, nil
	}

	// Recursively process embedded struct fields
	for i := range fieldType.NumField() {
		embeddedField := fieldType.Field(i)

		// Skip unexported fields, but descend into anonymous ones so an embedded
		// lowercase-named struct still promotes its exported fields.
		if skipStructField(embeddedField) {
			continue
		}

		// Handle nested embedded structs recursively
		if embeddedField.Anonymous {
			hasExplicitTag := embeddedField.Tag.Get("path") != "" ||
				embeddedField.Tag.Get("query") != "" ||
				embeddedField.Tag.Get("header") != "" ||
				embeddedField.Tag.Get("form") != "" ||
				embeddedField.Tag.Get("body") != "" ||
				embeddedField.Tag.Get("json") != ""

			if !hasExplicitTag {
				nestedComponents, err := extractEmbeddedComponents(schemaGen, embeddedField, bodyProperties, bodyRequired)
				if err != nil {
					return nil, err
				}

				components.PathParams = append(components.PathParams, nestedComponents.PathParams...)
				components.QueryParams = append(components.QueryParams, nestedComponents.QueryParams...)

				components.HeaderParams = append(components.HeaderParams, nestedComponents.HeaderParams...)
				if nestedComponents.HasBody {
					components.HasBody = true
				}

				if nestedComponents.IsMultipart {
					components.IsMultipart = true
				}

				continue
			}
		}

		// Check tags in priority order
		if pathTag := embeddedField.Tag.Get("path"); pathTag != "" {
			param := generatePathParamFromField(schemaGen, embeddedField, pathTag)
			components.PathParams = append(components.PathParams, param)
		} else if queryTag := embeddedField.Tag.Get("query"); queryTag != "" {
			param := generateQueryParamFromField(schemaGen, embeddedField, queryTag)
			components.QueryParams = append(components.QueryParams, param)
		} else if headerTag := embeddedField.Tag.Get("header"); headerTag != "" {
			param := generateHeaderParamFromField(schemaGen, embeddedField, headerTag)
			components.HeaderParams = append(components.HeaderParams, param)
		} else if formTag := embeddedField.Tag.Get("form"); formTag != "" {
			// Multipart form field
			components.HasBody = true
			components.IsMultipart = true

			formName := embeddedField.Name

			name, _ := parseTagWithOmitempty(formTag)
			if name != "" && name != "-" {
				formName = name
			}

			fieldSchema, err := schemaGen.generateSchemaFromType(embeddedField.Type)
			if err != nil {
				return nil, err
			}

			schemaGen.applyStructTags(fieldSchema, embeddedField)
			bodyProperties[formName] = fieldSchema

			if isFieldRequired(embeddedField) {
				*bodyRequired = append(*bodyRequired, formName)
			}
		} else if bodyTag := embeddedField.Tag.Get("body"); bodyTag != "" || embeddedField.Tag.Get("json") != "" {
			// Body field
			components.HasBody = true

			jsonName := embeddedField.Name
			if jsonTag := embeddedField.Tag.Get("json"); jsonTag != "" {
				name, _ := parseJSONTag(jsonTag)
				if name != "" && name != "-" {
					jsonName = name
				}
			} else if bodyTag != "" && bodyTag != "-" {
				jsonName = bodyTag
			}

			fieldSchema, err := schemaGen.generateSchemaFromType(embeddedField.Type)
			if err != nil {
				return nil, err
			}

			schemaGen.applyStructTags(fieldSchema, embeddedField)

			if fieldSchema.Format == "binary" {
				components.IsMultipart = true
			}

			bodyProperties[jsonName] = fieldSchema

			if isFieldRequired(embeddedField) {
				*bodyRequired = append(*bodyRequired, jsonName)
			}
		}
	}

	return components, nil
}

// generatePathParamFromField generates a path parameter from a struct field.
func generatePathParamFromField(schemaGen *schemaGenerator, field reflect.StructField, tagValue string) Parameter {
	return paramFromFieldOrFallback(schemaGen, field, "path", tagValue)
}

// generateQueryParamFromField generates a query parameter from a struct field.
func generateQueryParamFromField(schemaGen *schemaGenerator, field reflect.StructField, tagValue string) Parameter {
	return paramFromFieldOrFallback(schemaGen, field, "query", tagValue)
}

// generateHeaderParamFromField generates a header parameter from a struct field.
func generateHeaderParamFromField(schemaGen *schemaGenerator, field reflect.StructField, tagValue string) Parameter {
	return paramFromFieldOrFallback(schemaGen, field, "header", tagValue)
}

// paramFromFieldOrFallback resolves a parameter through paramFromField, the
// shared derivation the struct-walking helpers in openapi_params.go also use,
// and degrades to a bare string parameter when schema resolution fails (which
// it does only on a component-name collision).
//
// The unified request path cannot drop a parameter the way the struct-walking
// helpers do -- a path parameter silently missing from the document would not
// match the URL template -- so it substitutes rather than skips.
func paramFromFieldOrFallback(schemaGen *schemaGenerator, field reflect.StructField, in, tagValue string) Parameter {
	param, err := paramFromField(schemaGen, field, in, tagValue)
	if err == nil {
		return param
	}

	paramName, _ := parseTagWithOmitempty(tagValue)
	if paramName == "" {
		paramName = field.Name
	}

	return Parameter{
		Name:     paramName,
		In:       in,
		Required: true,
		Schema:   &Schema{Type: "string"},
	}
}

// isFieldRequired determines if a body field is required.
func isFieldRequired(field reflect.StructField) bool {
	// Explicit optional tag takes precedence (opt-out)
	if field.Tag.Get("optional") == "true" {
		return false
	}

	// Explicit required tag
	if field.Tag.Get("required") == "true" {
		return true
	}

	// Fields with default values are implicitly optional
	if field.Tag.Get("default") != "" {
		return false
	}

	// Check JSON tag for omitempty
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		_, omitempty := parseJSONTag(jsonTag)
		if omitempty {
			return false
		}
	}

	// Check body tag for omitempty
	if bodyTag := field.Tag.Get("body"); bodyTag != "" {
		if slices.Contains(strings.Split(bodyTag, ","), "omitempty") {
			return false
		}
	}

	// Non-pointer types without omitempty are required
	if field.Type.Kind() != reflect.Ptr {
		return true
	}

	return false
}

// hasUnifiedTags checks if a struct has path, query, header, or form tags
// This is used to determine if we should use unified extraction or legacy behavior.
//
// It descends into embedded structs, because extractUnifiedRequestComponents
// does: that function's field.Anonymous branch recurses and lifts an embedded
// base's query and header tags into parameters. A gate that looked only at
// top-level fields therefore disagreed with the extractor it gates, and sent a
// struct whose pagination tags lived on an embedded base down the legacy path --
// where the tags mean nothing and the fields land in the request body under
// their Go names instead.
func hasUnifiedTags(schemaType any) bool {
	rt := reflect.TypeOf(schemaType)
	if rt == nil {
		return false
	}

	return typeHasUnifiedTags(rt, 0)
}

// maxUnifiedTagDepth bounds the embedded-struct descent. Go forbids a struct
// from embedding itself, so the chain is finite, but it can be reached through
// pointers and this keeps a pathological or cyclic-through-pointer type from
// spinning.
const maxUnifiedTagDepth = 10

func typeHasUnifiedTags(rt reflect.Type, depth int) bool {
	if depth > maxUnifiedTagDepth {
		return false
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return false
	}

	for i := range rt.NumField() {
		field := rt.Field(i)

		// Check for unified tags
		if field.Tag.Get("path") != "" ||
			field.Tag.Get("query") != "" ||
			field.Tag.Get("header") != "" ||
			field.Tag.Get("form") != "" {
			return true
		}

		// An embedded field with none of those tags is flattened by the
		// extractor, so its own fields count as this struct's. Mirrors the
		// hasExplicitTag test in extractUnifiedRequestComponents: an embedded
		// field that names a tag is a field in its own right, not a base to
		// flatten, and is already covered by the check above.
		if field.Anonymous && field.Tag.Get("body") == "" && field.Tag.Get("json") == "" {
			if typeHasUnifiedTags(field.Type, depth+1) {
				return true
			}
		}
	}

	return false
}

// inferRequestComponents infers what parts of the request a struct represents
// Used for backward compatibility with existing code.
func inferRequestComponents(schemaType any) string {
	rt := reflect.TypeOf(schemaType)
	if rt == nil {
		return "body"
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return "body"
	}

	hasQuery := false
	hasHeader := false
	hasBody := false

	for i := range rt.NumField() {
		field := rt.Field(i)

		if field.Tag.Get("query") != "" {
			hasQuery = true
		}

		if field.Tag.Get("header") != "" {
			hasHeader = true
		}

		if field.Tag.Get("json") != "" || field.Tag.Get("body") != "" {
			hasBody = true
		}
	}

	// If only has one type, infer that
	if hasQuery && !hasHeader && !hasBody {
		return "query"
	}

	if hasHeader && !hasQuery && !hasBody {
		return "header"
	}

	// Default to body
	return "body"
}
