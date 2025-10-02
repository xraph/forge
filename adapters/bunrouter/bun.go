package bunrouter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/uptrace/bunrouter"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/router"
)

// Adapter adapts bunrouter.Router to implement RouterAdapter interface
type Adapter struct {
	router     *bunrouter.Router
	middleware []bunrouter.MiddlewareFunc
	logger     common.Logger
	started    bool
	mu         sync.RWMutex

	// For lifecycle management
	server *http.Server
	addr   string
}

// Config contains configuration for the bunrouter adapter
type Config struct {
	Logger                  common.Logger
	Addr                    string
	NotFoundHandler         bunrouter.HandlerFunc
	MethodNotAllowedHandler bunrouter.HandlerFunc
}

// NewBunRouterAdapter creates a new bunrouter adapter
func NewBunRouterAdapter(config Config) router.RouterAdapter {
	var opts []bunrouter.Option

	if config.NotFoundHandler != nil {
		opts = append(opts, bunrouter.WithNotFoundHandler(config.NotFoundHandler))
	}
	if config.MethodNotAllowedHandler != nil {
		opts = append(opts, bunrouter.WithMethodNotAllowedHandler(config.MethodNotAllowedHandler))
	}

	br := bunrouter.New(opts...)

	addr := config.Addr
	if addr == "" {
		addr = ":8080"
	}

	return &Adapter{
		router:     br,
		middleware: make([]bunrouter.MiddlewareFunc, 0),
		logger:     config.Logger,
		addr:       addr,
	}
}

// =============================================================================
// HTTP METHOD HANDLERS
// =============================================================================

func (b *Adapter) GET(path string, handler http.HandlerFunc) {
	b.router.GET(path, b.wrapHandler(handler))
}

func (b *Adapter) POST(path string, handler http.HandlerFunc) {
	b.router.POST(path, b.wrapHandler(handler))
}

func (b *Adapter) PUT(path string, handler http.HandlerFunc) {
	b.router.PUT(path, b.wrapHandler(handler))
}

func (b *Adapter) DELETE(path string, handler http.HandlerFunc) {
	b.router.DELETE(path, b.wrapHandler(handler))
}

func (b *Adapter) PATCH(path string, handler http.HandlerFunc) {
	b.router.PATCH(path, b.wrapHandler(handler))
}

func (b *Adapter) HEAD(path string, handler http.HandlerFunc) {
	b.router.HEAD(path, b.wrapHandler(handler))
}

func (b *Adapter) OPTIONS(path string, handler http.HandlerFunc) {
	b.router.OPTIONS(path, b.wrapHandler(handler))
}

func (b *Adapter) Handle(method, path string, handler http.HandlerFunc) {
	b.router.Handle(method, path, b.wrapHandler(handler))
}

// =============================================================================
// MIDDLEWARE AND MOUNTING
// =============================================================================

func (b *Adapter) Use(middleware func(http.Handler) http.Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Convert standard middleware to bunrouter middleware
	bunMiddleware := b.convertMiddleware(middleware)
	b.middleware = append(b.middleware, bunMiddleware)
	b.router.Use(bunMiddleware)
}

func (b *Adapter) Mount(pattern string, handler http.Handler) {
	// Ensure pattern format for mounting
	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}

	// Create a handler that strips the prefix and calls the mounted handler
	bunHandler := func(w http.ResponseWriter, req bunrouter.Request) error {
		// Get the original path
		originalPath := req.URL.Path

		// Strip the mount prefix
		if strings.HasPrefix(originalPath, strings.TrimSuffix(pattern, "/")) {
			req.URL.Path = strings.TrimPrefix(originalPath, strings.TrimSuffix(pattern, "/"))
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
		}

		// Call the mounted handler with the standard http.Request
		handler.ServeHTTP(w, req.Request)

		// Restore original path
		req.URL.Path = originalPath
		return nil
	}

	// Register the catch-all route
	catchAllPattern := pattern + "*path"
	b.router.Handle("*", catchAllPattern, bunHandler)
}

// =============================================================================
// GROUPING SUPPORT
// =============================================================================

func (b *Adapter) Group(prefix string) router.RouterAdapter {
	return NewBunRouterGroup(b, prefix)
}

