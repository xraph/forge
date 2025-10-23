package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/julienschmidt/httprouter"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// HttpRouterAdapter adapts httprouter.Router to implement RouterAdapter interface
type HttpRouterAdapter struct {
	router     *httprouter.Router
	middleware []func(http.Handler) http.Handler
	logger     common.Logger
	started    bool
	mu         sync.RWMutex

	// For lifecycle management
	server *http.Server
	addr   string
}

// HttpRouterConfig contains configuration for the httprouter adapter
type HttpRouterConfig struct {
	Logger                 common.Logger
	RedirectTrailingSlash  bool
	RedirectFixedPath      bool
	HandleMethodNotAllowed bool
	HandleOPTIONS          bool
	GlobalOPTIONS          http.Handler
	NotFound               http.Handler
	MethodNotAllowed       http.Handler
	PanicHandler           func(http.ResponseWriter, *http.Request, interface{})
	Addr                   string // For server lifecycle management
}

// NewHttpRouterAdapter creates a new httprouter adapter
func NewHttpRouterAdapter(config HttpRouterConfig) RouterAdapter {
	hr := httprouter.New()

	// Configure httprouter options
	hr.RedirectTrailingSlash = config.RedirectTrailingSlash
	hr.RedirectFixedPath = config.RedirectFixedPath
	hr.HandleMethodNotAllowed = config.HandleMethodNotAllowed
	hr.HandleOPTIONS = config.HandleOPTIONS

	if config.GlobalOPTIONS != nil {
		// hr.GlobalOPTIONS = config.GlobalOPTIONS
	}

	if config.NotFound != nil {
		hr.NotFound = config.NotFound
	}
	if config.MethodNotAllowed != nil {
		hr.MethodNotAllowed = config.MethodNotAllowed
	}
	if config.PanicHandler != nil {
		hr.PanicHandler = config.PanicHandler
	}

	addr := config.Addr
	if addr == "" {
		addr = ":8080"
	}

	return &HttpRouterAdapter{
		router:     hr,
		middleware: make([]func(http.Handler) http.Handler, 0),
		logger:     config.Logger,
		addr:       addr,
	}
}

// =============================================================================
// HTTP METHOD HANDLERS
// =============================================================================

func (h *HttpRouterAdapter) GET(path string, handler http.HandlerFunc) {
	h.router.GET(path, h.wrapHandler(handler))
}

func (h *HttpRouterAdapter) POST(path string, handler http.HandlerFunc) {
	h.router.POST(path, h.wrapHandler(handler))
}

func (h *HttpRouterAdapter) PUT(path string, handler http.HandlerFunc) {
	h.router.PUT(path, h.wrapHandler(handler))
}

func (h *HttpRouterAdapter) DELETE(path string, handler http.HandlerFunc) {
	h.router.DELETE(path, h.wrapHandler(handler))
}

func (h *HttpRouterAdapter) PATCH(path string, handler http.HandlerFunc) {
	h.router.PATCH(path, h.wrapHandler(handler))
}

func (h *HttpRouterAdapter) HEAD(path string, handler http.HandlerFunc) {
	h.router.HEAD(path, h.wrapHandler(handler))
}

func (h *HttpRouterAdapter) OPTIONS(path string, handler http.HandlerFunc) {
	h.router.OPTIONS(path, h.wrapHandler(handler))
}

func (h *HttpRouterAdapter) Handle(method, path string, handler http.HandlerFunc) {
	h.router.Handle(method, path, h.wrapHandler(handler))
}

// =============================================================================
// MIDDLEWARE AND MOUNTING
// =============================================================================

func (h *HttpRouterAdapter) Use(middleware func(http.Handler) http.Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.middleware = append(h.middleware, middleware)
}

