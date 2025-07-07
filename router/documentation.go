package router

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/xraph/forge/logger"
)

// DocumentationConfig represents configuration for API documentation
type DocumentationConfig struct {
	// OpenAPI configuration
	OpenAPI OpenAPIConfig `json:"openapi"`

	// AsyncAPI configuration
	AsyncAPI AsyncAPIConfig `json:"asyncapi"`

	// General settings
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Version     string      `json:"version"`
	Contact     ContactInfo `json:"contact,omitempty"`
	License     LicenseInfo `json:"license,omitempty"`

	// Server configuration
	Servers []ServerInfo `json:"servers,omitempty"`

	// Security definitions
	SecurityDefinitions map[string]SecurityScheme `json:"security_definitions,omitempty"`

	// Features
	EnableAutoGeneration bool `json:"enable_auto_generation"`
	EnableSwaggerUI      bool `json:"enable_swagger_ui"`
	EnableReDoc          bool `json:"enable_redoc"`
	EnableAsyncAPIUI     bool `json:"enable_asyncapi_ui"`
	EnablePostman        bool `json:"enable_postman"`

	// Paths
	SwaggerUIPath    string `json:"swagger_ui_path"`
	ReDocPath        string `json:"redoc_path"`
	AsyncAPIUIPath   string `json:"asyncapi_ui_path"`
	OpenAPISpecPath  string `json:"openapi_spec_path"`
	AsyncAPISpecPath string `json:"asyncapi_spec_path"`
	PostmanPath      string `json:"postman_path"`

	// Advanced features
	EnableSchemaValidation bool `json:"enable_schema_validation"`
	EnableExamples         bool `json:"enable_examples"`
	EnableCodeGeneration   bool `json:"enable_code_generation"`
	GroupByTags            bool `json:"group_by_tags"`
	SortRoutes             bool `json:"sort_routes"`
}

// ResponseDefinition defines an OpenAPI response for configuration
type ResponseDefinition struct {
	Description string                      `json:"description"`
	Schema      *Schema                     `json:"schema,omitempty"`
	Headers     map[string]HeaderDefinition `json:"headers,omitempty"`
	Examples    map[string]interface{}      `json:"examples,omitempty"`
	Content     map[string]*MediaType       `json:"content,omitempty"`
}

// HeaderDefinition defines an OpenAPI header for configuration
type HeaderDefinition struct {
	Description string      `json:"description,omitempty"`
	Type        string      `json:"type"`
	Format      string      `json:"format,omitempty"`
	Example     interface{} `json:"example,omitempty"`
}

// OpenAPIConfig represents OpenAPI-specific configuration
type OpenAPIConfig struct {
	Version string `json:"version"` // 3.0.0, 3.1.0

	// Schema generation options
	UseJSONTags          bool `json:"use_json_tags"`
	UseValidationTags    bool `json:"use_validation_tags"`
	GenerateExamples     bool `json:"generate_examples"`
	IncludePrivateFields bool `json:"include_private_fields"`

	// Component options
	ComponentPrefix     string `json:"component_prefix"`
	GenerateComponents  bool   `json:"generate_components"`
	ReferenceComponents bool   `json:"reference_components"`

	// Response options
	DefaultResponses   map[string]ResponseDefinition `json:"default_responses,omitempty"`
	IncludeErrorModels bool                          `json:"include_error_models"`

	// Security options
	DefaultSecurity []SecurityRequirement `json:"default_security,omitempty"`
}

// AsyncAPIConfig represents AsyncAPI-specific configuration
type AsyncAPIConfig struct {
	Version string `json:"version"` // 2.6.0, 3.0.0

	// Protocol bindings
	DefaultProtocol string `json:"default_protocol"` // ws, sse, http

	// Message options
	GenerateMessageExamples bool `json:"generate_message_examples"`
	IncludeMessageHeaders   bool `json:"include_message_headers"`

	// Channel options
	GroupChannelsByTags bool `json:"group_channels_by_tags"`

	// Server options
	DefaultServer string `json:"default_server"`
}

// ContactInfo represents contact information
type ContactInfo struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// LicenseInfo represents license information
type LicenseInfo struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// ServerInfo represents server information
type ServerInfo struct {
	URL         string                 `json:"url"`
	Description string                 `json:"description,omitempty"`
	Variables   map[string]ServerVar   `json:"variables,omitempty"`
	Protocol    string                 `json:"protocol,omitempty"` // For AsyncAPI
	Bindings    map[string]interface{} `json:"bindings,omitempty"`
}

