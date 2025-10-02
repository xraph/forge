package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// RouteGroup represents a grouping of routes with a shared prefix, middleware, and handler options.
type RouteGroup struct {
	router     *ForgeRouter
	prefix     string
	middleware []middlewareEntry // Support multiple middleware types
	options    []common.HandlerOption
}

// middlewareEntry wraps different middleware types for internal storage
type middlewareEntry struct {
	name     string
	priority int
	handler  common.Middleware // The actual function
}

// NewRouteGroup creates a new RouteGroup with the given router and prefix
func NewRouteGroup(router *ForgeRouter, prefix string) *RouteGroup {
	return &RouteGroup{
		router:     router,
		prefix:     prefix,
		middleware: make([]middlewareEntry, 0),
		options:    make([]common.HandlerOption, 0),
	}
}

// =============================================================================
// MIDDLEWARE MANAGEMENT
// =============================================================================

// Use adds middleware to the route group - supports all middleware types
func (rg *RouteGroup) Use(middleware interface{}) error {
	switch m := middleware.(type) {
	case common.Middleware:
		// Function middleware - generate name and default priority
		entry := middlewareEntry{
			name:     fmt.Sprintf("group_middleware_%d", time.Now().UnixNano()),
			priority: 50,
			handler:  m,
		}
		rg.middleware = append(rg.middleware, entry)

	case common.NamedMiddleware:
		// Named middleware - DO NOT register globally, just store for route-level application
		entry := middlewareEntry{
			name:     m.Name(),
			priority: m.Priority(),
			handler:  m.Handler(),
		}
		rg.middleware = append(rg.middleware, entry)

	case common.StatefulMiddleware:
		// Stateful middleware (also implements NamedMiddleware)
		entry := middlewareEntry{
			name:     m.Name(),
			priority: m.Priority(),
			handler:  m.Handler(),
		}
		rg.middleware = append(rg.middleware, entry)

	default:
		return common.ErrInvalidConfig("middleware", fmt.Errorf("unsupported middleware type: %T", middleware))
	}

	return nil
}

// UseNamed adds named middleware with explicit name and priority
func (rg *RouteGroup) UseNamed(name string, priority int, handler common.Middleware) error {
	entry := middlewareEntry{
		name:     name,
		priority: priority,
		handler:  handler,
	}
	rg.middleware = append(rg.middleware, entry)
	return nil
}

// UseMiddleware adds a standard Go middleware function (for compatibility)
func (rg *RouteGroup) UseMiddleware(handler func(http.Handler) http.Handler) {
	entry := middlewareEntry{
		name:     fmt.Sprintf("std_middleware_%d", time.Now().UnixNano()),
		priority: 50,
		handler:  common.Middleware(handler),
	}
	rg.middleware = append(rg.middleware, entry)
}

// RemoveMiddleware removes middleware by name
func (rg *RouteGroup) RemoveMiddleware(middlewareName string) error {
	for i, entry := range rg.middleware {
		if entry.name == middlewareName {
			rg.middleware = append(rg.middleware[:i], rg.middleware[i+1:]...)
			return nil
		}
	}
	return common.ErrServiceNotFound(middlewareName)
}

// getMiddlewareHandlers returns just the handler functions for route registration
func (rg *RouteGroup) getMiddlewareHandlers() []any {
	handlers := make([]any, len(rg.middleware))
	for i, entry := range rg.middleware {
		handlers[i] = entry.handler
	}
	return handlers
}

// =============================================================================
// ROUTE GROUP MANAGEMENT
// =============================================================================

// Group creates a new RouteGroup inheriting current group's properties
func (rg *RouteGroup) Group(prefix string) common.Router {
	// Deep copy middleware entries
	newMiddleware := make([]middlewareEntry, len(rg.middleware))
	copy(newMiddleware, rg.middleware)

	return &RouteGroup{
		router:     rg.router,
		prefix:     rg.combinePath(prefix),
		middleware: newMiddleware,
		options:    append([]common.HandlerOption{}, rg.options...),
	}
}

// GroupFunc creates a new RouteGroup and executes the provided function on it
func (rg *RouteGroup) GroupFunc(fn func(r common.Router)) common.Router {
	group := rg.Group("")
	fn(group)
	return group
}

// Route registers a sub-route within the current RouteGroup
func (rg *RouteGroup) Route(pattern string, fn func(r common.Router)) common.Router {
	group := rg.Group(pattern)
	fn(group)
	return group
}

// UseOptions appends handler options to the current route group
func (rg *RouteGroup) UseOptions(options ...common.HandlerOption) {
	rg.options = append(rg.options, options...)
}

// =============================================================================
// HTTP METHOD HANDLERS
// =============================================================================

// GET registers a GET route
func (rg *RouteGroup) GET(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("GET", path, handler, options...)
}

// POST registers a POST route
func (rg *RouteGroup) POST(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("POST", path, handler, options...)
}

