package plugins

import (
	"reflect"

	"github.com/xraph/forge/pkg/common"
)

// PluginRouteBuilder provides a fluent API for building plugin routes
type PluginRouteBuilder struct {
	routes []common.RouteDefinition
}

// NewPluginRouteBuilder creates a new route builder
func NewPluginRouteBuilder() *PluginRouteBuilder {
	return &PluginRouteBuilder{
		routes: make([]common.RouteDefinition, 0),
	}
}

// GET adds a GET route with automatic service injection
func (rb *PluginRouteBuilder) GET(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteBuilder {
	return rb.addRoute("GET", path, handler, options...)
}

// POST adds a POST route with automatic service injection
func (rb *PluginRouteBuilder) POST(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteBuilder {
	return rb.addRoute("POST", path, handler, options...)
}

// PUT adds a PUT route with automatic service injection
func (rb *PluginRouteBuilder) PUT(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteBuilder {
	return rb.addRoute("PUT", path, handler, options...)
}

// DELETE adds a DELETE route with automatic service injection
func (rb *PluginRouteBuilder) DELETE(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteBuilder {
	return rb.addRoute("DELETE", path, handler, options...)
}

// PATCH adds a PATCH route with automatic service injection
func (rb *PluginRouteBuilder) PATCH(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteBuilder {
	return rb.addRoute("PATCH", path, handler, options...)
}

// addRoute is the internal method that builds route definitions
func (rb *PluginRouteBuilder) addRoute(method, path string, handler interface{}, options ...common.HandlerOption) *PluginRouteBuilder {
	// Analyze handler signature for automatic type detection
	handlerType := reflect.TypeOf(handler)
	var requestType, responseType reflect.Type
	var dependencies []string

	if handlerType.Kind() == reflect.Func {
		// Extract types from handler signature
		requestType, responseType, dependencies = rb.analyzeHandlerSignature(handlerType)
	}

	// Create route definition
	route := common.RouteDefinition{
		Method:       method,
		Pattern:      path,
		Handler:      handler,
		Dependencies: dependencies,
		Tags:         make(map[string]string),
		Opinionated:  true, // Plugin routes are opinionated by default
		RequestType:  requestType,
		ResponseType: responseType,
		Middleware:   []any{},
	}

	// Apply options
	info := &common.RouteHandlerInfo{
		Method:       method,
		Path:         path,
		Tags:         make(map[string]string),
		Dependencies: dependencies,
		RequestType:  requestType,
		ResponseType: responseType,
	}

	for _, option := range options {
		option(info)
	}

	// Copy processed info back to route
	route.Tags = info.Tags
	route.Dependencies = info.Dependencies
	route.Middleware = info.Middleware
	route.Config = info.Config

	rb.routes = append(rb.routes, route)
	return rb
}

// analyzeHandlerSignature extracts type information from handler function signature
func (rb *PluginRouteBuilder) analyzeHandlerSignature(handlerType reflect.Type) (requestType, responseType reflect.Type, dependencies []string) {
	if handlerType.NumIn() < 2 || handlerType.NumOut() != 2 {
		return nil, nil, nil
	}

	// Expected signatures:
	// func(ctx Context, req RequestType) (*ResponseType, error)  // Pure opinionated
	// func(ctx Context, service ServiceType, req RequestType) (*ResponseType, error)  // Service-aware
	// func(ctx Context, svc1 Service1, svc2 Service2, req RequestType) (*ResponseType, error)  // Multi-service

	// First parameter should be Context
	if handlerType.In(0) != reflect.TypeOf((*common.Context)(nil)).Elem() {
		return nil, nil, nil
	}

	// Last input is request type
	requestType = handlerType.In(handlerType.NumIn() - 1)

	// First output is response type
	responseType = handlerType.Out(0)
	if responseType.Kind() == reflect.Ptr {
		responseType = responseType.Elem()
	}

	// Extract service dependencies (parameters between Context and request)
	for i := 1; i < handlerType.NumIn()-1; i++ {
		serviceType := handlerType.In(i)
		serviceName := serviceType.Name()
		if serviceName == "" {
			serviceName = serviceType.String()
		}
		dependencies = append(dependencies, serviceName)
	}

	return requestType, responseType, dependencies
}

// Build returns the built routes
func (rb *PluginRouteBuilder) Build() []common.RouteDefinition {
	return rb.routes
}

// Group creates a route group with a prefix
func (rb *PluginRouteBuilder) Group(prefix string, configureFn func(*PluginRouteGroup)) *PluginRouteBuilder {
	group := &PluginRouteGroup{
		prefix:  prefix,
		builder: rb,
	}
	configureFn(group)
	return rb
}

// PluginRouteGroup represents a group of routes with a common prefix
type PluginRouteGroup struct {
	prefix  string
	builder *PluginRouteBuilder
}

// GET adds a GET route to the group
func (rg *PluginRouteGroup) GET(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteGroup {
	rg.builder.GET(rg.prefix+path, handler, options...)
	return rg
}

// POST adds a POST route to the group
func (rg *PluginRouteGroup) POST(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteGroup {
	rg.builder.POST(rg.prefix+path, handler, options...)
	return rg
}

// PUT adds a PUT route to the group
func (rg *PluginRouteGroup) PUT(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteGroup {
	rg.builder.PUT(rg.prefix+path, handler, options...)
	return rg
}

// DELETE adds a DELETE route to the group
func (rg *PluginRouteGroup) DELETE(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteGroup {
	rg.builder.DELETE(rg.prefix+path, handler, options...)
	return rg
}

// PATCH adds a PATCH route to the group
func (rg *PluginRouteGroup) PATCH(path string, handler interface{}, options ...common.HandlerOption) *PluginRouteGroup {
	rg.builder.PATCH(rg.prefix+path, handler, options...)
	return rg
}

// Middleware adds middleware to subsequent routes in the group
func (rg *PluginRouteGroup) Middleware(middleware ...common.Middleware) *PluginRouteGroup {
	// This would apply middleware to the next routes added
	// Implementation would need to track group state
	return rg
}

// WithOptions adds common options to all routes in the group
func (rg *PluginRouteGroup) WithOptions(options ...common.HandlerOption) *PluginRouteGroup {
	// This would apply options to subsequent routes
	return rg
}
