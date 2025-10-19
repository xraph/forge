package common

import (
	"context"
	"mime/multipart"
	"net/http"
	"time"
)

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
