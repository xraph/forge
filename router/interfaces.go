package router

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/logger"
)

// Router represents the HTTP router interface
type Router interface {
	// Basic routing
	Get(pattern string, handler http.HandlerFunc) Router
	Post(pattern string, handler http.HandlerFunc) Router
	Put(pattern string, handler http.HandlerFunc) Router
	Patch(pattern string, handler http.HandlerFunc) Router
	Delete(pattern string, handler http.HandlerFunc) Router
	Head(pattern string, handler http.HandlerFunc) Router
	Options(pattern string, handler http.HandlerFunc) Router

	DocGet(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router
	DocPost(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router
	DocPut(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router
	DocPatch(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router
	DocDelete(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router
	DocHead(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router
	DocOptions(pattern string, handler http.HandlerFunc, doc RouteDocumentation) Router

	// Generic method routing
	Method(method, pattern string, handler http.HandlerFunc) Router
	Handle(pattern string, handler http.Handler) Router

	// Middleware
	Use(middleware ...func(http.Handler) http.Handler) Router
	With(middleware ...func(http.Handler) http.Handler) Router

	// Conditional middleware
	UseIf(condition func(*http.Request) bool, middleware func(http.Handler) http.Handler) Router
	UseForPath(pathPattern string, middleware func(http.Handler) http.Handler) Router
	UseForMethod(method string, middleware func(http.Handler) http.Handler) Router

	// Group routing - returns Group which embeds Router
	Group(prefix string, options ...GroupOption) Group
	GroupFunc(prefix string, fn func(Group), options ...GroupOption) Router
	GroupWith(prefix string, fn func(Group), options ...GroupOption) Group

	// Named groups for reuse
	NamedGroup(name, prefix string, options ...GroupOption) Group
	GetGroup(name string) (Group, bool)
	RemoveGroup(name string) bool

	// Route with callback
	Route(pattern string, fn func(Router)) Router
	Mount(path string, handler http.Handler) Router
	MountGroup(path string, group Group) Router

	// Additional group utilities
	ListGroups() []GroupInfo
	GroupCount() int
	GetGroupByPath(path string) (Group, bool)
	GroupMiddleware(name string) []func(http.Handler) http.Handler
	MountGroups(groups map[string]Group)
	RemoveAllGroups()

	EnableDocumentation(config DocumentationConfig) Router

	// Convenience group methods
	APIGroup(prefix string, options ...GroupOption) Group
	AdminGroup(prefix string, options ...GroupOption) Group
	PublicGroup(prefix string, options ...GroupOption) Group
	WebSocketGroup(prefix string, options ...GroupOption) Group

	// Static files
	Static(pattern, dir string) Router
	StaticFS(pattern string, fs http.FileSystem) Router
	FileServer(pattern, dir string) Router

	// WebSocket support
	WebSocket(pattern string, handler WebSocketHandler) Router
	DocWebSocket(pattern string, handler WebSocketHandler, doc AsyncRouteDocumentation) Router
	WebSocketUpgrade(pattern string, handler WebSocketHandler, options ...WebSocketOption) Router

	// Server-Sent Events
	SSE(pattern string, handler SSEHandler) Router
	DocSSE(pattern string, handler SSEHandler, doc AsyncRouteDocumentation) Router

	// API documentation
	OpenAPI(path string, spec OpenAPISpec) Router
	SwaggerUI(path string, specURL string) Router

	// Health and monitoring
	Health(path string, checker HealthChecker) Router
	Metrics(path string) Router

	// Error handling
	NotFound(handler http.HandlerFunc) Router
	MethodNotAllowed(handler http.HandlerFunc) Router
	ErrorHandler(handler ErrorHandler) Router

	// Configuration
	SetTimeout(timeout time.Duration) Router
	SetMaxBodySize(size int64) Router
	EnableCORS(config CORSConfig) Router
	EnableRateLimit(config RateLimitConfig) Router

	// Context
	WithContext(ctx context.Context) Router

	// Handler creation
	Handler() http.Handler

	// Introspection (available on both Router and Group)
	Routes() []RouteInfo
	Middleware() []func(http.Handler) http.Handler
}

// Group embeds Router and adds group-specific functionality
type Group interface {
	// Embed Router - Group "is-a" Router with additional capabilities
	Router

	// Group-specific methods that aren't in Router

	// Group-specific introspection
	Prefix() string
	FullPath() string
	SubGroups() []Group
	Parent() Router

	// Registration - converts group to handler for mounting
	Register(parent Router)
}

// GroupCondition represents a condition function for conditional middleware
type GroupCondition func(*http.Request) bool

// GroupOption represents configuration options for groups
type GroupOption interface {
	Apply(*GroupConfig)
}

// GroupConfig represents group configuration
type GroupConfig struct {
	Prefix      string
	Middleware  []func(http.Handler) http.Handler
	Description string
	Tags        []string
	Metadata    map[string]interface{}

	// Security
	RequireAuth   bool
	RequiredRoles []string
	RequiredPerms []string

	// Rate limiting
	RateLimit *RateLimitConfig

	// CORS
	CORS *CORSConfig

	// Timeouts
	Timeout time.Duration

	// Validation
	ValidateRequests  bool
	ValidateResponses bool
}

// GroupInfo provides information about a group
type GroupInfo struct {
	Name        string                 `json:"name"`
	Prefix      string                 `json:"prefix"`
	FullPath    string                 `json:"full_path"`
	Description string                 `json:"description,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	RouteCount  int                    `json:"route_count"`
	SubGroups   int                    `json:"sub_groups"`
	Middleware  int                    `json:"middleware_count"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// WebSocket interfaces

// WebSocketHandler handles WebSocket connections
type WebSocketHandler interface {
	HandleConnection(ctx context.Context, conn WebSocketConnection) error
}

// WebSocketConnection represents a WebSocket connection
type WebSocketConnection interface {
	// Connection info
	ID() string
	RemoteAddr() string
	UserAgent() string
	Headers() http.Header

	// Message handling
	ReadMessage() (messageType int, data []byte, err error)
	WriteMessage(messageType int, data []byte) error
	ReadJSON(v interface{}) error
	WriteJSON(v interface{}) error

	// Control
	Close() error
	CloseWithCode(code int, text string) error
	Ping(data []byte) error
	Pong(data []byte) error

	// Configuration
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(string) error)
	SetPingHandler(h func(string) error)
	SetCloseHandler(h func(int, string) error)

	// Context
	Context() context.Context
	SetContext(ctx context.Context)
}

// WebSocketHub manages WebSocket connections
type WebSocketHub interface {
	// Connection management
	Register(conn WebSocketConnection) error
	Unregister(conn WebSocketConnection) error

	// Broadcasting
	Broadcast(message []byte) error
	BroadcastJSON(v interface{}) error
	BroadcastTo(connIDs []string, message []byte) error
	BroadcastToGroups(groups []string, message []byte) error

	// Grouping
	JoinGroup(connID, group string) error
	LeaveGroup(connID, group string) error
	GetGroups(connID string) []string
	GetGroupMembers(group string) []string

	// Statistics
	ConnectionCount() int
	GroupCount() int
	Stats() WebSocketStats

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Server-Sent Events interfaces

// SSEHandler handles Server-Sent Events
type SSEHandler interface {
	HandleSSE(ctx context.Context, stream SSEStream) error
}

// SSEStream represents an SSE stream
type SSEStream interface {
	// Send events
	Send(event SSEEvent) error
	SendData(data string) error
	SendJSON(v interface{}) error
	SendEvent(name, data string) error
	SendComment(comment string) error

	// Connection info
	ID() string
	LastEventID() string
	RemoteAddr() string
	Headers() http.Header

	// Control
	Close() error
	IsClosed() bool

	// Configuration
	SetRetry(milliseconds int) error
	SetKeepAlive(interval time.Duration)

	// Context
	Context() context.Context
}

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	ID    string
	Event string
	Data  string
	Retry int
}

// Configuration types

// Config represents router configuration
type Config struct {
	// Timeouts
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ReadHeaderTimeout time.Duration

	// Limits
	MaxBodySize   int64
	MaxHeaderSize int

	// Features
	EnableCORS        bool
	EnableRateLimit   bool
	EnableCompression bool
	EnableRecovery    bool
	EnableLogging     bool

	// Security
	Security SecurityConfig

	// WebSocket
	WebSocket WebSocketConfig

	// SSE
	SSE SSEConfig

	// Logger
	Logger logger.Logger
}

// SecurityConfig represents security configuration
type SecurityConfig struct {
	// CORS
	CORS CORSConfig

	// Rate limiting
	RateLimit RateLimitConfig

	// Security headers
	ContentTypeOptions      string
	FrameOptions            string
	XSSProtection           string
	ReferrerPolicy          string
	ContentSecurityPolicy   string
	StrictTransportSecurity string

	// CSRF protection
	CSRFConfig CSRFConfig
}

// CORSConfig represents CORS configuration
type CORSConfig struct {
	AllowedOrigins     []string
	AllowedMethods     []string
	AllowedHeaders     []string
	ExposedHeaders     []string
	AllowCredentials   bool
	MaxAge             int
	OptionsPassthrough bool
}

// RateLimitConfig represents rate limiting configuration
type RateLimitConfig struct {
	RequestsPerSecond float64
	BurstSize         int
	Strategy          RateLimitStrategy
	SkipPaths         []string
	KeyGenerator      func(*http.Request) string
}

// RateLimitStrategy defines rate limiting strategies
type RateLimitStrategy string

const (
	RateLimitByIP     RateLimitStrategy = "ip"
	RateLimitByUser   RateLimitStrategy = "user"
	RateLimitByAPIKey RateLimitStrategy = "api_key"
	RateLimitByHeader RateLimitStrategy = "header"
	RateLimitGlobal   RateLimitStrategy = "global"
)

// CSRFConfig represents CSRF protection configuration
type CSRFConfig struct {
	Enabled        bool
	TokenLength    int
	TokenLookup    string
	ContextKey     string
	CookieName     string
	CookieDomain   string
	CookiePath     string
	CookieMaxAge   int
	CookieSecure   bool
	CookieHTTPOnly bool
	CookieSameSite http.SameSite
}

// WebSocketConfig represents WebSocket configuration
type WebSocketConfig struct {
	// Connection limits
	MaxConnections int
	MaxMessageSize int64

	// Timeouts
	HandshakeTimeout time.Duration
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	PingInterval     time.Duration
	PongTimeout      time.Duration

	// Buffer sizes
	ReadBufferSize  int
	WriteBufferSize int

	// Compression
	EnableCompression bool

	// Origin checking
	CheckOrigin func(*http.Request) bool

	// Subprotocols
	Subprotocols []string
}

// SSEConfig represents Server-Sent Events configuration
type SSEConfig struct {
	// Connection limits
	MaxConnections int

	// Timeouts
	ConnectionTimeout time.Duration
	KeepAliveInterval time.Duration

	// Retry
	DefaultRetry int

	// Headers
	AllowedOrigins []string
	Headers        map[string]string
}

// WebSocketOption represents WebSocket connection options
type WebSocketOption interface {
	Apply(*WebSocketConnectionConfig)
}

// WebSocketConnectionConfig represents WebSocket connection configuration
type WebSocketConnectionConfig struct {
	Subprotocols      []string
	ReadBufferSize    int
	WriteBufferSize   int
	CheckOrigin       func(*http.Request) bool
	EnableCompression bool
}

// Supporting types

// WebSocketStats represents WebSocket statistics
type WebSocketStats struct {
	TotalConnections  int64
	ActiveConnections int
	TotalGroups       int
	MessagesReceived  int64
	MessagesSent      int64
	ErrorCount        int64
	LastError         error
	LastErrorTime     time.Time
}

// HealthChecker represents a health check function
type HealthChecker func(ctx context.Context) error

// ErrorHandler represents an error handling function
type ErrorHandler func(http.ResponseWriter, *http.Request, error)

// OpenAPISpec represents an OpenAPI specification
type OpenAPISpec struct {
	OpenAPI      string                 `json:"openapi"`
	Info         InfoObject             `json:"info"`
	Servers      []ServerInfo           `json:"servers,omitempty"`
	Paths        map[string]PathItem    `json:"paths"`
	Components   *Components            `json:"components,omitempty"`
	Security     []SecurityRequirement  `json:"security,omitempty"`
	Tags         []Tag                  `json:"tags,omitempty"`
	ExternalDocs *ExternalDocs          `json:"externalDocs,omitempty"`
	Extensions   map[string]interface{} `json:"-"`
}

// PathItem represents an OpenAPI path item
type PathItem struct {
	Ref         string                 `json:"$ref,omitempty"`
	Summary     string                 `json:"summary,omitempty"`
	Description string                 `json:"description,omitempty"`
	Get         *Operation             `json:"get,omitempty"`
	Put         *Operation             `json:"put,omitempty"`
	Post        *Operation             `json:"post,omitempty"`
	Delete      *Operation             `json:"delete,omitempty"`
	Options     *Operation             `json:"options,omitempty"`
	Head        *Operation             `json:"head,omitempty"`
	Patch       *Operation             `json:"patch,omitempty"`
	Trace       *Operation             `json:"trace,omitempty"`
	Servers     []ServerInfo           `json:"servers,omitempty"`
	Parameters  []Parameter            `json:"parameters,omitempty"`
	Extensions  map[string]interface{} `json:"-"`
}

// Operation represents an OpenAPI operation
type Operation struct {
	Tags         []string               `json:"tags,omitempty"`
	Summary      string                 `json:"summary,omitempty"`
	Description  string                 `json:"description,omitempty"`
	ExternalDocs *ExternalDocs          `json:"externalDocs,omitempty"`
	OperationID  string                 `json:"operationId,omitempty"`
	Parameters   []Parameter            `json:"parameters,omitempty"`
	RequestBody  *RequestBody           `json:"requestBody,omitempty"`
	Responses    map[string]Response    `json:"responses"`
	Callbacks    map[string]*Callback   `json:"callbacks,omitempty"`
	Deprecated   bool                   `json:"deprecated,omitempty"`
	Security     []SecurityRequirement  `json:"security,omitempty"`
	Servers      []ServerInfo           `json:"servers,omitempty"`
	Extensions   map[string]interface{} `json:"-"`
}

// Parameter represents an OpenAPI parameter
type Parameter struct {
	Name        string
	In          string
	Description string
	Required    bool
	Type        string
	Format      string
	Schema      *Schema
}

// Header represents an OpenAPI header
type Header struct {
	Type        string
	Format      string
	Description string
}

// SecurityRequirement represents an OpenAPI security requirement
type SecurityRequirement map[string][]string

// Route information for introspection
type RouteInfo struct {
	Method   string
	Pattern  string
	Handler  string
	Name     string
	Metadata map[string]interface{}
}

// Middleware types

// MiddlewareFunc represents a middleware function
type MiddlewareFunc func(http.Handler) http.Handler

// RequestInfo provides request information to middleware
type RequestInfo struct {
	StartTime     time.Time
	RequestID     string
	Method        string
	Path          string
	RemoteAddr    string
	UserAgent     string
	ContentType   string
	ContentLength int64
	Headers       http.Header
}

// ResponseInfo provides response information to middleware
type ResponseInfo struct {
	StatusCode int
	Size       int64
	Duration   time.Duration
	Headers    http.Header
}

// Context keys for request/response information
type ContextKey string

const (
	RequestInfoKey  ContextKey = "request_info"
	ResponseInfoKey ContextKey = "response_info"
	RequestIDKey    ContextKey = "request_id"
	UserIDKey       ContextKey = "user_id"
	SessionIDKey    ContextKey = "session_id"
)

// Common group options
type groupOption struct {
	apply func(*GroupConfig)
}

func (o *groupOption) Apply(config *GroupConfig) {
	o.apply(config)
}

// WithDescription sets the group description
func WithDescription(description string) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.Description = description
		},
	}
}

// WithTags sets the group tags
func WithTags(tags ...string) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.Tags = tags
		},
	}
}