// ServerVar represents server variable
type ServerVar struct {
	Default     string   `json:"default"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// SecurityScheme represents security scheme
type SecurityScheme struct {
	Type             string `json:"type"`
	Description      string `json:"description,omitempty"`
	Name             string `json:"name,omitempty"`
	In               string `json:"in,omitempty"`
	Scheme           string `json:"scheme,omitempty"`
	BearerFormat     string `json:"bearerFormat,omitempty"`
	OpenIDConnectURL string `json:"openIdConnectUrl,omitempty"`

	// OAuth2 flows
	Flows *OAuth2Flows `json:"flows,omitempty"`
}

// OAuth2Flows represents OAuth2 flows
type OAuth2Flows struct {
	Implicit          *OAuth2Flow `json:"implicit,omitempty"`
	Password          *OAuth2Flow `json:"password,omitempty"`
	ClientCredentials *OAuth2Flow `json:"clientCredentials,omitempty"`
	AuthorizationCode *OAuth2Flow `json:"authorizationCode,omitempty"`
}

// OAuth2Flow represents OAuth2 flow
type OAuth2Flow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

// DocumentationGenerator handles automatic documentation generation
type DocumentationGenerator struct {
	config DocumentationConfig
	logger logger.Logger
	mu     sync.RWMutex

	// OpenAPI components
	openAPISpec *OpenAPISpec
	components  map[string]*Schema
	schemas     map[string]*Schema
	examples    map[string]interface{}

	// AsyncAPI components
	asyncAPISpec *AsyncAPISpec
	channels     map[string]*Channel
	messages     map[string]*Message

	// Route analysis
	routes    []EnhancedRouteInfo
	endpoints map[string]*EndpointInfo

	// Schema cache
	schemaCache map[reflect.Type]*Schema
	typeCache   map[string]reflect.Type
}

// EnhancedRouteInfo represents enhanced route information
type EnhancedRouteInfo struct {
	RouteInfo

	// OpenAPI specific
	OpenAPIDoc  *OpenAPIOperation     `json:"openapi,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"request_body,omitempty"`
	Responses   map[string]Response   `json:"responses,omitempty"`
	Security    []SecurityRequirement `json:"security,omitempty"`

	// AsyncAPI specific (for WebSocket/SSE)
	AsyncAPIDoc *AsyncAPIOperation `json:"asyncapi,omitempty"`
	Messages    []*Message         `json:"messages,omitempty"`

	// Additional metadata
	Group      string                 `json:"group,omitempty"`
	Tags       []string               `json:"tags,omitempty"`
	Examples   map[string]interface{} `json:"examples,omitempty"`
	Deprecated bool                   `json:"deprecated,omitempty"`
}

// EndpointInfo represents detailed endpoint information
type EndpointInfo struct {
	Path   string `json:"path"`
	Method string `json:"method"`

	// HTTP specific
	ContentTypes []string `json:"content_types,omitempty"`
	Consumes     []string `json:"consumes,omitempty"`
	Produces     []string `json:"produces,omitempty"`

	// WebSocket/SSE specific
	Protocol    string `json:"protocol,omitempty"`
	Subprotocol string `json:"subprotocol,omitempty"`

	// Analysis results
	RequestSchema  *Schema `json:"request_schema,omitempty"`
	ResponseSchema *Schema `json:"response_schema,omitempty"`

	// Examples
	RequestExamples  map[string]interface{} `json:"request_examples,omitempty"`
	ResponseExamples map[string]interface{} `json:"response_examples,omitempty"`
}

// AsyncAPI types
type AsyncAPISpec struct {
	AsyncAPI           string                     `json:"asyncapi"`
	ID                 string                     `json:"id,omitempty"`
	Info               InfoObject                 `json:"info"`
	Servers            map[string]*AsyncAPIServer `json:"servers,omitempty"`
	DefaultContentType string                     `json:"defaultContentType,omitempty"`
	Channels           map[string]*Channel        `json:"channels,omitempty"`
	Components         *AsyncAPIComponents        `json:"components,omitempty"`
	Tags               []Tag                      `json:"tags,omitempty"`
	ExternalDocs       *ExternalDocs              `json:"externalDocs,omitempty"`
}

// AsyncAPIServer represents AsyncAPI server
type AsyncAPIServer struct {
	URL             string                 `json:"url"`
	Description     string                 `json:"description,omitempty"`
	Protocol        string                 `json:"protocol"`
	ProtocolVersion string                 `json:"protocolVersion,omitempty"`
	Variables       map[string]ServerVar   `json:"variables,omitempty"`
	Security        []SecurityRequirement  `json:"security,omitempty"`
	Bindings        map[string]interface{} `json:"bindings,omitempty"`
	Tags            []Tag                  `json:"tags,omitempty"`
}

