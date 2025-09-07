package common

import (
	"reflect"
	"time"
)

// =============================================================================
// ASYNCAPI TYPES
// =============================================================================

// AsyncAPISpec represents an AsyncAPI 3.0 specification
type AsyncAPISpec struct {
	AsyncAPI           string                       `json:"asyncapi"`
	Info               AsyncAPIInfo                 `json:"info"`
	Servers            map[string]AsyncAPIServer    `json:"servers,omitempty"`
	Channels           map[string]AsyncAPIChannel   `json:"channels,omitempty"`
	Operations         map[string]AsyncAPIOperation `json:"operations,omitempty"`
	Components         AsyncAPIComponents           `json:"components,omitempty"`
	DefaultContentType string                       `json:"defaultContentType,omitempty"`
}

type AsyncAPIInfo struct {
	Title          string                 `json:"title"`
	Version        string                 `json:"version"`
	Description    string                 `json:"description,omitempty"`
	TermsOfService string                 `json:"termsOfService,omitempty"`
	Contact        *AsyncAPIContact       `json:"contact,omitempty"`
	License        *AsyncAPILicense       `json:"license,omitempty"`
	Extensions     map[string]interface{} `json:"-"`
}

type AsyncAPIContact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

type AsyncAPILicense struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type AsyncAPIServer struct {
	Host        string                        `json:"host"`
	Protocol    string                        `json:"protocol"`
	Description string                        `json:"description,omitempty"`
	Variables   map[string]AsyncAPIVariable   `json:"variables,omitempty"`
	Security    []AsyncAPISecurityRequirement `json:"security,omitempty"`
	Tags        []AsyncAPITag                 `json:"tags,omitempty"`
	Bindings    interface{}                   `json:"bindings,omitempty"`
}

type AsyncAPIVariable struct {
	Enum        []string `json:"enum,omitempty"`
	Default     string   `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// FIXED: Changed Messages to interface{} to support $ref structure
type AsyncAPIChannel struct {
	Address     string                       `json:"address"`
	Messages    interface{}                  `json:"messages,omitempty"` // Changed from map[string]AsyncAPIMessage
	Title       string                       `json:"title,omitempty"`
	Summary     string                       `json:"summary,omitempty"`
	Description string                       `json:"description,omitempty"`
	Servers     []string                     `json:"servers,omitempty"`
	Parameters  map[string]AsyncAPIParameter `json:"parameters,omitempty"`
	Tags        []AsyncAPITag                `json:"tags,omitempty"`
	Bindings    interface{}                  `json:"bindings,omitempty"`
}

// FIXED: Updated Messages field to support $ref arrays
type AsyncAPIOperation struct {
	Action      string                        `json:"action"`
	Channel     AsyncAPIChannelRef            `json:"channel"`
	Title       string                        `json:"title,omitempty"`
	Summary     string                        `json:"summary,omitempty"`
	Description string                        `json:"description,omitempty"`
	Security    []AsyncAPISecurityRequirement `json:"security,omitempty"`
	Tags        []AsyncAPITag                 `json:"tags,omitempty"`
	Messages    []interface{}                 `json:"messages,omitempty"` // Changed to support $ref objects
	Reply       *AsyncAPIReply                `json:"reply,omitempty"`
	Bindings    interface{}                   `json:"bindings,omitempty"`
}

type AsyncAPIChannelRef struct {
	Ref string `json:"$ref,omitempty"`
}

type AsyncAPIMessageRef struct {
	Ref string `json:"$ref,omitempty"`
}

// FIXED: Removed MessageId field and cleaned up structure
type AsyncAPIMessage struct {
	Headers       AsyncAPISchema           `json:"headers,omitempty"`
	Payload       AsyncAPISchema           `json:"payload,omitempty"`
	CorrelationId AsyncAPICorrelationId    `json:"correlationId,omitempty"`
	ContentType   string                   `json:"contentType,omitempty"`
	Name          string                   `json:"name,omitempty"`
	Title         string                   `json:"title,omitempty"`
	Summary       string                   `json:"summary,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Tags          []AsyncAPITag            `json:"tags,omitempty"`
	Examples      []AsyncAPIMessageExample `json:"examples,omitempty"`
	Bindings      interface{}              `json:"bindings,omitempty"`
	Traits        []interface{}            `json:"traits,omitempty"` // Added traits support
}