// WithMiddleware sets the group middleware
func WithMiddleware(middleware ...func(http.Handler) http.Handler) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.Middleware = append(config.Middleware, middleware...)
		},
	}
}

// WithAuth requires authentication for the group
func WithAuth(required bool) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.RequireAuth = required
		},
	}
}

// WithRoles requires specific roles for the group
func WithRoles(roles ...string) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.RequiredRoles = roles
		},
	}
}

// WithPermissions requires specific permissions for the group
func WithPermissions(perms ...string) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.RequiredPerms = perms
		},
	}
}

// WithRateLimit sets rate limiting for the group
func WithRateLimit(config RateLimitConfig) GroupOption {
	return &groupOption{
		apply: func(groupConfig *GroupConfig) {
			groupConfig.RateLimit = &config
		},
	}
}

// WithCORS sets CORS configuration for the group
func WithCORS(config CORSConfig) GroupOption {
	return &groupOption{
		apply: func(groupConfig *GroupConfig) {
			groupConfig.CORS = &config
		},
	}
}

// WithTimeout sets the group timeout
func WithTimeout(timeout time.Duration) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.Timeout = timeout
		},
	}
}

// WithValidation enables request/response validation
func WithValidation(requests, responses bool) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			config.ValidateRequests = requests
			config.ValidateResponses = responses
		},
	}
}