// Channel represents AsyncAPI channel
type Channel struct {
	Description string                 `json:"description,omitempty"`
	Subscribe   *AsyncAPIOperation     `json:"subscribe,omitempty"`
	Publish     *AsyncAPIOperation     `json:"publish,omitempty"`
	Parameters  map[string]*Parameter  `json:"parameters,omitempty"`
	Bindings    map[string]interface{} `json:"bindings,omitempty"`
	Tags        []Tag                  `json:"tags,omitempty"`
}

// AsyncAPIOperation represents AsyncAPI operation
type AsyncAPIOperation struct {
	OperationID  string                   `json:"operationId,omitempty"`
	Summary      string                   `json:"summary,omitempty"`
	Description  string                   `json:"description,omitempty"`
	Security     []SecurityRequirement    `json:"security,omitempty"`
	Tags         []Tag                    `json:"tags,omitempty"`
	ExternalDocs *ExternalDocs            `json:"externalDocs,omitempty"`
	Bindings     map[string]interface{}   `json:"bindings,omitempty"`
	Traits       []AsyncAPIOperationTrait `json:"traits,omitempty"`
	Message      *MessageRef              `json:"message,omitempty"`
}

// AsyncAPIOperationTrait represents operation trait
type AsyncAPIOperationTrait struct {
	OperationID  string                 `json:"operationId,omitempty"`
	Summary      string                 `json:"summary,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Security     []SecurityRequirement  `json:"security,omitempty"`
	Tags         []Tag                  `json:"tags,omitempty"`
	ExternalDocs *ExternalDocs          `json:"externalDocs,omitempty"`
	Bindings     map[string]interface{} `json:"bindings,omitempty"`
}

// Message represents AsyncAPI message
type Message struct {
	MessageID     string                 `json:"messageId,omitempty"`
	Headers       *Schema                `json:"headers,omitempty"`
	Payload       *Schema                `json:"payload,omitempty"`
	CorrelationID *CorrelationID         `json:"correlationId,omitempty"`
	ContentType   string                 `json:"contentType,omitempty"`
	Name          string                 `json:"name,omitempty"`
	Title         string                 `json:"title,omitempty"`
	Summary       string                 `json:"summary,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Tags          []Tag                  `json:"tags,omitempty"`
	ExternalDocs  *ExternalDocs          `json:"externalDocs,omitempty"`
	Bindings      map[string]interface{} `json:"bindings,omitempty"`
	Examples      []MessageExample       `json:"examples,omitempty"`
	Traits        []MessageTrait         `json:"traits,omitempty"`
}

// MessageRef represents message reference
type MessageRef struct {
	Reference string     `json:"$ref,omitempty"`
	OneOf     []*Message `json:"oneOf,omitempty"`
}

// MessageTrait represents message trait
type MessageTrait struct {
	MessageID     string                 `json:"messageId,omitempty"`
	Headers       *Schema                `json:"headers,omitempty"`
	CorrelationID *CorrelationID         `json:"correlationId,omitempty"`
	ContentType   string                 `json:"contentType,omitempty"`
	Name          string                 `json:"name,omitempty"`
	Title         string                 `json:"title,omitempty"`
	Summary       string                 `json:"summary,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Tags          []Tag                  `json:"tags,omitempty"`
	ExternalDocs  *ExternalDocs          `json:"externalDocs,omitempty"`
	Bindings      map[string]interface{} `json:"bindings,omitempty"`
}

// MessageExample represents message example
type MessageExample struct {
	Headers map[string]interface{} `json:"headers,omitempty"`
	Payload interface{}            `json:"payload,omitempty"`
	Name    string                 `json:"name,omitempty"`
	Summary string                 `json:"summary,omitempty"`
}

// CorrelationID represents correlation ID
type CorrelationID struct {
	Description string `json:"description,omitempty"`
	Location    string `json:"location"`
}

// AsyncAPIComponents represents AsyncAPI components
type AsyncAPIComponents struct {
	Schemas           map[string]*Schema                 `json:"schemas,omitempty"`
	Messages          map[string]*Message                `json:"messages,omitempty"`
	SecuritySchemes   map[string]*SecurityScheme         `json:"securitySchemes,omitempty"`
	Parameters        map[string]*Parameter              `json:"parameters,omitempty"`
	CorrelationIDs    map[string]*CorrelationID          `json:"correlationIds,omitempty"`
	OperationTraits   map[string]*AsyncAPIOperationTrait `json:"operationTraits,omitempty"`
	MessageTraits     map[string]*MessageTrait           `json:"messageTraits,omitempty"`
	ServerBindings    map[string]interface{}             `json:"serverBindings,omitempty"`
	ChannelBindings   map[string]interface{}             `json:"channelBindings,omitempty"`
	OperationBindings map[string]interface{}             `json:"operationBindings,omitempty"`
	MessageBindings   map[string]interface{}             `json:"messageBindings,omitempty"`
}

// Tag represents tag
type Tag struct {
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty"`
}