type AsyncAPISchema struct {
	// Reuse OpenAPI schema structure
	Type                 interface{}               `json:"type,omitempty"`
	Properties           map[string]AsyncAPISchema `json:"properties,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	Items                interface{}               `json:"items,omitempty"`
	AdditionalProperties interface{}               `json:"additionalProperties,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Default              interface{}               `json:"default,omitempty"`
	Examples             []interface{}             `json:"examples,omitempty"`
	Enum                 []interface{}             `json:"enum,omitempty"`
	Const                interface{}               `json:"const,omitempty"`
	Format               string                    `json:"format,omitempty"`
	Minimum              *float64                  `json:"minimum,omitempty"`
	Maximum              *float64                  `json:"maximum,omitempty"`
	MinLength            *int                      `json:"minLength,omitempty"`
	MaxLength            *int                      `json:"maxLength,omitempty"`
	Pattern              string                    `json:"pattern,omitempty"`
	Ref                  string                    `json:"$ref,omitempty"`
	AllOf                []AsyncAPISchema          `json:"allOf,omitempty"`
	AnyOf                []AsyncAPISchema          `json:"anyOf,omitempty"`
	OneOf                []AsyncAPISchema          `json:"oneOf,omitempty"`
	Not                  *AsyncAPISchema           `json:"not,omitempty"`
}

type AsyncAPIParameter struct {
	Description string         `json:"description,omitempty"`
	Schema      AsyncAPISchema `json:"schema,omitempty"`
	Location    string         `json:"location,omitempty"`
}

type AsyncAPITag struct {
	Name         string                `json:"name"`
	Description  string                `json:"description,omitempty"`
	ExternalDocs *AsyncAPIExternalDocs `json:"externalDocs,omitempty"`
}

type AsyncAPIExternalDocs struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

type AsyncAPICorrelationId struct {
	Description string `json:"description,omitempty"`
	Location    string `json:"location"`
}

type AsyncAPIMessageExample struct {
	Name    string                 `json:"name,omitempty"`
	Summary string                 `json:"summary,omitempty"`
	Headers map[string]interface{} `json:"headers,omitempty"`
	Payload interface{}            `json:"payload,omitempty"`
}

type AsyncAPIReply struct {
	Address  string               `json:"address,omitempty"`
	Channel  AsyncAPIChannelRef   `json:"channel,omitempty"`
	Messages []AsyncAPIMessageRef `json:"messages,omitempty"`
}

type AsyncAPIComponents struct {
	Schemas         map[string]AsyncAPISchema         `json:"schemas,omitempty"`
	Messages        map[string]AsyncAPIMessage        `json:"messages,omitempty"`
	SecuritySchemes map[string]AsyncAPISecurityScheme `json:"securitySchemes,omitempty"`
	Parameters      map[string]AsyncAPIParameter      `json:"parameters,omitempty"`
	CorrelationIds  map[string]AsyncAPICorrelationId  `json:"correlationIds,omitempty"`
	Replies         map[string]AsyncAPIReply          `json:"replies,omitempty"`
	ReplyAddresses  map[string]AsyncAPIReplyAddress   `json:"replyAddresses,omitempty"`
	ExternalDocs    map[string]AsyncAPIExternalDocs   `json:"externalDocs,omitempty"`
	Tags            map[string]AsyncAPITag            `json:"tags,omitempty"`
	MessageTraits   map[string]interface{}            `json:"messageTraits,omitempty"`   // Added message traits
	OperationTraits map[string]interface{}            `json:"operationTraits,omitempty"` // Added operation traits
}

type AsyncAPISecurityScheme struct {
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Name        string              `json:"name,omitempty"`
	In          string              `json:"in,omitempty"`
	Scheme      string              `json:"scheme,omitempty"`
	Flows       *AsyncAPIOAuthFlows `json:"flows,omitempty"`
}

type AsyncAPIOAuthFlows struct {
	Implicit          *AsyncAPIOAuthFlow `json:"implicit,omitempty"`
	Password          *AsyncAPIOAuthFlow `json:"password,omitempty"`
	ClientCredentials *AsyncAPIOAuthFlow `json:"clientCredentials,omitempty"`
	AuthorizationCode *AsyncAPIOAuthFlow `json:"authorizationCode,omitempty"`
}

