package router

import (
	"maps"
	"reflect"
	"strings"

	"github.com/xraph/forge/internal/shared"
)

// Type aliases for AsyncAPI types.
type AsyncAPIMessage = shared.AsyncAPIMessage
type AsyncAPIParameter = shared.AsyncAPIParameter

// asyncAPISchemaGenerator generates AsyncAPI message schemas from Go types
// It reuses the OpenAPI schema generator since AsyncAPI uses JSON Schema.
type asyncAPISchemaGenerator struct {
	schemaGen  *schemaGenerator
	components map[string]*Schema // Reference to AsyncAPI components for nested types
}

// setLogger sets the logger for collision warnings.
func (g *asyncAPISchemaGenerator) setLogger(logger Logger) {
	g.schemaGen.setLogger(logger)
}

// newAsyncAPISchemaGenerator creates a new AsyncAPI schema generator.
func newAsyncAPISchemaGenerator(components map[string]*Schema, logger Logger) *asyncAPISchemaGenerator {
	return &asyncAPISchemaGenerator{
		schemaGen:  newSchemaGenerator(components, logger), // AsyncAPI supports component references
		components: components,
	}
}

// GenerateMessageSchema generates an AsyncAPI message schema from a Go type.
func (g *asyncAPISchemaGenerator) GenerateMessageSchema(t any, contentType string) (*AsyncAPIMessage, error) {
	if t == nil {
		return nil, nil
	}

	message := &AsyncAPIMessage{
		ContentType: contentType,
	}

	// Generate payload schema
	payload, err := g.schemaGen.GenerateSchema(t)
	if err != nil {
		return nil, err
	}

	message.Payload = payload

	// Extract name and description from type if available
	typ := reflect.TypeOf(t)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ.Kind() == reflect.Struct {
		message.Name = typ.Name()
		message.Title = typ.Name()

		// Try to get description from struct tag (if it's a field) or doc comment
		// For now, we'll use the type name as title
	}

	return message, nil
}

// GenerateHeadersSchema generates headers schema from a Go type
// Headers are extracted from struct fields with `header:"name"` tag.
func (g *asyncAPISchemaGenerator) GenerateHeadersSchema(t any) *Schema {
	if t == nil {
		return nil
	}

	typ := reflect.TypeOf(t)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		return nil
	}

	schema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	var required []string

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			headerTag := field.Tag.Get("header")
			// Only flatten if no explicit header tag is provided
			if headerTag == "" {
				embeddedHeaders := g.flattenEmbeddedHeaders(field)
				maps.Copy(schema.Properties, embeddedHeaders.Properties)

				if embeddedHeaders.Required != nil {
					required = append(required, embeddedHeaders.Required...)
				}

				continue
			}
		}

		// Get header tag
		headerTag := field.Tag.Get("header")
		if headerTag == "" || headerTag == "-" {
			continue
		}

		headerName := headerTag

		// Generate field schema
		fieldSchema, err := g.schemaGen.generateFieldSchema(field)
		if err != nil {
			continue // Skip field on error
		}

		schema.Properties[headerName] = fieldSchema

		// Check if required
		if field.Tag.Get("required") == "true" {
			required = append(required, headerName)
		}
	}

	if len(schema.Properties) == 0 {
		return nil
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema
}

