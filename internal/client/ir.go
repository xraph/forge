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
	Entities      map[string]*EntityRef
	Security      []SecurityScheme
	Tags          []Tag

	// RoutingTypes holds the named types that are NOT entities but sit on a
	// path to one: a paginated envelope (`PageOrder{items: []Order}`), or an
	// intermediate hop (`Order -> Shipment -> Carrier`, where Shipment carries
	// no identity of its own).
	//
	// They are kept out of Entities rather than merged into it because
	// `spec.Entities[name]` is read at several call sites as the question "is
	// this type an entity" -- registerStreamBindingEntities skips a binding
	// whose type is already there, and endpoint resolution writes it. An entry
	// with no identity answering yes to that question is a defect waiting for
	// whoever writes the next reader, and the ordering that makes it safe today
	// is invisible from those call sites.
	//
	// The row shape is the same as an entity's, minus identity, so EntityRef is
	// reused with IDField always empty. The two maps are disjoint by
	// construction: resolveEntityFields builds this one as the useful types
	// MINUS the entities, and it is the only writer.
	//
	// Both maps are emitted into the one `entities` table the browser runtime
	// reads, where a row with no idField means "walk me, never store me".
	RoutingTypes map[string]*EntityRef

	// PrunedSchemas holds the component schemas Apply removed as unreachable,
	// keyed by the name they had in the document.
	//
	// They are kept rather than dropped because one question about this client
	// cannot be answered from this client alone. Two services fronted by one
	// gateway can each declare a type that strips to the same name --
	// `TwinOS_WorkspaceListResponse` and `Portal_WorkspaceListResponse` both
	// become `WorkspaceListResponse` -- and StripPrefix's collision check runs
	// after the filter, by which point the sibling's schema is gone and the
	// pair looks like a single unambiguous name. The consumer finds out
	// instead, by unioning the entity tables and getting one typename with two
	// field maps, where spread order decides which one wins and the losing
	// service's records stop normalizing.
	//
	// Nil when no filter ran, which is also when the question does not arise:
	// an unfiltered client carries every name the document declared, so the
	// ordinary check already sees both.
	PrunedSchemas map[string]*Schema

	// Warnings collected while building this specification: things that did
	// not stop the parse but that silently reduce what the generated client
	// can do (an entity whose declared id field does not exist in its response
	// schema; a stream binding naming an entity type no schema describes).
	//
	// A language generator is expected to surface these alongside its own
	// warnings. Silent degradation is the failure mode this whole path exists
	// to avoid: a cache key that never matches looks exactly like a cache that
	// simply is not very effective.
	Warnings []string

	// Streaming extension features
	Streaming *StreamingSpec

	// Kind records which document family this spec was parsed from. MergeSpecs
	// orders sources by this rather than by argument order, so that
	// `--from-spec a.json --from-spec b.json` and the reverse produce identical
	// output. A spec built by Introspector carries SourceIntrospection and
	// ranks with OpenAPI, because it is authoritative for REST the same way.
	Kind SourceKind
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
	CookieParams []Parameter

	// Request/Response
	RequestBody  *RequestBody
	Responses    map[int]*Response
	DefaultError *Response

	// Security
	Security []SecurityRequirement

	// Authorization is the endpoint's declared roles and permissions, read
	// from the x-forge-authz extension. Nil when none were declared.
	Authorization *Authorization

	// Metadata
	Metadata map[string]any

	// Cache metadata
	Entity    *EntityRef
	CacheTags TagSet

	// StaleTime is how long the client should consider this endpoint's result
	// fresh, in milliseconds. Zero means undeclared, and the client falls
	// through to its own default.
	StaleTime int64

	// RootType is the typename of this endpoint's success response -- or of its
	// ELEMENTS, when that response is a bare array, since a typename propagates
	// through an array unchanged. Empty when the response has no named type.
	//
	// It is not the same thing as Entity.Type and must not be conflated with
	// it. For `GET /orders` returning `PageOrder{items: []Order, total: int}`,
	// Entity.Type is "Order" -- the thing being cached and tagged -- while
	// RootType is "PageOrder", the type the response document actually IS. The
	// runtime looks the root up in the entities table to learn which of its
	// properties to descend, so handing it "Order" there would have it read
	// Order's field edges against an envelope's properties and find nothing.
	//
	// Populated for every endpoint with a named response type, entity or not:
	// it describes the document, and describing it costs nothing when there is
	// no entity beneath.
	RootType string
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

	// Authorization is the endpoint's declared roles and permissions, read
	// from the x-forge-authz extension. Nil when none were declared.
	Authorization *Authorization

	// Metadata
	Metadata map[string]any

	// Cache metadata
	StreamBindings []StreamBinding

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

	// Authorization is the endpoint's declared roles and permissions, read
	// from the x-forge-authz extension. Nil when none were declared.
	Authorization *Authorization

	// Metadata
	Metadata map[string]any

	// Cache metadata
	StreamBindings []StreamBinding
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

	// Authorization is the endpoint's declared roles and permissions, read
	// from the x-forge-authz extension. Nil when none were declared.
	Authorization *Authorization

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
	In          string // "path", "query", "header", "cookie"
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
	Deprecated  bool
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

	// Extensions
	Extensions map[string]any
}

