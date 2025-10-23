package router

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Add these type checks at the package level for efficiency
var (
	timeType            = reflect.TypeOf(time.Time{})
	durationType        = reflect.TypeOf(time.Duration(0))
	jsonMarshalerType   = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textMarshalerType   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// OpenAPIGenerator handles OpenAPI 3.1.1 specification generation
type OpenAPIGenerator struct {
	router     *ForgeRouter
	spec       *common.OpenAPISpec
	schemas    map[string]*common.SchemaDefinition
	mu         sync.RWMutex
	logger     common.Logger
	metrics    common.Metrics
	autoUpdate bool
	version    string
}

// NewOpenAPIGenerator creates a new OpenAPI generator
func NewOpenAPIGenerator(router *ForgeRouter, config common.OpenAPIConfig) *OpenAPIGenerator {
	generator := &OpenAPIGenerator{
		router:     router,
		schemas:    make(map[string]*common.SchemaDefinition),
		logger:     router.logger,
		metrics:    router.metrics,
		autoUpdate: config.AutoUpdate,
		version:    config.Version,
	}

	generator.initializeSpec(config)
	return generator
}

// initializeSpec initializes the OpenAPI specification
func (g *OpenAPIGenerator) initializeSpec(config common.OpenAPIConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.spec = &common.OpenAPISpec{
		OpenAPI: "3.1.1",
		Info: &common.InfoObject{
			Title:          config.Title,
			Description:    config.Description,
			Version:        config.Version,
			TermsOfService: config.TermsOfService,
			Contact:        config.Contact,
			License:        config.License,
		},
		Servers:  config.Servers,
		Paths:    make(map[string]*common.PathItem),
		Security: config.Security,
		Tags:     config.Tags,
		Components: &common.ComponentsObject{
			Schemas:         make(map[string]*common.Schema),
			Responses:       make(map[string]*common.Response),
			Parameters:      make(map[string]*common.Parameter),
			Examples:        make(map[string]*common.Example),
			RequestBodies:   make(map[string]*common.RequestBody),
			Headers:         make(map[string]*common.Header),
			SecuritySchemes: make(map[string]*common.SecurityScheme),
			Links:           make(map[string]*common.Link),
			Callbacks:       make(map[string]*common.Callback),
			PathItems:       make(map[string]*common.PathItem),
		},
	}

	g.addDefaultResponses()
	g.addDefaultSecuritySchemes()
}

// AddOperation adds an operation to the OpenAPI specification
func (g *OpenAPIGenerator) AddOperation(method, path string, handler interface{}, options ...common.HandlerOption) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.logger != nil {
		g.logger.Debug("Adding OpenAPI operation",
			logger.String("method", method),
			logger.String("path", path),
		)
	}

	handlerType := reflect.TypeOf(handler)
	if handlerType.Kind() != reflect.Func {
		return common.ErrInvalidConfig("handler", fmt.Errorf("handler must be a function"))
	}

	// Analyze handler signature - support both patterns
	serviceTypes, requestType, responseType, err := g.analyzeFlexibleHandlerSignature(handlerType)
	if err != nil {
		if g.logger != nil {
			g.logger.Warn("Failed to analyze handler signature",
				logger.String("method", method),
				logger.String("path", path),
				logger.Error(err),
			)
		}
		return err
	}

	// Convert Steel router path format to OpenAPI format
	openAPIPath := g.convertSteelPathToOpenAPI(path)

	if g.logger != nil {
		g.logger.Debug("Converting path format for OpenAPI",
			logger.String("original_path", path),
			logger.String("openapi_path", openAPIPath),
		)
	}

	operation := g.createOperation(method, path, serviceTypes, requestType, responseType, options...)

	// Use the converted path for OpenAPI spec
	pathItem := g.spec.Paths[openAPIPath]
	if pathItem == nil {
		pathItem = &common.PathItem{}
		g.spec.Paths[openAPIPath] = pathItem
	}

	switch strings.ToUpper(method) {
	case "GET":
		pathItem.Get = operation
	case "POST":
		pathItem.Post = operation
	case "PUT":
		pathItem.Put = operation
	case "DELETE":
		pathItem.Delete = operation
	case "PATCH":
		pathItem.Patch = operation
	case "HEAD":
		pathItem.Head = operation
	case "OPTIONS":
		pathItem.Options = operation
	case "TRACE":
		pathItem.Trace = operation
	default:
		return common.ErrInvalidConfig("method", fmt.Errorf("unsupported HTTP method: %s", method))
	}

	if requestType != nil {
		g.generateSchemaForType(requestType)
	}
	if responseType != nil {
		g.generateSchemaForType(responseType)
	}

	if g.logger != nil {
		g.logger.Info("OpenAPI operation added",
			logger.String("method", method),
			logger.String("original_path", path),
			logger.String("openapi_path", openAPIPath),
			logger.String("operation_id", operation.OperationID),
		)
	}

	if g.metrics != nil {
		g.metrics.Counter("forge.openapi.operations_added").Inc()
	}

	return nil
}

// analyzeFlexibleHandlerSignature analyzes handler signatures for both patterns:
// 1. Pure opinionated: func(ctx Context, req RequestType) (*ResponseType, error)
// 2. Service-aware: func(ctx Context, service1 ServiceType, service2 ServiceType, req RequestType) (*ResponseType, error)
func (g *OpenAPIGenerator) analyzeFlexibleHandlerSignature(handlerType reflect.Type) (serviceTypes []reflect.Type, requestType, responseType reflect.Type, err error) {
	numIn := handlerType.NumIn()
	numOut := handlerType.NumOut()

	// Must have at least 2 inputs (ctx, request) and exactly 2 outputs (response, error)
	if numIn < 2 {
		return nil, nil, nil, fmt.Errorf("handler must have at least 2 parameters: (ctx, request) or (ctx, ...services, request)")
	}

	if numOut != 2 {
		return nil, nil, nil, fmt.Errorf("handler must return (*ResponseType, error)")
	}

	// Validate first parameter (context)
	contextType := handlerType.In(0)
	expectedContextType := reflect.TypeOf((*common.Context)(nil)).Elem()
	if contextType != expectedContextType {
		return nil, nil, nil, fmt.Errorf("first parameter must be forge.Context, got %s", contextType)
	}

	// Validate last output parameter (error)
	errorType := handlerType.Out(1)
	expectedErrorType := reflect.TypeOf((*error)(nil)).Elem()
	if errorType != expectedErrorType {
		return nil, nil, nil, fmt.Errorf("second return value must be error, got %s", errorType)
	}

	// Extract types based on flexible pattern
	requestType = handlerType.In(numIn - 1) // Last input parameter is always request
	responseType = handlerType.Out(0)       // First output is response

	// Extract service types (all parameters between context and request)
	if numIn > 2 {
		// Service-aware pattern: func(ctx, service1, service2, ..., request)
		serviceTypes = make([]reflect.Type, numIn-2)
		for i := 1; i < numIn-1; i++ { // Skip ctx (index 0) and request (last index)
			serviceTypes[i-1] = handlerType.In(i)
		}
	}
	// else: Pure opinionated pattern: func(ctx, request) - serviceTypes remains nil

	// Remove pointer from response type for reflection
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	if g.logger != nil {
		g.logger.Debug("Handler signature analyzed",
			logger.Int("inputs", numIn),
			logger.Int("outputs", numOut),
			logger.Int("services", len(serviceTypes)),
			logger.String("request_type", requestType.String()),
			logger.String("response_type", responseType.String()),
		)
	}

	return serviceTypes, requestType, responseType, nil
}

