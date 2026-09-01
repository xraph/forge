package router

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/shared"
	forge_http "github.com/xraph/go-utils/http"
	"github.com/xraph/vessel"
)

// Context keys for internal use.
const (
	// ContextKeySensitiveFieldCleaning is the context key for sensitive field cleaning flag.
	ContextKeySensitiveFieldCleaning = "forge:sensitive_field_cleaning"
)

// router implements Router interface.
type router struct {
	adapter      RouterAdapter
	container    vessel.Vessel
	logger       Logger
	errorHandler ErrorHandler
	recovery     bool
	httpAddress  string // HTTP server address for automatic localhost server in OpenAPI

	routes      *[]*route // Pointer to shared slice for groups
	middleware  []Middleware
	prefix      string
	groupConfig *GroupConfig

	// routeRev counts every mutation of the shared route table. The spec
	// generators use it to tell whether a document they already built is still
	// current, so they can hand back the cached one instead of rebuilding it
	// per request. Shared with groups, exactly like routes and mu.
	//
	// Anything that ever adds to, removes from or edits *routes must bump this;
	// a mutation that does not is a stale spec.
	routeRev *atomic.Uint64

	// ext is the adapter's wide interface when it implements one, and nil
	// otherwise. caps is its self-report, read once so the hot path never
	// re-asserts.
	ext  ExtendedAdapter
	caps Capabilities

	// configErr holds a Configure failure. NewRouter returns a Router with no
	// error, so the failure is surfaced from the first register() call
	// instead of being swallowed.
	configErr error

	// OpenAPI support
	openAPIConfig    *OpenAPIConfig
	openAPIGenerator interface {
		Generate() (*OpenAPISpec, error)
		RegisterEndpoints()
	}

	// AsyncAPI support
	asyncAPIConfig    *AsyncAPIConfig
	asyncAPIGenerator interface {
		Generate() (*AsyncAPISpec, error)
		RegisterEndpoints()
	}

	// Observability support
	metrics       shared.Metrics
	healthManager shared.HealthManager
	metricsConfig *shared.MetricsConfig
	healthConfig  *shared.HealthConfig

	// WebTransport support
	webTransportEnabled bool
	webTransportConfig  WebTransportConfig
	http3Server         any // *http3.Server

	// WebSocket origin allow-list. Empty means same-origin only; see origin.go.
	webSocketOrigins []string

	// maxBodySize caps request bodies for every route. 0 means use
	// DefaultMaxRequestBodySize; negative means unlimited.
	maxBodySize int64

	mu *sync.RWMutex // Pointer to shared mutex for groups
}

// route represents a registered route.
type route struct {
	method     string
	path       string
	handler    any
	config     RouteConfig
	converted  http.Handler
	middleware []Middleware
	kind       RouteKind
}

// newRouteInfo projects an internal route onto the RouteInfo that inspection
// callers, interceptors and the spec generators all see.
//
// This is deliberately the only place that knows how to perform that
// projection. It used to be open-coded at every accessor, and the copies drifted:
// Routes() carried OperationID, Deprecated and Timeout while RouteByName(),
// RoutesByTag(), RoutesByMetadata() and the interceptor-facing info built during
// register() silently returned their zero values. Adding a field to RouteInfo
// without touching five constructors regenerates that bug, so there is one.
//
// Name intentionally reports OperationID in preference to the route's own name:
// openapi_generator.go reads RouteInfo.Name as the operation id. That
// conflation predates RouteInfo.OperationID and is preserved here so every
// constructor at least agrees on it — see the note on Name in RouteInfo.
func newRouteInfo(rt *route) RouteInfo {
	name := rt.config.Name
	if rt.config.OperationID != "" {
		name = rt.config.OperationID
	}

	return RouteInfo{
		Name:                   name,
		Method:                 rt.method,
		Path:                   rt.path,
		Pattern:                rt.path,
		Handler:                rt.handler,
		Middleware:             rt.middleware,
		Tags:                   rt.config.Tags,
		Metadata:               rt.config.Metadata,
		Extensions:             rt.config.Extensions,
		Summary:                rt.config.Summary,
		Description:            rt.config.Description,
		Kind:                   rt.kind,
		Interceptors:           rt.config.Interceptors,
		SkipInterceptors:       rt.config.SkipInterceptors,
		SensitiveFieldCleaning: rt.config.SensitiveFieldCleaning,
		OperationID:            rt.config.OperationID,
		Deprecated:             rt.config.Deprecated,
		Timeout:                rt.config.Timeout,
	}
}