// ExternalDocs represents external documentation
type ExternalDocs struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

// InfoObject represents info object
type InfoObject struct {
	Title          string       `json:"title"`
	Version        string       `json:"version"`
	Description    string       `json:"description,omitempty"`
	TermsOfService string       `json:"termsOfService,omitempty"`
	Contact        *ContactInfo `json:"contact,omitempty"`
	License        *LicenseInfo `json:"license,omitempty"`
}

// RequestBody represents request body
type RequestBody struct {
	Description string                `json:"description,omitempty"`
	Content     map[string]*MediaType `json:"content"`
	Required    bool                  `json:"required,omitempty"`
}

// MediaType represents media type
type MediaType struct {
	Schema   *Schema              `json:"schema,omitempty"`
	Example  interface{}          `json:"example,omitempty"`
	Examples map[string]*Example  `json:"examples,omitempty"`
	Encoding map[string]*Encoding `json:"encoding,omitempty"`
}

// Example represents example
type Example struct {
	Summary       string      `json:"summary,omitempty"`
	Description   string      `json:"description,omitempty"`
	Value         interface{} `json:"value,omitempty"`
	ExternalValue string      `json:"externalValue,omitempty"`
}

// Encoding represents encoding
type Encoding struct {
	ContentType   string             `json:"contentType,omitempty"`
	Headers       map[string]*Header `json:"headers,omitempty"`
	Style         string             `json:"style,omitempty"`
	Explode       bool               `json:"explode,omitempty"`
	AllowReserved bool               `json:"allowReserved,omitempty"`
}

// NewDocumentationGenerator creates a new documentation generator
func NewDocumentationGenerator(config DocumentationConfig, logger logger.Logger) *DocumentationGenerator {
	return &DocumentationGenerator{
		config:      config,
		logger:      logger,
		components:  make(map[string]*Schema),
		schemas:     make(map[string]*Schema),
		examples:    make(map[string]interface{}),
		channels:    make(map[string]*Channel),
		messages:    make(map[string]*Message),
		routes:      make([]EnhancedRouteInfo, 0),
		endpoints:   make(map[string]*EndpointInfo),
		schemaCache: make(map[reflect.Type]*Schema),
		typeCache:   make(map[string]reflect.Type),
	}
}

// AnalyzeRoutes analyzes routes and generates documentation
func (g *DocumentationGenerator) AnalyzeRoutes(routes []RouteInfo) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.routes = g.routes[:0] // Clear existing routes

	for _, route := range routes {
		enhanced := g.analyzeRoute(route)
		g.routes = append(g.routes, enhanced)
	}

	// Generate OpenAPI spec
	if g.config.EnableAutoGeneration {
		if err := g.generateOpenAPISpec(); err != nil {
			return fmt.Errorf("failed to generate OpenAPI spec: %w", err)
		}

		if err := g.generateAsyncAPISpec(); err != nil {
			return fmt.Errorf("failed to generate AsyncAPI spec: %w", err)
		}
	}

	g.logger.Info("Documentation analysis completed",
		logger.Int("routes", len(g.routes)),
		logger.Int("schemas", len(g.schemas)),
		logger.Int("channels", len(g.channels)),
	)

	return nil
}

// analyzeRoute analyzes a single route
func (g *DocumentationGenerator) analyzeRoute(route RouteInfo) EnhancedRouteInfo {
	enhanced := EnhancedRouteInfo{
		RouteInfo:  route,
		Parameters: make([]Parameter, 0),
		Responses:  make(map[string]Response),
		Security:   make([]SecurityRequirement, 0),
		Messages:   make([]*Message, 0),
		Examples:   make(map[string]interface{}),
		Tags:       make([]string, 0),
	}

	// Extract path parameters
	enhanced.Parameters = g.extractPathParameters(route.Pattern)

	// Analyze metadata for additional information
	if route.Metadata != nil {
		g.analyzeMetadata(&enhanced, route.Metadata)
	}

	// Determine content types
	enhanced = g.inferContentTypes(enhanced)

	// Generate default responses
	enhanced.Responses = g.generateDefaultResponses(enhanced)

	// Check if this is a WebSocket or SSE endpoint
	if g.isAsyncEndpoint(route) {
		enhanced.AsyncAPIDoc = g.generateAsyncAPIOperation(enhanced)
		enhanced.Messages = g.generateMessages(enhanced)
	} else {
		// Generate OpenAPI operation - generateOpenAPIOperation will handle nil OpenAPIDoc
		enhanced.OpenAPIDoc = g.generateOpenAPIOperation(enhanced)
	}

	return enhanced
}