// // createOperation creates an OpenAPI operation
// func (g *OpenAPIGenerator) createOperation(method, path string, serviceTypes []reflect.Type, requestType, responseType reflect.Type, options ...common.HandlerOption) *common.Operation {
// 	operationID := g.generateOperationID(method, path)
//
// 	operation := &common.Operation{
// 		OperationID: operationID,
// 		Summary:     fmt.Sprintf("%s %s", strings.ToUpper(method), path),
// 		Description: fmt.Sprintf("Handler for %s %s", strings.ToUpper(method), path),
// 		Parameters:  []*common.Parameter{},
// 		Responses:   make(map[string]*common.Response),
// 		Tags:        []string{}, // Initialize as empty array
// 	}
//
// 	// Apply handler options to extract tags
// 	tempInfo := &common.RouteHandlerInfo{
// 		Tags: make(map[string]string),
// 	}
// 	for _, option := range options {
// 		option(tempInfo)
// 	}
//
// 	// Extract OpenAPI tags from the handler info
// 	if openAPITags, exists := tempInfo.Tags["openapi_tags"]; exists {
// 		// Split comma-separated tags
// 		if openAPITags != "" {
// 			operation.Tags = strings.Split(openAPITags, ",")
// 			for i, tag := range operation.Tags {
// 				operation.Tags[i] = strings.TrimSpace(tag)
// 			}
// 		}
// 	}
//
// 	// Fallback: try to extract from "group" tag if openapi_tags not set
// 	if len(operation.Tags) == 0 {
// 		if group, exists := tempInfo.Tags["group"]; exists && group != "" {
// 			operation.Tags = []string{group}
// 		}
// 	}
//
// 	// Default tag if none specified
// 	if len(operation.Tags) == 0 {
// 		operation.Tags = []string{"default"}
// 	}
//
// 	// Generate enhanced summary and description based on tags and method
// 	operation.Summary = g.generateOperationSummary(method, path, tempInfo.Tags)
// 	operation.Description = g.generateOperationDescription(method, path, serviceTypes, tempInfo.Tags)
//
// 	if requestType != nil && requestType.Kind() == reflect.Struct {
// 		operation.Parameters = g.extractParameters(requestType)
//
// 		if g.hasBodyParameter(requestType) && (method == "POST" || method == "PUT" || method == "PATCH") {
// 			operation.RequestBody = g.createRequestBody(requestType)
// 		}
// 	}
//
// 	operation.Responses["200"] = g.createResponse("Success", responseType)
// 	operation.Responses["400"] = &common.Response{
// 		Description: "Bad Request",
// 		Content: map[string]*common.MediaType{
// 			"application/json": {
// 				Schema: &common.Schema{Ref: "#/components/schemas/ErrorResponse"},
// 			},
// 		},
// 	}
// 	operation.Responses["500"] = &common.Response{
// 		Description: "Internal Server Error",
// 		Content: map[string]*common.MediaType{
// 			"application/json": {
// 				Schema: &common.Schema{Ref: "#/components/schemas/ErrorResponse"},
// 			},
// 		},
// 	}
//
// 	return operation
// }

func (g *OpenAPIGenerator) generateOperationSummary(method, path string, tags map[string]string) string {
	operation := strings.ToLower(tags["operation"])
	if operation == "" {
		operation = strings.ToLower(method)
	}

	// Extract resource name from path
	resource := g.extractResourceFromPath(path)

	switch operation {
	case "create":
		return fmt.Sprintf("Create a new %s", resource)
	case "read", "get":
		return fmt.Sprintf("Get %s details", resource)
	case "update", "patch":
		return fmt.Sprintf("Update %s", resource)
	case "delete":
		return fmt.Sprintf("Delete %s", resource)
	case "list":
		return fmt.Sprintf("List %s with filtering and pagination", resource)
	case "upload":
		return fmt.Sprintf("Upload file for %s", resource)
	case "check":
		return "Health check endpoint"
	default:
		return fmt.Sprintf("%s %s", strings.ToUpper(method), path)
	}
}

func (g *OpenAPIGenerator) generateOperationDescription(method, path string, serviceTypes []reflect.Type, tags map[string]string) string {
	operation := strings.ToLower(tags["operation"])
	complexity := tags["complexity"]
	authLevel := tags["auth_level"]

	var serviceNames []string
	for _, serviceType := range serviceTypes {
		serviceName := serviceType.Name()
		if serviceName == "" {
			serviceName = serviceType.String()
		}
		serviceNames = append(serviceNames, serviceName)
	}

	var description string
	if len(serviceNames) > 0 {
		description = fmt.Sprintf("Endpoint for %s operation on %s using services: %s.", operation, path, strings.Join(serviceNames, ", "))
	} else {
		description = fmt.Sprintf("Endpoint for %s operation on %s.", operation, path)
	}

	if complexity != "" {
		description += fmt.Sprintf(" Complexity: %s.", complexity)
	}

	if authLevel != "" {
		description += fmt.Sprintf(" Requires %s level authentication.", authLevel)
	}

	// Add operation-specific descriptions
	switch operation {
	case "create":
		description += " Supports comprehensive validation, file uploads, and complex nested objects."
	case "list":
		description += " Supports advanced filtering, sorting, pagination, and search capabilities."
	case "update":
		description += " Supports partial updates using JSON Patch semantics."
	case "upload":
		description += " Supports multipart file uploads with metadata and categorization."
	}

	return description
}

func (g *OpenAPIGenerator) extractResourceFromPath(path string) string {
	// Extract the main resource name from the path
	// e.g., "/users/:userId" -> "user"
	// e.g., "/companies/:companyId/users" -> "user"

	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part != "" && !strings.HasPrefix(part, ":") && !strings.HasPrefix(part, "{") {
			// Remove 's' suffix for singular form
			if strings.HasSuffix(part, "s") && len(part) > 1 {
				return part[:len(part)-1]
			}
			return part
		}
	}
	return "resource"
}

// extractParameters extracts parameters from request type with comprehensive tag support
// extractParameters extracts parameters from request type with comprehensive tag support
func (g *OpenAPIGenerator) extractParameters(requestType reflect.Type) []*common.Parameter {
	var parameters []*common.Parameter

	// Process all fields including embedded ones
	g.extractParametersRecursive(requestType, "", &parameters)

	return parameters
}

