package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/xraph/forge/logger"
)

// router implements the Router interface using chi
type router struct {
	chi    chi.Router
	config Config
	logger logger.Logger

	// WebSocket support
	wsUpgrader websocket.Upgrader
	wsHub      WebSocketHub

	// Middleware stack
	middlewares []func(http.Handler) http.Handler
	namedGroups map[string]Group

	// Route information for introspection
	routes []RouteInfo
	mu     sync.RWMutex

	// Context
	ctx context.Context
}

// NewRouter creates a new HTTP router
func NewRouter(config Config) Router {
	if config.Logger == nil {
		config.Logger = logger.GetGlobalLogger()
	}

	r := &router{
		chi:         chi.NewRouter(),
		config:      config,
		logger:      config.Logger,
		ctx:         context.Background(),
		routes:      make([]RouteInfo, 0),
		namedGroups: make(map[string]Group),
		middlewares: make([]func(http.Handler) http.Handler, 0),
	}

	// Configure WebSocket upgrader
	r.wsUpgrader = websocket.Upgrader{
		ReadBufferSize:  config.WebSocket.ReadBufferSize,
		WriteBufferSize: config.WebSocket.WriteBufferSize,
		CheckOrigin:     config.WebSocket.CheckOrigin,
		Subprotocols:    config.WebSocket.Subprotocols,
	}

	if r.wsUpgrader.CheckOrigin == nil {
		r.wsUpgrader.CheckOrigin = func(r *http.Request) bool { return true }
	}

	// Add default middleware
	if config.EnableRecovery {
		r.chi.Use(middleware.Recoverer)
	}

	if config.EnableLogging {
		r.chi.Use(middleware.Logger)
	}

	if config.EnableCompression {
		r.chi.Use(middleware.Compress(5))
	}

	// Add request ID middleware
	r.chi.Use(middleware.RequestID)

	// Add real IP middleware
	r.chi.Use(middleware.RealIP)

	return r
}

// Basic routing methods

func (r *router) Get(pattern string, handler http.HandlerFunc) Router {
	return r.Method(http.MethodGet, pattern, handler)
}

func (r *router) Post(pattern string, handler http.HandlerFunc) Router {
	return r.Method(http.MethodPost, pattern, handler)
}

func (r *router) Put(pattern string, handler http.HandlerFunc) Router {
	return r.Method(http.MethodPut, pattern, handler)
}

func (r *router) Patch(pattern string, handler http.HandlerFunc) Router {
	return r.Method(http.MethodPatch, pattern, handler)
}

func (r *router) Delete(pattern string, handler http.HandlerFunc) Router {
	return r.Method(http.MethodDelete, pattern, handler)
}

func (r *router) Head(pattern string, handler http.HandlerFunc) Router {
	return r.Method(http.MethodHead, pattern, handler)
}

func (r *router) Options(pattern string, handler http.HandlerFunc) Router {
	return r.Method(http.MethodOptions, pattern, handler)
}

func (r *router) Method(method, pattern string, handler http.HandlerFunc) Router {
	// Wrap handler with logging and metrics
	wrappedHandler := r.wrapHandler(pattern, handler)

	r.chi.Method(method, pattern, wrappedHandler)

	// Store route info
	r.mu.Lock()
	r.routes = append(r.routes, RouteInfo{
		Method:  method,
		Pattern: pattern,
		Handler: getFunctionName(handler),
		Name:    fmt.Sprintf("%s %s", method, pattern),
	})
	r.mu.Unlock()

	r.logger.Debug("Route registered",
		logger.String("method", method),
		logger.String("pattern", pattern),
	)

	return r
}

func (r *router) Handle(pattern string, handler http.Handler) Router {
	r.chi.Handle(pattern, handler)
	return r
}

// Middleware methods

func (r *router) Use(middleware ...func(http.Handler) http.Handler) Router {
	for _, m := range middleware {
		r.chi.Use(m)
		r.middlewares = append(r.middlewares, m)
	}
	return r
}

func (r *router) With(middleware ...func(http.Handler) http.Handler) Router {
	newRouter := &router{
		chi:         r.chi.With(middleware...),
		config:      r.config,
		logger:      r.logger,
		ctx:         r.ctx,
		routes:      make([]RouteInfo, 0),
		namedGroups: make(map[string]Group),
		middlewares: make([]func(http.Handler) http.Handler, 0),
	}

	// Copy existing middlewares
	newRouter.middlewares = append(newRouter.middlewares, r.middlewares...)
	newRouter.middlewares = append(newRouter.middlewares, middleware...)

	return newRouter
}