// extractPathParameters extracts path parameters from route pattern
func (g *DocumentationGenerator) extractPathParameters(pattern string) []Parameter {
	var parameters []Parameter

	// Extract chi-style parameters like {id} or {id:regex}
	re := regexp.MustCompile(`\{([^}:]+)(?::([^}]+))?\}`)
	matches := re.FindAllStringSubmatch(pattern, -1)

	for _, match := range matches {
		paramName := match[1]
		paramType := "string"

		// Try to infer type from regex pattern
		if len(match) > 2 && match[2] != "" {
			paramType = g.inferTypeFromRegex(match[2])
		}

		param := Parameter{
			Name:        paramName,
			In:          "path",
			Required:    true,
			Description: fmt.Sprintf("Path parameter: %s", paramName),
			Schema: &Schema{
				Type: paramType,
			},
		}

		parameters = append(parameters, param)
	}

	return parameters
}

// inferTypeFromRegex infers parameter type from regex pattern
func (g *DocumentationGenerator) inferTypeFromRegex(regex string) string {
	// Common patterns
	switch {
	case strings.Contains(regex, "\\d") || regex == "[0-9]+":
		return "integer"
	case strings.Contains(regex, "uuid"):
		return "string"
	case strings.Contains(regex, "email"):
		return "string"
	default:
		return "string"
	}
}

// analyzeMetadata analyzes route metadata for documentation
func (g *DocumentationGenerator) analyzeMetadata(enhanced *EnhancedRouteInfo, metadata map[string]interface{}) {
	if summary, ok := metadata["summary"].(string); ok {
		enhanced.OpenAPIDoc = &OpenAPIOperation{Summary: summary}
	}

	if description, ok := metadata["description"].(string); ok {
		if enhanced.OpenAPIDoc == nil {
			enhanced.OpenAPIDoc = &OpenAPIOperation{}
		}
		enhanced.OpenAPIDoc.Description = description
	}

	if tags, ok := metadata["tags"].([]string); ok {
		enhanced.Tags = tags
	}

	if deprecated, ok := metadata["deprecated"].(bool); ok {
		enhanced.Deprecated = deprecated
	}

	if group, ok := metadata["group"].(string); ok {
		enhanced.Group = group
	}
}

// inferContentTypes infers content types for the route
func (g *DocumentationGenerator) inferContentTypes(enhanced EnhancedRouteInfo) EnhancedRouteInfo {
	method := strings.ToUpper(enhanced.Method)

	// Default content types based on method
	switch method {
	case "GET", "HEAD", "DELETE":
		// Usually no request body
	case "POST", "PUT", "PATCH":
		// Usually JSON request body
		if enhanced.RequestBody == nil {
			enhanced.RequestBody = &RequestBody{
				Description: "Request body",
				Content: map[string]*MediaType{
					"application/json": {
						Schema: &Schema{
							Type: "object",
						},
					},
				},
			}
		}
	}

	// Default response content type
	if _, ok := enhanced.Responses["200"]; !ok {
		enhanced.Responses["200"] = Response{
			Description: "Success",
			Content: map[string]*MediaType{
				"application/json": {
					Schema: &Schema{
						Type: "object",
					},
				},
			},
		}
	}

	return enhanced
}

// generateDefaultResponses generates default responses for a route
func (g *DocumentationGenerator) generateDefaultResponses(enhanced EnhancedRouteInfo) map[string]Response {
	responses := make(map[string]Response)

	// Copy existing responses
	for code, response := range enhanced.Responses {
		responses[code] = response
	}

	// Add default responses if configured
	for code, response := range g.config.OpenAPI.DefaultResponses {
		if _, exists := responses[code]; !exists {
			responses[code] = Response{
				Description: response.Description,
				Content: map[string]*MediaType{
					"application/json": {
						Schema: response.Schema,
					},
				},
			}
		}
	}

	// Add common error responses
	if g.config.OpenAPI.IncludeErrorModels {
		errorResponses := map[string]Response{
			"400": {
				Description: "Bad Request",
				Content: map[string]*MediaType{
					"application/json": {
						Schema: g.getErrorSchema(),
					},
				},
			},
			"401": {
				Description: "Unauthorized",
				Content: map[string]*MediaType{
					"application/json": {
						Schema: g.getErrorSchema(),
					},
				},
			},
			"403": {
				Description: "Forbidden",
				Content: map[string]*MediaType{
					"application/json": {
						Schema: g.getErrorSchema(),
					},
				},
			},
			"404": {
				Description: "Not Found",
				Content: map[string]*MediaType{
					"application/json": {
						Schema: g.getErrorSchema(),
					},
				},
			},
			"500": {
				Description: "Internal Server Error",
				Content: map[string]*MediaType{
					"application/json": {
						Schema: g.getErrorSchema(),
					},
				},
			},
		}

		for code, response := range errorResponses {
			if _, exists := responses[code]; !exists {
				responses[code] = response
			}
		}
	}

	return responses
}