// newRouter creates a new router instance.
func newRouter(opts ...RouterOption) *router {
	cfg := &routerConfig{
		recovery: false,
	}

	for _, opt := range opts {
		opt.Apply(cfg)
	}

	routes := make([]*route, 0)
	mu := &sync.RWMutex{} // Shared mutex for all groups
	r := &router{
		adapter:        cfg.adapter,
		container:      cfg.container,
		logger:         cfg.logger,
		errorHandler:   cfg.errorHandler,
		recovery:       cfg.recovery,
		httpAddress:    cfg.httpAddress,
		routes:         &routes, // Pointer to slice for sharing with groups
		routeRev:       &atomic.Uint64{},
		middleware:     make([]Middleware, 0),
		openAPIConfig:  cfg.openAPIConfig,
		asyncAPIConfig: cfg.asyncAPIConfig,
		metricsConfig:  cfg.metricsConfig,
		healthConfig:   cfg.healthConfig,

		webSocketOrigins: cfg.webSocketOrigins,
		maxBodySize:      cfg.maxBodySize,

		mu: mu, // Initialize shared mutex
	}

	// Create default BunRouter adapter if none provided
	if r.adapter == nil {
		r.adapter = newDefaultBunRouterAdapter()
	}

	// Detect the wide interface once, here, so no call site ever re-asserts.
	if ext, ok := r.adapter.(ExtendedAdapter); ok {
		r.ext = ext
		r.caps = ext.Capabilities()

		if err := ext.Configure(MatcherConfig{}); err != nil {
			r.configErr = fmt.Errorf("router: adapter configuration failed: %w", err)
		}
	}

	// Setup OpenAPI if configured
	r.setupOpenAPI()

	// Setup AsyncAPI if configured
	r.setupAsyncAPI()

	// // Setup observability
	// r.setupObservability()

	return r
}

// GET registers a GET route.
func (r *router) GET(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodGet, path, handler, opts...)
}

// POST registers a POST route.
func (r *router) POST(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodPost, path, handler, opts...)
}

// PUT registers a PUT route.
func (r *router) PUT(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodPut, path, handler, opts...)
}

// DELETE registers a DELETE route.
func (r *router) DELETE(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodDelete, path, handler, opts...)
}

// PATCH registers a PATCH route.
func (r *router) PATCH(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodPatch, path, handler, opts...)
}

// OPTIONS registers an OPTIONS route.
func (r *router) OPTIONS(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodOptions, path, handler, opts...)
}

// HEAD registers a HEAD route.
func (r *router) HEAD(path string, handler any, opts ...RouteOption) error {
	return r.register(http.MethodHead, path, handler, opts...)
}

// Any registers a route for all HTTP methods.
// This is useful for handlers that need to handle multiple methods,
// including pure Go http.Handler implementations.
func (r *router) Any(path string, handler any, opts ...RouteOption) error {
	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodOptions,
		http.MethodHead,
	}

	var errs []error

	for _, method := range methods {
		if err := r.register(method, path, handler, opts...); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", method, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to register some methods: %v", errs)
	}

	return nil
}

// Handle mounts an http.Handler at the given path, handling all HTTP methods.
// This behaves like http.Handle() - the handler is directly mounted and receives
// all requests to the path regardless of HTTP method.
//
// This is the most direct way to integrate existing http.Handlers, file servers,
// or other routers. Unlike Any(), Handle() bypasses the forge handler conversion
// and middleware system - the handler receives requests directly.
//
// Example:
//
//	// Mount a file server
//	fs := http.FileServer(http.Dir("./static"))
//	router.Handle("/static/*", http.StripPrefix("/static/", fs))
//
//	// Mount another router/mux
//	router.Handle("/api/*", apiRouter)
func (r *router) Handle(path string, handler http.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	fullPath := r.prefix + path

	if r.adapter != nil {
		r.adapter.Mount(fullPath, handler)
	}

	return nil
}