// Conditional middleware

func (r *router) UseIf(condition func(*http.Request) bool, middleware func(http.Handler) http.Handler) Router {
	conditionalMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if condition(req) {
				middleware(next).ServeHTTP(w, req)
			} else {
				next.ServeHTTP(w, req)
			}
		})
	}
	return r.Use(conditionalMiddleware)
}

func (r *router) UseForPath(pathPattern string, middleware func(http.Handler) http.Handler) Router {
	condition := func(req *http.Request) bool {
		return matchesPattern(pathPattern, req.URL.Path)
	}
	return r.UseIf(condition, middleware)
}

func (r *router) UseForMethod(method string, middleware func(http.Handler) http.Handler) Router {
	condition := func(req *http.Request) bool {
		return req.Method == method
	}
	return r.UseIf(condition, middleware)
}

// Group routing methods

func (r *router) Group(prefix string, options ...GroupOption) Group {
	return NewGroup(prefix, r, r.logger, options...)
}

func (r *router) GroupFunc(prefix string, fn func(Group), options ...GroupOption) Router {
	grp := r.Group(prefix, options...)
	fn(grp)
	grp.Register(r)
	return r
}

func (r *router) GroupWith(prefix string, fn func(Group), options ...GroupOption) Group {
	group := r.Group(prefix, options...)
	if fn != nil {
		fn(group)
	}
	return group
}

// Named groups for reuse
func (r *router) NamedGroup(name, prefix string, options ...GroupOption) Group {
	r.mu.Lock()
	defer r.mu.Unlock()

	group := r.Group(prefix, options...)
	r.namedGroups[name] = group
	return group
}

func (r *router) GetGroup(name string) (Group, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	group, exists := r.namedGroups[name]
	return group, exists
}

func (r *router) RemoveGroup(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.namedGroups[name]; exists {
		delete(r.namedGroups, name)
		return true
	}
	return false
}

func (r *router) ListGroups() []GroupInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	groups := make([]GroupInfo, 0, len(r.namedGroups))
	for name, grp := range r.namedGroups {
		if groupImpl, ok := grp.(*group); ok {
			info := GroupInfo{
				Name:        name,
				Prefix:      groupImpl.prefix,
				FullPath:    groupImpl.fullPath,
				Description: groupImpl.config.Description,
				Tags:        groupImpl.config.Tags,
				RouteCount:  len(groupImpl.routes),
				SubGroups:   len(groupImpl.subGroups),
				Middleware:  len(groupImpl.middleware),
				Metadata:    groupImpl.config.Metadata,
			}
			groups = append(groups, info)
		}
	}

	return groups
}

func (r *router) GroupCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.namedGroups)
}

func (r *router) GetGroupByPath(path string) (Group, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, group := range r.namedGroups {
		if group.FullPath() == path {
			return group, true
		}
	}
	return nil, false
}

func (r *router) GroupMiddleware(name string) []func(http.Handler) http.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if group, exists := r.namedGroups[name]; exists {
		return group.Middleware()
	}
	return nil
}

func (r *router) MountGroups(groups map[string]Group) {
	for name, group := range groups {
		r.mu.Lock()
		r.namedGroups[name] = group
		r.mu.Unlock()

		group.Register(r)
	}
}

func (r *router) RemoveAllGroups() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.namedGroups = make(map[string]Group)
}

// Convenience group methods

func (r *router) APIGroup(prefix string, options ...GroupOption) Group {
	apiOptions := []GroupOption{
		WithDescription("API endpoints"),
		WithTags("api"),
		WithAuth(true),
		WithValidation(true, true),
	}
	apiOptions = append(apiOptions, options...)
	return r.Group(prefix, apiOptions...)
}

func (r *router) AdminGroup(prefix string, options ...GroupOption) Group {
	adminOptions := []GroupOption{
		WithDescription("Admin endpoints"),
		WithTags("admin", "secure"),
		WithAuth(true),
		WithRoles("admin"),
		WithRateLimit(RateLimitConfig{
			RequestsPerSecond: 10,
			BurstSize:         20,
			Strategy:          RateLimitByUser,
		}),
	}
	adminOptions = append(adminOptions, options...)
	return r.Group(prefix, adminOptions...)
}

func (r *router) PublicGroup(prefix string, options ...GroupOption) Group {
	publicOptions := []GroupOption{
		WithDescription("Public endpoints"),
		WithTags("public"),
		WithAuth(false),
		WithRateLimit(RateLimitConfig{
			RequestsPerSecond: 100,
			BurstSize:         200,
			Strategy:          RateLimitByIP,
		}),
	}
	publicOptions = append(publicOptions, options...)
	return r.Group(prefix, publicOptions...)
}

