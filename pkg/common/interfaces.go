package common

import (
	"context"
	"fmt"
	"mime/multipart"
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

	// Prefix returns the route prefix for this controller (optional)
	Prefix() string

	// Dependencies returns the list of services this controller depends on
	Dependencies() []string

	// ConfigureRoutes configures routes using the provided router (UPDATED: flexible approach)
	ConfigureRoutes(router Router) error

	// Middleware returns controller-specific middleware
	Middleware() []any

	// Initialize initializes the controller with dependencies
	Initialize(container Container) error
}

// =============================================================================
// ROUTER INTERFACE
// =============================================================================

// Router defines the interface for HTTP routing with service integration
type Router interface {

	// Mount registers an HTTP handler on the given pattern for routing requests to the appropriate handler.
	Mount(pattern string, handler http.Handler)
	//
	Group(pattern string) Router

	// GET registers a route that handles HTTP GET requests for a specified path with the given handler and optional options.
	GET(path string, handler interface{}, options ...HandlerOption) error

	// POST registers a handler for HTTP POST requests to the specified path, with optional handler configurations provided.
	POST(path string, handler interface{}, options ...HandlerOption) error

	// PUT registers a handler for HTTP PUT requests on the specified
	PUT(path string, handler interface{}, options ...HandlerOption) error

	// DELETE registers a handler for the HTTP DELETE method for the given path with optional handler options.
	DELETE(path string, handler interface{}, options ...HandlerOption) error

	// PATCH registers a handler for the HTTP PATCH method at the specified path, with optional handler configuration.
	PATCH(path string, handler interface{}, options ...HandlerOption) error

	// OPTIONS registers an HTTP OPTIONS route with the specified path, handler, and optional handler options.
	OPTIONS(path string, handler interface{}, options ...HandlerOption) error

	// HEAD registers a handler for HTTP HEAD requests on the specified path with optional handler options.
	HEAD(path string, handler interface{}, options ...HandlerOption) error

	// RegisterController registers a controller with the router, initializing its routes, middleware, and dependencies.
	RegisterController(controller Controller) error

	// UnregisterController removes a
	UnregisterController(controllerName string) error

	// RegisterOpinionatedHandler registers an HTTP handler with additional options, supporting structured configuration.
	RegisterOpinionatedHandler(method, path string, handler interface{}, options ...HandlerOption) error

	// RegisterWebSocket registers a WebSocket endpoint at the specified path with the provided handler and options.
	RegisterWebSocket(path string, handler interface{}, options ...StreamingHandlerInfo) error

	// RegisterSSE registers a server-sent events (SSE) handler at the specified path with optional streaming settings.
	RegisterSSE(path string, handler interface{}, options ...StreamingHandlerInfo) error

	//
	EnableAsyncAPI(config AsyncAPIConfig)

	// GetAsyncAPISpec returns the current AsyncAPI specification configured for the router as a pointer to AsyncAPISpec.
	GetAsyncAPISpec() *AsyncAPISpec

	// UpdateAsyncAPISpec updates the AsyncAPI specification using the provided updater function
	UpdateAsyncAPISpec(updater func(*AsyncAPISpec))

	// Use adds a new middleware to the router and initializes it with the dependency injection container.
	Use(middleware any) error

	// RemoveMiddleware removes a middleware by
	RemoveMiddleware(middlewareName string) error

	// UseMiddleware registers a middleware handler to wrap
	UseMiddleware(handler func(http.Handler) http.Handler)

	// AddPlugin adds a new plugin
	AddPlugin(plugin Plugin) error

	// RemovePlugin removes a plugin from the application by its name and cleans up associated resources.
	RemovePlugin(pluginName string) error

	// EnableOpenAPI enables Open
	EnableOpenAPI(config OpenAPIConfig)

	// GetOpenAPISpec retrieves the current
	GetOpenAPISpec() *OpenAPISpec

	// UpdateOpenAPISpec updates the OpenAPI specification using the provided
	UpdateOpenAPISpec(updater func(*OpenAPISpec))

	// Start initializes and begins
	Start(ctx context.Context) error

	// Stop gracefully shuts down the router and associated services within the given context.
	Stop(ctx context.Context) error
	//
	HealthCheck(ctx context.Context) error

	// Serve
	ServeHTTP(w http.ResponseWriter, r *http.Request)

	// Handler
	Handler() http.Handler

	// GetStats returns the current
	GetStats() RouterStats

	// GetRouteStats retrieves statistics
	GetRouteStats(method, path string) (*RouteStats, error)

	// RegisterPluginRoute registers a route associated with the specified plugin ID.
	// Returns the registered route's unique identifier or an error if registration fails.
	RegisterPluginRoute(pluginID string, route RouteDefinition) (string, error)
	UnregisterPluginRoute(routeID string) error
	GetPluginRoutes(pluginID string) []string

	SetCurrentPlugin(pluginID string) // Set context for route registration
	ClearCurrentPlugin()              // Clear plugin context
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

	// Context returns the context associated with the current operation, carrying deadlines, cancellation signals, and other values.
	context.Context

	// Request returns the underlying *http.Request associated with the context.
	Request() *http.Request

	// ResponseWriter returns the underlying http.ResponseWriter for the current context, enabling direct response operations.
	ResponseWriter() http.ResponseWriter

	// WithRequest updates and returns a new context instance with the provided HTTP request.
	WithRequest(req *http.Request) Context

	// WithResponseWriter sets the HTTP response writer in the context and returns the updated context.
	WithResponseWriter(w http.ResponseWriter) Context

	// WithValue returns a new Context that carries a specific key-value pair.
	WithValue(key string, value interface{}) Context

	// WithTimeout creates a derived context with a timeout, returning the new context and a CancelFunc to cancel it.
	WithTimeout(timeout time.Duration) (Context, context.CancelFunc)

	// WithCancel creates a new Context derived from the current one and returns it along with a CancelFunc to cancel it.
	WithCancel() (Context, context.CancelFunc)

	// Copy creates and returns a copy of the current Context instance, including its internal state and configurations.
	Copy() Context

	// Container initializes and returns a new instance of a Container.
	Container() Container

	// Logger retrieves the current Logger for structured logging within the context.
	Logger() Logger

	// Metrics returns the interface for metrics collection, allowing operations like counters, gauges, timers, and histograms.
	Metrics() Metrics

	// Config returns the configuration manager associated with the context.
	Config() ConfigManager

	// Get retrieves the value associated with the specified key. It returns nil if the key does not exist.
	Get(key string) interface{}

	// GetString retrieves the value associated with the specified key as a string. Returns an empty string if the key is not found.
	GetString(key string) string

	// GetInt retrieves an integer value associated with the specified key from the context.
	GetInt(key string) int

	// GetBool retrieves the boolean value associated with the provided key. Returns false if the key does not exist or is not boolean.
	GetBool(key string) bool

	// Set assigns a value to a key within the context, enabling storage of custom data for later retrieval.
	Set(key string, value interface{})

	// Has checks if the given key exists within the context.
	Has(key string) bool

	// Delete removes a value associated with the specified key from the context.
	Delete(key string)

	// Resolve resolves a service instance of the given type from the dependency injection container.
	Resolve(serviceType interface{}) (interface{}, error)

	// ResolveNamed retrieves a registered service instance by its name and returns it along with an error, if any occurs.
	ResolveNamed(name string) (interface{}, error)

	// MustResolve resolves a service instance by its type and panics if the resolution fails or the service is not found.
	MustResolve(serviceType interface{}) interface{}

	// MustResolveNamed retrieves a service instance by its name and panics if the resolution fails.
	MustResolveNamed(name string) interface{}

	// Debug logs a debug-level message with optional structured fields for additional context.
	Debug(msg string, fields ...LogField)

	// Info logs a message at the info level with optional structured fields.
	Info(msg string, fields ...LogField)

	// Warn logs a message at the warning level with optional structured log fields.
	Warn(msg string, fields ...LogField)

	// Error logs an error-level message with the provided text and optional structured context fields.
	Error(msg string, fields ...LogField)

	// Counter creates a new Counter metric with a specified name and optional tags for identification.
	Counter(name string, tags ...string) Counter

	// Gauge creates or retrieves a gauge metric by name, optionally with associated tags.
	Gauge(name string, tags ...string) Gauge

	// Histogram creates or retrieves a histogram metric with the specified name and tags.
	Histogram(name string, tags ...string) Histogram

	// Timer creates or retrieves a timer metric with the specified name and optional tags.
	Timer(name string, tags ...string) Timer

	// GetConfig retrieves the configuration value associated with the provided key. Returns the value as an interface{}.
	GetConfig(key string) interface{}

	// GetConfigString retrieves a configuration value as a string for the given key. Returns an empty string if the key is not found.
	GetConfigString(key string) string

	// GetConfigInt retrieves the integer value associated with the specified configuration key.
	GetConfigInt(key string) int

	// GetConfigBool retrieves the boolean value associated with the specified configuration key.
	GetConfigBool(key string) bool

	// GetConfigDuration retrieves a configuration value by key and parses it as a time.Duration.
	GetConfigDuration(key string) time.Duration

	// BindConfig binds a configuration value from the ConfigManager to the provided target based on the specified key.
	BindConfig(key string, target interface{}) error

	// UserID returns the user identifier associated with the current context. It typically represents the authenticated user.
	UserID() string

	// SetUserID sets the user ID for the current context to the specified value.
	SetUserID(userID string)

	// RequestID returns the unique identifier associated with the current request.
	RequestID() string

	// SetRequestID sets the unique identifier for the current request context.
	SetRequestID(requestID string)

	// TraceID returns the unique trace identifier associated with the current context for tracking requests and operations.
	TraceID() string

	// SetTraceID sets the trace ID for the current context, enabling traceability across different operations or services.
	SetTraceID(traceID string)

	// IsAuthenticated checks if the current context represents an authenticated user and returns true if authenticated.
	IsAuthenticated() bool

	// IsAnonymous checks if the current context is associated with an anonymous user and returns true if so.
	IsAnonymous() bool

	// BindJSON binds the JSON payload from the request body into the provided target struct.
	BindJSON(target interface{}) error

	// BindXML parses the incoming XML request body and binds it to the specified target interface.
	BindXML(target interface{}) error

	// BindForm binds form-encoded request data to the provided target structure by matching field names with form keys.
	BindForm(target interface{}) error

	// BindQuery binds query string parameters from the request URL to the specified target structure.
	BindQuery(target interface{}) error

	// BindPath binds values from the request path to the fields of the provided target struct based on struct tags.
	BindPath(target interface{}) error

	// BindHeaders binds HTTP header values to the fields of the provided target struct based on matching tags or field names.
	BindHeaders(target interface{}) error

	// JSON sends a JSON response with the specified HTTP status code and data payload, serialized into JSON format.
	JSON(status int, data interface{}) error

	// XML serializes the given data to XML format and writes it to the response with the specified HTTP status code.
	XML(status int, data interface{}) error

	// String constructs an error based on the provided HTTP status code and message. Returns the formatted error.
	String(status int, message string) error

	// Status sets the HTTP status code for the response and returns the updated Context instance.
	Status(status int) Context

	// Header sets a key-value pair in the response header and returns the updated Context.
	Header(key, value string) Context

	// ClientIP returns the client's IP address as a string, typically derived from the request's headers or remote address.
	ClientIP() string

	// UserAgent returns the user agent string associated with the current client or request.
	UserAgent() string

	// Method returns a string result based on its implementation logic.
	Method() string

	// Path returns the file path or directory path as a string.
	Path() string

	// Query retrieves a string representation of a query to be executed, typically used for database or API requests.
	Query() string

	// Referer retrieves the Referer header from the incoming HTTP request, returning it as a string.
	Referer() string

	// ContentType returns the MIME type of the content as a string.
	ContentType() string

	// PathParam retrieves the value of a path parameter identified by the given key. Returns an empty string if not found.
	PathParam(key string) string

	// QueryParam retrieves the value of the specified query parameter from the URL by its key.
	QueryParam(key string) string

	// FormValue retrieves the first value associated with the given key from the form data.
	// It is used for accessing POST or PUT form values in requests.
	FormValue(key string) string

	// SetPathParams sets the path parameters for the current context and returns the updated context.
	SetPathParams(params map[string]string) Context

	// SetRoutePattern sets the route pattern for the current context and returns the updated Context.
	SetRoutePattern(pattern string) Context

	// ParseMultipartForm parses a request's multipart form data, storing up to maxMemory bytes in memory and the rest on disk.
	// Returns the parsed multipart.Form and any error encountered during parsing.
	ParseMultipartForm(maxMemory int64) (*multipart.Form, error)

	// FormFile retrieves the first file for the specified form key from the multipart form.
	// It returns the file, its header, and any potential error encountered while accessing the file.
	FormFile(key string) (multipart.File, *multipart.FileHeader, error)
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
	Middleware     []any
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

// AddMiddleware safely adds middleware to RouteHandlerInfo
func (rhi *RouteHandlerInfo) AddMiddleware(middleware interface{}) error {
	entry, err := NewMiddlewareEntry(middleware)
	if err != nil {
		return err
	}
	rhi.Middleware = append(rhi.Middleware, entry)
	return nil
}

// GetMiddlewareHandlers returns just the handler functions
func (rhi *RouteHandlerInfo) GetMiddlewareHandlers() []Middleware {
	handlers := make([]Middleware, len(rhi.Middleware))
	for i, entry := range rhi.Middleware {
		me, err := NewMiddlewareEntry(entry)
		if err != nil {
			continue
		}
		handlers[i] = me.Handler
	}
	return handlers
}

// // WithService sets the service name and type for the handler
// func WithService(name string, serviceType reflect.Type) HandlerOption {
// 	return func(info *RouteHandlerInfo) {
// 		info.ServiceName = name
// 		info.ServiceType = serviceType
// 	}
// }
//
// // WithMiddleware adds middleware to the handler
// func WithMiddleware(middleware ...Middleware) HandlerOption {
// 	return func(info *RouteHandlerInfo) {
// 		info.Middleware = append(info.Middleware, middleware...)
// 	}
// }
//
// // WithDependencies sets dependencies for the handler
// func WithDependencies(dependencies ...string) HandlerOption {
// 	return func(info *RouteHandlerInfo) {
// 		info.Dependencies = dependencies
// 	}
// }
//
// // WithConfig sets config for the handler
// func WithConfig(config interface{}) HandlerOption {
// 	return func(info *RouteHandlerInfo) {
// 		info.Config = config
// 	}
// }
//
// // WithTags adds tags to the handler
// func WithTags(tags map[string]string) HandlerOption {
// 	return func(info *RouteHandlerInfo) {
// 		info.Tags = tags
// 	}
// }
//
// // WithOpinionated sets opinionated mode for schema generation
// func WithOpinionated(opinionated bool) HandlerOption {
// 	return func(info *RouteHandlerInfo) {
// 		info.Opinionated = opinionated
// 	}
// }
//
// // WithTypes sets request and response types for the handler
// func WithTypes(reqType, respType reflect.Type) HandlerOption {
// 	return func(info *RouteHandlerInfo) {
// 		info.RequestType = reqType
// 		info.ResponseType = respType
// 	}
// }

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
	Middleware   []any             `json:"middleware"`
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
	Name          string            `json:"name"`
	Type          interface{}       `json:"-"`
	Constructor   interface{}       `json:"-"`
	Instance      interface{}       `json:"-"`
	Singleton     bool              `json:"singleton"`
	Dependencies  []string          `json:"dependencies"`
	Config        interface{}       `json:"config"`
	Lifecycle     ServiceLifecycle  `json:"-"`
	Extensions    map[string]any    `json:"extensions"`
	Tags          map[string]string `json:"tags"`
	ReferenceName string            `json:"reference_names"`
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

// GetReferenceName returns the reference name for this service
func (sd *ServiceDefinition) GetReferenceName() string {
	// Return explicit reference name if set
	if sd.ReferenceName != "" {
		return sd.ReferenceName
	}

	// Backward compatibility support for Extensions
	if extRef, exists := sd.Extensions["referenceName"]; exists {
		if refName, ok := extRef.(string); ok {
			return refName
		}
	}

	return ""
}

// GetAllReferenceNames returns the reference name for this service
func (sd *ServiceDefinition) GetAllReferenceNames() []string {
	// Return explicit reference name if set
	if sd.ReferenceName != "" {
		return []string{sd.ReferenceName}
	}

	// Backward compatibility support for Extensions
	if extRef, exists := sd.Extensions["referenceName"]; exists {
		if refName, ok := extRef.(string); ok {
			return []string{refName}
		}
	}

	return []string{""}
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

// ConfigManager defines the comprehensive interface for configuration management
type ConfigManager interface {
	// Lifecycle
	Name() string

	// Loading and management
	LoadFrom(sources ...ConfigSource) error
	Watch(ctx context.Context) error
	Reload() error
	ReloadContext(ctx context.Context) error
	Validate() error
	Stop() error

	// Basic getters
	Get(key string) interface{}
	GetString(key string) string
	GetInt(key string) int
	GetInt8(key string) int8
	GetInt16(key string) int16
	GetInt32(key string) int32
	GetInt64(key string) int64
	GetUint(key string) uint
	GetUint8(key string) uint8
	GetUint16(key string) uint16
	GetUint32(key string) uint32
	GetUint64(key string) uint64
	GetFloat32(key string) float32
	GetFloat64(key string) float64
	GetBool(key string) bool
	GetDuration(key string) time.Duration
	GetTime(key string) time.Time
	GetSizeInBytes(key string) uint64

	// Collection getters
	GetStringSlice(key string) []string
	GetIntSlice(key string) []int
	GetInt64Slice(key string) []int64
	GetFloat64Slice(key string) []float64
	GetBoolSlice(key string) []bool
	GetStringMap(key string) map[string]string
	GetStringMapString(key string) map[string]string
	GetStringMapStringSlice(key string) map[string][]string

	// Getters with default values
	GetWithDefault(key string, val any) interface{}
	GetStringWithDefault(key string, val string) string
	GetIntWithDefault(key string, val int) int
	GetInt8WithDefault(key string, val int8) int8
	GetInt16WithDefault(key string, val int16) int16
	GetInt32WithDefault(key string, val int32) int32
	GetInt64WithDefault(key string, val int64) int64
	GetUintWithDefault(key string, val uint) uint
	GetUint8WithDefault(key string, val uint8) uint8
	GetUint16WithDefault(key string, val uint16) uint16
	GetUint32WithDefault(key string, val uint32) uint32
	GetUint64WithDefault(key string, val uint64) uint64
	GetFloat32WithDefault(key string, val float32) float32
	GetFloat64WithDefault(key string, val float64) float64
	GetBoolWithDefault(key string, val bool) bool
	GetDurationWithDefault(key string, val time.Duration) time.Duration
	GetTimeWithDefault(key string, val time.Time) time.Time
	GetSizeInBytesWithDefault(key string, val uint64) uint64

	// Collection getters with defaults
	GetStringSliceWithDefault(key string, val []string) []string
	GetIntSliceWithDefault(key string, val []int) []int
	GetInt64SliceWithDefault(key string, val []int64) []int64
	GetFloat64SliceWithDefault(key string, val []float64) []float64
	GetBoolSliceWithDefault(key string, val []bool) []bool
	GetStringMapWithDefault(key string, val map[string]string) map[string]string
	GetStringMapStringSliceWithDefault(key string, val map[string][]string) map[string][]string

	// Configuration modification
	Set(key string, value interface{})

	// Binding methods
	Bind(key string, target interface{}) error
	BindWithDefault(key string, target interface{}, defaultValue interface{}) error
	BindWithOptions(key string, target interface{}, options BindOptions) error

	// Watching and callbacks
	WatchWithCallback(key string, callback func(string, interface{}))
	WatchChanges(callback func(ConfigChange))

	// Metadata and introspection
	GetSourceMetadata() map[string]*SourceMetadata
	GetKeys() []string
	GetSection(key string) map[string]interface{}
	HasKey(key string) bool
	IsSet(key string) bool
	Size() int

	// Structure operations
	Sub(key string) ConfigManager
	MergeWith(other ConfigManager) error
	Clone() ConfigManager
	GetAllSettings() map[string]interface{}

	// Utility methods
	Reset()
	ExpandEnvVars() error
	SafeGet(key string, expectedType reflect.Type) (interface{}, error)

	// Compatibility aliases
	GetBytesSize(key string) uint64
	GetBytesSizeWithDefault(key string, defaultValue uint64) uint64
	InConfig(key string) bool
	UnmarshalKey(key string, rawVal interface{}) error
	Unmarshal(rawVal interface{}) error
	AllKeys() []string
	AllSettings() map[string]interface{}
	ReadInConfig() error
	SetConfigType(configType string)
	SetConfigFile(filePath string) error
	ConfigFileUsed() string
	WatchConfig() error
	OnConfigChange(callback func(ConfigChange))
}

// ConfigSource represents a source of configuration data
type ConfigSource interface {
	// Name returns the unique name of the configuration source
	Name() string

	// Priority returns the priority of this source (higher = more important)
	Priority() int

	// Load loads configuration data from the source
	Load(ctx context.Context) (map[string]interface{}, error)

	// Watch starts watching for configuration changes
	Watch(ctx context.Context, callback func(map[string]interface{})) error

	// StopWatch stops watching for configuration changes
	StopWatch() error

	// Reload forces a reload of the configuration source
	Reload(ctx context.Context) error

	// IsWatchable returns true if the source supports watching for changes
	IsWatchable() bool

	// SupportsSecrets returns true if the source supports secret management
	SupportsSecrets() bool

	// GetSecret retrieves a secret value from the source
	GetSecret(ctx context.Context, key string) (string, error)
}

// SourceMetadata contains metadata about a configuration source
type SourceMetadata struct {
	Name         string                 `json:"name"`
	Priority     int                    `json:"priority"`
	Type         string                 `json:"type"`
	LastLoaded   time.Time              `json:"last_loaded"`
	LastModified time.Time              `json:"last_modified"`
	IsWatching   bool                   `json:"is_watching"`
	KeyCount     int                    `json:"key_count"`
	ErrorCount   int                    `json:"error_count"`
	LastError    string                 `json:"last_error,omitempty"`
	Properties   map[string]interface{} `json:"properties"`
}

// ConfigChange represents a configuration change event
type ConfigChange struct {
	Source    string      `json:"source"`
	Type      ChangeType  `json:"type"`
	Key       string      `json:"key"`
	OldValue  interface{} `json:"old_value,omitempty"`
	NewValue  interface{} `json:"new_value,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// ChangeType represents the type of configuration change
type ChangeType string

const (
	ChangeTypeSet    ChangeType = "set"
	ChangeTypeUpdate ChangeType = "update"
	ChangeTypeDelete ChangeType = "delete"
	ChangeTypeReload ChangeType = "reload"
)

// BindOptions provides flexible options for binding configuration to structs
type BindOptions struct {
	// DefaultValue to use when the key is not found or is nil
	DefaultValue interface{}

	// UseDefaults when true, uses struct field default values for missing keys
	UseDefaults bool

	// IgnoreCase when true, performs case-insensitive key matching
	IgnoreCase bool

	// TagName specifies which struct tag to use for field mapping (default: "yaml")
	TagName string

	// Required specifies field names that must be present in the configuration
	Required []string

	// ErrorOnMissing when true, returns error if any field cannot be bound
	ErrorOnMissing bool

	// DeepMerge when true, performs deep merging for nested structs
	DeepMerge bool
}

// DefaultBindOptions returns default bind options
func DefaultBindOptions() BindOptions {
	return BindOptions{
		UseDefaults:    true,
		IgnoreCase:     false,
		TagName:        "yaml",
		Required:       []string{},
		ErrorOnMissing: false,
		DeepMerge:      true,
	}
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