func (h *HttpRouterAdapter) Mount(pattern string, handler http.Handler) {
	// httprouter doesn't support mounting directly, so we simulate it
	// by creating a catch-all route that strips the prefix and forwards to the handler

	// Ensure pattern doesn't end with slash for consistency
	pattern = strings.TrimSuffix(pattern, "/")

	// Create catch-all route
	catchAll := pattern + "/*catchall"

	h.router.Handle("GET", catchAll, h.mountHandler(pattern, handler))
	h.router.Handle("POST", catchAll, h.mountHandler(pattern, handler))
	h.router.Handle("PUT", catchAll, h.mountHandler(pattern, handler))
	h.router.Handle("DELETE", catchAll, h.mountHandler(pattern, handler))
	h.router.Handle("PATCH", catchAll, h.mountHandler(pattern, handler))
	h.router.Handle("HEAD", catchAll, h.mountHandler(pattern, handler))
	h.router.Handle("OPTIONS", catchAll, h.mountHandler(pattern, handler))

	// Also handle exact pattern match (without trailing path)
	h.router.Handle("GET", pattern, h.mountHandler(pattern, handler))
	h.router.Handle("POST", pattern, h.mountHandler(pattern, handler))
	h.router.Handle("PUT", pattern, h.mountHandler(pattern, handler))
	h.router.Handle("DELETE", pattern, h.mountHandler(pattern, handler))
	h.router.Handle("PATCH", pattern, h.mountHandler(pattern, handler))
	h.router.Handle("HEAD", pattern, h.mountHandler(pattern, handler))
	h.router.Handle("OPTIONS", pattern, h.mountHandler(pattern, handler))
}

// =============================================================================
// GROUPING SUPPORT
// =============================================================================

func (h *HttpRouterAdapter) Group(prefix string) RouterAdapter {
	// httprouter doesn't support native grouping, so we create a wrapper
	return NewHttpRouterGroup(h, prefix)
}

func (h *HttpRouterAdapter) SupportsGroups() bool {
	return false // httprouter doesn't have native group support
}

// =============================================================================
// PARAMETER EXTRACTION
// =============================================================================

func (h *HttpRouterAdapter) ExtractParams(r *http.Request) map[string]string {
	params := make(map[string]string)

	// Get httprouter params from context
	if ps := httprouter.ParamsFromContext(r.Context()); ps != nil {
		for _, param := range ps {
			params[param.Key] = param.Value
		}
	}

	return params
}

func (h *HttpRouterAdapter) ParamFormat() ParamFormat {
	return ParamFormatColon // httprouter uses :param format
}

// =============================================================================
// SERVER INTEGRATION
// =============================================================================

func (h *HttpRouterAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Apply middleware chain
	handler := h.applyMiddleware(h.router)
	handler.ServeHTTP(w, r)
}

func (h *HttpRouterAdapter) Handler() http.Handler {
	return h.applyMiddleware(h.router)
}

// =============================================================================
// LIFECYCLE MANAGEMENT
// =============================================================================

func (h *HttpRouterAdapter) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.started {
		return fmt.Errorf("httprouter adapter already started")
	}

	// Create HTTP server
	h.server = &http.Server{
		Addr:    h.addr,
		Handler: h.Handler(),
	}

	h.started = true

	if h.logger != nil {
		h.logger.Info("httprouter adapter started",
			logger.String("addr", h.addr),
		)
	}

	// Start server in a goroutine
	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if h.logger != nil {
				h.logger.Error("httprouter server error", logger.Error(err))
			}
		}
	}()

	return nil
}

func (h *HttpRouterAdapter) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.started {
		return fmt.Errorf("httprouter adapter not started")
	}

	if h.server != nil {
		if err := h.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown httprouter server: %w", err)
		}
	}

	h.started = false

	if h.logger != nil {
		h.logger.Info("httprouter adapter stopped")
	}

	return nil
}

// =============================================================================
// ROUTER INFO
// =============================================================================

func (h *HttpRouterAdapter) GetRouterInfo() RouterInfo {
	return RouterInfo{
		Name:           "httprouter",
		Version:        "1.3.0", // Current httprouter version
		SupportsGroups: false,
		SupportsMount:  true,
		ParamFormat:    ":param",
	}
}

// =============================================================================
// INTERNAL HELPER METHODS
// =============================================================================

// wrapHandler converts http.HandlerFunc to httprouter.Handle
func (h *HttpRouterAdapter) wrapHandler(handler http.HandlerFunc) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// Store params in request context for later extraction
		ctx := context.WithValue(r.Context(), httprouter.ParamsKey, ps)
		r = r.WithContext(ctx)

		// Call the handler
		handler(w, r)
	}
}

// mountHandler creates a handler that strips prefix and forwards to mounted handler
func (h *HttpRouterAdapter) mountHandler(prefix string, handler http.Handler) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// Strip the prefix from the request path
		originalPath := r.URL.Path

		if strings.HasPrefix(originalPath, prefix) {
			r.URL.Path = strings.TrimPrefix(originalPath, prefix)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}

		// Call the mounted handler
		handler.ServeHTTP(w, r)

		// Restore original path
		r.URL.Path = originalPath
	}
}