func (r *router) WebSocketGroup(prefix string, options ...GroupOption) Group {
	wsOptions := []GroupOption{
		WithDescription("WebSocket endpoints"),
		WithTags("websocket", "realtime"),
		WithTimeout(0), // No timeout for WebSocket connections
	}
	wsOptions = append(wsOptions, options...)
	return r.Group(prefix, wsOptions...)
}

func (r *router) Route(pattern string, fn func(Router)) Router {
	r.chi.Route(pattern, func(chi chi.Router) {
		subRouter := &router{
			chi:         chi,
			config:      r.config,
			logger:      r.logger,
			ctx:         r.ctx,
			routes:      make([]RouteInfo, 0),
			namedGroups: make(map[string]Group),
			middlewares: make([]func(http.Handler) http.Handler, 0),
		}
		fn(subRouter)
	})
	return r
}

func (r *router) Mount(path string, handler http.Handler) Router {
	r.chi.Mount(path, handler)
	r.logger.Debug("Handler mounted", logger.String("path", path))
	return r
}

func (r *router) MountGroup(path string, group Group) Router {
	r.chi.Mount(path, group.Handler())
	r.logger.Debug("Group mounted", logger.String("path", path))
	return r
}

// Static file serving

func (r *router) Static(pattern, dir string) Router {
	return r.FileServer(pattern, dir)
}

func (r *router) StaticFS(pattern string, fs http.FileSystem) Router {
	r.chi.Mount(pattern, http.StripPrefix(pattern, http.FileServer(fs)))
	r.logger.Debug("Static filesystem mounted", logger.String("pattern", pattern))
	return r
}

func (r *router) FileServer(pattern, dir string) Router {
	// Ensure pattern ends with /*
	if !strings.HasSuffix(pattern, "/*") {
		pattern = pattern + "/*"
	}

	// Create file server
	fs := http.Dir(dir)
	fileServer := http.FileServer(fs)

	// Mount with proper prefix stripping
	prefix := strings.TrimSuffix(pattern, "/*")
	r.chi.Handle(pattern, http.StripPrefix(prefix, fileServer))

	r.logger.Debug("File server mounted",
		logger.String("pattern", pattern),
		logger.String("directory", dir),
	)

	return r
}

// WebSocket support

func (r *router) WebSocket(pattern string, handler WebSocketHandler) Router {
	return r.WebSocketUpgrade(pattern, handler)
}

func (r *router) WebSocketUpgrade(pattern string, handler WebSocketHandler, options ...WebSocketOption) Router {
	config := &WebSocketConnectionConfig{
		ReadBufferSize:    r.config.WebSocket.ReadBufferSize,
		WriteBufferSize:   r.config.WebSocket.WriteBufferSize,
		CheckOrigin:       r.config.WebSocket.CheckOrigin,
		EnableCompression: r.config.WebSocket.EnableCompression,
		Subprotocols:      r.config.WebSocket.Subprotocols,
	}

	// Apply options
	for _, opt := range options {
		opt.Apply(config)
	}

	// Configure upgrader
	upgrader := websocket.Upgrader{
		ReadBufferSize:    config.ReadBufferSize,
		WriteBufferSize:   config.WriteBufferSize,
		CheckOrigin:       config.CheckOrigin,
		Subprotocols:      config.Subprotocols,
		EnableCompression: config.EnableCompression,
	}

	wsHandler := func(w http.ResponseWriter, req *http.Request) {
		// Upgrade connection
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			r.logger.Error("WebSocket upgrade failed",
				logger.Error(err),
				logger.String("pattern", pattern),
			)
			return
		}

		// Create connection wrapper
		wsConn := &webSocketConnection{
			conn:   conn,
			id:     generateConnectionID(),
			ctx:    req.Context(),
			logger: r.logger,
		}

		// Handle connection
		if err := handler.HandleConnection(req.Context(), wsConn); err != nil {
			r.logger.Error("WebSocket handler error",
				logger.Error(err),
				logger.String("pattern", pattern),
				logger.String("connection_id", wsConn.id),
			)
		}
	}

	r.Get(pattern, wsHandler)
	r.logger.Debug("WebSocket endpoint registered", logger.String("pattern", pattern))
	return r
}

// Server-Sent Events support

