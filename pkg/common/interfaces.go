package common

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/xraph/forge/pkg/logger"
)

// =============================================================================
// CORE SERVICE INTERFACES
// =============================================================================

// Service defines the interface that all business services must implement
type Service interface {
	// Name returns the unique name of the service
	Name() string

	// Dependencies returns the list of service names this service depends on
	Dependencies() []string

	// OnStart is called when the service should start
	OnStart(ctx context.Context) error

	// OnStop is called when the service should stop
	OnStop(ctx context.Context) error

	// OnHealthCheck is called to check if the service is healthy
	OnHealthCheck(ctx context.Context) error
}

// Controller defines the interface for HTTP controllers
type Controller interface {
	// Name returns the unique name of the controller
	Name() string

	// Dependencies returns the list of services this controller depends on
	Dependencies() []string

	// Routes returns the routes handled by this controller
	Routes() []RouteDefinition

	// Middleware returns controller-specific middleware
	Middleware() []Middleware

	// Initialize initializes the controller with dependencies
	Initialize(container Container) error
}

// Plugin defines the interface for framework plugins
type Plugin interface {
	// Name returns the unique name of the plugin
	Name() string

	// Version returns the version of the plugin
	Version() string

	// Description returns a description of what the plugin does
	Description() string

	// Dependencies returns the list of services this plugin depends on
	Dependencies() []string

	// Initialize initializes the plugin with the DI container
	Initialize(container Container) error

	// Middleware returns middleware provided by this plugin
	Middleware() []Middleware

	// Routes returns routes provided by this plugin
	Routes() []RouteDefinition

	// OnStart is called when the plugin should start
	OnStart(ctx context.Context) error

	// OnStop is called when the plugin should stop
	OnStop(ctx context.Context) error
}

// =============================================================================
// ROUTER INTERFACE
// =============================================================================

// Router defines the interface for HTTP routing with service integration
type Router interface {
	Mount(pattern string, handler http.Handler)
	Group(pattern string) Router

	// Service-aware route registration
	GET(path string, handler interface{}, options ...HandlerOption) error
	POST(path string, handler interface{}, options ...HandlerOption) error
	PUT(path string, handler interface{}, options ...HandlerOption) error
	DELETE(path string, handler interface{}, options ...HandlerOption) error
	PATCH(path string, handler interface{}, options ...HandlerOption) error
	OPTIONS(path string, handler interface{}, options ...HandlerOption) error
	HEAD(path string, handler interface{}, options ...HandlerOption) error

	// Controller registration
	RegisterController(controller Controller) error
	UnregisterController(controllerName string) error

	// Opinionated handler registration (automatic schema generation)
	RegisterOpinionatedHandler(method, path string, handler interface{}, options ...HandlerOption) error

	// Streaming handler registration (updated to use proper types)
	RegisterWebSocket(path string, handler interface{}, options ...StreamingHandlerInfo) error
	RegisterSSE(path string, handler interface{}, options ...StreamingHandlerInfo) error

	// AsyncAPI support (new methods)
	EnableAsyncAPI(config AsyncAPIConfig)
	GetAsyncAPISpec() *AsyncAPISpec
	UpdateAsyncAPISpec(updater func(*AsyncAPISpec))

	// Middleware management
	AddMiddleware(middleware Middleware) error
	RemoveMiddleware(middlewareName string) error
	UseMiddleware(handler func(http.Handler) http.Handler)

	// Plugin support
	AddPlugin(plugin Plugin) error
	RemovePlugin(pluginName string) error

	// OpenAPI support
	EnableOpenAPI(config OpenAPIConfig)
	GetOpenAPISpec() *OpenAPISpec
	UpdateOpenAPISpec(updater func(*OpenAPISpec))

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HealthCheck(ctx context.Context) error

	// HTTP Server integration
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Handler() http.Handler

	// Statistics
	GetStats() RouterStats
	GetRouteStats(method, path string) (*RouteStats, error)
}

