package orpc

import (
	"reflect"
	"sort"
	"strings"

	"github.com/xraph/forge"
)

// ResponseSchemaDef mirrors the OpenAPI response schema definition
// This is imported from the router package's internal structure.
type ResponseSchemaDef struct {
	Description string
	Schema      any
}

// selectPrimaryResponseSchema selects the best response schema for oRPC from multiple OpenAPI responses.
func (s *server) selectPrimaryResponseSchema(
	route forge.RouteInfo,
	responseSchemas map[int]*ResponseSchemaDef,
) (int, *ResponseSchemaDef) {
	// 1. Check for explicit oRPC primary response annotation
	if primaryCode, ok := route.Metadata["orpc.primaryResponse"].(int); ok {
		if schema, exists := responseSchemas[primaryCode]; exists {
			s.logger.Debug("orpc: using explicit primary response",
				forge.F("route", route.Path),
				forge.F("status_code", primaryCode),
			)

			return primaryCode, schema
		}
	}

	// 2. Use method-aware priority list
	priorityList := getResponsePriorityByMethod(route.Method)

	for _, statusCode := range priorityList {
		if schema, exists := responseSchemas[statusCode]; exists {
			// Skip 204 No Content (no body for RPC result)
			if statusCode == 204 {
				continue
			}
			// Skip responses with nil schema
			if schema.Schema == nil {
				continue
			}

			s.logger.Debug("orpc: selected response schema via priority",
				forge.F("route", route.Path),
				forge.F("method", route.Method),
				forge.F("status_code", statusCode),
				forge.F("available_codes", getStatusCodes(responseSchemas)),
			)

			return statusCode, schema
		}
	}

	// 3. Fallback: First success response (200-299) with non-nil schema
	for statusCode := 200; statusCode < 300; statusCode++ {
		if schema, exists := responseSchemas[statusCode]; exists {
			if statusCode != 204 && schema.Schema != nil {
				s.logger.Debug("orpc: selected first available success response",
					forge.F("route", route.Path),
					forge.F("status_code", statusCode),
				)

				return statusCode, schema
			}
		}
	}

	// 4. No suitable response found
	s.logger.Debug("orpc: no suitable response schema found",
		forge.F("route", route.Path),
		forge.F("available_codes", getStatusCodes(responseSchemas)),
	)

	return 0, nil
}

// getResponsePriorityByMethod returns priority order of status codes by HTTP method.
func getResponsePriorityByMethod(method string) []int {
	switch method {
	case "POST":
		// POST: Prefer 201 Created, then 200 OK, then 202 Accepted
		return []int{201, 200, 202}

	case "PUT", "PATCH":
		// PUT/PATCH: Prefer 200 OK, then 204 (but skip 204 if no body)
		return []int{200, 204}

	case "DELETE":
		// DELETE: Prefer 200 OK, then 204
		return []int{200, 204}

	case "GET", "HEAD":
		// GET/HEAD: Prefer 200 OK
		return []int{200}

	default:
		// Default: 200, 201, 202, others
		return []int{200, 201, 202}
	}
}

// getStatusCodes extracts and sorts status codes for logging.
func getStatusCodes(schemas map[int]*ResponseSchemaDef) []int {
	codes := make([]int, 0, len(schemas))
	for code := range schemas {
		codes = append(codes, code)
	}

	sort.Ints(codes)

	return codes
}

// convertOpenAPIResponseToORPC converts OpenAPI response to oRPC result schema.
func convertOpenAPIResponseToORPC(responseDef *ResponseSchemaDef) *ResultSchema {
	if responseDef == nil || responseDef.Schema == nil {
		return &ResultSchema{
			Type:        "object",
			Description: responseDef.Description,
		}
	}

	// Check if schema is already a map (from OpenAPI generator)
	if schemaMap, ok := responseDef.Schema.(map[string]any); ok {
		return convertSchemaMapToORPC(schemaMap, responseDef.Description)
	}

	// Use reflection to generate schema from Go type
	return generateResultSchemaFromType(responseDef.Schema, responseDef.Description)
}