// PUT registers a PUT route
func (rg *RouteGroup) PUT(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("PUT", path, handler, options...)
}

// DELETE registers a DELETE route
func (rg *RouteGroup) DELETE(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("DELETE", path, handler, options...)
}

// PATCH registers a PATCH route
func (rg *RouteGroup) PATCH(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("PATCH", path, handler, options...)
}

// OPTIONS registers an OPTIONS route
func (rg *RouteGroup) OPTIONS(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("OPTIONS", path, handler, options...)
}

// HEAD registers a HEAD route
func (rg *RouteGroup) HEAD(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("HEAD", path, handler, options...)
}

// =============================================================================
// ADVANCED REGISTRATION METHODS
// =============================================================================

// RegisterController registers a controller with group prefix and middleware
func (rg *RouteGroup) RegisterController(controller common.Controller) error {
	// Initialize controller with container
	if err := controller.Initialize(rg.router.container); err != nil {
		return common.ErrContainerError("initialize_controller", err)
	}

	// Create a sub-router with the controller's prefix and middleware
	prefix := controller.Prefix()
	controllerRouter := rg.Group(prefix)

	// Add controller middleware to the sub-router
	for _, mw := range controller.Middleware() {
		if err := controllerRouter.Use(mw); err != nil {
			return fmt.Errorf("failed to register controller middleware: %w", err)
		}
	}

	// Let the controller configure its own routes
	return controller.ConfigureRoutes(controllerRouter)
}

// RegisterOpinionatedHandler registers an opinionated handler with group configuration
func (rg *RouteGroup) RegisterOpinionatedHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	combinedOptions := append(append([]common.HandlerOption{}, rg.options...), options...)

	// Add group middleware as ROUTE-SPECIFIC middleware if any
	if len(rg.middleware) > 0 {
		middlewareHandlers := rg.getMiddlewareHandlers()
		combinedOptions = append(combinedOptions, WithMiddleware(middlewareHandlers...))
	}

	return rg.router.RegisterOpinionatedHandler(method, rg.combinePath(path), handler, combinedOptions...)
}

// RegisterWebSocket registers a WebSocket handler with group configuration
func (rg *RouteGroup) RegisterWebSocket(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	fullPath := rg.combinePath(path)
	combinedOptions := rg.combineStreamingOptions(options...)
	return rg.router.RegisterWebSocket(fullPath, handler, combinedOptions...)
}

// RegisterSSE registers a Server-Sent Events handler with group configuration
func (rg *RouteGroup) RegisterSSE(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	fullPath := rg.combinePath(path)
	combinedOptions := rg.combineStreamingOptions(options...)
	return rg.router.RegisterSSE(fullPath, handler, combinedOptions...)
}

// =============================================================================
// PLUGIN SUPPORT
// =============================================================================

// RegisterPluginRoute registers a plugin route with group configuration
func (rg *RouteGroup) RegisterPluginRoute(pluginID string, route common.RouteDefinition) (string, error) {
	// Apply group prefix to the route pattern
	route.Pattern = rg.combinePath(route.Pattern)

	// Add group middleware to route middleware
	if len(rg.middleware) > 0 {
		groupHandlers := rg.getMiddlewareHandlers()
		// Ensure route.Middleware is initialized
		if route.Middleware == nil {
			route.Middleware = make([]any, 0)
		}
		// Prepend group middleware to route middleware
		route.Middleware = append(groupHandlers, route.Middleware...)
	}

	return rg.router.RegisterPluginRoute(pluginID, route)
}

// UnregisterPluginRoute removes a plugin route
func (rg *RouteGroup) UnregisterPluginRoute(routeID string) error {
	return rg.router.UnregisterPluginRoute(routeID)
}

// GetPluginRoutes returns all route IDs for a plugin
func (rg *RouteGroup) GetPluginRoutes(pluginID string) []string {
	return rg.router.GetPluginRoutes(pluginID)
}

// SetCurrentPlugin sets the current plugin context
func (rg *RouteGroup) SetCurrentPlugin(pluginID string) {
	rg.router.SetCurrentPlugin(pluginID)
}

// ClearCurrentPlugin clears the plugin context
func (rg *RouteGroup) ClearCurrentPlugin() {
	rg.router.ClearCurrentPlugin()
}

// AddPlugin adds a plugin to the underlying router
func (rg *RouteGroup) AddPlugin(plugin common.Plugin) error {
	return rg.router.AddPlugin(plugin)
}

// RemovePlugin removes a plugin from the underlying router
func (rg *RouteGroup) RemovePlugin(pluginName string) error {
	return rg.router.RemovePlugin(pluginName)
}

// =============================================================================
// MOUNTING AND DELEGATION
// =============================================================================

// Mount attaches an http.Handler to a specific route pattern
func (rg *RouteGroup) Mount(pattern string, handler http.Handler) {
	rg.router.Mount(rg.combinePath(pattern), handler)
}