// Group creates a route group.
func (r *router) Group(prefix string, opts ...GroupOption) Router {
	cfg := &GroupConfig{}

	// Inherit parent group config if this is a nested group
	if r.groupConfig != nil {
		// Copy parent's tags
		cfg.Tags = append([]string{}, r.groupConfig.Tags...)

		// Copy parent's metadata (but NOT middleware - that's inherited via router.middleware)
		if len(r.groupConfig.Metadata) > 0 {
			cfg.Metadata = make(map[string]any)
			maps.Copy(cfg.Metadata, r.groupConfig.Metadata)
		}

		// Copy parent's interceptors
		cfg.Interceptors = append([]Interceptor{}, r.groupConfig.Interceptors...)

		// Copy parent's skip interceptors
		if len(r.groupConfig.SkipInterceptors) > 0 {
			cfg.SkipInterceptors = make(map[string]bool)
			maps.Copy(cfg.SkipInterceptors, r.groupConfig.SkipInterceptors)
		}
	}

	// Apply new options (can override/extend parent config)
	for _, opt := range opts {
		opt.Apply(cfg)
	}

	return &router{
		adapter:      r.adapter,
		container:    r.container,
		logger:       r.logger,
		errorHandler: r.errorHandler,
		recovery:     r.recovery,
		routes:       r.routes,   // Share routes pointer (all groups use same slice)
		routeRev:     r.routeRev, // Share the revision counter with the same slice
		middleware:   append([]Middleware{}, r.middleware...),
		prefix:       r.prefix + prefix,
		groupConfig:  cfg,

		// Groups inherit the parent's WebSocket origin policy; otherwise a
		// group-registered socket would silently fall back to same-origin only.
		webSocketOrigins: r.webSocketOrigins,
		maxBodySize:      r.maxBodySize,

		// Groups share the parent's adapter, so they must share what forge
		// learned about it. Without this a group-registered route would fall
		// back to the narrow Handle path.
		ext:       r.ext,
		caps:      r.caps,
		configErr: r.configErr,

		mu: r.mu, // Share mutex with parent (CRITICAL for thread safety)
	}
}

// Use adds middleware.
func (r *router) Use(middleware ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.middleware = append(r.middleware, middleware...)

	// Note: Middleware is applied during route registration (see register() method)
	// This ensures proper scoping - router/group middleware only applies to routes
	// registered through that router/group instance
}

// UseGlobal adds middleware globally to ALL routes.
// This middleware will be applied to every route in the application,
// regardless of which router or group it was registered through.
// This is useful for extensions and cross-cutting concerns like CORS, logging, security.
//
// When called on a group, the middleware still applies globally to all routes,
// not just routes in that group. This ensures consistent behavior regardless of
// where UseGlobal is called from.
func (r *router) UseGlobal(middleware ...Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Register as global middleware with the adapter
	// This ensures middleware runs for ALL routes
	if r.adapter != nil {
		for _, mw := range middleware {
			// Convert forge.Middleware to http.Handler middleware
			httpMiddleware := convertForgeMiddlewareToHTTP(mw, r.container, r.errorHandler)
			r.adapter.UseGlobal(httpMiddleware)
		}
	}
}

// RegisterController registers a controller.
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

// Start initializes the router.
func (r *router) Start(ctx context.Context) error {
	// TODO: Implement lifecycle
	return nil
}

// Stop shuts down the router.
func (r *router) Stop(ctx context.Context) error {
	// TODO: Implement lifecycle
	if r.adapter != nil {
		return r.adapter.Close()
	}

	return nil
}

// ServeHTTP implements http.Handler.
func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Middleware is applied during route registration (see register() method),
	// so we just delegate to the adapter which has routes with middleware already applied.
	// This prevents double-wrapping of middleware.
	if r.adapter != nil {
		r.adapter.ServeHTTP(w, req)
	} else {
		http.NotFound(w, req)
	}
}

// Handler returns the router as http.Handler.
func (r *router) Handler() http.Handler {
	return r
}

// Routes returns all registered routes.
func (r *router) Routes() []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := *r.routes // Dereference pointer

	infos := make([]RouteInfo, len(routes))
	for i, route := range routes {
		infos[i] = newRouteInfo(route)
	}

	return infos
}

// routeRevisionSource is a router that can say whether its route table has
// changed since it was last asked. The spec generators use it to cache a
// generated document; a router that does not implement it simply never gets a
// cache hit, so the optimisation is opt-in and cannot produce a stale spec.
type routeRevisionSource interface {
	routeRevision() (uint64, bool)
}