// extractParametersRecursive recursively processes struct fields including embedded structs
func (g *OpenAPIGenerator) extractParametersRecursive(t reflect.Type, prefix string, parameters *[]*common.Parameter) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous fields by recursing into them
		if field.Anonymous {
			g.extractParametersRecursive(field.Type, prefix, parameters)
			continue
		}

		tagInfo := g.parseFieldTags(field)

		// Skip path parameters since they're handled separately
		if tagInfo.Path != "" {
			continue
		}

		// Query parameters
		if tagInfo.Query != "" {
			param := &common.Parameter{
				Name:        tagInfo.Query,
				In:          "query",
				Required:    tagInfo.Required,
				Description: tagInfo.Description,
				Schema:      g.convertTypeToQueryParameterSchema(field.Type, tagInfo),
			}
			if tagInfo.Example != nil {
				param.Example = tagInfo.Example
			}
			if tagInfo.Deprecated {
				param.Deprecated = true
			}
			if tagInfo.AllowEmpty {
				param.AllowEmptyValue = true
			}
			*parameters = append(*parameters, param)
		}

		// Header parameters
		if tagInfo.Header != "" {
			param := &common.Parameter{
				Name:        tagInfo.Header,
				In:          "header",
				Required:    tagInfo.Required,
				Description: tagInfo.Description,
				Schema:      g.convertTypeToQueryParameterSchema(field.Type, tagInfo),
			}
			if tagInfo.Example != nil {
				param.Example = tagInfo.Example
			}
			if tagInfo.Deprecated {
				param.Deprecated = true
			}
			*parameters = append(*parameters, param)
		}

		// Cookie parameters
		if tagInfo.Cookie != "" {
			param := &common.Parameter{
				Name:        tagInfo.Cookie,
				In:          "cookie",
				Required:    tagInfo.Required,
				Description: tagInfo.Description,
				Schema:      g.convertTypeToQueryParameterSchema(field.Type, tagInfo),
			}
			if tagInfo.Example != nil {
				param.Example = tagInfo.Example
			}
			*parameters = append(*parameters, param)
		}
	}
}

// convertTypeToQueryParameterSchema converts a type to schema suitable for query parameters
// Query parameters are always strings in HTTP, so we need special handling
func (g *OpenAPIGenerator) convertTypeToQueryParameterSchema(t reflect.Type, tagInfo *common.FieldTagInfo) *common.Schema {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	schema := &common.Schema{}

	// Apply tag overrides first
	if tagInfo.Type != "" {
		schema.Type = tagInfo.Type
	}
	if tagInfo.Format != "" {
		schema.Format = tagInfo.Format
	}
	if tagInfo.Description != "" {
		schema.Description = tagInfo.Description
	}
	if tagInfo.Pattern != "" {
		schema.Pattern = tagInfo.Pattern
	}
	if tagInfo.Example != nil {
		schema.Example = tagInfo.Example
	}
	if tagInfo.Default != nil {
		schema.Default = tagInfo.Default
	}
	if tagInfo.Deprecated {
		schema.Deprecated = true
	}

	// Apply numeric constraints if present
	if tagInfo.Min != nil {
		schema.Minimum = tagInfo.Min
	}
	if tagInfo.Max != nil {
		schema.Maximum = tagInfo.Max
	}
	if tagInfo.MultipleOf != nil {
		schema.MultipleOf = tagInfo.MultipleOf
	}
	if tagInfo.MinLength != nil {
		schema.MinLength = tagInfo.MinLength
	}
	if tagInfo.MaxLength != nil {
		schema.MaxLength = tagInfo.MaxLength
	}
	if tagInfo.Enum != nil {
		schema.Enum = tagInfo.Enum
	}

	// If type/format already set by tags, return early
	if schema.Type != "" {
		return schema
	}

	// Handle specific well-known types for query parameters
	if g.isTimeType(t) {
		schema.Type = "string"
		if schema.Format == "" {
			schema.Format = "date-time"
		}
		if schema.Description == "" {
			schema.Description = "ISO 8601 date-time format"
		}
		if schema.Example == nil {
			schema.Example = "2023-01-01T12:00:00Z"
		}
		return schema
	}

	if g.isDurationType(t) {
		schema.Type = "string"
		if schema.Format == "" {
			schema.Format = "duration"
		}
		if schema.Description == "" {
			schema.Description = "Duration in Go format (e.g., '1h30m', '5s')"
		}
		if schema.Example == nil {
			schema.Example = "1h30m"
		}
		return schema
	}

	// Handle XID type specifically
	if g.isXIDType(t) {
		schema.Type = "string"
		if schema.Format == "" {
			schema.Format = "xid"
		}
		if schema.Description == "" {
			schema.Description = "XID - globally unique identifier"
		}
		if schema.Example == nil {
			schema.Example = "9m4e2mr0ui3e8a215n4g"
		}
		if schema.Pattern == "" {
			schema.Pattern = "^[0-9a-v]{20}$"
		}
		schema.MinLength = ptrInt(20)
		schema.MaxLength = ptrInt(20)
		return schema
	}

	// Handle UUID types
	if g.isUUIDType(t) {
		schema.Type = "string"
		if schema.Format == "" {
			schema.Format = "uuid"
		}
		if schema.Description == "" {
			schema.Description = "UUID - universally unique identifier"
		}
		if schema.Example == nil {
			schema.Example = "550e8400-e29b-41d4-a716-446655440000"
		}
		if schema.Pattern == "" {
			schema.Pattern = "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
		}
		return schema
	}

	// Handle types that implement text marshaling - treat as strings for query parameters
	if g.implementsTextMarshaler(t) {
		schema.Type = "string"
		if schema.Description == "" {
			schema.Description = fmt.Sprintf("%s as string", t.Name())
		}
		return schema
	}

	// Handle basic Go types for query parameters
	switch t.Kind() {
	case reflect.String:
		schema.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema.Type = "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = "integer"
		schema.Minimum = ptrFloat64(0)
	case reflect.Float32, reflect.Float64:
		schema.Type = "number"
	case reflect.Bool:
		schema.Type = "boolean"
	case reflect.Slice, reflect.Array:
		// For query parameters, arrays are typically comma-separated strings
		schema.Type = "array"
		schema.Items = g.convertTypeToQueryParameterSchema(t.Elem(), &common.FieldTagInfo{})
		// Add style information for array parameters
		if schema.Extensions == nil {
			schema.Extensions = make(map[string]interface{})
		}
		schema.Extensions["x-style"] = "form"
		schema.Extensions["x-explode"] = false
	default:
		// For complex types in query parameters, treat as string
		schema.Type = "string"
		if schema.Description == "" {
			schema.Description = fmt.Sprintf("%s serialized as string", t.Name())
		}
	}

	return schema
}

// hasBodyParameter checks for comprehensive body tags
func (g *OpenAPIGenerator) hasBodyParameter(requestType reflect.Type) bool {
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		tagInfo := g.parseFieldTags(field)

		if tagInfo.Body || tagInfo.JSON != "" || tagInfo.XML != "" || tagInfo.Form != "" {
			return true
		}
	}
	return false
}