// Middleware defines the interface for service-aware middleware
type Middleware interface {
	Service

	// Priority returns the priority of the middleware (lower = higher priority)
	Priority() int

	// Initialize initializes the middleware with the DI container
	Initialize(container Container) error

	// Handler returns the HTTP handler function
	Handler() func(next http.Handler) http.Handler

	// GetStats returns middleware-specific statistics
	GetStats() MiddlewareStats
}

// MiddlewareStats contains statistics about middleware performance
type MiddlewareStats struct {
	Name           string        `json:"name"`
	Priority       int           `json:"priority"`
	Applied        bool          `json:"applied"`
	AppliedAt      time.Time     `json:"applied_at"`
	CallCount      int64         `json:"call_count"`
	ErrorCount     int64         `json:"error_count"`
	TotalLatency   time.Duration `json:"total_latency"`
	AverageLatency time.Duration `json:"average_latency"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LastError      string        `json:"last_error,omitempty"`
	Dependencies   []string      `json:"dependencies"`
	Status         string        `json:"status"`
}

// =============================================================================
// CONTAINER INTERFACE
// =============================================================================

// Container defines the dependency injection container interface
type Container interface {
	// Service registration and resolution
	Register(definition ServiceDefinition) error
	Resolve(serviceType interface{}) (interface{}, error)
	ResolveNamed(name string) (interface{}, error)

	// Service management
	Has(serviceType interface{}) bool
	HasNamed(name string) bool
	Services() []ServiceDefinition

	// Lifecycle management
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HealthCheck(ctx context.Context) error

	// Validation
	GetValidator() Validator

	LifecycleManager() LifecycleManager
}

// =============================================================================
// CONTEXT INTERFACE
// =============================================================================

// Context defines the enhanced context for Forge framework operations
type Context interface {
	context.Context

	// Request/Response management
	Request() *http.Request
	ResponseWriter() http.ResponseWriter
	WithRequest(req *http.Request) Context
	WithResponseWriter(w http.ResponseWriter) Context

	// Value management
	WithValue(key string, value interface{}) Context
	WithTimeout(timeout time.Duration) (Context, context.CancelFunc)
	WithCancel() (Context, context.CancelFunc)
	Copy() Context

	// Framework access
	Container() Container
	Logger() Logger
	Metrics() Metrics
	Config() ConfigManager

	// Value access
	Get(key string) interface{}
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	Set(key string, value interface{})
	Has(key string) bool
	Delete(key string)

	// Service resolution
	Resolve(serviceType interface{}) (interface{}, error)
	ResolveNamed(name string) (interface{}, error)
	MustResolve(serviceType interface{}) interface{}
	MustResolveNamed(name string) interface{}

	// Logging
	Debug(msg string, fields ...LogField)
	Info(msg string, fields ...LogField)
	Warn(msg string, fields ...LogField)
	Error(msg string, fields ...LogField)

	// Metrics
	Counter(name string, tags ...string) Counter
	Gauge(name string, tags ...string) Gauge
	Histogram(name string, tags ...string) Histogram
	Timer(name string, tags ...string) Timer

	// Configuration
	GetConfig(key string) interface{}
	GetConfigString(key string) string
	GetConfigInt(key string) int
	GetConfigBool(key string) bool
	GetConfigDuration(key string) time.Duration
	BindConfig(key string, target interface{}) error

	// Request metadata
	UserID() string
	SetUserID(userID string)
	RequestID() string
	SetRequestID(requestID string)
	TraceID() string
	SetTraceID(traceID string)

	// Authentication
	IsAuthenticated() bool
	IsAnonymous() bool

	// Request binding
	BindJSON(target interface{}) error
	BindXML(target interface{}) error
	BindForm(target interface{}) error
	BindQuery(target interface{}) error
	BindPath(target interface{}) error
	BindHeaders(target interface{}) error

	// Response helpers
	JSON(status int, data interface{}) error
	XML(status int, data interface{}) error
	String(status int, message string) error
	Status(status int) Context
	Header(key, value string) Context

	// Request utilities
	ClientIP() string
	UserAgent() string
	Method() string
	Path() string
	Query() string
}

// =============================================================================
// HANDLER DEFINITIONS
// =============================================================================

// HandlerOption defines options for route handlers
type HandlerOption func(*RouteHandlerInfo)

// RouteHandlerInfo contains information about a route handler
type RouteHandlerInfo struct {
	ServiceName    string
	ServiceType    reflect.Type
	HandlerFunc    interface{}
	Method         string
	Path           string
	Middleware     []Middleware
	Dependencies   []string
	Config         interface{}
	Tags           map[string]string
	Opinionated    bool // For automatic schema generation
	RequestType    reflect.Type
	ResponseType   reflect.Type
	RegisteredAt   time.Time
	CallCount      int64
	LastCalled     time.Time
	AverageLatency time.Duration
	ErrorCount     int64
	LastError      error
}

// OpinionatedHandler defines the signature for opinionated handlers with automatic schema generation
// Signature: func(ctx Context, req RequestType) (*ResponseType, error)
type OpinionatedHandler interface{}

// ServiceHandler defines the signature for service-aware handlers
// Signature: func(ctx Context, service ServiceType, req RequestType) (*ResponseType, error)
type ServiceHandler interface{}

// ControllerHandler defines the signature for controller handlers
// Signature: func(ctx Context) error (with manual response handling)
type ControllerHandler func(ctx Context) error

// =============================================================================
// ROUTE AND MIDDLEWARE DEFINITIONS
// =============================================================================

// RouteDefinition defines a route component
type RouteDefinition struct {
	Method       string            `json:"method"`
	Pattern      string            `json:"pattern"`
	Handler      interface{}       `json:"-"`
	Middleware   []Middleware      `json:"middleware"`
	Dependencies []string          `json:"dependencies"`
	Config       interface{}       `json:"config"`
	Tags         map[string]string `json:"tags"`
	Opinionated  bool              `json:"opinionated"`
	RequestType  reflect.Type      `json:"-"`
	ResponseType reflect.Type      `json:"-"`
}

// MiddlewareDefinition defines a middleware component
type MiddlewareDefinition struct {
	Name         string                               `json:"name"`
	Priority     int                                  `json:"priority"`
	Handler      func(next http.Handler) http.Handler `json:"-"`
	Dependencies []string                             `json:"dependencies"`
	Config       interface{}                          `json:"config"`
}

// =============================================================================
// SERVICE DEFINITIONS
// =============================================================================

// ServiceDefinition defines how a service should be registered
type ServiceDefinition struct {
	Name         string            `json:"name"`
	Type         interface{}       `json:"-"`
	Constructor  interface{}       `json:"-"`
	Instance     interface{}       `json:"-"`
	Singleton    bool              `json:"singleton"`
	Dependencies []string          `json:"dependencies"`
	Config       interface{}       `json:"config"`
	Lifecycle    ServiceLifecycle  `json:"-"`
	Extensions   map[string]any    `json:"extensions"`
	Tags         map[string]string `json:"tags"`
}

// ServiceName returns the name of the service.
// If Name is not empty, it returns Name.
// If Name is empty, it returns the string representation of Type.
// If both are empty/nil, it returns "unknown".
func (sd *ServiceDefinition) ServiceName() string {
	// If Name is explicitly set, use it
	if sd.Name != "" {
		return sd.Name
	}

	// If Name is empty, try to use the Type
	if sd.Type != nil {
		serviceType := reflect.TypeOf(sd.Type)
		// If it's a pointer to an interface, get the interface type
		if serviceType.Kind() == reflect.Ptr {
			serviceType = serviceType.Elem()
		}
		return serviceType.String()
	}

	// Fallback if both Name and Type are empty/nil
	return ""
}

func (sd *ServiceDefinition) ServiceType() (reflect.Type, error) {
	// Get the service type from the definition
	if sd.Type == nil {
		return nil, ErrContainerError("register", fmt.Errorf("service type cannot be nil"))
	}

	var serviceType reflect.Type
	serviceType = reflect.TypeOf(sd.Type)
	// If it's a pointer to an interface, get the interface type
	if serviceType.Kind() == reflect.Ptr {
		serviceType = serviceType.Elem()
	}
	return serviceType, nil
}

// ServiceLifecycle defines lifecycle hooks for services
type ServiceLifecycle struct {
	OnStart       func(ctx context.Context, service interface{}) error
	OnStop        func(ctx context.Context, service interface{}) error
	OnHealthCheck func(ctx context.Context, service interface{}) error
	OnConfig      func(ctx context.Context, service interface{}, config interface{}) error
}

// =============================================================================
// SUPPORTING INTERFACES
// =============================================================================

// LifecycleManager manages the lifecycle of services
type LifecycleManager interface {
	AddService(service Service) error
	RemoveService(name string) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	StartServices(ctx context.Context) error
	StopServices(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	GetService(name string) (Service, error)
	GetServices() []Service
}

// Validator defines the interface for dependency validation
type Validator interface {
	ValidateService(serviceName string) error
	GetValidationReport() ValidationReport
	ValidateServiceHandlers(serviceHandlers map[string]*RouteHandlerInfo) error
	ValidateAll() error
	ValidateAllWithHandlers(serviceHandlers map[string]*RouteHandlerInfo) error
}

// ValidationReport contains validation results
type ValidationReport struct {
	Services                map[string]ServiceValidationResult `json:"services"`
	Valid                   bool                               `json:"valid"`
	CircularDependencyError string                             `json:"circular_dependency_error,omitempty"`
}

// ServiceValidationResult contains validation results for a service
type ServiceValidationResult struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Dependencies []string `json:"dependencies"`
	Valid        bool     `json:"valid"`
	Error        string   `json:"error,omitempty"`
}

// =============================================================================
// METRICS AND LOGGING
// =============================================================================

// Logger defines the interface for structured logging
type Logger = logger.Logger

// LogField represents a structured log field
type LogField = logger.Field

// =============================================================================
// CONFIGURATION MANAGEMENT
// =============================================================================

// ConfigManager defines the interface for configuration management
type ConfigManager interface {
	Get(key string) interface{}
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	GetDuration(key string) time.Duration
	Set(key string, value interface{})
	Bind(key string, target interface{}) error
	Watch(ctx context.Context) error
	WatchWithCallback(key string, callback func(key string, value interface{}))
	Reload() error
}

// =============================================================================
// APPLICATION STATUS
// =============================================================================

// ApplicationStatus represents the application status
type ApplicationStatus string

const (
	ApplicationStatusNotStarted ApplicationStatus = "not_started"
	ApplicationStatusStarting   ApplicationStatus = "starting"
	ApplicationStatusRunning    ApplicationStatus = "running"
	ApplicationStatusStopping   ApplicationStatus = "stopping"
	ApplicationStatusStopped    ApplicationStatus = "stopped"
	ApplicationStatusError      ApplicationStatus = "error"
)

// ApplicationInfo contains information about the application
type ApplicationInfo struct {
	Name              string            `json:"name"`
	Version           string            `json:"version"`
	Description       string            `json:"description"`
	Status            ApplicationStatus `json:"status"`
	StartTime         time.Time         `json:"start_time"`
	Uptime            time.Duration     `json:"uptime"`
	Services          int               `json:"services"`
	Controllers       int               `json:"controllers"`
	Plugins           int               `json:"plugins"`
	Routes            int               `json:"routes"`
	Middleware        int               `json:"middleware"`
	WebSocketHandlers int               `json:"websocket_handlers"`
	SSEHandlers       int               `json:"sse_handlers"`
	StreamingEnabled  bool              `json:"streaming_enabled"`
	ActiveConnections int64             `json:"active_connections"`
}

// RouterStats contains router statistics
type RouterStats struct {
	HandlersRegistered int                    `json:"handlers_registered"`
	ControllersCount   int                    `json:"controllers_count"`
	MiddlewareCount    int                    `json:"middleware_count"`
	PluginCount        int                    `json:"plugin_count"`
	WebSocketHandlers  int                    `json:"websocket_handlers"` // NEW
	SSEHandlers        int                    `json:"sse_handlers"`
	RouteStats         map[string]*RouteStats `json:"route_stats"`
	Started            bool                   `json:"started"`
	OpenAPIEnabled     bool                   `json:"openapi_enabled"`
}

// RouteStats contains statistics about a route
type RouteStats struct {
	Method         string        `json:"method"`
	Path           string        `json:"path"`
	CallCount      int64         `json:"call_count"`
	ErrorCount     int64         `json:"error_count"`
	TotalLatency   time.Duration `json:"total_latency"`
	AverageLatency time.Duration `json:"average_latency"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LastCalled     time.Time     `json:"last_called"`
	LastError      error         `json:"last_error,omitempty"`
}