// convertSchemaMapToORPC converts OpenAPI schema map to oRPC ResultSchema.
func convertSchemaMapToORPC(schemaMap map[string]any, description string) *ResultSchema {
	result := &ResultSchema{
		Description: description,
	}

	if schemaType, ok := schemaMap["type"].(string); ok {
		result.Type = schemaType
	} else {
		result.Type = "object"
	}

	if props, ok := schemaMap["properties"].(map[string]any); ok {
		result.Properties = convertPropertiesMapToORPC(props)
	}

	return result
}

// convertPropertiesMapToORPC converts properties map to oRPC PropertySchema map.
func convertPropertiesMapToORPC(props map[string]any) map[string]*PropertySchema {
	result := make(map[string]*PropertySchema)

	for name, prop := range props {
		if propMap, ok := prop.(map[string]any); ok {
			propSchema := &PropertySchema{
				Type:        getStringField(propMap, "type"),
				Description: getStringField(propMap, "description"),
			}

			// Handle nested properties for object types
			if propSchema.Type == "object" {
				if nestedProps, ok := propMap["properties"].(map[string]any); ok {
					propSchema.Properties = convertPropertiesMapToORPC(nestedProps)
				}
			}

			// Handle example values
			if example, ok := propMap["example"]; ok {
				propSchema.Example = example
			}

			result[name] = propSchema
		}
	}

	return result
}

// generateResultSchemaFromType generates ResultSchema from Go type using reflection.
func generateResultSchemaFromType(schemaType any, description string) *ResultSchema {
	result := &ResultSchema{
		Description: description,
	}

	rt := reflect.TypeOf(schemaType)
	if rt == nil {
		result.Type = "object"

		return result
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	switch rt.Kind() {
	case reflect.Struct:
		result.Type = "object"
		result.Properties = generatePropertiesFromStruct(rt)

	case reflect.Slice, reflect.Array:
		result.Type = "array"
		// Could add Items schema here if needed

	case reflect.Map:
		result.Type = "object"

	case reflect.String:
		result.Type = "string"

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		result.Type = "integer"

	case reflect.Float32, reflect.Float64:
		result.Type = "number"

	case reflect.Bool:
		result.Type = "boolean"

	default:
		result.Type = "object"
	}

	return result
}

// generatePropertiesFromStruct generates properties from struct fields.
func generatePropertiesFromStruct(rt reflect.Type) map[string]*PropertySchema {
	props := make(map[string]*PropertySchema)

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		if !field.IsExported() {
			continue
		}

		// Get JSON field name
		jsonName := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			name, omitempty := parseJSONTag(jsonTag)
			if name != "" && name != "-" {
				jsonName = name
			}

			_ = omitempty // Could use for required fields
		}

		// Get description from tag
		description := field.Tag.Get("description")

		// Generate property schema with nested struct support
		propSchema := generatePropertySchemaFromType(field.Type, description)
		props[jsonName] = propSchema
	}

	return props
}

// generatePropertySchemaFromType generates PropertySchema from a reflect.Type
// This handles nested structs recursively.
func generatePropertySchemaFromType(rt reflect.Type, description string) *PropertySchema {
	propSchema := &PropertySchema{
		Type:        getJSONTypeFromReflectType(rt),
		Description: description,
	}

	// Handle pointer types
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	// Handle nested structs
	if rt.Kind() == reflect.Struct {
		propSchema.Properties = generatePropertiesFromStruct(rt)
	}

	// Handle arrays/slices of structs
	if rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
		elemType := rt.Elem()
		if elemType.Kind() == reflect.Ptr {
			elemType = elemType.Elem()
		}

		if elemType.Kind() == reflect.Struct {
			// For arrays of structs, add an items property
			propSchema.Items = &PropertySchema{
				Type:       "object",
				Properties: generatePropertiesFromStruct(elemType),
			}
		}
	}

	return propSchema
}

// getJSONTypeFromReflectType maps Go types to JSON Schema types.
func getJSONTypeFromReflectType(rt reflect.Type) string {
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	switch rt.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "object"
	}
}