// extractRequestBodyField checks if a struct has a "Body" field for request body extraction
func (g *OpenAPIGenerator) extractRequestBodyField(requestType reflect.Type) *reflect.StructField {
	if requestType.Kind() == reflect.Ptr {
		requestType = requestType.Elem()
	}

	if requestType.Kind() != reflect.Struct {
		return nil
	}

	// Look for a field with body tag or named "Body"
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if !field.IsExported() {
			continue
		}

		tagInfo := g.parseFieldTags(field)

		// Check for explicit body tag or field named "Body"
		if tagInfo.Body || field.Name == "Body" {
			if g.logger != nil {
				g.logger.Debug("Found request body field",
					logger.String("struct_name", requestType.Name()),
					logger.String("body_field_type", field.Type.String()),
					logger.String("body_field_name", field.Name),
				)
			}
			return &field
		}
	}

	return nil
}

// createRequestBody creates a request body specification with comprehensive content type support
func (g *OpenAPIGenerator) createRequestBody(requestType reflect.Type) *common.RequestBody {
	if requestType.Kind() == reflect.Ptr {
		requestType = requestType.Elem()
	}

	requestBody := &common.RequestBody{
		Description: fmt.Sprintf("Request body for %s", requestType.Name()),
		Required:    g.isRequestBodyRequired(requestType),
		Content:     make(map[string]*common.MediaType),
	}

	// Check if there's a dedicated Body field
	bodyField := g.extractRequestBodyField(requestType)
	var schemaType reflect.Type

	if bodyField != nil {
		// Use the Body field's type for the schema
		schemaType = bodyField.Type
		if g.logger != nil {
			g.logger.Debug("Using Body field for request schema",
				logger.String("request_type", requestType.Name()),
				logger.String("body_field_type", schemaType.String()),
			)
		}
	} else {
		// Use the entire struct for the schema (existing behavior)
		schemaType = requestType
	}

	hasJSON := false
	hasXML := false
	hasForm := false
	hasMultipart := false

	// Analyze the original struct for content type detection
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		tagInfo := g.parseFieldTags(field)

		if tagInfo.Body || tagInfo.JSON != "" {
			hasJSON = true
		}
		if tagInfo.XML != "" {
			hasXML = true
		}
		if tagInfo.Form != "" {
			hasForm = true
		}
		if tagInfo.Multipart != "" {
			hasMultipart = true
		}
	}

	if !hasJSON && !hasXML && !hasForm && !hasMultipart {
		hasJSON = true
	}

	// Generate schemas using the appropriate type
	if hasJSON {
		requestBody.Content["application/json"] = &common.MediaType{
			Schema: g.convertTypeToSchema(schemaType), // Use schemaType instead of requestType
		}
	}

	if hasXML {
		requestBody.Content["application/xml"] = &common.MediaType{
			Schema: g.convertTypeToSchema(schemaType),
		}
	}

	if hasForm {
		requestBody.Content["application/x-www-form-urlencoded"] = &common.MediaType{
			Schema: g.convertFormSchema(requestType), // Form still uses the full struct
		}
	}

	if hasMultipart {
		requestBody.Content["multipart/form-data"] = &common.MediaType{
			Schema: g.convertFormSchema(requestType), // Multipart still uses the full struct
		}
	}

	return requestBody
}

// convertTypeToSchemaWithTags uses tag information
func (g *OpenAPIGenerator) convertTypeToSchemaWithTags(t reflect.Type, tagInfo *common.FieldTagInfo) *common.Schema {
	schema := g.generateSchemaForType(t)

	if tagInfo.Description != "" {
		schema.Description = tagInfo.Description
	}
	if tagInfo.Title != "" {
		schema.Title = tagInfo.Title
	}
	if tagInfo.Format != "" {
		schema.Format = tagInfo.Format
	}
	if tagInfo.Type != "" {
		schema.Type = tagInfo.Type
	}
	if tagInfo.Pattern != "" {
		schema.Pattern = tagInfo.Pattern
	}
	if tagInfo.Example != nil {
		schema.Example = tagInfo.Example
	}
	if tagInfo.Default != nil {
		schema.Default = tagInfo.Default
	}
	if tagInfo.Deprecated {
		schema.Deprecated = true
	}

	if tagInfo.Min != nil {
		schema.Minimum = tagInfo.Min
	}
	if tagInfo.Max != nil {
		schema.Maximum = tagInfo.Max
	}
	if tagInfo.MultipleOf != nil {
		schema.MultipleOf = tagInfo.MultipleOf
	}

	if tagInfo.MinLength != nil {
		schema.MinLength = tagInfo.MinLength
	}
	if tagInfo.MaxLength != nil {
		schema.MaxLength = tagInfo.MaxLength
	}

	if tagInfo.Enum != nil {
		schema.Enum = tagInfo.Enum
	}

	return schema
}

// generateOperationID generates a unique operation ID
func (g *OpenAPIGenerator) generateOperationID(method, path string) string {
	parts := strings.Split(path, "/")
	var cleanParts []string

	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, ":") {
			part = "By" + strings.Title(part[1:])
		} else if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			part = "By" + strings.Title(part[1:len(part)-1])
		} else {
			part = strings.Title(part)
		}
		cleanParts = append(cleanParts, part)
	}

	pathStr := strings.Join(cleanParts, "")
	return strings.ToLower(method) + pathStr
}

// createResponse creates a response specification
func (g *OpenAPIGenerator) createResponse(description string, responseType reflect.Type) *common.Response {
	response := &common.Response{
		Description: description,
	}

	if responseType == nil {
		return response
	}

	// Remove pointer if present
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	// Check if the response type has a "Body" field
	bodyField := g.extractBodyField(responseType)

	// Determine the schema type - use Body field if present, otherwise use entire struct
	var schemaType reflect.Type
	if bodyField != nil {
		// Use the Body field type as the response schema
		schemaType = bodyField.Type
		if g.logger != nil {
			g.logger.Debug("Using Body field for response schema",
				logger.String("response_type", responseType.Name()),
				logger.String("body_field_type", schemaType.String()),
			)
		}
	} else {
		// Use the entire struct as the response schema
		schemaType = responseType
	}

	// Check if response type has body tags to determine content types
	contentTypes := g.extractResponseContentTypes(responseType)

	if len(contentTypes) == 0 {
		// Default to JSON if no specific content types found
		contentTypes = map[string]bool{"application/json": true}
	}

	response.Content = make(map[string]*common.MediaType)

	// Generate media types based on detected content types
	// FIXED: Use schemaType instead of responseType for schema generation
	for contentType := range contentTypes {
		schema := g.convertTypeToResponseSchema(schemaType, contentType)
		response.Content[contentType] = &common.MediaType{
			Schema: schema,
		}

		// Add examples if available (still check original responseType for tags)
		if examples := g.extractResponseExamples(responseType, contentType); len(examples) > 0 {
			response.Content[contentType].Examples = examples
		}
	}

	return response
}