func (b *Adapter) SupportsGroups() bool {
	return true
}

// =============================================================================
// PARAMETER EXTRACTION
// =============================================================================

func (b *Adapter) ExtractParams(r *http.Request) map[string]string {
	params := bunrouter.ParamsFromContext(r.Context())
	return params.Map()
}

// bunRouterRequestKey is used to store the bunrouter.Request in context
type contextKey string

const bunRouterRequestKey contextKey = "bunrouter.request"

func (b *Adapter) ParamFormat() router.ParamFormat {
	return router.ParamFormatColon // BunRouter uses :param format
}

// =============================================================================
// SERVER INTEGRATION
// =============================================================================

func (b *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.router.ServeHTTP(w, r)
}

func (b *Adapter) Handler() http.Handler {
	return b.router
}

// =============================================================================
// LIFECYCLE MANAGEMENT
// =============================================================================

func (b *Adapter) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started {
		return fmt.Errorf("bunrouter adapter already started")
	}

	// Create HTTP server
	b.server = &http.Server{
		Addr:    b.addr,
		Handler: b.router,
	}

	b.started = true

	if b.logger != nil {
		b.logger.Info("bunrouter adapter started",
			logger.String("addr", b.addr),
		)
	}

	// Start server in a goroutine
	go func() {
		if err := b.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if b.logger != nil {
				b.logger.Error("bunrouter server error", logger.Error(err))
			}
		}
	}()

	return nil
}

func (b *Adapter) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.started {
		return fmt.Errorf("bunrouter adapter not started")
	}

	if b.server != nil {
		if err := b.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown bunrouter server: %w", err)
		}
	}

	b.started = false

	if b.logger != nil {
		b.logger.Info("bunrouter adapter stopped")
	}

	return nil
}

// =============================================================================
// ROUTER INFO
// =============================================================================

func (b *Adapter) GetRouterInfo() router.RouterInfo {
	return router.RouterInfo{
		Name:           "bunrouter",
		Version:        "1.0.0", // Current bunrouter version
		SupportsGroups: true,
		SupportsMount:  true,
		ParamFormat:    ":param",
	}
}

// =============================================================================
// INTERNAL HELPER METHODS
// =============================================================================

// wrapHandler converts http.HandlerFunc to bunrouter.HandlerFunc
func (b *Adapter) wrapHandler(handler http.HandlerFunc) bunrouter.HandlerFunc {
	return bunrouter.HTTPHandlerFunc(handler)
}

// convertMiddleware converts standard middleware to bunrouter middleware using BunRouter's utilities
func (b *Adapter) convertMiddleware(mw func(http.Handler) http.Handler) bunrouter.MiddlewareFunc {
	return func(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
		// Apply middleware to next (which implements http.Handler)
		wrapped := mw(next)
		// Convert back to bunrouter.HandlerFunc
		return bunrouter.HTTPHandler(wrapped)
	}
}

// extractParamsFromPath manually extracts parameters (fallback method)
func (b *Adapter) extractParamsFromPath(path string, r *http.Request) map[string]string {
	params := make(map[string]string)

	// Try to get bunrouter.Request from context to access Param method
	if bunReq, ok := r.Context().Value(bunRouterRequestKey).(*bunrouter.Request); ok {
		// Unfortunately, without knowing the route pattern, we can't extract all params
		// This is a limitation of the current approach
		// In practice, you'd need to store route patterns or use BunRouter's built-in methods

		// For common parameter names, we can try to extract them
		commonParams := []string{"id", "uuid", "name", "slug", "key", "token", "path", "file"}
		for _, paramName := range commonParams {
			if value := bunReq.Param(paramName); value != "" {
				params[paramName] = value
			}
		}
	}

	return params
}

// =============================================================================
// GROUP WRAPPER
// =============================================================================

// Group implements RouterAdapter by wrapping a bunrouter group
type Group struct {
	parent     *Adapter
	group      *bunrouter.Group
	prefix     string
	middleware []bunrouter.MiddlewareFunc // Store middleware locally
	mu         sync.RWMutex
}