// applyMiddleware applies all registered middleware to the handler
func (h *HttpRouterAdapter) applyMiddleware(handler http.Handler) http.Handler {
	h.mu.RLock()
	middleware := make([]func(http.Handler) http.Handler, len(h.middleware))
	copy(middleware, h.middleware)
	h.mu.RUnlock()

	// Apply middleware in reverse order (last added, first applied)
	result := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		result = middleware[i](result)
	}

	return result
}

// =============================================================================
// GROUP WRAPPER
// =============================================================================

// HttpRouterGroup implements RouterAdapter by wrapping an HttpRouterAdapter with a prefix
type HttpRouterGroup struct {
	parent *HttpRouterAdapter
	prefix string
}

// NewHttpRouterGroup creates a new group wrapper
func NewHttpRouterGroup(parent *HttpRouterAdapter, prefix string) *HttpRouterGroup {
	// Ensure prefix starts with / and doesn't end with /
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")

	return &HttpRouterGroup{
		parent: parent,
		prefix: prefix,
	}
}

func (g *HttpRouterGroup) GET(path string, handler http.HandlerFunc) {
	g.parent.GET(g.combinePath(path), handler)
}

func (g *HttpRouterGroup) POST(path string, handler http.HandlerFunc) {
	g.parent.POST(g.combinePath(path), handler)
}

func (g *HttpRouterGroup) PUT(path string, handler http.HandlerFunc) {
	g.parent.PUT(g.combinePath(path), handler)
}

func (g *HttpRouterGroup) DELETE(path string, handler http.HandlerFunc) {
	g.parent.DELETE(g.combinePath(path), handler)
}

func (g *HttpRouterGroup) PATCH(path string, handler http.HandlerFunc) {
	g.parent.PATCH(g.combinePath(path), handler)
}

func (g *HttpRouterGroup) HEAD(path string, handler http.HandlerFunc) {
	g.parent.HEAD(g.combinePath(path), handler)
}

func (g *HttpRouterGroup) OPTIONS(path string, handler http.HandlerFunc) {
	g.parent.OPTIONS(g.combinePath(path), handler)
}

func (g *HttpRouterGroup) Handle(method, path string, handler http.HandlerFunc) {
	g.parent.Handle(method, g.combinePath(path), handler)
}

func (g *HttpRouterGroup) Mount(pattern string, handler http.Handler) {
	g.parent.Mount(g.combinePath(pattern), handler)
}

func (g *HttpRouterGroup) Use(middleware func(http.Handler) http.Handler) {
	g.parent.Use(middleware)
}

func (g *HttpRouterGroup) Group(prefix string) RouterAdapter {
	return NewHttpRouterGroup(g.parent, g.combinePath(prefix))
}

func (g *HttpRouterGroup) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.parent.ServeHTTP(w, r)
}

func (g *HttpRouterGroup) Handler() http.Handler {
	return g.parent.Handler()
}

func (g *HttpRouterGroup) ExtractParams(r *http.Request) map[string]string {
	return g.parent.ExtractParams(r)
}

func (g *HttpRouterGroup) GetRouterInfo() RouterInfo {
	info := g.parent.GetRouterInfo()
	info.SupportsGroups = true // Group wrapper provides group support
	return info
}

func (g *HttpRouterGroup) SupportsGroups() bool {
	return true
}

func (g *HttpRouterGroup) ParamFormat() ParamFormat {
	return g.parent.ParamFormat()
}

func (g *HttpRouterGroup) Start(ctx context.Context) error {
	return g.parent.Start(ctx)
}

func (g *HttpRouterGroup) Stop(ctx context.Context) error {
	return g.parent.Stop(ctx)
}

// combinePath combines the group prefix with a relative path
func (g *HttpRouterGroup) combinePath(path string) string {
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
// FACTORY FUNCTION
// =============================================================================

// NewHttpRouterAdapterWithDefaults creates an httprouter adapter with sensible defaults
func NewHttpRouterAdapterWithDefaults(l common.Logger, addr string) RouterAdapter {
	return NewHttpRouterAdapter(HttpRouterConfig{
		Logger:                 l,
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      true,
		HandleMethodNotAllowed: true,
		HandleOPTIONS:          true,
		Addr:                   addr,
		PanicHandler: func(w http.ResponseWriter, r *http.Request, err interface{}) {
			if l != nil {
				l.Error("httprouter panic recovered",
					logger.String("method", r.Method),
					logger.String("path", r.URL.Path),
					logger.Any("panic", err),
				)
			}

			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		},
	})
}