// routeRevision returns the current revision of the shared route table, and
// whether it is being tracked at all. A router assembled without the counter
// reports false, which keeps its spec uncached rather than frozen.
func (r *router) routeRevision() (uint64, bool) {
	if r.routeRev == nil {
		return 0, false
	}

	return r.routeRev.Load(), true
}

// bumpRouteRevision records that the shared route table changed.
func (r *router) bumpRouteRevision() {
	if r.routeRev != nil {
		r.routeRev.Add(1)
	}
}

// RouteByName returns a route by name.
func (r *router) RouteByName(name string) (RouteInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, route := range *r.routes {
		if route.config.Name == name {
			return newRouteInfo(route), true
		}
	}

	return RouteInfo{}, false
}

// RoutesByTag returns routes with a specific tag.
func (r *router) RoutesByTag(tag string) []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var infos []RouteInfo

	for _, route := range *r.routes {
		if slices.Contains(route.config.Tags, tag) {
			infos = append(infos, newRouteInfo(route))
		}
	}

	return infos
}

// RoutesByMetadata returns routes with specific metadata.
func (r *router) RoutesByMetadata(key string, value any) []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var infos []RouteInfo

	for _, route := range *r.routes {
		if v, ok := route.config.Metadata[key]; ok && v == value {
			infos = append(infos, newRouteInfo(route))
		}
	}

	return infos
}

// register registers a route (internal).
func (r *router) register(method, path string, handler any, opts ...RouteOption) error {
	if r.configErr != nil {
		return r.configErr
	}

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

		// Clone the group slice before appending. Appending straight onto
		// r.groupConfig.Middleware writes into the group's backing array
		// whenever it has spare capacity, so every route in the group would
		// overwrite the previous route's entries.
		cfg.Middleware = append(slices.Clone(r.groupConfig.Middleware), cfg.Middleware...)

		for k, v := range r.groupConfig.Metadata {
			if cfg.Metadata == nil {
				cfg.Metadata = make(map[string]any)
			}

			if _, exists := cfg.Metadata[k]; !exists {
				cfg.Metadata[k] = v
			}
		}

		// Inherit group interceptors (group interceptors run before route
		// interceptors). Cloned for the same reason as Middleware above —
		// cfg is shallow-copied into route.config and closed over by
		// applyMiddlewareAndInterceptors, so an aliased slice would let one
		// route silently inherit a sibling's interceptors.
		cfg.Interceptors = append(slices.Clone(r.groupConfig.Interceptors), cfg.Interceptors...)

		// Merge skip interceptors from group
		if len(r.groupConfig.SkipInterceptors) > 0 {
			if cfg.SkipInterceptors == nil {
				cfg.SkipInterceptors = make(map[string]bool)
			}

			for k, v := range r.groupConfig.SkipInterceptors {
				if _, exists := cfg.SkipInterceptors[k]; !exists {
					cfg.SkipInterceptors[k] = v
				}
			}
		}
	}

	// Combine router middleware with route-specific middleware
	// Router middleware (from Use()) runs first, then route middleware (from WithMiddleware())
	// This ensures proper scoping - middleware from groups only applies to routes in that group
	combinedMiddleware := append([]Middleware{}, r.middleware...)
	combinedMiddleware = append(combinedMiddleware, cfg.Middleware...)

	fullPath := r.prefix + path

	// Detect handler pattern to extract type information for OpenAPI
	handlerInfo, err := detectHandlerPattern(handler)
	if err != nil {
		return fmt.Errorf("failed to detect handler pattern: %w", err)
	}

	// Convert handler to http.Handler
	converted, err := convertHandler(handler, r.container, r.errorHandler)
	if err != nil {
		return fmt.Errorf("failed to convert handler: %w", err)
	}

	// Store handler type info in metadata for OpenAPI generation
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	// Only store if not already manually specified
	if handlerInfo != nil {
		if _, hasRequestSchema := cfg.Metadata["openapi.requestSchema"]; !hasRequestSchema && handlerInfo.requestType != nil {
			cfg.Metadata["openapi.requestType"] = handlerInfo.requestType
		}

		if _, hasResponseSchema := cfg.Metadata["openapi.responseSchema"]; !hasResponseSchema && handlerInfo.responseType != nil {
			cfg.Metadata["openapi.responseType"] = handlerInfo.responseType
		}

		cfg.Metadata["openapi.handlerPattern"] = handlerInfo.pattern
	}

	// Create route
	rt := &route{
		method:     method,
		path:       fullPath,
		handler:    handler,
		config:     *cfg,
		converted:  converted,
		middleware: combinedMiddleware,
		kind:       cfg.Kind,
	}

	// Append to shared routes slice (dereference, append, update)
	*r.routes = append(*r.routes, rt)

	// Bump after the append, never before: a reader that sees the new revision
	// must be able to see the route it accounts for.
	r.bumpRouteRevision()

	// Build RouteInfo for interceptors (they need access to route metadata).
	// Same projection the inspection accessors use, so an interceptor and a
	// Routes() caller never disagree about the route they are looking at.
	routeInfo := newRouteInfo(rt)

	// Validate the path once, here, so a route that could never match fails at
	// startup with a legible error instead of 404ing silently or panicking
	// inside a backend. Adapters re-parse the same string and can assume it is
	// well formed.
	if _, err := pathspec.Parse(fullPath); err != nil {
		return err
	}

	// Register with adapter
	if r.adapter != nil {
		// Apply all middleware and interceptors
		// Execution order: middleware -> interceptors -> handler
		finalHandler := applyMiddlewareAndInterceptors(
			converted,
			combinedMiddleware,
			cfg.Interceptors,
			cfg.SkipInterceptors,
			routeInfo,
			r.container,
			r.errorHandler,
			r.resolveMaxBodySize(cfg.MaxBodySize),
		)

		// Wrap with panic recovery if enabled
		if r.recovery {
			inner := finalHandler
			logger := r.logger
			finalHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				defer func() {
					if rec := recover(); rec != nil {
						if logger != nil {
							logger.Error(fmt.Sprintf("panic recovered: %v", rec))
						}

						http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					}
				}()

				inner.ServeHTTP(w, req)
			})
		}

		r.adapter.Handle(method, fullPath, finalHandler)
	}

	return nil
}

