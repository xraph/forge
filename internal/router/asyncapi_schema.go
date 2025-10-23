package router

import (
	"reflect"
	"strings"

	"github.com/xraph/forge/internal/shared"
)

// Type aliases for AsyncAPI types
type AsyncAPIMessage = shared.AsyncAPIMessage
type AsyncAPIParameter = shared.AsyncAPIParameter

// asyncAPISchemaGenerator generates AsyncAPI message schemas from Go types
// It reuses the OpenAPI schema generator since AsyncAPI uses JSON Schema
type asyncAPISchemaGenerator struct {
	schemaGen *schemaGenerator
}

// newAsyncAPISchemaGenerator creates a new AsyncAPI schema generator
func newAsyncAPISchemaGenerator() *asyncAPISchemaGenerator {
	return &asyncAPISchemaGenerator{
		schemaGen: newSchemaGenerator(),
	}
}

// GenerateMessageSchema generates an AsyncAPI message schema from a Go type
func (g *asyncAPISchemaGenerator) GenerateMessageSchema(t interface{}, contentType string) *AsyncAPIMessage {
	if t == nil {
		return nil
	}

	message := &AsyncAPIMessage{
		ContentType: contentType,
	}

	// Generate payload schema
	message.Payload = g.schemaGen.GenerateSchema(t)

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

	return message
}

// GenerateHeadersSchema generates headers schema from a Go type
// Headers are extracted from struct fields with `header:"name"` tag
func (g *asyncAPISchemaGenerator) GenerateHeadersSchema(t interface{}) *Schema {
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

		// Get header tag
		headerTag := field.Tag.Get("header")
		if headerTag == "" || headerTag == "-" {
			continue
		}

		headerName := headerTag

		// Generate field schema
		fieldSchema := g.schemaGen.generateFieldSchema(field)
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

// GeneratePayloadSchema generates only the payload schema from a Go type
func (g *asyncAPISchemaGenerator) GeneratePayloadSchema(t interface{}) *Schema {
	return g.schemaGen.GenerateSchema(t)
}

// ExtractMessageMetadata extracts AsyncAPI message metadata from struct tags
func (g *asyncAPISchemaGenerator) ExtractMessageMetadata(t interface{}) (name, title, summary, description string) {
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
// This is useful when a struct has both header:"" and json:"" tags
func (g *asyncAPISchemaGenerator) SplitMessageComponents(t interface{}) (headers *Schema, payload *Schema) {
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
		fieldSchema := g.schemaGen.generateFieldSchema(field)
		payloadSchema.Properties[jsonName] = fieldSchema
		hasPayloadFields = true

		// Check if required
		if requiredTag := field.Tag.Get("required"); requiredTag == "true" {
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

// CreateMessageWithSchema creates an AsyncAPI message with the given schema
func (g *asyncAPISchemaGenerator) CreateMessageWithSchema(schema *Schema, contentType string) *AsyncAPIMessage {
	return &AsyncAPIMessage{
		ContentType: contentType,
		Payload:     schema,
	}
}

// pathToChannelID converts a route path to a valid AsyncAPI channel ID
// Example: /ws/chat/{roomId} -> ws-chat-roomId
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
// Example: /ws/chat/{roomId}/{userId} -> roomId, userId parameters
func extractChannelParameters(path string) map[string]*AsyncAPIParameter {
	params := make(map[string]*AsyncAPIParameter)

	// Find all parameters in path
	start := -1
	for i := 0; i < len(path); i++ {
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
// It keeps the path format for AsyncAPI channel addresses
func createChannelAddress(path string) string {
	// AsyncAPI channel addresses can use parameter syntax like {paramName}
	return path
}
