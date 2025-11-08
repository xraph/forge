package client

import "time"

// APISpec represents the complete API specification in an intermediate representation.
type APISpec struct {
	Info          APIInfo
	Servers       []Server
	Endpoints     []Endpoint
	WebSockets    []WebSocketEndpoint
	SSEs          []SSEEndpoint
	WebTransports []WebTransportEndpoint
	Schemas       map[string]*Schema
	Security      []SecurityScheme
	Tags          []Tag
}

// APIInfo contains metadata about the API.
type APIInfo struct {
	Title       string
	Version     string
	Description string
	Contact     *Contact
	License     *License
}

// Contact represents contact information.
type Contact struct {
	Name  string
	URL   string
	Email string
}

// License represents license information.
type License struct {
	Name string
	URL  string
}

// Server represents an API server.
type Server struct {
	URL         string
	Description string
	Variables   map[string]ServerVariable
}

// ServerVariable represents a variable in server URL.
type ServerVariable struct {
	Default     string
	Description string
	Enum        []string
}

// Endpoint represents a REST API endpoint.
type Endpoint struct {
	ID          string
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	OperationID string
	Deprecated  bool

	// Parameters
	PathParams   []Parameter
	QueryParams  []Parameter
	HeaderParams []Parameter

	// Request/Response
	RequestBody  *RequestBody
	Responses    map[int]*Response
	DefaultError *Response

	// Security
	Security []SecurityRequirement

	// Metadata
	Metadata map[string]any
}

// WebSocketEndpoint represents a WebSocket endpoint.
type WebSocketEndpoint struct {
	ID          string
	Path        string
	Summary     string
	Description string
	Tags        []string

	// Message schemas
	SendSchema    *Schema // Client -> Server
	ReceiveSchema *Schema // Server -> Client

	// Security
	Security []SecurityRequirement

	// Metadata
	Metadata map[string]any
}

// SSEEndpoint represents a Server-Sent Events endpoint.
type SSEEndpoint struct {
	ID          string
	Path        string
	Summary     string
	Description string
	Tags        []string

	// Event schemas (event name -> schema)
	EventSchemas map[string]*Schema

	// Security
	Security []SecurityRequirement

	// Metadata
	Metadata map[string]any
}

// WebTransportEndpoint represents a WebTransport endpoint.
type WebTransportEndpoint struct {
	ID          string
	Path        string
	Summary     string
	Description string
	Tags        []string

	// Stream schemas
	UniStreamSchema *StreamSchema // Unidirectional streams
	BiStreamSchema  *StreamSchema // Bidirectional streams
	DatagramSchema  *Schema       // Unreliable datagrams

	// Security
	Security []SecurityRequirement

	// Metadata
	Metadata map[string]any
}

// StreamSchema represents a streaming data schema.
type StreamSchema struct {
	SendSchema    *Schema // Client -> Server
	ReceiveSchema *Schema // Server -> Client
}

// Parameter represents a request parameter.
type Parameter struct {
	Name        string
	In          string // "path", "query", "header"
	Description string
	Required    bool
	Deprecated  bool
	Schema      *Schema
	Example     any
}

// RequestBody represents a request body.
type RequestBody struct {
	Description string
	Required    bool
	Content     map[string]*MediaType // content-type -> media type
}

// Response represents an API response.
type Response struct {
	Description string
	Content     map[string]*MediaType // content-type -> media type
	Headers     map[string]*Parameter
}

// MediaType represents a media type with schema.
type MediaType struct {
	Schema   *Schema
	Example  any
	Examples map[string]*Example
}

// Example represents an example value.
type Example struct {
	Summary     string
	Description string
	Value       any
}

// Schema represents a data schema.
type Schema struct {
	Type        string // "object", "array", "string", "number", "integer", "boolean", "null"
	Format      string // "date-time", "email", "uuid", etc.
	Description string
	Required    []string // For object types
	Properties  map[string]*Schema
	Items       *Schema // For array types
	Enum        []any   // For enum types
	Default     any
	Example     any
	Nullable    bool
	ReadOnly    bool
	WriteOnly   bool
	MinLength   *int
	MaxLength   *int
	Minimum     *float64
	Maximum     *float64
	Pattern     string
	Ref         string // Reference to another schema (e.g., "#/components/schemas/User")

	// Polymorphism
	OneOf         []*Schema
	AnyOf         []*Schema
	AllOf         []*Schema
	Discriminator *Discriminator

	// Additional properties
	AdditionalProperties any // bool or *Schema
}

// Discriminator supports polymorphism.
type Discriminator struct {
	PropertyName string
	Mapping      map[string]string // value -> schema reference
}

// SecurityScheme represents an authentication/authorization scheme.
type SecurityScheme struct {
	Type             string // "apiKey", "http", "oauth2", "openIdConnect"
	Name             string // Scheme name
	Description      string
	In               string            // "query", "header", "cookie" (for apiKey)
	Scheme           string            // "bearer", "basic" (for http)
	BearerFormat     string            // "JWT" (for http bearer)
	Flows            *OAuthFlows       // For oauth2
	OpenIDConnectURL string            // For openIdConnect
	CustomHeaders    map[string]string // Custom headers
}