// Discriminator supports polymorphism.
type Discriminator struct {
	PropertyName string
	Mapping      map[string]string // value -> schema reference
}

// SecurityScheme represents an authentication/authorization scheme.
//
// `Key` and `ParamName` are separate because they are separate things, and
// conflating them is what made the generated client send `X-API-Key` for every
// apiKey scheme whatever the document declared. `Key` identifies the scheme
// within the document and is what an endpoint's security requirement refers
// to; `ParamName` is the name that goes on the wire.
type SecurityScheme struct {
	// Key is the `components.securitySchemes` map key, e.g. "sessionAuth".
	Key string
	// ParamName is the wire name for an apiKey scheme, e.g. "session_id".
	// Empty for every other type: an http scheme's location is the
	// Authorization header by definition, and oauth2 carries no name of its own.
	ParamName        string
	Type             string // "apiKey", "http", "oauth2", "openIdConnect"
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

// Authorization is the static authorization requirement a route declared,
// carried over the wire as the x-forge-authz extension.
//
// Nil means the route declared none. An empty-but-present value is never
// produced: see resolveEndpointAuthz.
type Authorization struct {
	// Roles: holding any one of them satisfies the requirement.
	Roles []string

	// Permissions: all of them are required.
	Permissions []string
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

	name := ComponentRefName(ref)
	if name == "" {
		return nil
	}

	return spec.Schemas[name]
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

	// Collect unique tags, sorted.
	//
	// The sort is load-bearing. This list is joined straight into the README's
	// API overview, `forge client check` diffs that README byte-for-byte, and
	// Go randomises map iteration -- so an unsorted walk reported the client as
	// out of date on a random subset of runs with nothing changed.
	tagSet := make(map[string]bool, len(spec.Tags))
	for _, tag := range spec.Tags {
		tagSet[tag.Name] = true
	}

	stats.Tags = sortedStringKeys(tagSet)

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

// EntityRef names the entity a payload carries and the JSON property that
// identifies it. Resolved in Go at generation time; the browser runtime never
// re-derives identity from a response.
type EntityRef struct {
	Type    string // typename, e.g. "Order"
	IDField string // JSON property name, e.g. "id"

	// Fields maps a JSON property of this type to the typename of what that
	// property contains -- the ELEMENT typename for an array, so a
	// `[]LineItem` records "LineItem" rather than any array marker.
	//
	// It is the only way the browser runtime can recognise a nested entity of
	// a different type: a JSON response carries no typename, and the runtime
	// refuses to derive one from shape for the same reason InferEntity does --
	// a guess made wrong on a type carrying both an id and a tenant id keys
	// two tenants' records to one entry. So `Order.customer` normalizes into
	// `Customer:c-3` only because this map says so.
	//
	// Populated by resolveEntityFields after every entity in a spec is known,
	// because resolving a property's $ref needs the whole spec.Schemas table
	// and InferEntity sees one schema at a time. Nil when the type has no
	// entity-typed property.
	Fields map[string]string
}

// TagSet is one operation's cache contract, expressed as two tag lists rather
// than one because a single operation can both satisfy existing cached reads
// and stale others. Provides is what this operation's result can satisfy --
// a GET tags the item and, if it is a list, the collection. Invalidates is
// what this operation makes stale on the client and must be refetched -- a
// POST or DELETE tags the collection it changed membership of. The two never
// merge into one list: a write's Invalidates names the same collection tag a
// read's Provides names, and conflating them would make a write look like it
// also satisfies a read it never returned data for.
type TagSet struct {
	Provides    []string
	Invalidates []string
}

// StreamIntent is what a stream message does to the cache.
type StreamIntent string

const (
	StreamUpsert StreamIntent = "upsert"
	StreamPatch  StreamIntent = "patch"
	StreamEvict  StreamIntent = "evict"
)

// StreamBinding binds one channel message to an entity type.
type StreamBinding struct {
	Message     string
	EntityType  string
	Intent      StreamIntent
	Invalidates []string
}
