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

	// Streaming extension features
	Streaming *StreamingSpec
}

// StreamingSpec represents streaming extension features extracted from AsyncAPI.
type StreamingSpec struct {
	// Feature flags indicating what's available
	EnableRooms    bool
	EnableChannels bool
	EnablePresence bool
	EnableTyping   bool
	EnableHistory  bool

	// Room operations and schemas
	Rooms *RoomOperations

	// Presence tracking
	Presence *PresenceOperations

	// Typing indicators
	Typing *TypingOperations

	// Pub/sub channels
	Channels *ChannelOperations
}

// RoomOperations defines room-related message schemas and operations.
type RoomOperations struct {
	// Path for the room WebSocket endpoint
	Path string

	// Parameters for the path (e.g., roomId)
	Parameters []Parameter

	// Message schemas
	JoinSchema    *Schema // Client request to join room
	LeaveSchema   *Schema // Client request to leave room
	SendSchema    *Schema // Client message to room
	ReceiveSchema *Schema // Server message from room

	// Member event schemas
	MemberJoinSchema  *Schema // Member joined notification
	MemberLeaveSchema *Schema // Member left notification

	// History configuration
	HistoryEnabled bool
	HistorySchema  *Schema // History query/response schema
}

// PresenceOperations defines presence tracking schemas and operations.
type PresenceOperations struct {
	// Path for the presence WebSocket endpoint
	Path string

	// Status update schema (client -> server)
	UpdateSchema *Schema

	// Presence event schema (server -> client)
	EventSchema *Schema

	// Available statuses
	Statuses []string // e.g., ["online", "away", "busy", "offline"]
}

// TypingOperations defines typing indicator schemas and operations.
type TypingOperations struct {
	// Path for the typing WebSocket endpoint
	Path string

	// Parameters for the path (e.g., roomId)
	Parameters []Parameter

	// Typing start schema
	StartSchema *Schema

	// Typing stop schema
	StopSchema *Schema

	// Timeout duration for auto-stop (in milliseconds)
	TimeoutMs int
}

// ChannelOperations defines pub/sub channel schemas and operations.
type ChannelOperations struct {
	// Path for the channel WebSocket endpoint
	Path string

	// Parameters for the path (e.g., channelId)
	Parameters []Parameter

	// Subscribe schema
	SubscribeSchema *Schema

	// Unsubscribe schema
	UnsubscribeSchema *Schema

	// Publish schema
	PublishSchema *Schema

	// Message received schema
	MessageSchema *Schema
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

	// Path parameters (e.g., roomId, channelId)
	Parameters []Parameter

	// Message schemas
	SendSchema    *Schema // Client -> Server
	ReceiveSchema *Schema // Server -> Client

	// Additional message types for multiplexed connections
	MessageTypes map[string]*Schema // message type -> schema

	// Security
	Security []SecurityRequirement

	// Metadata
	Metadata map[string]any

	// Streaming extension features (if this endpoint supports them)
	StreamingFeatures *WebSocketStreamingFeatures
}

// WebSocketStreamingFeatures indicates which streaming features this endpoint supports.
type WebSocketStreamingFeatures struct {
	// Feature flags
	SupportsRooms    bool
	SupportsPresence bool
	SupportsTyping   bool
	SupportsChannels bool
	SupportsHistory  bool

	// Feature-specific configurations
	RoomConfig     *RoomFeatureConfig
	PresenceConfig *PresenceFeatureConfig
	TypingConfig   *TypingFeatureConfig
	ChannelConfig  *ChannelFeatureConfig
}

// RoomFeatureConfig configures room-related features for a WebSocket endpoint.
type RoomFeatureConfig struct {
	// Maximum rooms a user can join
	MaxRoomsPerUser int

	// Maximum members per room
	MaxMembersPerRoom int

	// Whether to broadcast member events
	BroadcastMemberEvents bool
}

// PresenceFeatureConfig configures presence tracking for a WebSocket endpoint.
type PresenceFeatureConfig struct {
	// Heartbeat interval in milliseconds
	HeartbeatIntervalMs int

	// Idle timeout before marking as away (in milliseconds)
	IdleTimeoutMs int
}

// TypingFeatureConfig configures typing indicators for a WebSocket endpoint.
type TypingFeatureConfig struct {
	// Auto-stop timeout in milliseconds
	TimeoutMs int

	// Debounce interval in milliseconds
	DebounceMs int
}

// ChannelFeatureConfig configures pub/sub channels for a WebSocket endpoint.
type ChannelFeatureConfig struct {
	// Maximum channels a user can subscribe to
	MaxChannelsPerUser int

	// Whether to support channel patterns/wildcards
	SupportPatterns bool
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

// GetType returns the type of endpoint.
func (e *Endpoint) GetType() EndpointType {
	return EndpointTypeREST
}

// GetType returns the type of endpoint.
func (e *WebSocketEndpoint) GetType() EndpointType {
	return EndpointTypeWebSocket
}

// GetType returns the type of endpoint.
func (e *SSEEndpoint) GetType() EndpointType {
	return EndpointTypeSSE
}

// GetType returns the type of endpoint.
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

// APIStats returns statistics about the API spec.
type APIStats struct {
	TotalEndpoints   int
	RESTEndpoints    int
	WebSocketCount   int
	SSECount         int
	SecuredEndpoints int
	Tags             []string
	UpdatedAt        time.Time

	// Streaming features
	HasRooms    bool
	HasPresence bool
	HasTyping   bool
	HasChannels bool
	HasHistory  bool
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

	// Check streaming features
	if spec.Streaming != nil {
		stats.HasRooms = spec.Streaming.EnableRooms
		stats.HasPresence = spec.Streaming.EnablePresence
		stats.HasTyping = spec.Streaming.EnableTyping
		stats.HasChannels = spec.Streaming.EnableChannels
		stats.HasHistory = spec.Streaming.EnableHistory
	}

	return stats
}

// HasStreamingFeatures returns true if any streaming features are enabled.
func (spec *APISpec) HasStreamingFeatures() bool {
	if spec.Streaming == nil {
		return false
	}

	return spec.Streaming.EnableRooms ||
		spec.Streaming.EnableChannels ||
		spec.Streaming.EnablePresence ||
		spec.Streaming.EnableTyping
}

// HasRooms returns true if room support is enabled.
func (spec *APISpec) HasRooms() bool {
	return spec.Streaming != nil && spec.Streaming.EnableRooms
}

// HasPresence returns true if presence tracking is enabled.
func (spec *APISpec) HasPresence() bool {
	return spec.Streaming != nil && spec.Streaming.EnablePresence
}

// HasTyping returns true if typing indicators are enabled.
func (spec *APISpec) HasTyping() bool {
	return spec.Streaming != nil && spec.Streaming.EnableTyping
}

// HasChannels returns true if pub/sub channels are enabled.
func (spec *APISpec) HasChannels() bool {
	return spec.Streaming != nil && spec.Streaming.EnableChannels
}

// HasHistory returns true if message history is enabled.
func (spec *APISpec) HasHistory() bool {
	return spec.Streaming != nil && spec.Streaming.EnableHistory
}