// WebSocketHandler defines the signature for WebSocket handlers
// Supported signatures:
// - func(ctx Context, conn Connection) error
// - func(conn Connection) error
type WebSocketHandler interface{}

// SSEHandler defines the signature for Server-Sent Events handlers
// Supported signatures:
// - func(ctx Context, conn Connection) error
// - func(conn Connection) error
type SSEHandler interface{}

// StreamingMessage represents a generic streaming message
type StreamingMessage struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	ID        string                 `json:"id,omitempty"`
	Timestamp time.Time              `json:"timestamp,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// StreamingEvent represents a Server-Sent Event
type StreamingEvent struct {
	Type  string      `json:"type"`
	Data  interface{} `json:"data"`
	ID    string      `json:"id,omitempty"`
	Retry int         `json:"retry,omitempty"`
}

// StreamingConnection represents a generic streaming connection
type StreamingConnection interface {
	// Connection management
	ID() string
	UserID() string
	IsAlive() bool
	Close() error

	// Message sending
	Send(message interface{}) error
	SendMessage(msg StreamingMessage) error
	SendEvent(event StreamingEvent) error

	// Connection metadata
	Protocol() string
	RemoteAddr() string
	ConnectedAt() time.Time
	LastActivity() time.Time

	// Context access
	Context() context.Context
	WithContext(ctx context.Context) StreamingConnection
}

// StreamingStats represents statistics for streaming operations
type StreamingStats struct {
	TotalConnections  int64            `json:"total_connections"`
	ActiveConnections int64            `json:"active_connections"`
	MessagesSent      int64            `json:"messages_sent"`
	MessagesReceived  int64            `json:"messages_received"`
	ErrorCount        int64            `json:"error_count"`
	AverageLatency    time.Duration    `json:"average_latency"`
	ConnectionsByType map[string]int64 `json:"connections_by_type"`
	LastActivity      time.Time        `json:"last_activity"`
}

// StreamingHandlerOption defines options specific to streaming handlers
type StreamingHandlerOption func(*StreamingHandlerInfo)

// StreamingHandlerInfo contains information about streaming handlers
type StreamingHandlerInfo struct {
	Path              string
	Protocol          string // "websocket", "sse", "longpolling"
	Handler           interface{}
	MessageTypes      map[string]reflect.Type
	EventTypes        map[string]reflect.Type
	Summary           string
	Description       string
	Tags              []string
	Authentication    bool
	ConnectionLimit   int
	HeartbeatInterval time.Duration
	RegisteredAt      time.Time
	ConnectionCount   int64
	MessageCount      int64
	ErrorCount        int64
	LastConnected     time.Time
	LastError         error
}

// Streaming handler options
func WithProtocol(protocol string) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Protocol = protocol
	}
}

func WithStreamingSummary(summary string) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Summary = summary
	}
}

func WithStreamingDescription(description string) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Description = description
	}
}

func WithStreamingAuth(auth bool) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.Authentication = auth
	}
}

func WithStreamingConnectionLimit(limit int) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.ConnectionLimit = limit
	}
}

func WithStreamingHeartbeat(interval time.Duration) StreamingHandlerOption {
	return func(info *StreamingHandlerInfo) {
		info.HeartbeatInterval = interval
	}
}
