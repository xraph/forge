package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/middleware"
)

// RouteGroup represents a grouping of routes with a shared prefix, middleware, and handler options.
// It enables defining nested route structures and managing middleware.
// The prefix string determines the path for route grouping under this instance.
// Middleware provides pre-processing or modifying requests before a handler executes.
// Handler options define properties or behaviors for individual route handlers.
// The router field references the underlying ForgeRouter for managing the HTTP routing logic.
type RouteGroup struct {
	router     *ForgeRouter
	prefix     string
	middleware []common.Middleware
	options    []common.HandlerOption
}

// NewRouteGroup creates a new RouteGroup with the given router and prefix, initializing middleware and handler options.
func NewRouteGroup(router *ForgeRouter, prefix string) *RouteGroup {
	return &RouteGroup{
		router:     router,
		prefix:     prefix,
		middleware: make([]common.Middleware, 0),
		options:    make([]common.HandlerOption, 0),
	}
}

// Group creates a new RouteGroup by inheriting the current group's properties, such as prefix and middleware.
func (rg *RouteGroup) Group(prefix string) common.Router {
	return &RouteGroup{
		router:     rg.router,
		prefix:     rg.prefix,
		middleware: append([]common.Middleware{}, rg.middleware...),
		options:    append([]common.HandlerOption{}, rg.options...),
	}
}

// GroupFunc creates a new RouteGroup, executes the provided function on it, and returns the resulting Router.
func (rg *RouteGroup) GroupFunc(fn func(r common.Router)) common.Router {
	group := rg.Group("")
	fn(group)
	return group
}

// Route registers a sub-route within the current RouteGroup using the specified pattern and configuration function.
func (rg *RouteGroup) Route(pattern string, fn func(r common.Router)) common.Router {
	group := &RouteGroup{
		router:     rg.router,
		prefix:     rg.combinePath(pattern),
		middleware: append([]common.Middleware{}, rg.middleware...),
		options:    append([]common.HandlerOption{}, rg.options...),
	}
	fn(group)
	return group
}

// Mount attaches an `http.Handler` to a specific route pattern within the RouteGroup's prefix.
func (rg *RouteGroup) Mount(pattern string, handler http.Handler) {
	rg.router.Mount(rg.combinePath(pattern), handler)
}

// Use appends one or more middleware definitions to the RouteGroup's middleware stack.
func (rg *RouteGroup) Use(middleware ...common.Middleware) {
	rg.middleware = append(rg.middleware, middleware...)
}

// UseOptions appends the provided handler options to the current route group's options slice.
func (rg *RouteGroup) UseOptions(options ...common.HandlerOption) {
	rg.options = append(rg.options, options...)
}

// GET registers a new handler for the HTTP GET method at the specified path with optional handler options.
func (rg *RouteGroup) GET(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("GET", path, handler, options...)
}

// POST registers an HTTP POST route with the specified path, handler, and optional handler options.
// It allows adding middleware and custom configurations for the route.
func (rg *RouteGroup) POST(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("POST", path, handler, options...)
}

// PUT registers a handler for HTTP PUT requests for the specified path with optional handler options.
func (rg *RouteGroup) PUT(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("PUT", path, handler, options...)
}

// DELETE registers a route with the HTTP DELETE method in the current RouteGroup. It takes a path, a handler, and optional settings.
func (rg *RouteGroup) DELETE(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("DELETE", path, handler, options...)
}

// PATCH registers a handler for HTTP PATCH requests at the specified path within the route group.
func (rg *RouteGroup) PATCH(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("PATCH", path, handler, options...)
}

// OPTIONS registers a handler for the OPTIONS method at the specified path with optional handler options.
func (rg *RouteGroup) OPTIONS(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("OPTIONS", path, handler, options...)
}

// HEAD registers a handler for HTTP HEAD requests at the specified path with optional handler options.
func (rg *RouteGroup) HEAD(path string, handler interface{}, options ...common.HandlerOption) error {
	return rg.registerHandler("HEAD", path, handler, options...)
}

// RegisterController registers a controller for the route group, applying the group's prefix and middleware to its routes.
func (rg *RouteGroup) RegisterController(controller common.Controller) error {
	// Controllers in groups need their routes prefixed
	wrappedController := &groupController{
		Controller: controller,
		prefix:     rg.prefix,
		middleware: rg.middleware,
	}
	return rg.router.RegisterController(wrappedController)
}