// OAuthFlows defines OAuth 2.0 flows.
type OAuthFlows struct {
	Implicit          *OAuthFlow
	Password          *OAuthFlow
	ClientCredentials *OAuthFlow
	AuthorizationCode *OAuthFlow
}

// OAuthFlow defines a single OAuth 2.0 flow.
type OAuthFlow struct {
	AuthorizationURL string
	TokenURL         string
	RefreshURL       string
	Scopes           map[string]string
}

// SecurityRequirement represents a security requirement for an operation.
type SecurityRequirement struct {
	SchemeName string
	Scopes     []string
}

// Tag represents an API tag for grouping.
type Tag struct {
	Name        string
	Description string
}

// EndpointType represents the type of endpoint.
type EndpointType string

const (
	EndpointTypeREST         EndpointType = "REST"
	EndpointTypeWebSocket    EndpointType = "WebSocket"
	EndpointTypeSSE          EndpointType = "SSE"
	EndpointTypeWebTransport EndpointType = "WebTransport"
)

// GetEndpointType returns the type of endpoint.
func (e *Endpoint) GetType() EndpointType {
	return EndpointTypeREST
}

// GetEndpointType returns the type of endpoint.
func (e *WebSocketEndpoint) GetType() EndpointType {
	return EndpointTypeWebSocket
}

// GetEndpointType returns the type of endpoint.
func (e *SSEEndpoint) GetType() EndpointType {
	return EndpointTypeSSE
}

// GetEndpointType returns the type of endpoint.
func (e *WebTransportEndpoint) GetType() EndpointType {
	return EndpointTypeWebTransport
}

// ResolveSchemaRef resolves a schema reference in the spec.
func (spec *APISpec) ResolveSchemaRef(ref string) *Schema {
	// Simple reference resolution: #/components/schemas/SchemaName
	if len(ref) == 0 || spec.Schemas == nil {
		return nil
	}

	// Extract schema name from reference
	const prefix = "#/components/schemas/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		schemaName := ref[len(prefix):]

		return spec.Schemas[schemaName]
	}

	return nil
}

// ValidationOptions for API spec validation.
type ValidationOptions struct {
	RequireOperationIDs bool
	RequireDescriptions bool
	RequireExamples     bool
	RequireSecurity     bool
}

// Validate validates the API spec.
func (spec *APISpec) Validate(opts ValidationOptions) []ValidationError {
	var errors []ValidationError

	// Validate endpoints
	for i, endpoint := range spec.Endpoints {
		if opts.RequireOperationIDs && endpoint.OperationID == "" {
			errors = append(errors, ValidationError{
				Type:    "endpoint",
				Path:    endpoint.Path,
				Message: "missing operation ID",
				Index:   i,
			})
		}

		if opts.RequireDescriptions && endpoint.Description == "" {
			errors = append(errors, ValidationError{
				Type:    "endpoint",
				Path:    endpoint.Path,
				Message: "missing description",
				Index:   i,
			})
		}

		if opts.RequireSecurity && len(endpoint.Security) == 0 && len(spec.Security) == 0 {
			errors = append(errors, ValidationError{
				Type:    "endpoint",
				Path:    endpoint.Path,
				Message: "no security requirements defined",
				Index:   i,
			})
		}
	}

	return errors
}

// ValidationError represents a validation error.
type ValidationError struct {
	Type    string
	Path    string
	Message string
	Index   int
}

// Error implements error interface.
func (e ValidationError) Error() string {
	return e.Message
}

// Stats returns statistics about the API spec.
type APIStats struct {
	TotalEndpoints   int
	RESTEndpoints    int
	WebSocketCount   int
	SSECount         int
	SecuredEndpoints int
	Tags             []string
	UpdatedAt        time.Time
}

// GetStats returns statistics about the API spec.
func (spec *APISpec) GetStats() APIStats {
	stats := APIStats{
		TotalEndpoints: len(spec.Endpoints) + len(spec.WebSockets) + len(spec.SSEs),
		RESTEndpoints:  len(spec.Endpoints),
		WebSocketCount: len(spec.WebSockets),
		SSECount:       len(spec.SSEs),
		UpdatedAt:      time.Now(),
	}

	// Count secured endpoints
	for _, endpoint := range spec.Endpoints {
		if len(endpoint.Security) > 0 {
			stats.SecuredEndpoints++
		}
	}

	for _, ws := range spec.WebSockets {
		if len(ws.Security) > 0 {
			stats.SecuredEndpoints++
		}
	}

	for _, sse := range spec.SSEs {
		if len(sse.Security) > 0 {
			stats.SecuredEndpoints++
		}
	}

	// Collect unique tags
	tagSet := make(map[string]bool)
	for _, tag := range spec.Tags {
		tagSet[tag.Name] = true
	}

	for tag := range tagSet {
		stats.Tags = append(stats.Tags, tag)
	}

	return stats
}