// getErrorSchema returns the standard error schema
func (g *DocumentationGenerator) getErrorSchema() *Schema {
	return &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"error": {
				Type:        "string",
				Description: "Error message",
			},
			"code": {
				Type:        "integer",
				Description: "Error code",
			},
			"details": {
				Type:        "object",
				Description: "Error details",
			},
		},
		Required: []string{"error", "code"},
	}
}

// isAsyncEndpoint checks if the route is for WebSocket or SSE
func (g *DocumentationGenerator) isAsyncEndpoint(route RouteInfo) bool {
	// Check metadata for protocol type
	if metadata := route.Metadata; metadata != nil {
		if protocol, ok := metadata["protocol"].(string); ok {
			return protocol == "websocket" || protocol == "sse"
		}
	}

	// Check pattern for WebSocket/SSE indicators
	pattern := strings.ToLower(route.Pattern)
	return strings.Contains(pattern, "ws") ||
		strings.Contains(pattern, "websocket") ||
		strings.Contains(pattern, "sse") ||
		strings.Contains(pattern, "stream")
}

// generateOpenAPIOperation generates OpenAPI operation
func (g *DocumentationGenerator) generateOpenAPIOperation(enhanced EnhancedRouteInfo) *OpenAPIOperation {
	operation := &OpenAPIOperation{
		OperationID: g.generateOperationID(enhanced),
		Tags:        enhanced.Tags,
		Parameters:  enhanced.Parameters,
		Responses:   enhanced.Responses,
		Security:    enhanced.Security,
		Deprecated:  enhanced.Deprecated,
	}

	// Safely access OpenAPIDoc fields, handling nil case
	if enhanced.OpenAPIDoc != nil {
		operation.Summary = enhanced.OpenAPIDoc.Summary
		operation.Description = enhanced.OpenAPIDoc.Description
	}

	// Add request body if present
	if enhanced.RequestBody != nil {
		operation.RequestBody = enhanced.RequestBody
	}

	return operation
}

// generateAsyncAPIOperation generates AsyncAPI operation
func (g *DocumentationGenerator) generateAsyncAPIOperation(enhanced EnhancedRouteInfo) *AsyncAPIOperation {
	operation := &AsyncAPIOperation{
		OperationID: g.generateOperationID(enhanced),
		Tags:        g.convertToAsyncAPITags(enhanced.Tags),
		Security:    enhanced.Security,
	}

	// Safely access AsyncAPIDoc fields, handling nil case
	if enhanced.AsyncAPIDoc != nil {
		operation.Summary = enhanced.AsyncAPIDoc.Summary
		operation.Description = enhanced.AsyncAPIDoc.Description
	}

	// Add message reference
	if len(enhanced.Messages) > 0 {
		operation.Message = &MessageRef{
			OneOf: enhanced.Messages,
		}
	}

	return operation
}

// generateOperationID generates a unique operation ID
func (g *DocumentationGenerator) generateOperationID(enhanced EnhancedRouteInfo) string {
	method := strings.ToLower(enhanced.Method)
	pattern := strings.ReplaceAll(enhanced.Pattern, "/", "_")
	pattern = strings.ReplaceAll(pattern, "{", "")
	pattern = strings.ReplaceAll(pattern, "}", "")
	pattern = strings.Trim(pattern, "_")

	if pattern == "" {
		return method + "_root"
	}

	return method + "_" + pattern
}

// convertToAsyncAPITags converts string tags to AsyncAPI tags
func (g *DocumentationGenerator) convertToAsyncAPITags(tags []string) []Tag {
	asyncTags := make([]Tag, len(tags))
	for i, tag := range tags {
		asyncTags[i] = Tag{Name: tag}
	}
	return asyncTags
}

