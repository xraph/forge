package router

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/xraph/forge/internal/di"
	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/shared"
)

// Re-export HTTP error types and constructors for backward compatibility
type HTTPError = errors.HTTPError

var (
	NewHTTPError  = errors.NewHTTPError
	BadRequest    = errors.BadRequest
	Unauthorized  = errors.Unauthorized
	Forbidden     = errors.Forbidden
	NotFound      = errors.NotFound
	InternalError = errors.InternalError
)

// Router provides HTTP routing with multiple backend support
type Router interface {
	// HTTP Methods - register routes
	GET(path string, handler any, opts ...RouteOption) error
	POST(path string, handler any, opts ...RouteOption) error
	PUT(path string, handler any, opts ...RouteOption) error
	DELETE(path string, handler any, opts ...RouteOption) error
	PATCH(path string, handler any, opts ...RouteOption) error
	OPTIONS(path string, handler any, opts ...RouteOption) error
	HEAD(path string, handler any, opts ...RouteOption) error

	// Grouping - organize routes
	Group(prefix string, opts ...GroupOption) Router

	// Middleware - wrap handlers
	Use(middleware ...Middleware)

	// Controller registration
	RegisterController(controller Controller) error

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// HTTP serving
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Handler() http.Handler

	// Inspection
	Routes() []RouteInfo
	RouteByName(name string) (RouteInfo, bool)
	RoutesByTag(tag string) []RouteInfo
	RoutesByMetadata(key string, value any) []RouteInfo

	// OpenAPI
	OpenAPISpec() *OpenAPISpec

	// AsyncAPI
	AsyncAPISpec() *AsyncAPISpec

	// Streaming
	WebSocket(path string, handler WebSocketHandler, opts ...RouteOption) error
	EventStream(path string, handler SSEHandler, opts ...RouteOption) error

	// WebTransport
	WebTransport(path string, handler WebTransportHandler, opts ...RouteOption) error
	EnableWebTransport(config WebTransportConfig) error
	StartHTTP3(addr string, tlsConfig *tls.Config) error
	StopHTTP3() error
}

// RouteOption configures a route
type RouteOption interface {
	Apply(*RouteConfig)
}

// GroupOption configures a route group
type GroupOption interface {
	Apply(*GroupConfig)
}

// Handler is a forge handler function that takes a Context and returns an error
// This is the preferred handler pattern for forge applications
type Handler func(ctx Context) error

// Middleware wraps forge handlers (new pattern)
// This is the preferred middleware pattern for forge applications
type Middleware func(next Handler) Handler

// PureMiddleware wraps HTTP handlers
type PureMiddleware func(http.Handler) http.Handler

// RouteConfig holds route configuration
type RouteConfig struct {
	Name        string
	Summary     string
	Description string
	Tags        []string
	Middleware  []Middleware
	Timeout     time.Duration
	Metadata    map[string]any
	Extensions  map[string]Extension

	// OpenAPI metadata
	OperationID string
	Deprecated  bool
}

// GroupConfig holds route group configuration
type GroupConfig struct {
	Middleware []Middleware
	Tags       []string
	Metadata   map[string]any
}

// RouteInfo provides route information for inspection
type RouteInfo struct {
	Name        string
	Method      string
	Path        string
	Pattern     string
	Handler     any
	Middleware  []Middleware
	Tags        []string
	Metadata    map[string]any
	Extensions  map[string]Extension
	Summary     string
	Description string
}

// RouteExtension represents a route-level extension (e.g., OpenAPI, custom validation)
// Note: This is different from app-level Extension which manages app components
type RouteExtension interface {
	Name() string
	Validate() error
}

// NewRouter creates a new router with options
func NewRouter(opts ...RouterOption) Router {
	return newRouter(opts...)
}

// RouterOption configures the router
type RouterOption interface {
	Apply(*routerConfig)
}

// routerConfig holds router configuration
type routerConfig struct {
	adapter        RouterAdapter
	container      di.Container
	logger         Logger
	errorHandler   ErrorHandler
	recovery       bool
	openAPIConfig  *OpenAPIConfig
	asyncAPIConfig *AsyncAPIConfig
	metricsConfig  *shared.MetricsConfig
	healthConfig   *shared.HealthConfig
}

// RouterAdapter wraps a routing backend
type RouterAdapter = shared.RouterAdapter

// ErrorHandler handles errors from handlers
type ErrorHandler = shared.ErrorHandler

// NewDefaultErrorHandler creates a default error handler
func NewDefaultErrorHandler(l Logger) ErrorHandler {
	return shared.NewDefaultErrorHandler(l)
}

// Route option constructors
func WithName(name string) RouteOption {
	return &nameOpt{name}
}

func WithSummary(summary string) RouteOption {
	return &summaryOpt{summary}
}

func WithDescription(desc string) RouteOption {
	return &descriptionOpt{desc}
}

func WithTags(tags ...string) RouteOption {
	return &tagsOpt{tags}
}

func WithMiddleware(mw ...Middleware) RouteOption {
	return &middlewareOpt{mw}
}

func WithTimeout(d time.Duration) RouteOption {
	return &timeoutOpt{d}
}

func WithMetadata(key string, value any) RouteOption {
	return &metadataOpt{key, value}
}

func WithExtension(name string, ext Extension) RouteOption {
	return &extensionOpt{name, ext}
}

func WithOperationID(id string) RouteOption {
	return &operationIDOpt{id}
}

func WithDeprecated() RouteOption {
	return &deprecatedOpt{}
}

// Group option constructors
func WithGroupMiddleware(mw ...Middleware) GroupOption {
	return &groupMiddlewareOpt{mw}
}

func WithGroupTags(tags ...string) GroupOption {
	return &groupTagsOpt{tags}
}

func WithGroupMetadata(key string, value any) GroupOption {
	return &groupMetadataOpt{key, value}
}

// Router option constructors
func WithAdapter(adapter RouterAdapter) RouterOption {
	return &adapterOpt{adapter}
}

func WithContainer(container di.Container) RouterOption {
	return &containerOpt{container}
}

func WithLogger(logger Logger) RouterOption {
	return &loggerOpt{logger}
}

func WithErrorHandler(handler ErrorHandler) RouterOption {
	return &errorHandlerOpt{handler}
}

func WithRecovery() RouterOption {
	return &recoveryOpt{}
}
