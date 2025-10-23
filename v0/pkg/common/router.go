package common

import (
	"context"
	"net/http"
	"reflect"
	"time"
)

// =============================================================================
// ROUTER INTERFACE
// =============================================================================

// Router defines the interface for HTTP routing with service integration
type Router interface {

	// Mount registers an HTTP handler on the given pattern for routing requests to the appropriate handler.
	Mount(pattern string, handler http.Handler)

	// Group creates a sub-router for organizing routes under a common path prefix and returns the new Router instance.
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

	// HealthCheck checks the health and operational status of the router and returns an error if any issues are detected.
	HealthCheck(ctx context.Context) error

	// ServeHTTP handles HTTP requests and dispatches them to the appropriate handlers based on the routing configuration.
	ServeHTTP(w http.ResponseWriter, r *http.Request)

	// Handler returns the underlying http.Handler used by the router for request handling.
	Handler() http.Handler

	// GetStats returns the current
	GetStats() RouterStats

	// GetRouteStats retrieves statistics
	GetRouteStats(method, path string) (*RouteStats, error)

	// RegisterPluginRoute registers a new route for a plugin by its ID and returns the route ID or an error on failure.
	RegisterPluginRoute(pluginID string, route RouteDefinition) (string, error)

	// UnregisterPluginRoute removes a previously registered plugin route identified by the given routeID.
	UnregisterPluginRoute(routeID string) error

	// GetPluginRoutes retrieves a list of registered route IDs associated with the specified plugin ID.
	GetPluginRoutes(pluginID string) []string

	SetCurrentPlugin(pluginID string) // SetCurrentPlugin sets the current plugin context for routing operations.

	ClearCurrentPlugin() // ClearCurrentPlugin resets the router's context to unset the currently active plugin.
}

// =============================================================================
// ROUTE DEFINITIONS
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

// =============================================================================
// ROUTER STATISTICS
// =============================================================================

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