// convertTypeToSchema converts a Go type to an OpenAPI schema
func (g *OpenAPIGenerator) convertTypeToSchema(t reflect.Type) *common.Schema {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Check for well-known types that should not be referenced
	if schema := g.checkWellKnownTypes(t); schema != nil {
		return schema
	}

	// Check for types that should be treated as strings
	if g.shouldTreatAsString(t) {
		return g.createStringSchemaForType(t)
	}

	typeName := t.Name()

	// Only create references for complex struct types, not for simple types
	if typeName != "" && g.shouldCreateReference(t) {
		if _, exists := g.spec.Components.Schemas[typeName]; exists {
			return &common.Schema{
				Ref: fmt.Sprintf("#/components/schemas/%s", typeName),
			}
		}
	}

	schema := g.generateSchemaForType(t)

	// Only store complex types in components
	if typeName != "" && g.shouldCreateReference(t) && t.Kind() == reflect.Struct {
		g.spec.Components.Schemas[typeName] = schema
		return &common.Schema{
			Ref: fmt.Sprintf("#/components/schemas/%s", typeName),
		}
	}

	return schema
}

// shouldCreateReference determines if a type should be referenced vs inlined
func (g *OpenAPIGenerator) shouldCreateReference(t reflect.Type) bool {
	// Don't create references for:
	// 1. Types that should be treated as primitives
	// 2. Well-known types like time.Time
	// 3. Simple wrapper types

	if t == timeType || t == durationType {
		return false
	}

	if g.shouldTreatAsString(t) {
		return false
	}

	// Only create references for complex structs
	if t.Kind() == reflect.Struct {
		// If struct has only 1-2 fields, consider inlining
		if t.NumField() <= 2 {
			// Check if it's a simple wrapper
			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				if !field.IsExported() {
					continue
				}
				// If it's just wrapping a basic type, don't reference
				if field.Type.Kind() == reflect.String ||
					field.Type.Kind() == reflect.Int ||
					field.Type.Kind() == reflect.Int64 {
					return false
				}
			}
		}
		return true
	}

	return false
}

// createStringSchemaForType creates a string schema with appropriate format for the type
func (g *OpenAPIGenerator) createStringSchemaForType(t reflect.Type) *common.Schema {
	schema := &common.Schema{Type: "string"}

	typeName := t.Name()
	if typeName != "" {
		schema.Description = fmt.Sprintf("%s as string", typeName)

		// Set format based on type name patterns
		lowerName := strings.ToLower(typeName)
		switch {
		case strings.Contains(lowerName, "email"):
			schema.Format = "email"
		case strings.Contains(lowerName, "url") || strings.Contains(lowerName, "uri"):
			schema.Format = "uri"
		case strings.Contains(lowerName, "phone"):
			schema.Format = "phone"
		case strings.Contains(lowerName, "uuid"):
			schema.Format = "uuid"
		case strings.Contains(lowerName, "date"):
			schema.Format = "date"
		case strings.Contains(lowerName, "time"):
			schema.Format = "date-time"
		case strings.Contains(lowerName, "password") || strings.Contains(lowerName, "secret"):
			schema.Format = "password"
		}
	}

	return schema
}

