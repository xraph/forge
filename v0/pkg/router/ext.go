package router

import (
	"fmt"
	"reflect"
	"strings"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// extractBodyField checks if a struct has a "Body" field and returns it
func (g *OpenAPIGenerator) extractBodyField(responseType reflect.Type) *reflect.StructField {
	if responseType.Kind() != reflect.Struct {
		return nil
	}

	// Look for a field named "Body" (case sensitive)
	for i := 0; i < responseType.NumField(); i++ {
		field := responseType.Field(i)
		if field.Name == "Body" && field.IsExported() {
			if g.logger != nil {
				g.logger.Debug("Found Body field in response struct",
					logger.String("struct_name", responseType.Name()),
					logger.String("body_field_type", field.Type.String()),
					logger.String("body_field_name", field.Name),
				)
			}
			return &field
		}
	}

	return nil
}

// extractResponseContentTypes analyzes a response type to determine supported content types
func (g *OpenAPIGenerator) extractResponseContentTypes(responseType reflect.Type) map[string]bool {
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	contentTypes := make(map[string]bool)

	if responseType.Kind() != reflect.Struct {
		// Non-struct types default to JSON
		return map[string]bool{"application/json": true}
	}

	// Check struct fields for body-related tags
	hasJSON := false
	hasXML := false
	hasHTML := false
	hasPlainText := false
	hasBinary := false
	hasCustom := make(map[string]bool)

	for i := 0; i < responseType.NumField(); i++ {
		field := responseType.Field(i)
		if !field.IsExported() {
			continue
		}

		tagInfo := g.parseFieldTags(field)

		// Check for body-related tags
		if tagInfo.Body {
			// Generic body tag - include JSON by default
			hasJSON = true
		}

		if tagInfo.JSON != "" && tagInfo.JSON != "-" {
			hasJSON = true
		}

		if tagInfo.XML != "" && tagInfo.XML != "-" {
			hasXML = true
		}

		// Check for specific content type tags
		if contentType := field.Tag.Get("content-type"); contentType != "" {
			for _, ct := range strings.Split(contentType, ",") {
				hasCustom[strings.TrimSpace(ct)] = true
			}
		}

		// Check for response format tags
		if format := field.Tag.Get("response-format"); format != "" {
			switch strings.ToLower(format) {
			case "json":
				hasJSON = true
			case "xml":
				hasXML = true
			case "html":
				hasHTML = true
			case "text":
				hasPlainText = true
			case "binary":
				hasBinary = true
			default:
				hasCustom["application/"+format] = true
			}
		}

		// Check for media type tag
		if mediaType := field.Tag.Get("media-type"); mediaType != "" {
			hasCustom[mediaType] = true
		}

		// Check field type for binary data
		if g.isBinaryField(field) {
			hasBinary = true
		}

		// Check for HTML response tag
		if field.Tag.Get("html") != "" {
			hasHTML = true
		}

		// Check for text response tag
		if field.Tag.Get("text") != "" {
			hasPlainText = true
		}
	}

	// Build content types map
	if hasJSON {
		contentTypes["application/json"] = true
	}
	if hasXML {
		contentTypes["application/xml"] = true
		contentTypes["text/xml"] = true
	}
	if hasHTML {
		contentTypes["text/html"] = true
	}
	if hasPlainText {
		contentTypes["text/plain"] = true
	}
	if hasBinary {
		contentTypes["application/octet-stream"] = true
	}

	// Add custom content types
	for ct := range hasCustom {
		contentTypes[ct] = true
	}

	// If no specific content types found, default to JSON
	if len(contentTypes) == 0 {
		contentTypes["application/json"] = true
	}

	return contentTypes
}

// convertTypeToResponseSchema converts a type to schema with content-type specific handling
func (g *OpenAPIGenerator) convertTypeToResponseSchema(t reflect.Type, contentType string) *common.Schema {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if g.logger != nil {
		g.logger.Debug("Converting type to response schema",
			logger.String("type", t.String()),
			logger.String("content_type", contentType),
			logger.String("type_kind", t.Kind().String()),
		)
	}

	// Handle different content types
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		return g.convertTypeToJSONSchema(t)
	case strings.HasPrefix(contentType, "application/xml") || strings.HasPrefix(contentType, "text/xml"):
		return g.convertTypeToXMLSchema(t)
	case strings.HasPrefix(contentType, "text/html"):
		return g.convertTypeToHTMLSchema(t)
	case strings.HasPrefix(contentType, "text/plain"):
		return g.convertTypeToTextSchema(t)
	case strings.HasPrefix(contentType, "application/octet-stream"):
		return g.convertTypeToBinarySchema(t)
	default:
		// Default to JSON schema for unknown types
		return g.convertTypeToJSONSchema(t)
	}
}