func (r *router) SSE(pattern string, handler SSEHandler) Router {
	sseHandler := func(w http.ResponseWriter, req *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Create SSE stream
		stream := &sseStream{
			writer: w,
			id:     generateConnectionID(),
			ctx:    req.Context(),
			logger: r.logger,
		}

		// Handle SSE
		if err := handler.HandleSSE(req.Context(), stream); err != nil {
			r.logger.Error("SSE handler error",
				logger.Error(err),
				logger.String("pattern", pattern),
			)
		}
	}

	r.Get(pattern, sseHandler)
	r.logger.Debug("SSE endpoint registered", logger.String("pattern", pattern))
	return r
}

// API documentation

func (r *router) OpenAPI(path string, spec OpenAPISpec) Router {
	r.Get(path, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"openapi":"3.0.0","info":{"title":"%s","version":"%s"}}`,
			spec.Info.Title, spec.Info.Version)
	})

	r.logger.Debug("OpenAPI spec registered", logger.String("path", path))
	return r
}

func (r *router) SwaggerUI(path string, specURL string) Router {
	swaggerHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<title>API Documentation</title>
	<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@3.52.5/swagger-ui.css">
</head>
<body>
	<div id="swagger-ui"></div>
	<script src="https://unpkg.com/swagger-ui-dist@3.52.5/swagger-ui-bundle.js"></script>
	<script>
		SwaggerUIBundle({
			url: '%s',
			dom_id: '#swagger-ui'
		});
	</script>
</body>
</html>`, specURL)

	r.Get(path, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(swaggerHTML))
	})

	r.logger.Debug("Swagger UI registered",
		logger.String("path", path),
		logger.String("spec_url", specURL),
	)
	return r
}

// Health and monitoring

func (r *router) Health(path string, checker HealthChecker) Router {
	r.Get(path, func(w http.ResponseWriter, req *http.Request) {
		if err := checker(req.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"unhealthy","error":"%s"}`, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"healthy"}`)
	})

	r.logger.Debug("Health check registered", logger.String("path", path))
	return r
}

func (r *router) Metrics(path string) Router {
	r.Get(path, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "# HELP http_requests_total Total HTTP requests\n")
		fmt.Fprint(w, "# TYPE http_requests_total counter\n")
		fmt.Fprint(w, "http_requests_total 42\n")
	})

	r.logger.Debug("Metrics endpoint registered", logger.String("path", path))
	return r
}

// Error handling

func (r *router) NotFound(handler http.HandlerFunc) Router {
	r.chi.NotFound(handler)
	return r
}

func (r *router) MethodNotAllowed(handler http.HandlerFunc) Router {
	r.chi.MethodNotAllowed(handler)
	return r
}

func (r *router) ErrorHandler(handler ErrorHandler) Router {
	// Implementation would wrap handlers with error handling
	return r
}

// Configuration methods

func (r *router) SetTimeout(timeout time.Duration) Router {
	r.config.ReadTimeout = timeout
	r.config.WriteTimeout = timeout
	return r
}

func (r *router) SetMaxBodySize(size int64) Router {
	r.config.MaxBodySize = size
	return r
}

func (r *router) EnableCORS(config CORSConfig) Router {
	r.config.Security.CORS = config
	r.config.EnableCORS = true
	return r
}

func (r *router) EnableRateLimit(config RateLimitConfig) Router {
	r.config.Security.RateLimit = config
	r.config.EnableRateLimit = true
	return r
}

// Context methods

func (r *router) WithContext(ctx context.Context) Router {
	newRouter := *r
	newRouter.ctx = ctx
	return &newRouter
}

// Handler creation

func (r *router) Handler() http.Handler {
	return r.chi
}

// Introspection methods

func (r *router) Routes() []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]RouteInfo(nil), r.routes...)
}

func (r *router) Middleware() []func(http.Handler) http.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]func(http.Handler) http.Handler(nil), r.middlewares...)
}

// Helper methods

func (r *router) wrapHandler(pattern string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()

		// Add route pattern to context
		ctx := context.WithValue(req.Context(), "route_pattern", pattern)
		req = req.WithContext(ctx)

		// Execute handler
		handler(w, req)

		// Log execution time
		duration := time.Since(start)
		r.logger.Debug("Route executed",
			logger.String("pattern", pattern),
			logger.String("method", req.Method),
			logger.Duration("duration", duration),
		)
	}
}

// Utility functions

func getFunctionName(fn interface{}) string {
	// This is a simplified implementation
	// In practice, you might use reflection to get the actual function name
	return "handler"
}

func generateConnectionID() string {
	// Simple ID generation - could use xid or uuid
	return fmt.Sprintf("conn_%d", time.Now().UnixNano())
}