// generateSchemaForType generates an OpenAPI schema for a Go type
func (g *OpenAPIGenerator) generateSchemaForType(t reflect.Type) *common.Schema {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Check for specific well-known types first
	if schema := g.checkWellKnownTypes(t); schema != nil {
		return schema
	}

	// Check for types that should definitely be treated as strings
	if g.shouldTreatAsString(t) {
		return g.createStringSchemaForType(t)
	}

	// Handle types with custom marshaling but unknown output
	if g.implementsJSONMarshaler(t) {
		return g.generateSchemaForCustomMarshalerType(t)
	}

	// Handle basic Go kinds as before...
	switch t.Kind() {
	case reflect.String:
		return &common.Schema{Type: "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &common.Schema{Type: "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &common.Schema{Type: "integer", Minimum: ptrFloat64(0)}
	case reflect.Float32, reflect.Float64:
		return &common.Schema{Type: "number"}
	case reflect.Bool:
		return &common.Schema{Type: "boolean"}
	case reflect.Slice, reflect.Array:
		return &common.Schema{
			Type:  "array",
			Items: g.convertTypeToSchema(t.Elem()),
		}
	case reflect.Map:
		return &common.Schema{
			Type:                 "object",
			AdditionalProperties: g.convertTypeToSchema(t.Elem()),
		}
	case reflect.Struct:
		return g.generateStructSchema(t)
	case reflect.Interface:
		return &common.Schema{Type: "object"}
	default:
		return &common.Schema{Type: "string"}
	}
}

// generateStructSchema generates a schema for a struct type with comprehensive tag support
func (g *OpenAPIGenerator) generateStructSchema(t reflect.Type) *common.Schema {
	schema := &common.Schema{
		Type:       "object",
		Properties: make(map[string]*common.Schema),
		Required:   []string{},
	}

	// Add type information
	if t.Name() != "" {
		schema.Title = t.Name()
		schema.Description = fmt.Sprintf("%s object", t.Name())
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if !field.IsExported() {
			continue
		}

		// Handle embedded/anonymous fields (composition)
		if field.Anonymous {
			g.handleAnonymousField(schema, field)
			continue
		}

		tagInfo := g.parseFieldTags(field)
		fieldName := g.getJSONFieldName(field, tagInfo)

		if fieldName == "-" || g.shouldIgnoreField(field, tagInfo) {
			continue
		}

		fieldSchema := g.convertTypeToSchemaWithTags(field.Type, tagInfo)

		// Add field metadata
		if fieldSchema.Description == "" {
			fieldSchema.Description = g.getFieldDescription(field)
		}

		schema.Properties[fieldName] = fieldSchema

		if tagInfo.Required {
			schema.Required = append(schema.Required, fieldName)
		}
	}

	return schema
}

// handleAnonymousField handles embedded/anonymous fields for composition
func (g *OpenAPIGenerator) handleAnonymousField(schema *common.Schema, field reflect.StructField) {
	fieldType := field.Type
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	if fieldType.Kind() == reflect.Struct {
		// Inline the embedded struct's properties
		embeddedSchema := g.generateStructSchema(fieldType)

		// Merge properties
		for propName, propSchema := range embeddedSchema.Properties {
			if _, exists := schema.Properties[propName]; !exists {
				schema.Properties[propName] = propSchema
			}
		}

		// Merge required fields
		for _, requiredField := range embeddedSchema.Required {
			found := false
			for _, existing := range schema.Required {
				if existing == requiredField {
					found = true
					break
				}
			}
			if !found {
				schema.Required = append(schema.Required, requiredField)
			}
		}
	}
}

// Helper methods
func (g *OpenAPIGenerator) getJSONFieldName(field reflect.StructField, tagInfo *common.FieldTagInfo) string {
	if tagInfo.JSON != "" && tagInfo.JSON != "-" {
		parts := strings.Split(tagInfo.JSON, ",")
		if parts[0] != "" {
			return parts[0]
		}
	}
	return field.Name
}

// checkWellKnownTypes checks for specific well-known types and returns appropriate schemas
func (g *OpenAPIGenerator) checkWellKnownTypes(t reflect.Type) *common.Schema {
	// Handle time.Time
	if t == timeType {
		return &common.Schema{
			Type:        "string",
			Format:      "date-time",
			Description: "RFC3339 formatted date-time",
			Example:     "2023-01-01T12:00:00Z",
		}
	}

	// Handle time.Duration
	if t == durationType {
		return &common.Schema{
			Type:        "string",
			Format:      "duration",
			Description: "Duration in Go format (e.g., '1h30m', '5s')",
			Example:     "1h30m",
			Pattern:     "^[0-9]+(ns|us|µs|ms|s|m|h)+$",
		}
	}

	// Handle xid.ID (by checking package path and type name)
	if t.PkgPath() == "github.com/rs/xid" && t.Name() == "ID" {
		return &common.Schema{
			Type:        "string",
			Format:      "xid",
			Description: "XID - globally unique identifier",
			Example:     "9m4e2mr0ui3e8a215n4g",
			Pattern:     "^[0-9a-v]{20}$",
			MinLength:   ptrInt(20),
			MaxLength:   ptrInt(20),
		}
	}

	// Handle UUID types (common packages)
	if (t.PkgPath() == "github.com/google/uuid" || t.PkgPath() == "github.com/satori/go.uuid") && t.Name() == "UUID" {
		return &common.Schema{
			Type:        "string",
			Format:      "uuid",
			Description: "UUID - universally unique identifier",
			Example:     "550e8400-e29b-41d4-a716-446655440000",
			Pattern:     "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
		}
	}

	// Handle common string-based types
	pkgPath := t.PkgPath()
	typeName := t.Name()

	// Handle ObjectID from MongoDB
	if strings.Contains(pkgPath, "go.mongodb.org/mongo-driver/bson") && typeName == "ObjectID" {
		return &common.Schema{
			Type:        "string",
			Format:      "objectid",
			Description: "MongoDB ObjectID",
			Example:     "507f1f77bcf86cd799439011",
			Pattern:     "^[0-9a-f]{24}$",
			MinLength:   ptrInt(24),
			MaxLength:   ptrInt(24),
		}
	}

	// Handle other common ID types that should be strings
	if strings.HasSuffix(typeName, "ID") || strings.HasSuffix(typeName, "Id") {
		if g.isLikelyStringBasedID(t) {
			return &common.Schema{
				Type:        "string",
				Description: fmt.Sprintf("%s identifier", typeName),
			}
		}
	}

	return nil
}

// isLikelyStringBasedID checks if an ID type is likely string-based
func (g *OpenAPIGenerator) isLikelyStringBasedID(t reflect.Type) bool {
	// Check if type implements json.Marshaler/Unmarshaler
	if reflect.PtrTo(t).Implements(jsonMarshalerType) || t.Implements(jsonMarshalerType) {
		return true
	}
	if reflect.PtrTo(t).Implements(jsonUnmarshalerType) || t.Implements(jsonUnmarshalerType) {
		return true
	}

	// Check if type implements encoding.TextMarshaler/TextUnmarshaler
	if reflect.PtrTo(t).Implements(textMarshalerType) || t.Implements(textMarshalerType) {
		return true
	}
	if reflect.PtrTo(t).Implements(textUnmarshalerType) || t.Implements(textUnmarshalerType) {
		return true
	}

	// Check for string-based types by name patterns
	typeName := t.Name()
	if typeName != "" {
		// Common patterns for string-based types
		stringPatterns := []string{
			"URL", "URI", "Email", "Phone", "Hash", "Token", "Key", "Secret",
			"Code", "Slug", "Tag", "Name", "Title", "Status", "State", "Type",
		}

		for _, pattern := range stringPatterns {
			if strings.Contains(typeName, pattern) && t.Kind() != reflect.Struct {
				return true
			}
		}
	}

	return false
}

func (g *OpenAPIGenerator) shouldIgnoreField(field reflect.StructField, tagInfo *common.FieldTagInfo) bool {
	if tagInfo.JSON == "-" {
		return true
	}

	// Don't ignore Body fields in the OpenAPI schema - they should be documented
	if field.Name == "Body" {
		return false
	}

	// If field is used for parameter binding (path, query, header, cookie) and not body,
	// ignore it in response schema unless it's also tagged for body/json
	if (tagInfo.Path != "" || tagInfo.Query != "" || tagInfo.Header != "" || tagInfo.Cookie != "") &&
		!tagInfo.Body && tagInfo.JSON == "" {
		return true
	}

	return false
}

func (g *OpenAPIGenerator) parseExample(exampleStr string, fieldType reflect.Type) interface{} {
	switch fieldType.Kind() {
	case reflect.String:
		return exampleStr
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val, err := strconv.ParseInt(exampleStr, 10, 64); err == nil {
			return val
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if val, err := strconv.ParseUint(exampleStr, 10, 64); err == nil {
			return val
		}
	case reflect.Float32, reflect.Float64:
		if val, err := strconv.ParseFloat(exampleStr, 64); err == nil {
			return val
		}
	case reflect.Bool:
		if val, err := strconv.ParseBool(exampleStr); err == nil {
			return val
		}
	}
	return exampleStr
}

func (g *OpenAPIGenerator) parseDefault(defaultStr string, fieldType reflect.Type) interface{} {
	return g.parseExample(defaultStr, fieldType)
}

func (g *OpenAPIGenerator) parseOpenAPIExtensions(tagInfo *common.FieldTagInfo, field reflect.StructField) {
	// List of common OpenAPI extension keys
	extensionKeys := []string{
		"x-go-name", "x-go-type", "x-nullable", "x-omitempty", "x-order",
		"x-example", "x-examples", "x-enum-varnames", "x-enum-descriptions",
		"x-go-custom-tag", "x-go-json-ignore", "x-go-struct-tag",
		"x-description", "x-title", "x-deprecated", "x-readonly", "x-writeonly",
		"x-format", "x-pattern", "x-minimum", "x-maximum", "x-min-length", "x-max-length",
	}

	// Check for known extension keys
	for _, key := range extensionKeys {
		if value := field.Tag.Get(key); value != "" {
			tagInfo.Extensions[key] = value
		}
	}

	// Handle specific known extensions that don't start with "x-" but should be included
	if binding := field.Tag.Get("binding"); binding != "" {
		tagInfo.Extensions["x-binding"] = binding
	}
	if swagger := field.Tag.Get("swagger"); swagger != "" {
		tagInfo.Extensions["x-swagger"] = swagger
	}
}

func (g *OpenAPIGenerator) parseValidationTag(tagInfo *common.FieldTagInfo, validate string) {
	if validate == "" {
		return
	}

	rules := strings.Split(validate, ",")
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)

		switch {
		case rule == "required":
			tagInfo.Required = true
		case strings.HasPrefix(rule, "min="):
			if val, err := strconv.ParseFloat(rule[4:], 64); err == nil {
				tagInfo.Min = &val
			}
		case strings.HasPrefix(rule, "max="):
			if val, err := strconv.ParseFloat(rule[4:], 64); err == nil {
				tagInfo.Max = &val
			}
		case strings.HasPrefix(rule, "len="):
			if val, err := strconv.Atoi(rule[4:]); err == nil {
				tagInfo.MinLength = &val
				tagInfo.MaxLength = &val
			}
		case strings.HasPrefix(rule, "oneof="):
			enumStr := rule[6:]
			enumValues := strings.Split(enumStr, " ")
			tagInfo.Enum = make([]interface{}, len(enumValues))
			for i, val := range enumValues {
				tagInfo.Enum[i] = strings.TrimSpace(val)
			}
		case rule == "email":
			tagInfo.Format = "email"
		case rule == "url":
			tagInfo.Format = "uri"
		case rule == "uuid":
			tagInfo.Format = "uuid"
		case rule == "alphanum":
			tagInfo.Pattern = "^[a-zA-Z0-9]+$"
		case rule == "alpha":
			tagInfo.Pattern = "^[a-zA-Z]+$"
		case rule == "numeric":
			tagInfo.Pattern = "^[0-9]+$"
		}
	}
}

