package router

import (
	"reflect"
)

// ResponseComponents holds all components of a response extracted from a unified schema.
type ResponseComponents struct {
	Headers        map[string]*Header
	BodySchema     *Schema
	HasBody        bool
	BodySchemaName string // Custom schema name from schema:"name" tag
}

// extractUnifiedResponseComponents extracts headers and body from a unified response struct
// Fields are classified based on struct tags:
// 1. header:"HeaderName" - Response header
// 2. body:"" or json:"name" - Body field
// If no header tags exist, the entire struct is treated as the body.
func extractUnifiedResponseComponents(schemaGen *schemaGenerator, schemaType interface{}) (*ResponseComponents, error) {
	components := &ResponseComponents{
		Headers: make(map[string]*Header),
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

	// Check if any field has header tags or body:"" tags (unwrap markers)
	hasHeaderFields := false
	hasBodyUnwrapTag := false

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		if field.Tag.Get("header") != "" {
			hasHeaderFields = true
		}

		// Check for body:"" (unwrap marker)
		if bodyTag, hasBodyTag := field.Tag.Lookup("body"); hasBodyTag && bodyTag == "" {
			hasBodyUnwrapTag = true
		}
	}

	// If no header fields AND no body:"" tags, treat entire struct as body
	if !hasHeaderFields && !hasBodyUnwrapTag {
		schema, err := schemaGen.GenerateSchema(schemaType)
		if err != nil {
			return nil, err
		}
		components.BodySchema = schema
		components.HasBody = true

		return components, nil
	}

	// Extract headers and body fields
	bodyProperties := make(map[string]*Schema)
	var bodyRequired []string

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			// Check if any tag is explicitly set
			hasExplicitTag := field.Tag.Get("header") != "" ||
				field.Tag.Get("body") != "" ||
				field.Tag.Get("json") != ""

			if !hasExplicitTag {
				// Recursively extract components from embedded struct
				embeddedComponents, err := extractEmbeddedResponseComponents(schemaGen, field, bodyProperties, &bodyRequired)
				if err != nil {
					return nil, err
				}

				// Merge headers
				for headerName, header := range embeddedComponents.Headers {
					components.Headers[headerName] = header
				}

				if embeddedComponents.HasBody {
					components.HasBody = true
				}

				continue
			}
		}

		// Check for header tag first
		if headerTag, hasHeader := field.Tag.Lookup("header"); hasHeader && headerTag != "-" {
			// Response header
			headerName := headerTag

			// Generate schema
			fieldSchema, err := schemaGen.generateSchemaFromType(field.Type)
			if err != nil {
				continue // Skip on error
			}
			schemaGen.applyStructTags(fieldSchema, field)

			// Determine if required
			// Headers are required by default (unless marked optional or as pointers)
			required := true
			if field.Tag.Get("optional") == "true" {
				required = false
			} else if field.Tag.Get("required") == "false" {
				required = false
			} else if field.Type.Kind() == reflect.Ptr {
				required = false
			}

			components.Headers[headerName] = &Header{
				Description: fieldSchema.Description,
				Required:    required,
				Schema:      fieldSchema,
				Example:     fieldSchema.Example,
				Deprecated:  fieldSchema.Deprecated,
			}
		} else if bodyTag, hasBodyTag := field.Tag.Lookup("body"); hasBodyTag || field.Tag.Get("json") != "" {
			// Body field
			components.HasBody = true

			// Special case: if body:"" (empty value), use the field's content directly as the body
			if hasBodyTag && bodyTag == "" {
				// This field IS the body, not a property of the body
				// Create a zero value instance to properly resolve type aliases and generics
				fieldValue := reflect.New(field.Type).Elem().Interface()
				fieldSchema, err := schemaGen.GenerateSchema(fieldValue)
				if err != nil {
					continue
				}

				// Check if this should be registered as a component
				fieldType := field.Type
				if fieldType.Kind() == reflect.Ptr {
					fieldType = fieldType.Elem()
				}

				// For struct types (including generics), register as component and return ref
				if fieldType.Kind() == reflect.Struct {
					// Check for explicit schema name override in struct tag
					// Example: Body MyType `body:"" schema:"CustomName"`
					typeName := field.Tag.Get("schema")

					// If no override, use GetTypeName which cleans generic names
					if typeName == "" {
						typeName = GetTypeName(field.Type)
					}

					if typeName != "" && schemaGen.components != nil {
						// Register the body type as a component
						schemaGen.components[typeName] = fieldSchema
						// Return a reference to it
						components.BodySchema = &Schema{
							Ref: "#/components/schemas/" + typeName,
						}
						// Store the custom schema name for later use
						components.BodySchemaName = typeName
					} else {
						// No type name, use inline schema
						components.BodySchema = fieldSchema
					}
				} else {
					// Non-struct types use inline schema
					components.BodySchema = fieldSchema
				}

				// Don't add to bodyProperties - this IS the body
				continue
			}

			// Regular body field - part of the body object
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
			fieldSchema, err := schemaGen.generateFieldSchema(field)
			if err != nil {
				continue // Skip on error
			}

			bodyProperties[jsonName] = fieldSchema

			// Determine if required
			if isFieldRequired(field) {
				bodyRequired = append(bodyRequired, jsonName)
			}
		}
	}

	// Build body schema ONLY if we have body fields
	if len(bodyProperties) > 0 {
		components.BodySchema = &Schema{
			Type:       "object",
			Properties: bodyProperties,
		}
		if len(bodyRequired) > 0 {
			components.BodySchema.Required = bodyRequired
		}
		components.HasBody = true
	}
	// else: HasBody remains false, no body schema created

	return components, nil
}