// UnregisterController removes a registered controller by its name from the route group. Returns an error if the operation fails.
func (rg *RouteGroup) UnregisterController(controllerName string) error {
	return rg.router.UnregisterController(controllerName)
}

// RegisterOpinionatedHandler registers a handler for a specific HTTP method and path with additional configurable options.
func (rg *RouteGroup) RegisterOpinionatedHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	combinedOptions := append(append([]common.HandlerOption{}, rg.options...), options...)
	return rg.router.RegisterOpinionatedHandler(method, rg.combinePath(path), handler, combinedOptions...)
}

// RegisterWebSocket registers a WebSocket handler at the specified path with optional handler options.
func (rg *RouteGroup) RegisterWebSocket(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	// Apply group prefix to path
	fullPath := rg.combinePath(path)

	// Apply any group-level streaming configurations if needed
	combinedOptions := rg.combineStreamingOptions(options...)

	return rg.router.RegisterWebSocket(fullPath, handler, combinedOptions...)
}

// RegisterSSE registers a Server-Sent Events handler at the specified path with optional handler options.
func (rg *RouteGroup) RegisterSSE(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	// Apply group prefix to path
	fullPath := rg.combinePath(path)

	// Apply any group-level streaming configurations if needed
	combinedOptions := rg.combineStreamingOptions(options...)

	return rg.router.RegisterSSE(fullPath, handler, combinedOptions...)
}

// combineStreamingOptions combines group-level and handler-specific streaming options
func (rg *RouteGroup) combineStreamingOptions(options ...common.StreamingHandlerInfo) []common.StreamingHandlerInfo {
	// For now, just return the provided options
	// In the future, we could add group-level streaming configurations
	return options
}

// EnableAsyncAPI enables AsyncAPI documentation for the route group's router
func (rg *RouteGroup) EnableAsyncAPI(config common.AsyncAPIConfig) {
	rg.router.EnableAsyncAPI(config)
}

// GetAsyncAPISpec gets the AsyncAPI specification from the router
func (rg *RouteGroup) GetAsyncAPISpec() *common.AsyncAPISpec {
	return rg.router.GetAsyncAPISpec()
}

// UpdateAsyncAPISpec updates the AsyncAPI specification
func (rg *RouteGroup) UpdateAsyncAPISpec(updater func(*common.AsyncAPISpec)) {
	rg.router.UpdateAsyncAPISpec(updater)
}

// AddMiddleware appends a middleware to the RouteGroup's middleware list and returns an error in case of failure.
func (rg *RouteGroup) AddMiddleware(middleware common.Middleware) error {
	rg.middleware = append(rg.middleware, middleware)
	return nil
}

// RemoveMiddleware removes a middleware from the RouteGroup by its name and returns an error if it is not found.
func (rg *RouteGroup) RemoveMiddleware(middlewareName string) error {
	for i, mw := range rg.middleware {
		if mw.Name() == middlewareName {
			rg.middleware = append(rg.middleware[:i], rg.middleware[i+1:]...)
			return nil
		}
	}
	return common.ErrServiceNotFound(middlewareName)
}

// UseMiddleware registers a standard middleware function to the RouteGroup, converting it into a MiddlewareDefinition.
func (rg *RouteGroup) UseMiddleware(handler func(http.Handler) http.Handler) {
	// Convert standard middleware to our format
	mw := rg.createServiceMiddlewareFromDefinition(common.MiddlewareDefinition{
		Name:     "group-middleware",
		Priority: 50,
		Handler:  handler,
	})
	rg.middleware = append(rg.middleware, mw)
}

// AddPlugin adds a plugin to the route group's underlying router and returns an error if the operation fails.
func (rg *RouteGroup) AddPlugin(plugin common.Plugin) error {
	return rg.router.AddPlugin(plugin)
}

// RemovePlugin removes a plugin by its name from the underlying router. Returns an error if the plugin is not found or failed to remove.
func (rg *RouteGroup) RemovePlugin(pluginName string) error {
	return rg.router.RemovePlugin(pluginName)
}

// EnableOpenAPI configures the OpenAPI documentation generation for the current RouteGroup using the provided configuration.
func (rg *RouteGroup) EnableOpenAPI(config common.OpenAPIConfig) {
	rg.router.EnableOpenAPI(config)
}

// GetOpenAPISpec retrieves the OpenAPI specification for the current RouteGroup.
func (rg *RouteGroup) GetOpenAPISpec() *common.OpenAPISpec {
	return rg.router.GetOpenAPISpec()
}