// generateMessages generates messages for AsyncAPI
func (g *DocumentationGenerator) generateMessages(enhanced EnhancedRouteInfo) []*Message {
	messages := make([]*Message, 0)

	// Generate default messages for WebSocket/SSE
	if g.isAsyncEndpoint(enhanced.RouteInfo) {
		// Subscribe message (client -> server)
		subscribeMsg := &Message{
			MessageID:   enhanced.Name + "_subscribe",
			Name:        "Subscribe",
			Title:       "Subscribe to " + enhanced.Pattern,
			Description: "Client subscription message",
			ContentType: "application/json",
			Payload: &Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"type": {
						Type:        "string",
						Description: "Message type",
						Example:     "subscribe",
					},
					"channel": {
						Type:        "string",
						Description: "Channel to subscribe to",
						Example:     enhanced.Pattern,
					},
				},
			},
		}
		messages = append(messages, subscribeMsg)

		// Event message (server -> client)
		eventMsg := &Message{
			MessageID:   enhanced.Name + "_event",
			Name:        "Event",
			Title:       "Event from " + enhanced.Pattern,
			Description: "Server event message",
			ContentType: "application/json",
			Payload: &Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"type": {
						Type:        "string",
						Description: "Event type",
						Example:     "event",
					},
					"data": {
						Type:        "object",
						Description: "Event data",
					},
					"timestamp": {
						Type:        "string",
						Format:      "date-time",
						Description: "Event timestamp",
					},
				},
			},
		}
		messages = append(messages, eventMsg)
	}

	return messages
}

// generateOpenAPISpec generates the complete OpenAPI specification
func (g *DocumentationGenerator) generateOpenAPISpec() error {
	spec := &OpenAPISpec{
		OpenAPI: g.config.OpenAPI.Version,
		Info: InfoObject{
			Title:       g.config.Title,
			Version:     g.config.Version,
			Description: g.config.Description,
			Contact:     &g.config.Contact,
			License:     &g.config.License,
		},
		Servers: make([]ServerInfo, len(g.config.Servers)),
		Paths:   make(map[string]PathItem),
		Components: &Components{
			Schemas:         g.schemas,
			SecuritySchemes: g.convertSecuritySchemes(),
		},
		Security: g.config.OpenAPI.DefaultSecurity,
		Tags:     g.generateTags(),
	}

	// Copy servers
	copy(spec.Servers, g.config.Servers)

	// Generate paths
	for _, route := range g.routes {
		if route.OpenAPIDoc != nil {
			g.addPathToSpec(spec, route)
		}
	}

	g.openAPISpec = spec
	return nil
}

// generateAsyncAPISpec generates the complete AsyncAPI specification
func (g *DocumentationGenerator) generateAsyncAPISpec() error {
	spec := &AsyncAPISpec{
		AsyncAPI: g.config.AsyncAPI.Version,
		Info: InfoObject{
			Title:       g.config.Title,
			Version:     g.config.Version,
			Description: g.config.Description,
			Contact:     &g.config.Contact,
			License:     &g.config.License,
		},
		Servers:  g.convertToAsyncAPIServers(),
		Channels: make(map[string]*Channel),
		Components: &AsyncAPIComponents{
			Schemas:         g.schemas,
			Messages:        g.messages,
			SecuritySchemes: g.convertSecuritySchemes(),
		},
		Tags: g.generateTags(),
	}

	// Generate channels
	for _, route := range g.routes {
		if route.AsyncAPIDoc != nil {
			g.addChannelToSpec(spec, route)
		}
	}

	g.asyncAPISpec = spec
	return nil
}

// Helper methods for spec generation
func (g *DocumentationGenerator) convertSecuritySchemes() map[string]*SecurityScheme {
	schemes := make(map[string]*SecurityScheme)
	for name, scheme := range g.config.SecurityDefinitions {
		schemes[name] = &scheme
	}
	return schemes
}

func (g *DocumentationGenerator) convertToAsyncAPIServers() map[string]*AsyncAPIServer {
	servers := make(map[string]*AsyncAPIServer)
	for i, server := range g.config.Servers {
		name := fmt.Sprintf("server_%d", i)
		if server.Protocol == "" {
			server.Protocol = g.config.AsyncAPI.DefaultProtocol
		}
		servers[name] = &AsyncAPIServer{
			URL:         server.URL,
			Description: server.Description,
			Protocol:    server.Protocol,
			Variables:   server.Variables,
			Bindings:    server.Bindings,
		}
	}
	return servers
}

func (g *DocumentationGenerator) generateTags() []Tag {
	tagMap := make(map[string]bool)
	for _, route := range g.routes {
		for _, tag := range route.Tags {
			tagMap[tag] = true
		}
	}

	tags := make([]Tag, 0, len(tagMap))
	for tag := range tagMap {
		tags = append(tags, Tag{Name: tag})
	}

	return tags
}