// parseJSONTag parses json tag and returns name and omitempty flag.
func parseJSONTag(tag string) (string, bool) {
	parts := strings.Split(tag, ",")
	name := parts[0]
	omitempty := false

	for i := 1; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "omitempty" {
			omitempty = true

			break
		}
	}

	return name, omitempty
}

// getStringField safely gets string field from map.
func getStringField(m map[string]any, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}

	return ""
}

// convertUnifiedSchemaToORPCParams converts unified OpenAPI request schema to oRPC params.
func (s *server) convertUnifiedSchemaToORPCParams(unifiedSchema any, route forge.RouteInfo) *ParamsSchema {
	schema := &ParamsSchema{
		Type:       "object",
		Properties: make(map[string]*PropertySchema),
		Required:   []string{},
	}

	rt := reflect.TypeOf(unifiedSchema)
	if rt == nil {
		return schema
	}

	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		// Not a struct, treat as body parameter
		schema.Properties["body"] = &PropertySchema{
			Type:        getJSONTypeFromReflectType(rt),
			Description: "Request body",
		}

		return schema
	}

	// Parse struct fields with tags
	queryProps := make(map[string]*PropertySchema)
	headerProps := make(map[string]*PropertySchema)
	bodyProps := make(map[string]*PropertySchema)

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		if !field.IsExported() {
			continue
		}

		description := field.Tag.Get("description")
		fieldType := getJSONTypeFromReflectType(field.Type)

		// Handle path parameters
		if pathTag := field.Tag.Get("path"); pathTag != "" {
			paramName, _ := parseTagValue(pathTag)
			if paramName == "" {
				paramName = field.Name
			}

			schema.Properties[paramName] = &PropertySchema{
				Type:        fieldType,
				Description: description,
			}
			schema.Required = append(schema.Required, paramName)
		} else if queryTag := field.Tag.Get("query"); queryTag != "" {
			// Query parameters
			paramName, _ := parseTagValue(queryTag)
			if paramName == "" {
				paramName = field.Name
			}

			queryProps[paramName] = &PropertySchema{
				Type:        fieldType,
				Description: description,
			}
		} else if headerTag := field.Tag.Get("header"); headerTag != "" {
			// Header parameters
			paramName, _ := parseTagValue(headerTag)
			if paramName == "" {
				paramName = field.Name
			}

			headerProps[paramName] = &PropertySchema{
				Type:        fieldType,
				Description: description,
			}
		} else if field.Tag.Get("json") != "" || field.Tag.Get("body") != "" {
			// Body fields
			jsonName := field.Name
			if jsonTag := field.Tag.Get("json"); jsonTag != "" {
				name, _ := parseJSONTag(jsonTag)
				if name != "" && name != "-" {
					jsonName = name
				}
			}

			bodyProps[jsonName] = &PropertySchema{
				Type:        fieldType,
				Description: description,
			}
		}
	}

	// Add query object if we have query params
	if len(queryProps) > 0 {
		schema.Properties["query"] = &PropertySchema{
			Type:        "object",
			Description: "Query parameters",
			Properties:  queryProps,
		}
	}

	// Add headers object if we have header params
	if len(headerProps) > 0 {
		schema.Properties["headers"] = &PropertySchema{
			Type:        "object",
			Description: "Header parameters",
			Properties:  headerProps,
		}
	}

	// Add body object if we have body fields
	if len(bodyProps) > 0 {
		schema.Properties["body"] = &PropertySchema{
			Type:        "object",
			Description: "Request body",
			Properties:  bodyProps,
		}
	}

	s.logger.Debug("orpc: converted unified schema to params",
		forge.F("route", route.Path),
		forge.F("path_params", len(schema.Required)),
		forge.F("query_params", len(queryProps)),
		forge.F("header_params", len(headerProps)),
		forge.F("body_fields", len(bodyProps)),
	)

	return schema
}

// parseTagValue parses a tag value and returns the name and omitempty flag.
func parseTagValue(tag string) (string, bool) {
	parts := strings.Split(tag, ",")
	name := parts[0]
	omitempty := false

	for i := 1; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "omitempty" {
			omitempty = true

			break
		}
	}

	return name, omitempty
}
