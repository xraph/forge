package router

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/xraph/forge/v2/internal/di"
	"github.com/xraph/forge/v2/internal/shared"
)

// router implements Router interface
type router struct {
	adapter      RouterAdapter
	container    di.Container
	logger       Logger
	errorHandler ErrorHandler
	recovery     bool

	routes      []*route
	middleware  []Middleware
	prefix      string
	groupConfig *GroupConfig

	// OpenAPI support
	openAPIConfig    *OpenAPIConfig
	openAPIGenerator interface {
		Generate() *OpenAPISpec
		RegisterEndpoints()
	}

	// Observability support
	metrics       shared.Metrics
	healthManager shared.HealthManager
	metricsConfig *shared.MetricsConfig
	healthConfig  *shared.HealthConfig

	mu sync.RWMutex
}

// route represents a registered route
type route struct {
	method     string
	path       string
	handler    any
	config     RouteConfig
	converted  http.Handler
	middleware []Middleware
}

// newRouter creates a new router instance
func newRouter(opts ...RouterOption) *router {
	cfg := &routerConfig{
		recovery: false,
	}

	for _, opt := range opts {
		opt.Apply(cfg)
	}

	r := &router{
		adapter:       cfg.adapter,
		container:     cfg.container,
		logger:        cfg.logger,
		errorHandler:  cfg.errorHandler,
		recovery:      cfg.recovery,
		routes:        make([]*route, 0),
		middleware:    make([]Middleware, 0),
		openAPIConfig: cfg.openAPIConfig,
		metricsConfig: cfg.metricsConfig,
		healthConfig:  cfg.healthConfig,
	}

	// Create default BunRouter adapter if none provided
	if r.adapter == nil {
		r.adapter = newDefaultBunRouterAdapter()
	}

	// Setup OpenAPI if configured
	r.setupOpenAPI()

	// // Setup observability
	// r.setupObservability()

	return r
}

// GET registers a GET route
func (r *router) GET(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodGet, path, handler, opts...)
}

// POST registers a POST route
func (r *router) POST(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodPost, path, handler, opts...)
}

// PUT registers a PUT route
func (r *router) PUT(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodPut, path, handler, opts...)
}

// DELETE registers a DELETE route
func (r *router) DELETE(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodDelete, path, handler, opts...)
}

// PATCH registers a PATCH route
func (r *router) PATCH(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodPatch, path, handler, opts...)
}

// OPTIONS registers an OPTIONS route
func (r *router) OPTIONS(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodOptions, path, handler, opts...)
}

// HEAD registers a HEAD route
func (r *router) HEAD(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodHead, path, handler, opts...)
}

// Group creates a route group
func (r *router) Group(prefix string, opts ...GroupOption) Router {
	cfg := &GroupConfig{}
	for _, opt := range opts {
		opt.Apply(cfg)
	}

	return &router{
		adapter:      r.adapter,
		container:    r.container,
		logger:       r.logger,
		errorHandler: r.errorHandler,
		recovery:     r.recovery,
		routes:       r.routes, // Share routes slice
		middleware:   append([]Middleware{}, r.middleware...),
		prefix:       r.prefix + prefix,
		groupConfig:  cfg,
	}
}

// Use adds middleware
func (r *router) Use(middleware ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, middleware...)
}

// RegisterController registers a controller
func (r *router) RegisterController(controller Controller) error {
	// Create a router for the controller
	cr := r

	// Apply prefix if controller implements ControllerWithPrefix
	if pc, ok := controller.(ControllerWithPrefix); ok {
		cr = r.Group(pc.Prefix()).(*router)
	}

	// Apply middleware if controller implements ControllerWithMiddleware
	if mc, ok := controller.(ControllerWithMiddleware); ok {
		cr.Use(mc.Middleware()...)
	}

	// Register routes
	return controller.Routes(cr)
}

// Start initializes the router
func (r *router) Start(ctx context.Context) error {
	// TODO: Implement lifecycle
	return nil
}

// Stop shuts down the router
func (r *router) Stop(ctx context.Context) error {
	// TODO: Implement lifecycle
	if r.adapter != nil {
		return r.adapter.Close()
	}
	return nil
}

// ServeHTTP implements http.Handler
func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.adapter != nil {
		r.adapter.ServeHTTP(w, req)
	} else {
		http.NotFound(w, req)
	}
}

// Handler returns the router as http.Handler
func (r *router) Handler() http.Handler {
	return r
}

// Routes returns all registered routes
func (r *router) Routes() []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]RouteInfo, len(r.routes))
	for i, route := range r.routes {
		infos[i] = RouteInfo{
			Name:        route.config.Name,
			Method:      route.method,
			Path:        route.path,
			Pattern:     route.path,
			Handler:     route.handler,
			Middleware:  route.middleware,
			Tags:        route.config.Tags,
			Metadata:    route.config.Metadata,
			Extensions:  route.config.Extensions,
			Summary:     route.config.Summary,
			Description: route.config.Description,
		}
	}
	return infos
}