// newDefaultBunRouterAdapter creates the default BunRouter adapter.
func newDefaultBunRouterAdapter() RouterAdapter {
	return NewBunRouterAdapter()
}

func applyMiddleware(h http.Handler, middleware []Middleware, container vessel.Vessel, errorHandler ErrorHandler) http.Handler {
	// Convert http.Handler to forge Handler
	forgeHandler := func(ctx Context) error {
		h.ServeHTTP(ctx.Response(), ctx.Request())

		return nil
	}

	// Apply middleware chain
	for i := len(middleware) - 1; i >= 0; i-- {
		forgeHandler = middleware[i](forgeHandler)
	}

	// Convert back to http.Handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create forge context
		ctx := forge_http.NewContext(w, r, container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Execute the forge handler
		if err := forgeHandler(ctx); err != nil {
			// Error handling
			if errorHandler != nil {
				_ = errorHandler.HandleError(ctx.Context(), err)
			} else {
				// Same mapping the typed-handler path uses, so an error
				// carries its status and envelope whether it was returned
				// from a handler or from middleware. Falling back to
				// http.Error here turned every middleware error into a
				// 500 with a plain-text body — a middleware returning
				// Unauthorized produced a 500, which pushed callers into
				// hand-writing ctx.JSON responses instead.
				handleError(ctx, err)
			}
		}
	})
}

// DefaultMaxRequestBodySize is the request body cap applied when neither the
// router nor the route specifies one. Chosen to be generous for JSON APIs while
// still bounding a single request's allocation; raise it per route for upload
// endpoints via WithMaxBodySize, or set a negative value for no limit.
const DefaultMaxRequestBodySize int64 = 10 << 20 // 10 MiB

// resolveMaxBodySize picks the effective body cap for a route: the route's own
// value if set, otherwise the router's, otherwise the default. A negative value
// at either level means unlimited and is returned as 0 (no limiter installed).
func (r *router) resolveMaxBodySize(routeMax int64) int64 {
	size := routeMax
	if size == 0 {
		size = r.maxBodySize
	}

	if size == 0 {
		size = DefaultMaxRequestBodySize
	}

	if size < 0 {
		return 0 // unlimited
	}

	return size
}