// WithMetadata sets group metadata
func WithMetadata(metadata map[string]interface{}) GroupOption {
	return &groupOption{
		apply: func(config *GroupConfig) {
			if config.Metadata == nil {
				config.Metadata = make(map[string]interface{})
			}
			for k, v := range metadata {
				config.Metadata[k] = v
			}
		},
	}
}

// Helper functions for creating conditional middleware

// IfPath creates a condition based on path pattern
func IfPath(pattern string) GroupCondition {
	return func(r *http.Request) bool {
		return matchesPattern(pattern, r.URL.Path)
	}
}

// IfMethod creates a condition based on HTTP method
func IfMethod(methods ...string) GroupCondition {
	methodMap := make(map[string]bool)
	for _, method := range methods {
		methodMap[method] = true
	}
	return func(r *http.Request) bool {
		return methodMap[r.Method]
	}
}

// IfHeader creates a condition based on header presence/value
func IfHeader(name, value string) GroupCondition {
	return func(r *http.Request) bool {
		if value == "" {
			return r.Header.Get(name) != ""
		}
		return r.Header.Get(name) == value
	}
}

// IfContentType creates a condition based on content type
func IfContentType(contentTypes ...string) GroupCondition {
	return func(r *http.Request) bool {
		ct := r.Header.Get("Content-Type")
		for _, expectedCT := range contentTypes {
			if ct == expectedCT {
				return true
			}
		}
		return false
	}
}