func (g *DocumentationGenerator) addPathToSpec(spec *OpenAPISpec, route EnhancedRouteInfo) {
	path := route.Pattern
	method := strings.ToLower(route.Method)

	pathItem, exists := spec.Paths[path]
	if !exists {
		pathItem = PathItem{}
	}

	operation := Operation{
		Summary:     route.OpenAPIDoc.Summary,
		Description: route.OpenAPIDoc.Description,
		OperationID: route.OpenAPIDoc.OperationID,
		Tags:        route.Tags,
		Parameters:  route.Parameters,
		Responses:   route.Responses,
		Security:    route.Security,
		Deprecated:  route.Deprecated,
	}

	if route.RequestBody != nil {
		operation.RequestBody = route.RequestBody
	}

	// Add operation to path item
	switch method {
	case "get":
		pathItem.Get = &operation
	case "post":
		pathItem.Post = &operation
	case "put":
		pathItem.Put = &operation
	case "patch":
		pathItem.Patch = &operation
	case "delete":
		pathItem.Delete = &operation
	case "head":
		pathItem.Head = &operation
	case "options":
		pathItem.Options = &operation
	}

	spec.Paths[path] = pathItem
}

func (g *DocumentationGenerator) addChannelToSpec(spec *AsyncAPISpec, route EnhancedRouteInfo) {
	channel := &Channel{
		Description: route.AsyncAPIDoc.Description,
		Subscribe:   route.AsyncAPIDoc,
		Tags:        route.AsyncAPIDoc.Tags,
	}

	spec.Channels[route.Pattern] = channel
}

// GetOpenAPISpec returns the generated OpenAPI specification
func (g *DocumentationGenerator) GetOpenAPISpec() *OpenAPISpec {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.openAPISpec
}

// GetAsyncAPISpec returns the generated AsyncAPI specification
func (g *DocumentationGenerator) GetAsyncAPISpec() *AsyncAPISpec {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.asyncAPISpec
}

// GetOpenAPIJSON returns the OpenAPI spec as JSON
func (g *DocumentationGenerator) GetOpenAPIJSON() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.openAPISpec == nil {
		return nil, fmt.Errorf("OpenAPI spec not generated")
	}

	return json.MarshalIndent(g.openAPISpec, "", "  ")
}

// GetAsyncAPIJSON returns the AsyncAPI spec as JSON
func (g *DocumentationGenerator) GetAsyncAPIJSON() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.asyncAPISpec == nil {
		return nil, fmt.Errorf("AsyncAPI spec not generated")
	}

	return json.MarshalIndent(g.asyncAPISpec, "", "  ")
}

// DefaultDocumentationConfig Default documentation configuration
func DefaultDocumentationConfig() DocumentationConfig {
	return DocumentationConfig{
		OpenAPI: OpenAPIConfig{
			Version:              "3.0.0",
			UseJSONTags:          true,
			UseValidationTags:    true,
			GenerateExamples:     true,
			IncludePrivateFields: false,
			ComponentPrefix:      "Forge",
			GenerateComponents:   true,
			ReferenceComponents:  true,
			IncludeErrorModels:   true,
			DefaultResponses: map[string]ResponseDefinition{
				"200": {
					Description: "Success",
					Schema: &Schema{
						Type: "object",
					},
				},
			},
		},
		AsyncAPI: AsyncAPIConfig{
			Version:                 "2.6.0",
			DefaultProtocol:         "ws",
			GenerateMessageExamples: true,
			IncludeMessageHeaders:   true,
			GroupChannelsByTags:     true,
		},
		Title:       "Forge API",
		Description: "API documentation generated by Forge framework",
		Version:     "1.0.0",
		Contact: ContactInfo{
			Name:  "API Support",
			Email: "support@example.com",
		},
		License: LicenseInfo{
			Name: "MIT",
			URL:  "https://opensource.org/licenses/MIT",
		},
		EnableAutoGeneration:   true,
		EnableSwaggerUI:        true,
		EnableReDoc:            true,
		EnableAsyncAPIUI:       true,
		EnablePostman:          true,
		SwaggerUIPath:          "/docs",
		ReDocPath:              "/redoc",
		AsyncAPIUIPath:         "/asyncapi",
		OpenAPISpecPath:        "/openapi.json",  // FIXED: Added default path
		AsyncAPISpecPath:       "/asyncapi.json", // FIXED: Added default path
		PostmanPath:            "/postman.json",
		EnableSchemaValidation: true,
		EnableExamples:         true,
		EnableCodeGeneration:   true,
		GroupByTags:            true,
		SortRoutes:             true,
	}
}
