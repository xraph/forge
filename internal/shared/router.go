package shared

import (
	"context"
	"net/http"
	"time"

	"github.com/xraph/forge/internal/logger"
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

	// // Controller registration
	// RegisterController(controller Controller) error

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

	// // OpenAPI
	// OpenAPISpec() *OpenAPISpec
	//
	// // Streaming
	// WebSocket(path string, handler WebSocketHandler, opts ...RouteOption) error
	// EventStream(path string, handler SSEHandler, opts ...RouteOption) error
}

// RouteOption configures a route
type RouteOption interface {
	Apply(*RouteConfig)
}

// GroupOption configures a route group
type GroupOption interface {
	Apply(*GroupConfig)
}

// Middleware wraps HTTP handlers
type Middleware func(http.Handler) http.Handler

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

// RouterOption configures the router
type RouterOption interface {
	Apply(*routerConfig)
}

// routerConfig holds router configuration
type routerConfig struct {
	adapter       RouterAdapter
	container     Container
	logger        logger.Logger
	errorHandler  ErrorHandler
	recovery      bool
	openAPIConfig *OpenAPIConfig
	metricsConfig *MetricsConfig
	healthConfig  *HealthConfig
}

// RouterAdapter wraps a routing backend
type RouterAdapter interface {
	// Handle registers a route
	Handle(method, path string, handler http.Handler)

	// Mount registers a sub-handler
	Mount(path string, handler http.Handler)

	// ServeHTTP dispatches requests
	ServeHTTP(w http.ResponseWriter, r *http.Request)

	// Close cleans up resources
	Close() error
}