// IfUserAgent creates a condition based on user agent
func IfUserAgent(pattern string) GroupCondition {
	return func(r *http.Request) bool {
		ua := r.UserAgent()
		return matchesPattern(pattern, ua)
	}
}

// Combine conditions

// AndCondition combines conditions with AND logic
func AndCondition(conditions ...GroupCondition) GroupCondition {
	return func(r *http.Request) bool {
		for _, condition := range conditions {
			if !condition(r) {
				return false
			}
		}
		return true
	}
}

// OrCondition combines conditions with OR logic
func OrCondition(conditions ...GroupCondition) GroupCondition {
	return func(r *http.Request) bool {
		for _, condition := range conditions {
			if condition(r) {
				return true
			}
		}
		return false
	}
}

// NotCondition negates a condition
func NotCondition(condition GroupCondition) GroupCondition {
	return func(r *http.Request) bool {
		return !condition(r)
	}
}

// Utility functions

// GetRequestInfo extracts request information from context
func GetRequestInfo(ctx context.Context) *RequestInfo {
	if info, ok := ctx.Value(RequestInfoKey).(*RequestInfo); ok {
		return info
	}
	return nil
}

// GetResponseInfo extracts response information from context
func GetResponseInfo(ctx context.Context) *ResponseInfo {
	if info, ok := ctx.Value(ResponseInfoKey).(*ResponseInfo); ok {
		return info
	}
	return nil
}

// GetRequestID extracts request ID from context
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetUserID extracts user ID from context
func GetUserID(ctx context.Context) string {
	if id, ok := ctx.Value(UserIDKey).(string); ok {
		return id
	}
	return ""
}

// Utility function for pattern matching
func matchesPattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == value {
		return true
	}
	// Add wildcard support
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}
	return false
}

// Error types
var (
	ErrRouteNotFound     = fmt.Errorf("route not found")
	ErrMethodNotAllowed  = fmt.Errorf("method not allowed")
	ErrInvalidPattern    = fmt.Errorf("invalid route pattern")
	ErrWebSocketUpgrade  = fmt.Errorf("websocket upgrade failed")
	ErrSSENotSupported   = fmt.Errorf("server-sent events not supported")
	ErrConnectionClosed  = fmt.Errorf("connection closed")
	ErrMessageTooLarge   = fmt.Errorf("message too large")
	ErrInvalidMessage    = fmt.Errorf("invalid message")
	ErrRateLimitExceeded = fmt.Errorf("rate limit exceeded")
	ErrCSRFTokenInvalid  = fmt.Errorf("csrf token invalid")
)