// convertTypeToJSONSchema converts a type to JSON schema
func (g *OpenAPIGenerator) convertTypeToJSONSchema(t reflect.Type) *common.Schema {
	return g.convertTypeToSchema(t)
}

// convertTypeToXMLSchema converts a type to XML schema with XML-specific attributes
func (g *OpenAPIGenerator) convertTypeToXMLSchema(t reflect.Type) *common.Schema {
	schema := g.convertTypeToSchema(t)

	if t.Kind() == reflect.Struct {
		// Add XML-specific properties
		g.enhanceSchemaWithXMLTags(schema, t)
	}

	return schema
}

// convertTypeToHTMLSchema creates schema for HTML content
func (g *OpenAPIGenerator) convertTypeToHTMLSchema(t reflect.Type) *common.Schema {
	return &common.Schema{
		Type:        "string",
		Format:      "html",
		Description: "HTML content response",
	}
}

// convertTypeToTextSchema creates schema for plain text content
func (g *OpenAPIGenerator) convertTypeToTextSchema(t reflect.Type) *common.Schema {
	return &common.Schema{
		Type:        "string",
		Description: "Plain text response",
	}
}

// convertTypeToBinarySchema creates schema for binary content
func (g *OpenAPIGenerator) convertTypeToBinarySchema(t reflect.Type) *common.Schema {
	return &common.Schema{
		Type:        "string",
		Format:      "binary",
		Description: "Binary data response",
	}
}

// enhanceSchemaWithXMLTags adds XML-specific schema enhancements
func (g *OpenAPIGenerator) enhanceSchemaWithXMLTags(schema *common.Schema, t reflect.Type) {
	if schema.Properties == nil {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		tagInfo := g.parseFieldTags(field)
		fieldName := g.getJSONFieldName(field, tagInfo)

		if fieldName == "-" {
			continue
		}

		if fieldSchema, exists := schema.Properties[fieldName]; exists {
			// Add XML-specific properties
			if tagInfo.XML != "" && tagInfo.XML != "-" {
				if fieldSchema.Extensions == nil {
					fieldSchema.Extensions = make(map[string]interface{})
				}

				xmlName := tagInfo.XML
				parts := strings.Split(xmlName, ",")
				if len(parts) > 0 && parts[0] != "" {
					fieldSchema.Extensions["xml"] = map[string]interface{}{
						"name": parts[0],
					}
				}

				// Handle XML attributes
				for _, part := range parts[1:] {
					switch strings.TrimSpace(part) {
					case "attr":
						if xmlObj, ok := fieldSchema.Extensions["xml"].(map[string]interface{}); ok {
							xmlObj["attribute"] = true
						}
					case "cdata":
						if xmlObj, ok := fieldSchema.Extensions["xml"].(map[string]interface{}); ok {
							xmlObj["wrapped"] = true
						}
					}
				}
			}
		}
	}
}

// extractResponseExamples extracts examples for response content types
func (g *OpenAPIGenerator) extractResponseExamples(responseType reflect.Type, contentType string) map[string]*common.Example {
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	examples := make(map[string]*common.Example)

	if responseType.Kind() != reflect.Struct {
		return examples
	}

	// Look for example tags in fields
	for i := 0; i < responseType.NumField(); i++ {
		field := responseType.Field(i)
		if !field.IsExported() {
			continue
		}

		// Check for content-type specific examples
		exampleKey := fmt.Sprintf("example-%s", strings.ReplaceAll(contentType, "/", "-"))
		if exampleValue := field.Tag.Get(exampleKey); exampleValue != "" {
			examples["default"] = &common.Example{
				Summary: fmt.Sprintf("Example %s response", contentType),
				Value:   g.parseExampleValue(exampleValue, contentType),
			}
			break
		}

		// Check for generic example tag
		if exampleValue := field.Tag.Get("example"); exampleValue != "" && len(examples) == 0 {
			examples["default"] = &common.Example{
				Summary: "Example response",
				Value:   g.parseExampleValue(exampleValue, contentType),
			}
		}

		// Check for response example tag
		if exampleValue := field.Tag.Get("response-example"); exampleValue != "" {
			examples["default"] = &common.Example{
				Summary: "Response example",
				Value:   g.parseExampleValue(exampleValue, contentType),
			}
		}
	}

	return examples
}

// parseExampleValue parses example value based on content type
func (g *OpenAPIGenerator) parseExampleValue(exampleStr, contentType string) interface{} {
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		var jsonValue interface{}
		if err := json.Unmarshal([]byte(exampleStr), &jsonValue); err == nil {
			return jsonValue
		}
		return exampleStr
	case strings.HasPrefix(contentType, "application/xml") || strings.HasPrefix(contentType, "text/xml"):
		return exampleStr // Return as-is for XML
	case strings.HasPrefix(contentType, "text/html"):
		return exampleStr // Return as-is for HTML
	case strings.HasPrefix(contentType, "text/plain"):
		return exampleStr // Return as-is for plain text
	default:
		return exampleStr
	}
}