// RouteByName returns a route by name
func (r *router) RouteByName(name string) (RouteInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, route := range r.routes {
		if route.config.Name == name {
			return RouteInfo{
				Name:        route.config.Name,
				Method:      route.method,
				Path:        route.path,
				Pattern:     route.path,
				Handler:     route.handler,
				Middleware:  route.middleware,
				Tags:        route.config.Tags,
				Metadata:    route.config.Metadata,
				Extensions:  route.config.Extensions,
				Summary:     route.config.Summary,
				Description: route.config.Description,
			}, true
		}
	}
	return RouteInfo{}, false
}

// RoutesByTag returns routes with a specific tag
func (r *router) RoutesByTag(tag string) []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var infos []RouteInfo
	for _, route := range r.routes {
		for _, t := range route.config.Tags {
			if t == tag {
				infos = append(infos, RouteInfo{
					Name:        route.config.Name,
					Method:      route.method,
					Path:        route.path,
					Pattern:     route.path,
					Handler:     route.handler,
					Middleware:  route.middleware,
					Tags:        route.config.Tags,
					Metadata:    route.config.Metadata,
					Extensions:  route.config.Extensions,
					Summary:     route.config.Summary,
					Description: route.config.Description,
				})
				break
			}
		}
	}
	return infos
}

// RoutesByMetadata returns routes with specific metadata
func (r *router) RoutesByMetadata(key string, value any) []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var infos []RouteInfo
	for _, route := range r.routes {
		if v, ok := route.config.Metadata[key]; ok && v == value {
			infos = append(infos, RouteInfo{
				Name:        route.config.Name,
				Method:      route.method,
				Path:        route.path,
				Pattern:     route.path,
				Handler:     route.handler,
				Middleware:  route.middleware,
				Tags:        route.config.Tags,
				Metadata:    route.config.Metadata,
				Extensions:  route.config.Extensions,
				Summary:     route.config.Summary,
				Description: route.config.Description,
			})
		}
	}
	return infos
}

// register registers a route (internal)
func (r *router) register(method, path string, handler any, opts ...RouteOption) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Build route config
	cfg := &RouteConfig{}
	for _, opt := range opts {
		opt.Apply(cfg)
	}

	// Inherit group config
	if r.groupConfig != nil {
		cfg.Tags = append(cfg.Tags, r.groupConfig.Tags...)
		cfg.Middleware = append(r.groupConfig.Middleware, cfg.Middleware...)
		for k, v := range r.groupConfig.Metadata {
			if cfg.Metadata == nil {
				cfg.Metadata = make(map[string]any)
			}
			if _, exists := cfg.Metadata[k]; !exists {
				cfg.Metadata[k] = v
			}
		}
	}

	// Combine with router middleware
	combinedMiddleware := append([]Middleware{}, r.middleware...)
	combinedMiddleware = append(combinedMiddleware, cfg.Middleware...)

	fullPath := r.prefix + path

	// Convert handler to http.Handler
	converted, err := convertHandler(handler, r.container, r.errorHandler)
	if err != nil {
		return fmt.Errorf("failed to convert handler: %w", err)
	}

	// Create route
	rt := &route{
		method:     method,
		path:       fullPath,
		handler:    handler,
		config:     *cfg,
		converted:  converted,
		middleware: combinedMiddleware,
	}

	r.routes = append(r.routes, rt)

	// Register with adapter
	if r.adapter != nil {
		finalHandler := applyMiddleware(converted, combinedMiddleware)
		r.adapter.Handle(method, fullPath, finalHandler)
	}

	return nil
}

// newDefaultBunRouterAdapter creates the default BunRouter adapter
func newDefaultBunRouterAdapter() RouterAdapter {
	return NewBunRouterAdapter()
}

func applyMiddleware(h http.Handler, middleware []Middleware) http.Handler {
	// Apply in reverse order
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

// simpleAdapter is a basic in-memory adapter for testing
type simpleAdapter struct {
	routes map[string]map[string]http.Handler // method -> path -> handler
}

func (a *simpleAdapter) Handle(method, path string, handler http.Handler) {
	if a.routes[method] == nil {
		a.routes[method] = make(map[string]http.Handler)
	}
	a.routes[method][path] = handler
}

func (a *simpleAdapter) Mount(path string, handler http.Handler) {
	// Simple implementation: mount on all methods
	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"} {
		a.Handle(method, path, handler)
	}
}

func (a *simpleAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if handlers, ok := a.routes[r.Method]; ok {
		if handler, ok := handlers[r.URL.Path]; ok {
			handler.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

func (a *simpleAdapter) Close() error {
	a.routes = nil
	return nil
}