// applyMiddlewareAndInterceptors applies middleware chain and interceptors to a handler.
// Execution order: middleware -> interceptors -> handler
// If any interceptor blocks, the handler is not executed.
func applyMiddlewareAndInterceptors(
	h http.Handler,
	middleware []Middleware,
	interceptors []Interceptor,
	skipInterceptors map[string]bool,
	routeInfo RouteInfo,
	container vessel.Vessel,
	errorHandler ErrorHandler,
	maxBodySize int64,
) http.Handler {
	// Convert http.Handler to forge Handler that includes interceptor execution
	forgeHandler := func(ctx Context) error {
		// Execute interceptors before the handler
		if len(interceptors) > 0 {
			if err := executeInterceptors(ctx, routeInfo, interceptors, skipInterceptors); err != nil {
				return err
			}
		}

		// All interceptors passed, execute the handler
		h.ServeHTTP(ctx.Response(), ctx.Request())

		return nil
	}

	// Apply middleware chain (middleware wraps the handler + interceptors)
	for i := len(middleware) - 1; i >= 0; i-- {
		forgeHandler = middleware[i](forgeHandler)
	}

	// A long-lived route bypasses the per-handler timeout. The string
	// comparison this replaces omitted "webtransport", so WebTransport
	// sessions were given a deadline they could not survive.
	isStreaming := routeInfo.Kind.LongLived()

	// Convert back to http.Handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set sensitive field cleaning flag in request context if route has it enabled
		// This allows nested handlers to access the flag even when they create new forge contexts
		if routeInfo.SensitiveFieldCleaning {
			r = r.WithContext(context.WithValue(r.Context(), forge_http.ContextKeyForSensitiveCleaning, true))
		}

		// Cap the request body before any handler, binder or middleware can
		// read it. Without this, decoding is bounded only by what the client
		// chooses to send, so a single unauthenticated request can drive
		// arbitrary allocation. MaxBytesReader also signals the server to
		// close the connection once the cap is hit.
		if maxBodySize > 0 && r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		}

		// Apply per-route timeout for non-streaming handlers. The server-level
		// WriteTimeout is 0 (disabled) to support SSE/WebSocket, so regular
		// REST handlers use a context deadline instead.
		if !isStreaming && routeInfo.Timeout > 0 {
			timeoutCtx, cancel := context.WithTimeout(r.Context(), routeInfo.Timeout)
			defer cancel()
			r = r.WithContext(timeoutCtx)
		}

		// Create forge context
		ctx := forge_http.NewContext(w, r, container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Also set in forge context for direct access
		if routeInfo.SensitiveFieldCleaning {
			ctx.Set(ContextKeySensitiveFieldCleaning, true)
		}

		// Execute the forge handler (middleware -> interceptors -> handler)
		if err := forgeHandler(ctx); err != nil {
			// Error handling
			if errorHandler != nil {
				_ = errorHandler.HandleError(ctx.Context(), err)
			} else {
				// Same mapping the typed-handler path uses, so an error
				// carries its status and envelope whether it was returned
				// from a handler or from middleware. Falling back to
				// http.Error here turned every middleware error into a
				// 500 with a plain-text body — a middleware returning
				// Unauthorized produced a 500, which pushed callers into
				// hand-writing ctx.JSON responses instead.
				handleError(ctx, err)
			}
		}
	})
}

// convertForgeMiddlewareToHTTP converts a forge.Middleware to http.Handler middleware.
// This is used to register forge middleware as global middleware with the router adapter.
func convertForgeMiddlewareToHTTP(mw Middleware, container vessel.Vessel, errorHandler ErrorHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create forge context
			ctx := forge_http.NewContext(w, r, container)
			defer ctx.(forge_http.ContextWithClean).Cleanup()

			// Convert next http.Handler to forge.Handler
			nextForgeHandler := func(ctx Context) error {
				next.ServeHTTP(ctx.Response(), ctx.Request())

				return nil
			}

			// Apply the middleware
			wrappedHandler := mw(nextForgeHandler)

			// Execute the wrapped handler
			if err := wrappedHandler(ctx); err != nil {
				// Error handling
				if errorHandler != nil {
					_ = errorHandler.HandleError(ctx.Context(), err)
				} else {
					// See applyMiddleware — global middleware maps errors
					// the same way route middleware does.
					handleError(ctx, err)
				}
			}
		})
	}
}