// isBinaryField checks if a field represents binary data
func (g *OpenAPIGenerator) isBinaryField(field reflect.StructField) bool {
	fieldType := field.Type

	// Remove pointer
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// Check for byte slice
	if fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.Uint8 {
		return true
	}

	// Check for binary format tag
	if field.Tag.Get("format") == "binary" {
		return true
	}

	// Check for binary type tag
	if field.Tag.Get("type") == "binary" {
		return true
	}

	return false
}

// Enhanced createOperation with better response handling
func (g *OpenAPIGenerator) createOperation(method, path string, serviceTypes []reflect.Type, requestType, responseType reflect.Type, options ...common.HandlerOption) *common.Operation {
	operationID := g.generateOperationID(method, path)

	operation := &common.Operation{
		OperationID: operationID,
		Summary:     fmt.Sprintf("%s %s", strings.ToUpper(method), path),
		Description: fmt.Sprintf("Handler for %s %s", strings.ToUpper(method), path),
		Parameters:  []*common.Parameter{},
		Responses:   make(map[string]*common.Response),
		Tags:        []string{},
	}

	// Apply handler options to extract tags
	tempInfo := &common.RouteHandlerInfo{
		Tags: make(map[string]string),
	}
	for _, option := range options {
		option(tempInfo)
	}

	// Extract OpenAPI tags from the handler info
	if openAPITags, exists := tempInfo.Tags["openapi_tags"]; exists {
		if openAPITags != "" {
			operation.Tags = strings.Split(openAPITags, ",")
			for i, tag := range operation.Tags {
				operation.Tags[i] = strings.TrimSpace(tag)
			}
		}
	}

	// Fallback: try to extract from "group" tag if openapi_tags not set
	if len(operation.Tags) == 0 {
		if group, exists := tempInfo.Tags["group"]; exists && group != "" {
			operation.Tags = []string{group}
		}
	}

	// Default tag if none specified
	if len(operation.Tags) == 0 {
		operation.Tags = []string{"default"}
	}

	// Generate enhanced summary and description
	operation.Summary = g.generateOperationSummary(method, path, tempInfo.Tags)
	operation.Description = g.generateOperationDescription(method, path, serviceTypes, tempInfo.Tags)

	// Add path parameters FIRST - extract from the original Steel router path
	pathParams := g.extractPathParameters(path)
	for _, paramName := range pathParams {
		param := &common.Parameter{
			Name:        paramName,
			In:          "path",
			Required:    true,
			Description: fmt.Sprintf("Path parameter: %s", paramName),
			Schema: &common.Schema{
				Type: "string",
			},
		}
		operation.Parameters = append(operation.Parameters, param)

		if g.logger != nil {
			g.logger.Debug("Added path parameter to operation",
				logger.String("param_name", paramName),
				logger.String("method", method),
				logger.String("path", path),
			)
		}
	}

	// Then add other parameters from request type
	if requestType != nil && requestType.Kind() == reflect.Struct {
		otherParams := g.extractParameters(requestType)
		operation.Parameters = append(operation.Parameters, otherParams...)

		if g.hasBodyParameter(requestType) && (method == "POST" || method == "PUT" || method == "PATCH") {
			operation.RequestBody = g.createRequestBody(requestType)
		}
	}

	// Enhanced response handling with body tag support
	operation.Responses["200"] = g.createResponse("Success", responseType)

	// Add additional response codes based on response type analysis
	if responseType != nil {
		g.addConditionalResponses(operation, responseType, tempInfo.Tags)
	}

	// Default error responses
	operation.Responses["400"] = &common.Response{
		Description: "Bad Request",
		Content: map[string]*common.MediaType{
			"application/json": {
				Schema: &common.Schema{Ref: "#/components/schemas/ErrorResponse"},
			},
		},
	}
	operation.Responses["500"] = &common.Response{
		Description: "Internal Server Error",
		Content: map[string]*common.MediaType{
			"application/json": {
				Schema: &common.Schema{Ref: "#/components/schemas/ErrorResponse"},
			},
		},
	}

	return operation
}