// =============================================================================
// OPENAPI AND ASYNCAPI SUPPORT
// =============================================================================

// EnableOpenAPI enables OpenAPI documentation
func (rg *RouteGroup) EnableOpenAPI(config common.OpenAPIConfig) {
	rg.router.EnableOpenAPI(config)
}

// GetOpenAPISpec retrieves the OpenAPI specification
func (rg *RouteGroup) GetOpenAPISpec() *common.OpenAPISpec {
	return rg.router.GetOpenAPISpec()
}

// UpdateOpenAPISpec updates the OpenAPI specification
func (rg *RouteGroup) UpdateOpenAPISpec(updater func(*common.OpenAPISpec)) {
	rg.router.UpdateOpenAPISpec(updater)
}

// EnableAsyncAPI enables AsyncAPI documentation
func (rg *RouteGroup) EnableAsyncAPI(config common.AsyncAPIConfig) {
	rg.router.EnableAsyncAPI(config)
}

// GetAsyncAPISpec gets the AsyncAPI specification
func (rg *RouteGroup) GetAsyncAPISpec() *common.AsyncAPISpec {
	return rg.router.GetAsyncAPISpec()
}

// UpdateAsyncAPISpec updates the AsyncAPI specification
func (rg *RouteGroup) UpdateAsyncAPISpec(updater func(*common.AsyncAPISpec)) {
	rg.router.UpdateAsyncAPISpec(updater)
}

// =============================================================================
// LIFECYCLE AND SERVICE METHODS
// =============================================================================

// Start starts the underlying router
func (rg *RouteGroup) Start(ctx context.Context) error {
	return rg.router.Start(ctx)
}

// Stop stops the underlying router
func (rg *RouteGroup) Stop(ctx context.Context) error {
	return rg.router.Stop(ctx)
}

// HealthCheck performs health check on the underlying router
func (rg *RouteGroup) HealthCheck(ctx context.Context) error {
	return rg.router.HealthCheck(ctx)
}

// ServeHTTP handles HTTP requests
func (rg *RouteGroup) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rg.router.ServeHTTP(w, req)
}

// Handler returns the HTTP handler
func (rg *RouteGroup) Handler() http.Handler {
	return rg.router.Handler()
}

// =============================================================================
// STATISTICS AND MONITORING
// =============================================================================

// GetStats returns router statistics
func (rg *RouteGroup) GetStats() common.RouterStats {
	return rg.router.GetStats()
}

// GetRouteStats retrieves statistics for a specific route
func (rg *RouteGroup) GetRouteStats(method, path string) (*common.RouteStats, error) {
	return rg.router.GetRouteStats(method, rg.combinePath(path))
}

// UnregisterController removes a controller
func (rg *RouteGroup) UnregisterController(controllerName string) error {
	return rg.router.UnregisterController(controllerName)
}

// =============================================================================
// INTERNAL HELPER METHODS
// =============================================================================

// registerHandler configures a handler with group middleware and options
func (rg *RouteGroup) registerHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	// Combine group options with handler-specific options
	allOptions := append(append([]common.HandlerOption{}, rg.options...), options...)

	// Add group middleware to options if any
	if len(rg.middleware) > 0 { // Create a route-specific middleware option that wraps the handler
		middlewareHandlers := rg.getMiddlewareHandlers()
		allOptions = append(allOptions, WithMiddleware(middlewareHandlers...))
	}

	fullPath := rg.combinePath(path)
	switch method {
	case "GET":
		return rg.router.GET(fullPath, handler, allOptions...)
	case "POST":
		return rg.router.POST(fullPath, handler, allOptions...)
	case "PUT":
		return rg.router.PUT(fullPath, handler, allOptions...)
	case "DELETE":
		return rg.router.DELETE(fullPath, handler, allOptions...)
	case "PATCH":
		return rg.router.PATCH(fullPath, handler, allOptions...)
	case "OPTIONS":
		return rg.router.OPTIONS(fullPath, handler, allOptions...)
	case "HEAD":
		return rg.router.HEAD(fullPath, handler, allOptions...)
	default:
		return common.ErrInvalidConfig("method", fmt.Errorf("unsupported HTTP method: %s", method))
	}
}

// combinePath constructs a full path by combining prefix and relative path
func (rg *RouteGroup) combinePath(path string) string {
	if rg.prefix == "" || rg.prefix == "/" {
		if path == "" || path == "/" {
			return "/"
		}
		return path
	}

	prefix := strings.TrimSuffix(rg.prefix, "/")
	if path == "" || path == "/" {
		return prefix
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return prefix + path
}

// combineStreamingOptions combines group-level and handler-specific streaming options
func (rg *RouteGroup) combineStreamingOptions(options ...common.StreamingHandlerInfo) []common.StreamingHandlerInfo {
	// For now, just return the provided options
	// In the future, we could add group-level streaming configurations
	return options
}