func (g *OpenAPIGenerator) isRequestBodyRequired(requestType reflect.Type) bool {
	if requestType.Kind() == reflect.Ptr {
		requestType = requestType.Elem()
	}

	// Check if there's a Body field and if it's required
	bodyField := g.extractRequestBodyField(requestType)
	if bodyField != nil {
		tagInfo := g.parseFieldTags(*bodyField)
		return tagInfo.Required
	}

	// Existing logic for non-Body field structs
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		tagInfo := g.parseFieldTags(field)

		if (tagInfo.Body || tagInfo.JSON != "") && tagInfo.Required {
			return true
		}
	}
	return false
}

func (g *OpenAPIGenerator) convertFormSchema(requestType reflect.Type) *common.Schema {
	schema := &common.Schema{
		Type:       "object",
		Properties: make(map[string]*common.Schema),
		Required:   []string{},
	}

	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)

		if !field.IsExported() {
			continue
		}

		tagInfo := g.parseFieldTags(field)

		if tagInfo.Form == "" && tagInfo.Multipart == "" {
			continue
		}

		fieldName := tagInfo.Form
		if fieldName == "" {
			fieldName = tagInfo.Multipart
		}
		if fieldName == "" {
			fieldName = field.Name
		}

		fieldSchema := g.convertTypeToSchemaWithTags(field.Type, tagInfo)

		if tagInfo.Multipart != "" && field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Uint8 {
			fieldSchema.Type = "string"
			fieldSchema.Format = "binary"
		}

		schema.Properties[fieldName] = fieldSchema

		if tagInfo.Required {
			schema.Required = append(schema.Required, fieldName)
		}
	}

	return schema
}

func (g *OpenAPIGenerator) getFieldDescription(field reflect.StructField) string {
	if desc := field.Tag.Get("description"); desc != "" {
		return desc
	}
	if desc := field.Tag.Get("doc"); desc != "" {
		return desc
	}
	if desc := field.Tag.Get("comment"); desc != "" {
		return desc
	}

	return generateDescriptionFromFieldName(field.Name)
}

func generateDescriptionFromFieldName(fieldName string) string {
	var result []string
	var current strings.Builder

	for i, r := range fieldName {
		if i > 0 && isUpper(r) && (i+1 < len(fieldName) && isLower(rune(fieldName[i+1])) || isLower(rune(fieldName[i-1]))) {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		}
		current.WriteRune(toLower(r))
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	if len(result) == 0 {
		return fieldName
	}

	if len(result) > 0 {
		result[0] = strings.Title(result[0])
	}

	return strings.Join(result, " ")
}

func isUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func isLower(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func toLower(r rune) rune {
	if isUpper(r) {
		return r + ('a' - 'A')
	}
	return r
}

func (g *OpenAPIGenerator) addDefaultResponses() {
	g.spec.Components.Schemas["ErrorResponse"] = &common.Schema{
		Type: "object",
		Properties: map[string]*common.Schema{
			"error": {
				Type: "object",
				Properties: map[string]*common.Schema{
					"code":    {Type: "string", Description: "Error code"},
					"message": {Type: "string", Description: "Error message"},
					"details": {Type: "object", Description: "Additional error details"},
				},
				Required: []string{"code", "message"},
			},
		},
		Required: []string{"error"},
	}

	g.spec.Components.Schemas["SuccessResponse"] = &common.Schema{
		Type: "object",
		Properties: map[string]*common.Schema{
			"success": {Type: "boolean", Description: "Operation success status"},
			"message": {Type: "string", Description: "Success message"},
		},
		Required: []string{"success"},
	}
}

func (g *OpenAPIGenerator) addDefaultSecuritySchemes() {
	g.spec.Components.SecuritySchemes = map[string]*common.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "JWT Bearer token authentication",
		},
		"apiKey": {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-API-Key",
			Description: "API key authentication",
		},
	}
}

// Public methods
func (g *OpenAPIGenerator) GetSpec() *common.OpenAPISpec {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.spec
}

func (g *OpenAPIGenerator) GetSpecJSON() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return json.MarshalIndent(g.spec, "", "  ")
}

func (g *OpenAPIGenerator) UpdateSpec(updater func(*common.OpenAPISpec)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	updater(g.spec)
}

func (g *OpenAPIGenerator) AddSecurityScheme(name string, scheme *common.SecurityScheme) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.spec.Components.SecuritySchemes == nil {
		g.spec.Components.SecuritySchemes = make(map[string]*common.SecurityScheme)
	}
	g.spec.Components.SecuritySchemes[name] = scheme
}

func (g *OpenAPIGenerator) AddTag(tag *common.TagObject) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.spec.Tags = append(g.spec.Tags, tag)
}

// Improved type detection that doesn't assume all marshalers are strings
func (g *OpenAPIGenerator) shouldTreatAsString(t reflect.Type) bool {
	// Handle specific known types first
	if g.checkWellKnownTypes(t) != nil {
		return true // Well-known types are already handled as strings
	}

	// Check for common string-like patterns by name and underlying type
	typeName := t.Name()
	if typeName != "" {
		// Strong indicators it's a string-based type
		if g.isStringLikeType(t, typeName) {
			return true
		}
	}

	// For types implementing marshalers, be more conservative
	if g.implementsJSONMarshaler(t) {
		// Only treat as string if we have strong evidence
		return g.isLikelyStringMarshaler(t, typeName)
	}

	return false
}

func (g *OpenAPIGenerator) isStringLikeType(t reflect.Type, typeName string) bool {
	// Check underlying type
	underlyingKind := t.Kind()
	if underlyingKind == reflect.String {
		return true
	}

	// Common string-like type name patterns
	stringPatterns := []string{
		"ID", "Id", "UUID", "URL", "URI", "Email", "Phone",
		"Hash", "Token", "Key", "Secret", "Code", "Slug",
		"Tag", "Status", "State", "Type", "Kind", "Name",
	}

	for _, pattern := range stringPatterns {
		if strings.Contains(typeName, pattern) {
			return true
		}
	}

	return false
}

func (g *OpenAPIGenerator) isLikelyStringMarshaler(t reflect.Type, typeName string) bool {
	// Conservative approach: only assume string for common patterns
	if strings.HasSuffix(typeName, "ID") || strings.HasSuffix(typeName, "Id") {
		return true
	}

	if strings.Contains(strings.ToLower(typeName), "time") ||
		strings.Contains(strings.ToLower(typeName), "date") {
		return true
	}

	// If underlying type is string, likely still a string
	if t.Kind() == reflect.String {
		return true
	}

	return false
}