// extractEmbeddedResponseComponents recursively extracts components from embedded structs.
func extractEmbeddedResponseComponents(schemaGen *schemaGenerator, field reflect.StructField, bodyProperties map[string]*Schema, bodyRequired *[]string) (*ResponseComponents, error) {
	components := &ResponseComponents{
		Headers: make(map[string]*Header),
	}

	fieldType := field.Type
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	if fieldType.Kind() != reflect.Struct {
		return components, nil
	}

	for i := 0; i < fieldType.NumField(); i++ {
		embeddedField := fieldType.Field(i)

		if !embeddedField.IsExported() {
			continue
		}

		// Handle nested embedded structs
		if embeddedField.Anonymous {
			nestedComponents, err := extractEmbeddedResponseComponents(schemaGen, embeddedField, bodyProperties, bodyRequired)
			if err != nil {
				return nil, err
			}

			// Merge headers
			for headerName, header := range nestedComponents.Headers {
				components.Headers[headerName] = header
			}

			if nestedComponents.HasBody {
				components.HasBody = true
			}

			continue
		}

		// Check for header tag
		if headerTag, hasHeader := embeddedField.Tag.Lookup("header"); hasHeader && headerTag != "-" {
			headerName := headerTag

			fieldSchema, err := schemaGen.generateSchemaFromType(embeddedField.Type)
			if err != nil {
				continue
			}
			schemaGen.applyStructTags(fieldSchema, embeddedField)

			// Headers are required by default
			required := true
			if embeddedField.Tag.Get("optional") == "true" {
				required = false
			} else if embeddedField.Tag.Get("required") == "false" {
				required = false
			} else if embeddedField.Type.Kind() == reflect.Ptr {
				required = false
			}

			components.Headers[headerName] = &Header{
				Description: fieldSchema.Description,
				Required:    required,
				Schema:      fieldSchema,
				Example:     fieldSchema.Example,
				Deprecated:  fieldSchema.Deprecated,
			}
		} else if bodyTag, hasBodyTag := embeddedField.Tag.Lookup("body"); hasBodyTag || embeddedField.Tag.Get("json") != "" {
			// Body field
			components.HasBody = true

			// Special case: if body:"" (empty value), this field IS the body
			if hasBodyTag && bodyTag == "" {
				// Note: In embedded structs, we can't set the whole body from one field
				// So we'll treat body:"" same as body:"fieldName" for embedded structs
				// This is a reasonable limitation - fall through to regular handling
			}

			jsonName := embeddedField.Name
			if jsonTag := embeddedField.Tag.Get("json"); jsonTag != "" {
				name, _ := parseJSONTag(jsonTag)
				if name != "" && name != "-" {
					jsonName = name
				}
			} else if bodyTag != "" && bodyTag != "-" {
				jsonName = bodyTag
			}

			fieldSchema, err := schemaGen.generateFieldSchema(embeddedField)
			if err != nil {
				continue
			}

			bodyProperties[jsonName] = fieldSchema

			if isFieldRequired(embeddedField) {
				*bodyRequired = append(*bodyRequired, jsonName)
			}
		}
	}

	return components, nil
}