// NewBunRouterGroup creates a new bunrouter group wrapper
func NewBunRouterGroup(parent *Adapter, prefix string) *Group {
	// Ensure prefix format
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")

	// Create bunrouter group
	group := parent.router.NewGroup(prefix)

	return &Group{
		parent:     parent,
		group:      group,
		prefix:     prefix,
		middleware: make([]bunrouter.MiddlewareFunc, 0),
	}
}

func (g *Group) GET(path string, handler http.HandlerFunc) {
	g.group.GET(path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) POST(path string, handler http.HandlerFunc) {
	g.group.POST(path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) PUT(path string, handler http.HandlerFunc) {
	g.group.PUT(path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) DELETE(path string, handler http.HandlerFunc) {
	g.group.DELETE(path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) PATCH(path string, handler http.HandlerFunc) {
	g.group.PATCH(path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) HEAD(path string, handler http.HandlerFunc) {
	g.group.HEAD(path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) OPTIONS(path string, handler http.HandlerFunc) {
	g.group.OPTIONS(path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) Handle(method, path string, handler http.HandlerFunc) {
	g.group.Handle(method, path, g.wrapHandlerWithMiddleware(handler))
}

func (g *Group) Mount(pattern string, handler http.Handler) {
	// Convert and mount to the group
	bunHandler := func(w http.ResponseWriter, req bunrouter.Request) error {
		// Strip the group prefix and mount pattern
		originalPath := req.URL.Path

		fullPattern := g.combinePath(pattern)
		if strings.HasPrefix(originalPath, fullPattern) {
			req.URL.Path = strings.TrimPrefix(originalPath, fullPattern)
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
		}

		handler.ServeHTTP(w, req.Request)

		// Restore original path
		req.URL.Path = originalPath
		return nil
	}

	if !strings.HasSuffix(pattern, "/") {
		pattern += "/"
	}
	pattern += "*path"

	// Apply group middleware to mount handler
	g.mu.RLock()
	for _, mw := range g.middleware {
		bunHandler = mw(bunHandler)
	}
	g.mu.RUnlock()

	g.group.Handle("*", pattern, bunHandler)
}

func (g *Group) Use(middleware func(http.Handler) http.Handler) {
	g.mu.Lock()
	defer g.mu.Unlock()

	bunMiddleware := g.convertMiddleware(middleware)
	g.middleware = append(g.middleware, bunMiddleware)
}

func (g *Group) wrapHandlerWithMiddleware(handler http.HandlerFunc) bunrouter.HandlerFunc {
	// First convert to bunrouter handler
	bunHandler := g.parent.wrapHandler(handler)

	g.mu.RLock()
	middlewares := make([]bunrouter.MiddlewareFunc, len(g.middleware))
	copy(middlewares, g.middleware)
	g.mu.RUnlock()

	// Apply group middleware in order
	for _, mw := range middlewares {
		bunHandler = mw(bunHandler)
	}

	return bunHandler
}

func (g *Group) convertMiddleware(mw func(http.Handler) http.Handler) bunrouter.MiddlewareFunc {
	return func(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
		return func(w http.ResponseWriter, req bunrouter.Request) error {
			// Debug logging
			if g.parent.logger != nil {
				g.parent.logger.Debug("bunrouter middleware executing",
					logger.String("path", req.URL.Path),
					logger.String("method", req.Method),
					logger.String("group_prefix", g.prefix),
				)
			}

			// Track if handler was called and any errors
			var handlerErr error
			handlerCalled := false

			// Create next handler that preserves bunrouter.Request context
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true

				if g.parent.logger != nil {
					g.parent.logger.Debug("bunrouter middleware calling next handler",
						logger.String("path", req.URL.Path),
					)
				}

				// CRITICAL: Call with the original bunrouter.Request, not r
				handlerErr = next(w, req)
			})

			// Apply middleware - this calls the middleware with nextHandler
			wrapped := mw(nextHandler)

			if g.parent.logger != nil {
				g.parent.logger.Debug("bunrouter middleware wrapped, executing",
					logger.String("path", req.URL.Path),
				)
			}

			// Execute middleware chain with the bunrouter.Request's underlying http.Request
			// The middleware will call nextHandler which will then call next with bunrouter.Request
			wrapped.ServeHTTP(w, req.Request)

			// Return any error from the handler
			if handlerErr != nil {
				if g.parent.logger != nil {
					g.parent.logger.Error("bunrouter handler error in middleware chain",
						logger.Error(handlerErr),
						logger.String("path", req.URL.Path),
					)
				}
				return handlerErr
			}

			if !handlerCalled {
				if g.parent.logger != nil {
					g.parent.logger.Warn("bunrouter middleware did not call next handler",
						logger.String("path", req.URL.Path),
					)
				}
			}

			return nil
		}
	}
}

func (g *Group) Group(prefix string) router.RouterAdapter {
	newPrefix := g.combinePath(prefix)
	newGroup := NewBunRouterGroup(g.parent, newPrefix)

	// Inherit parent group middleware
	g.mu.RLock()
	newGroup.middleware = make([]bunrouter.MiddlewareFunc, len(g.middleware))
	copy(newGroup.middleware, g.middleware)
	g.mu.RUnlock()

	return newGroup
}

func (g *Group) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.parent.ServeHTTP(w, r)
}

func (g *Group) Handler() http.Handler {
	return g.parent.Handler()
}

func (g *Group) ExtractParams(r *http.Request) map[string]string {
	return g.parent.ExtractParams(r)
}

func (g *Group) GetRouterInfo() router.RouterInfo {
	return g.parent.GetRouterInfo()
}

func (g *Group) SupportsGroups() bool {
	return true
}

func (g *Group) ParamFormat() router.ParamFormat {
	return g.parent.ParamFormat()
}

func (g *Group) Start(ctx context.Context) error {
	return g.parent.Start(ctx)
}

func (g *Group) Stop(ctx context.Context) error {
	return g.parent.Stop(ctx)
}

// combinePath combines the group prefix with a relative path
func (g *Group) combinePath(path string) string {
	if g.prefix == "" {
		return path
	}

	if path == "" || path == "/" {
		return g.prefix
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return g.prefix + path
}

// =============================================================================
// ADVANCED BUNROUTER FEATURES
// =============================================================================

// HandlerFunc represents a native bunrouter handler
type HandlerFunc func(w http.ResponseWriter, req bunrouter.Request) error

// RegisterBunHandler registers a native bunrouter handler (for advanced usage)
func (b *Adapter) RegisterBunHandler(method, path string, handler HandlerFunc) {
	b.router.Handle(method, path, bunrouter.HandlerFunc(handler))
}

// RegisterBunMiddleware registers native bunrouter middleware
func (b *Adapter) RegisterBunMiddleware(middleware bunrouter.MiddlewareFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.middleware = append(b.middleware, middleware)
	b.router.Use(middleware)
}

// GetBunRouter returns the underlying bunrouter.Router for advanced usage
func (b *Adapter) GetBunRouter() *bunrouter.Router {
	return b.router
}

// =============================================================================
// FACTORY FUNCTIONS
// =============================================================================

// NewBunRouterAdapterWithDefaults creates a bunrouter adapter with sensible defaults
func NewBunRouterAdapterWithDefaults(l common.Logger, addr string) router.RouterAdapter {
	return NewBunRouterAdapter(Config{
		Logger: l,
		Addr:   addr,
		NotFoundHandler: func(w http.ResponseWriter, req bunrouter.Request) error {
			if l != nil {
				l.Warn("route not found",
					logger.String("method", req.Method),
					logger.String("path", req.URL.Path),
				)
			}
			http.Error(w, "Not Found", http.StatusNotFound)
			return nil
		},
		MethodNotAllowedHandler: func(w http.ResponseWriter, req bunrouter.Request) error {
			if l != nil {
				l.Warn("method not allowed",
					logger.String("method", req.Method),
					logger.String("path", req.URL.Path),
				)
			}
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return nil
		},
	})
}

// NewHighPerformanceBunRouter creates a bunrouter adapter optimized for performance
func NewHighPerformanceBunRouter(logger common.Logger, addr string) router.RouterAdapter {
	return NewBunRouterAdapter(Config{
		Logger: logger,
		Addr:   addr,
		// Minimal error handlers for better performance
		NotFoundHandler: func(w http.ResponseWriter, req bunrouter.Request) error {
			w.WriteHeader(http.StatusNotFound)
			return nil
		},
	})
}