func (g *OpenAPIGenerator) generateSchemaForCustomMarshalerType(t reflect.Type) *common.Schema {
	typeName := t.Name()

	// For unknown marshaler types, create a flexible schema
	schema := &common.Schema{
		Description: fmt.Sprintf("Custom type %s with JSON marshaling", typeName),
	}

	// Try to infer from underlying type and name
	if g.isLikelyStringMarshaler(t, typeName) {
		schema.Type = "string"
		schema.Description += " (likely string-based)"
	} else {
		// Use oneOf to allow multiple possible JSON types
		schema.OneOf = []*common.Schema{
			{Type: "string"},
			{Type: "number"},
			{Type: "object"},
			{Type: "array"},
			{Type: "boolean"},
		}
		schema.Description += " (flexible JSON type)"
	}

	return schema
}

func (g *OpenAPIGenerator) implementsJSONMarshaler(t reflect.Type) bool {
	return reflect.PtrTo(t).Implements(jsonMarshalerType) ||
		t.Implements(jsonMarshalerType)
}

// Alternative approach: Runtime inspection (more accurate but has overhead)
func (g *OpenAPIGenerator) inspectMarshalerOutput(t reflect.Type) string {
	// Create zero value and try to marshal it
	if t.Kind() == reflect.Ptr {
		return "unknown" // Can't safely create pointer types
	}

	defer func() {
		if recover() != nil {
			// If marshaling panics, can't determine type
		}
	}()

	zeroValue := reflect.Zero(t)
	if !zeroValue.CanInterface() {
		return "unknown"
	}

	if marshaler, ok := zeroValue.Interface().(json.Marshaler); ok {
		data, err := marshaler.MarshalJSON()
		if err != nil {
			return "unknown"
		}

		// Parse the JSON to determine its type
		var parsed interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return "unknown"
		}

		switch parsed.(type) {
		case string:
			return "string"
		case float64:
			return "number"
		case bool:
			return "boolean"
		case []interface{}:
			return "array"
		case map[string]interface{}:
			return "object"
		case nil:
			return "null"
		default:
			return "unknown"
		}
	}

	return "unknown"
}

func (g *OpenAPIGenerator) isTimeType(t reflect.Type) bool {
	return t == timeType
}

func (g *OpenAPIGenerator) isDurationType(t reflect.Type) bool {
	return t == durationType
}

func (g *OpenAPIGenerator) isXIDType(t reflect.Type) bool {
	return t.PkgPath() == "github.com/rs/xid" && t.Name() == "ID"
}

func (g *OpenAPIGenerator) isUUIDType(t reflect.Type) bool {
	return (t.PkgPath() == "github.com/google/uuid" || t.PkgPath() == "github.com/satori/go.uuid") &&
		t.Name() == "UUID"
}

func (g *OpenAPIGenerator) implementsTextMarshaler(t reflect.Type) bool {
	return reflect.PtrTo(t).Implements(textMarshalerType) || t.Implements(textMarshalerType)
}

// Update the parseFieldTags method to better handle query tag parsing
func (g *OpenAPIGenerator) parseFieldTags(field reflect.StructField) *common.FieldTagInfo {
	tagInfo := &common.FieldTagInfo{
		Extensions: make(map[string]interface{}),
	}

	// Parameter location tags
	tagInfo.Path = field.Tag.Get("path")
	tagInfo.Query = field.Tag.Get("query")
	tagInfo.Header = field.Tag.Get("header")
	tagInfo.Cookie = field.Tag.Get("cookie")
	tagInfo.Body = field.Tag.Get("body") != ""
	tagInfo.JSON = field.Tag.Get("json")
	tagInfo.XML = field.Tag.Get("xml")
	tagInfo.Form = field.Tag.Get("form")
	tagInfo.Multipart = field.Tag.Get("multipart")

	// If no explicit query tag, check if field should be inferred as query parameter
	if tagInfo.Query == "" && tagInfo.JSON != "" && tagInfo.JSON != "-" &&
		tagInfo.Path == "" && tagInfo.Header == "" && tagInfo.Cookie == "" && !tagInfo.Body {
		// Check if the JSON tag looks like it could be a query parameter
		jsonParts := strings.Split(tagInfo.JSON, ",")
		if len(jsonParts) > 0 && jsonParts[0] != "" {
			// Only infer as query if it's not marked for body serialization
			tagInfo.Query = jsonParts[0]
		}
	}

	// Required validation
	tagInfo.Required = field.Tag.Get("required") == "true" ||
		strings.Contains(field.Tag.Get("validate"), "required")

	// Numeric constraints
	if minStr := field.Tag.Get("min"); minStr != "" {
		if min, err := strconv.ParseFloat(minStr, 64); err == nil {
			tagInfo.Min = &min
		}
	}
	if maxStr := field.Tag.Get("max"); maxStr != "" {
		if max, err := strconv.ParseFloat(maxStr, 64); err == nil {
			tagInfo.Max = &max
		}
	}

	// String length constraints
	if minLenStr := field.Tag.Get("minlength"); minLenStr != "" {
		if minLen, err := strconv.Atoi(minLenStr); err == nil {
			tagInfo.MinLength = &minLen
		}
	}
	if maxLenStr := field.Tag.Get("maxlength"); maxLenStr != "" {
		if maxLen, err := strconv.Atoi(maxLenStr); err == nil {
			tagInfo.MaxLength = &maxLen
		}
	}

	tagInfo.Pattern = field.Tag.Get("pattern")

	if enumStr := field.Tag.Get("enum"); enumStr != "" {
		enumValues := strings.Split(enumStr, ",")
		tagInfo.Enum = make([]interface{}, len(enumValues))
		for i, val := range enumValues {
			tagInfo.Enum[i] = strings.TrimSpace(val)
		}
	}

	if multipleOfStr := field.Tag.Get("multipleOf"); multipleOfStr != "" {
		if multipleOf, err := strconv.ParseFloat(multipleOfStr, 64); err == nil {
			tagInfo.MultipleOf = &multipleOf
		}
	}

	tagInfo.Description = g.getFieldDescription(field)
	tagInfo.Title = field.Tag.Get("title")
	tagInfo.Deprecated = field.Tag.Get("deprecated") == "true"

	if exampleStr := field.Tag.Get("example"); exampleStr != "" {
		tagInfo.Example = g.parseExample(exampleStr, field.Type)
	}

	tagInfo.Format = field.Tag.Get("format")
	tagInfo.Type = field.Tag.Get("type")
	tagInfo.AllowEmpty = field.Tag.Get("allowEmpty") == "true"

	if defaultStr := field.Tag.Get("default"); defaultStr != "" {
		tagInfo.Default = g.parseDefault(defaultStr, field.Type)
	}

	g.parseValidationTag(tagInfo, field.Tag.Get("validate"))
	g.parseOpenAPIExtensions(tagInfo, field)

	return tagInfo
}

func ptrFloat64(f float64) *float64 {
	return &f
}

func ptrInt(i int) *int {
	return &i
}

func ptrBool(b bool) *bool {
	return &b
}