// addConditionalResponses adds additional response codes based on response type analysis
func (g *OpenAPIGenerator) addConditionalResponses(operation *common.Operation, responseType reflect.Type, tags map[string]string) {
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	if responseType.Kind() != reflect.Struct {
		return
	}

	// Check for conditional response tags
	for i := 0; i < responseType.NumField(); i++ {
		field := responseType.Field(i)
		if !field.IsExported() {
			continue
		}

		// Check for status code specific responses
		if statusCode := field.Tag.Get("status-code"); statusCode != "" {
			description := field.Tag.Get("description")
			if description == "" {
				description = fmt.Sprintf("Response with status %s", statusCode)
			}

			response := g.createResponse(description, field.Type)
			operation.Responses[statusCode] = response
		}

		// Check for error response indicators
		if field.Tag.Get("error-response") == "true" {
			statusCode := field.Tag.Get("error-status")
			if statusCode == "" {
				statusCode = "400"
			}

			description := field.Tag.Get("description")
			if description == "" {
				description = "Error response"
			}

			response := g.createResponse(description, field.Type)
			operation.Responses[statusCode] = response
		}
	}

	// Add responses based on operation tags
	if authLevel, exists := tags["auth_level"]; exists && authLevel != "none" {
		operation.Responses["401"] = &common.Response{
			Description: "Unauthorized",
			Content: map[string]*common.MediaType{
				"application/json": {
					Schema: &common.Schema{Ref: "#/components/schemas/ErrorResponse"},
				},
			},
		}

		if authLevel == "admin" {
			operation.Responses["403"] = &common.Response{
				Description: "Forbidden",
				Content: map[string]*common.MediaType{
					"application/json": {
						Schema: &common.Schema{Ref: "#/components/schemas/ErrorResponse"},
					},
				},
			}
		}
	}
}

func (g *OpenAPIGenerator) validateBodyFieldUsage(responseType reflect.Type) error {
	bodyField := g.extractBodyField(responseType)
	if bodyField == nil {
		return nil // No Body field, nothing to validate
	}

	// Validate that Body field type is reasonable for serialization
	bodyFieldType := bodyField.Type
	if bodyFieldType.Kind() == reflect.Ptr {
		bodyFieldType = bodyFieldType.Elem()
	}

	switch bodyFieldType.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array,
		reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Bool, reflect.Interface:
		// These types are fine for Body fields
		return nil
	default:
		if g.logger != nil {
			g.logger.Warn("Body field has potentially problematic type",
				logger.String("response_type", responseType.Name()),
				logger.String("body_field_type", bodyFieldType.String()),
				logger.String("body_field_kind", bodyFieldType.Kind().String()),
			)
		}
		return nil // Don't fail, just warn
	}
}

// // generateOperationID generates a unique operation ID
// func (g *OpenAPIGenerator) generateOperationID(method, path string) string {
// 	parts := strings.Split(path, "/")
// 	var cleanParts []string
//
// 	for _, part := range parts {
// 		if part == "" {
// 			continue
// 		}
// 		if strings.HasPrefix(part, ":") {
// 			part = "By" + strings.Title(part[1:])
// 		} else if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
// 			part = "By" + strings.Title(part[1:len(part)-1])
// 		} else {
// 			part = strings.Title(part)
// 		}
// 		cleanParts = append(cleanParts, part)
// 	}
//
// 	pathStr := strings.Join(cleanParts, "")
// 	return strings.ToLower(method) + pathStr
// }

// // generateOperationSummary generates operation summary
// func (g *OpenAPIGenerator) generateOperationSummary(method, path string, tags map[string]string) string {
// 	operation := strings.ToLower(tags["operation"])
// 	if operation == "" {
// 		operation = strings.ToLower(method)
// 	}
//
// 	resource := g.extractResourceFromPath(path)
//
// 	switch operation {
// 	case "create":
// 		return fmt.Sprintf("Create a new %s", resource)
// 	case "read", "get":
// 		return fmt.Sprintf("Get %s details", resource)
// 	case "update", "patch":
// 		return fmt.Sprintf("Update %s", resource)
// 	case "delete":
// 		return fmt.Sprintf("Delete %s", resource)
// 	case "list":
// 		return fmt.Sprintf("List %s with filtering and pagination", resource)
// 	case "upload":
// 		return fmt.Sprintf("Upload file for %s", resource)
// 	case "check":
// 		return "Health check endpoint"
// 	default:
// 		return fmt.Sprintf("%s %s", strings.ToUpper(method), path)
// 	}
// }

// convertSteelPathToOpenAPI converts Steel router path format (:param) to OpenAPI format ({param})
func (g *OpenAPIGenerator) convertSteelPathToOpenAPI(path string) string {
	// Convert :param to {param} for OpenAPI specification
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			// Convert :name to {name}
			paramName := part[1:] // Remove the ':' prefix
			parts[i] = "{" + paramName + "}"
		}
	}
	return strings.Join(parts, "/")
}

// extractPathParameters extracts parameter names from a path (supports both :name and {name} formats)
func (g *OpenAPIGenerator) extractPathParameters(path string) []string {
	var params []string
	parts := strings.Split(path, "/")

	for _, part := range parts {
		if strings.HasPrefix(part, ":") {
			// Steel router format :name
			params = append(params, part[1:])
		} else if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			// OpenAPI format {name}
			params = append(params, part[1:len(part)-1])
		}
	}

	return params
}