// flattenEmbeddedHeaders recursively extracts header fields from an embedded struct.
func (g *asyncAPISchemaGenerator) flattenEmbeddedHeaders(field reflect.StructField) *Schema {
	schema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	fieldType := field.Type
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	if fieldType.Kind() != reflect.Struct {
		return schema
	}

	var required []string

	for i := 0; i < fieldType.NumField(); i++ {
		embeddedField := fieldType.Field(i)

		if !embeddedField.IsExported() {
			continue
		}

		// Handle nested embedded structs
		if embeddedField.Anonymous {
			headerTag := embeddedField.Tag.Get("header")
			if headerTag == "" {
				nestedHeaders := g.flattenEmbeddedHeaders(embeddedField)
				maps.Copy(schema.Properties, nestedHeaders.Properties)

				if nestedHeaders.Required != nil {
					required = append(required, nestedHeaders.Required...)
				}

				continue
			}
		}

		// Get header tag
		headerTag := embeddedField.Tag.Get("header")
		if headerTag == "" || headerTag == "-" {
			continue
		}

		headerName := headerTag

		// Generate field schema
		fieldSchema, err := g.schemaGen.generateFieldSchema(embeddedField)
		if err != nil {
			continue
		}

		schema.Properties[headerName] = fieldSchema

		if embeddedField.Tag.Get("required") == "true" {
			required = append(required, headerName)
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema
}

// GeneratePayloadSchema generates only the payload schema from a Go type.
func (g *asyncAPISchemaGenerator) GeneratePayloadSchema(t any) *Schema {
	schema, _ := g.schemaGen.GenerateSchema(t)

	return schema
}

// ExtractMessageMetadata extracts AsyncAPI message metadata from struct tags.
func (g *asyncAPISchemaGenerator) ExtractMessageMetadata(t any) (name, title, summary, description string) {
	if t == nil {
		return
	}

	typ := reflect.TypeOf(t)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		name = typ.Name()

		return
	}

	name = typ.Name()
	title = typ.Name()

	// Look for struct-level tags (would require custom reflection or build tags)
	// For now, we derive from type name
	return
}

// SplitMessageComponents splits a Go type into separate header and payload schemas
// This is useful when a struct has both header:"" and json:"" tags.
func (g *asyncAPISchemaGenerator) SplitMessageComponents(t any) (headers *Schema, payload *Schema) {
	if t == nil {
		return nil, nil
	}

	typ := reflect.TypeOf(t)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		return nil, g.GeneratePayloadSchema(t)
	}

	// Extract headers
	headers = g.GenerateHeadersSchema(t)

	// Generate payload from non-header fields
	payloadSchema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	var required []string

	hasPayloadFields := false

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous struct fields - flatten them
		if field.Anonymous {
			jsonTag := field.Tag.Get("json")
			jsonName, _ := parseJSONTag(jsonTag)

			// Only flatten if no explicit JSON name is provided
			if jsonName == "" {
				embeddedProps, embeddedRequired, err := g.schemaGen.flattenEmbeddedStruct(field)
				if err != nil {
					continue
				}

				// Merge embedded properties (skip header fields)
				for propName, propSchema := range embeddedProps {
					payloadSchema.Properties[propName] = propSchema
					hasPayloadFields = true
				}

				required = append(required, embeddedRequired...)

				continue
			}
		}

		// Skip header fields
		if field.Tag.Get("header") != "" && field.Tag.Get("header") != "-" {
			continue
		}

		// Get JSON tag for payload
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		jsonName, omitempty := parseJSONTag(jsonTag)
		if jsonName == "" {
			jsonName = field.Name
		}

		// Generate field schema
		fieldSchema, err := g.schemaGen.generateFieldSchema(field)
		if err != nil {
			continue // Skip field on error
		}

		payloadSchema.Properties[jsonName] = fieldSchema
		hasPayloadFields = true

		// Check if required
		// Check for optional tag first (explicit opt-out), then required tag (explicit opt-in), then fall back to omitempty logic
		if optionalTag := field.Tag.Get("optional"); optionalTag == "true" {
			// Explicitly marked as optional, skip adding to required
		} else if requiredTag := field.Tag.Get("required"); requiredTag == "true" {
			required = append(required, jsonName)
		} else if !omitempty && field.Type.Kind() != reflect.Ptr {
			required = append(required, jsonName)
		}
	}

	if !hasPayloadFields {
		return headers, nil
	}

	if len(required) > 0 {
		payloadSchema.Required = required
	}

	return headers, payloadSchema
}

// CreateMessageWithSchema creates an AsyncAPI message with the given schema.
func (g *asyncAPISchemaGenerator) CreateMessageWithSchema(schema *Schema, contentType string) *AsyncAPIMessage {
	return &AsyncAPIMessage{
		ContentType: contentType,
		Payload:     schema,
	}
}

// pathToChannelID converts a route path to a valid AsyncAPI channel ID
// Example: /ws/chat/{roomId} -> ws-chat-roomId.
func pathToChannelID(path string) string {
	// Remove leading slash
	path = strings.TrimPrefix(path, "/")

	// Replace slashes with hyphens
	path = strings.ReplaceAll(path, "/", "-")

	// Remove braces from parameters but keep the name
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")

	// Convert to lowercase for consistency
	path = strings.ToLower(path)

	return path
}

// extractChannelParameters extracts parameter definitions from a route path
// Example: /ws/chat/{roomId}/{userId} -> roomId, userId parameters.
func extractChannelParameters(path string) map[string]*AsyncAPIParameter {
	params := make(map[string]*AsyncAPIParameter)

	// Find all parameters in path
	start := -1
	for i := range len(path) {
		if path[i] == '{' {
			start = i + 1
		} else if path[i] == '}' && start != -1 {
			paramName := path[start:i]
			params[paramName] = &AsyncAPIParameter{
				Description: paramName + " parameter",
				Schema: &Schema{
					Type: "string",
				},
			}
			start = -1
		}
	}

	return params
}

// createChannelAddress creates a channel address from a route path
// It keeps the path format for AsyncAPI channel addresses.
func createChannelAddress(path string) string {
	// AsyncAPI channel addresses can use parameter syntax like {paramName}
	return path
}