type AsyncAPIOAuthFlow struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

type AsyncAPISecurityRequirement map[string][]string

type AsyncAPIReplyAddress struct {
	Description string `json:"description,omitempty"`
	Location    string `json:"location"`
}

// =============================================================================
// WEBSOCKET AND SSE HANDLER TYPES
// =============================================================================

// WSHandlerInfo contains information about WebSocket handlers
type WSHandlerInfo struct {
	Path            string
	Handler         interface{}
	MessageTypes    map[string]reflect.Type // message type name -> type
	EventTypes      map[string]reflect.Type // event type name -> type
	Summary         string
	Description     string
	Tags            []string
	Authentication  bool
	RegisteredAt    time.Time
	ConnectionCount int64
	MessageCount    int64
	ErrorCount      int64
	CallCount       int64
	LastConnected   time.Time
	LastError       error
}

// SSEHandlerInfo contains information about Server-Sent Events handlers
type SSEHandlerInfo struct {
	Path            string
	Handler         interface{}
	EventTypes      map[string]reflect.Type // event type name -> type
	Summary         string
	Description     string
	Tags            []string
	Authentication  bool
	RegisteredAt    time.Time
	ConnectionCount int64
	EventCount      int64
	CallCount       int64
	ErrorCount      int64
	LastConnected   time.Time
	LastError       error
}

// AsyncHandlerOption defines options for async handlers
type AsyncHandlerOption func(*AsyncHandlerInfo)

// AsyncHandlerInfo contains common info for async handlers
type AsyncHandlerInfo struct {
	Path         string
	Summary      string
	Description  string
	Tags         []string
	Auth         bool
	MessageTypes map[string]reflect.Type
	EventTypes   map[string]reflect.Type
}

// WebSocket message types
type WSMessage struct {
	Type    string      `json:"type"`
	Data    interface{} `json:"data"`
	ID      string      `json:"id,omitempty"`
	ReplyTo string      `json:"replyTo,omitempty"`
}

type WSConnection interface {
	Send(message WSMessage) error
	Close() error
	ID() string
	UserID() string
	IsAlive() bool
}

// SSE event types
type SSEEvent struct {
	Type  string      `json:"type"`
	Data  interface{} `json:"data"`
	ID    string      `json:"id,omitempty"`
	Retry int         `json:"retry,omitempty"`
}

type SSEConnection interface {
	Send(event SSEEvent) error
	Close() error
	ID() string
	UserID() string
	IsAlive() bool
}

// =============================================================================
// ASYNCAPI HANDLER OPTIONS
// =============================================================================

// WithAsyncSummary sets the summary for async handler
func WithAsyncSummary(summary string) AsyncHandlerOption {
	return func(h *AsyncHandlerInfo) {
		h.Summary = summary
	}
}

// WithAsyncDescription sets the description for async handler
func WithAsyncDescription(desc string) AsyncHandlerOption {
	return func(h *AsyncHandlerInfo) {
		h.Description = desc
	}
}

// WithAsyncTags sets tags for async handler
func WithAsyncTags(tags ...string) AsyncHandlerOption {
	return func(h *AsyncHandlerInfo) {
		h.Tags = tags
	}
}

// WithAsyncAuth indicates the handler requires authentication
func WithAsyncAuth(auth bool) AsyncHandlerOption {
	return func(h *AsyncHandlerInfo) {
		h.Auth = auth
	}
}

// WithMessageTypes defines the message types for WebSocket handlers
func WithMessageTypes(types map[string]reflect.Type) AsyncHandlerOption {
	return func(h *AsyncHandlerInfo) {
		h.MessageTypes = types
	}
}

// WithEventTypes defines the event types for SSE handlers
func WithEventTypes(types map[string]reflect.Type) AsyncHandlerOption {
	return func(h *AsyncHandlerInfo) {
		h.EventTypes = types
	}
}

// AsyncAPIConfig Configuration types for AsyncAPI
type AsyncAPIConfig struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
	SpecPath    string `json:"spec_path"` // Default: "/asyncapi.json"
	UIPath      string `json:"ui_path"`   // Default: "/asyncapi"
	EnableUI    bool   `json:"enable_ui"` // Default: false
}