// UpdateOpenAPISpec updates the OpenAPI specification by applying the provided updater function.
func (rg *RouteGroup) UpdateOpenAPISpec(updater func(*common.OpenAPISpec)) {
	rg.router.UpdateOpenAPISpec(updater)
}

// Start starts the router associated with the RouteGroup within the provided context and returns any errors encountered.
func (rg *RouteGroup) Start(ctx context.Context) error {
	return rg.router.Start(ctx)
}

// Stop gracefully shuts down the router associated with the RouteGroup using the provided context.
func (rg *RouteGroup) Stop(ctx context.Context) error {
	return rg.router.Stop(ctx)
}

// HealthCheck performs a health check for the RouteGroup by delegating to the underlying router's HealthCheck method.
func (rg *RouteGroup) HealthCheck(ctx context.Context) error {
	return rg.router.HealthCheck(ctx)
}

// ServeHTTP handles incoming HTTP requests and delegates them to the underlying router.
func (rg *RouteGroup) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rg.router.ServeHTTP(w, req)
}

// Handler returns the HTTP handler associated with the RouteGroup's router.
func (rg *RouteGroup) Handler() http.Handler {
	return rg.router.Handler()
}

// GetStats returns statistics about the registered routes, middleware, controllers, and plugins in the router.
func (rg *RouteGroup) GetStats() common.RouterStats {
	return rg.router.GetStats()
}

// GetRouteStats retrieves statistics for the specified HTTP method and route path in the current RouteGroup.
// It returns a pointer to common.RouteStats containing data such as call count, latency, and error details.
// Returns an error if the route does not exist or another issue occurs during retrieval.
func (rg *RouteGroup) GetRouteStats(method, path string) (*common.RouteStats, error) {
	return rg.router.GetRouteStats(method, rg.combinePath(path))
}

// registerHandler configures a handler for a specific HTTP method and route path with optional route-level settings.
func (rg *RouteGroup) registerHandler(method, path string, handler interface{}, options ...common.HandlerOption) error {
	// Combine group options with handler-specific options
	allOptions := append(append([]common.HandlerOption{}, rg.options...), options...)

	// Add group middleware to options if any
	if len(rg.middleware) > 0 {
		allOptions = append(allOptions, WithMiddleware(rg.middleware...))
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

// combinePath constructs a full path by combining the route group's prefix and the given relative path.
// Returns "/" if both prefix and path are empty or "/".
// Ensures no duplicate slashes and adds a leading slash to the path if missing.
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

func (rg *RouteGroup) createServiceMiddlewareFromDefinition(def common.MiddlewareDefinition) middleware.Middleware {
	return &MiddlewareDefinitionAdapter{
		definition:            def,
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware(def.Name, def.Priority, def.Dependencies),
	}
}

// groupController is a struct that wraps a Controller to apply a route prefix and additional middleware.
// prefix represents the route path prefix applied to all routes of the wrapped controller.
// middleware is the list of middleware definitions specific to the group and applied to all routes.
type groupController struct {
	common.Controller
	prefix     string
	middleware []common.Middleware
}

// Routes returns the list of route definitions handled by the controller with prefixed patterns and applied middleware.
func (gc *groupController) Routes() []common.RouteDefinition {
	routes := gc.Controller.Routes()
	prefixedRoutes := make([]common.RouteDefinition, len(routes))

	for i, route := range routes {
		prefixedRoutes[i] = common.RouteDefinition{
			Method:       route.Method,
			Pattern:      gc.combinePath(route.Pattern),
			Handler:      route.Handler,
			Middleware:   append(append([]common.Middleware{}, gc.middleware...), route.Middleware...),
			Dependencies: route.Dependencies,
			Config:       route.Config,
			Tags:         route.Tags,
			Opinionated:  route.Opinionated,
			RequestType:  route.RequestType,
			ResponseType: route.ResponseType,
		}
	}

	return prefixedRoutes
}

// Middleware returns a combined list of middleware definitions from the groupController and its embedded Controller.
func (gc *groupController) Middleware() []common.Middleware {
	return append(append([]common.Middleware{}, gc.middleware...), gc.Controller.Middleware()...)
}

// combinePath combines the groupController's prefix with the provided path, ensuring proper formatting with slashes.
func (gc *groupController) combinePath(path string) string {
	if gc.prefix == "" || gc.prefix == "/" {
		return path
	}

	prefix := strings.TrimSuffix(gc.prefix, "/")
	if path == "" || path == "/" {
		return prefix
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return prefix + path
}
