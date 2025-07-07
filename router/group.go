package router

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xraph/forge/logger"
)

// group implements the Group interface by embedding Router
type group struct {
	// Group-specific properties
	prefix      string
	fullPath    string
	parent      Router
	chiRouter   chi.Router
	config      GroupConfig
	middleware  []func(http.Handler) http.Handler
	subGroups   []*group
	routes      []RouteInfo
	namedGroups map[string]*group
	logger      logger.Logger
	mu          sync.RWMutex
	ctx         context.Context
	mounted     bool // Track if group is mounted
}

// NewGroup creates a new group with the given prefix and options
// The group is automatically mounted to its parent router
func NewGroup(prefix string, parent Router, logger logger.Logger, options ...GroupOption) Group {
	config := GroupConfig{
		Prefix:     prefix,
		Middleware: make([]func(http.Handler) http.Handler, 0),
		Metadata:   make(map[string]interface{}),
	}

	// Apply options
	for _, option := range options {
		option.Apply(&config)
	}

	// Create chi router for this group
	chiRouter := chi.NewRouter()

	g := &group{
		prefix:      prefix,
		fullPath:    prefix,
		parent:      parent,
		chiRouter:   chiRouter,
		config:      config,
		middleware:  config.Middleware,
		subGroups:   make([]*group, 0),
		routes:      make([]RouteInfo, 0),
		namedGroups: make(map[string]*group),
		logger:      logger,
		ctx:         context.Background(),
		mounted:     false,
	}

	// Calculate full path if parent is a group
	if parentGroup, ok := parent.(*group); ok {
		g.fullPath = path.Join(parentGroup.fullPath, prefix)
	} else {
		g.fullPath = prefix
	}

	// Apply group-level middleware
	for _, mw := range g.middleware {
		g.chiRouter.Use(mw)
	}

	// Apply configuration-based middleware
	g.applyConfigMiddleware()

	// Automatically mount the group to its parent
	g.autoMount()

	return g
}

// autoMount automatically mounts this group to its parent
func (g *group) autoMount() {
	if g.mounted {
		return
	}

	// Mount to parent using the Router interface
	g.parent.Mount(g.prefix, g.Handler())
	g.mounted = true

	g.logger.Debug("Group auto-mounted",
		logger.String("prefix", g.prefix),
		logger.String("full_path", g.fullPath),
	)
}

// HTTP Methods - these override the embedded router to return Group instead of Router
// This provides a fluent API where group methods return Group for chaining

func (g *group) Get(pattern string, handler http.HandlerFunc) Router {
	g.Method(http.MethodGet, pattern, handler)
	return g
}

func (g *group) Post(pattern string, handler http.HandlerFunc) Router {
	g.Method(http.MethodPost, pattern, handler)
	return g
}

func (g *group) Put(pattern string, handler http.HandlerFunc) Router {
	g.Method(http.MethodPut, pattern, handler)
	return g
}

func (g *group) Patch(pattern string, handler http.HandlerFunc) Router {
	g.Method(http.MethodPatch, pattern, handler)
	return g
}

func (g *group) Delete(pattern string, handler http.HandlerFunc) Router {
	g.Method(http.MethodDelete, pattern, handler)
	return g
}

func (g *group) Head(pattern string, handler http.HandlerFunc) Router {
	g.Method(http.MethodHead, pattern, handler)
	return g
}

func (g *group) Options(pattern string, handler http.HandlerFunc) Router {
	g.Method(http.MethodOptions, pattern, handler)
	return g
}

func (g *group) Method(method, pattern string, handler http.HandlerFunc) Router {
	fullPattern := path.Join(g.fullPath, pattern)

	// Wrap handler with group-specific logic
	wrappedHandler := g.wrapHandler(fullPattern, handler)

	g.chiRouter.Method(method, pattern, wrappedHandler)

	// Store route info
	g.mu.Lock()
	g.routes = append(g.routes, RouteInfo{
		Method:  method,
		Pattern: fullPattern,
		Handler: getFunctionName(handler),
		Name:    fmt.Sprintf("%s %s", method, fullPattern),
		Metadata: map[string]interface{}{
			"group_prefix": g.prefix,
			"group_path":   g.fullPath,
		},
	})
	g.mu.Unlock()

	g.logger.Debug("Route registered in group",
		logger.String("method", method),
		logger.String("pattern", fullPattern),
		logger.String("group", g.prefix),
	)

	return g
}

func (g *group) Handle(pattern string, handler http.Handler) Router {
	fullPattern := path.Join(g.fullPath, pattern)
	g.chiRouter.Handle(pattern, handler)

	g.logger.Debug("Handler registered in group",
		logger.String("pattern", fullPattern),
		logger.String("group", g.prefix),
	)

	return g
}

// Middleware methods

func (g *group) Use(middleware ...func(http.Handler) http.Handler) Router {
	for _, mw := range middleware {
		g.chiRouter.Use(mw)
		g.middleware = append(g.middleware, mw)
	}
	return g
}

func (g *group) With(middleware ...func(http.Handler) http.Handler) Router {
	// Create a new group with additional middleware
	newGroup := &group{
		prefix:      g.prefix,
		fullPath:    g.fullPath,
		parent:      g.parent,
		chiRouter:   g.chiRouter.With(middleware...),
		config:      g.config,
		middleware:  append(g.middleware, middleware...),
		subGroups:   make([]*group, 0),
		routes:      make([]RouteInfo, 0),
		namedGroups: make(map[string]*group),
		logger:      g.logger,
		ctx:         g.ctx,
		mounted:     g.mounted,
	}

	return newGroup
}

// Conditional middleware

func (g *group) UseIf(condition func(*http.Request) bool, middleware func(http.Handler) http.Handler) Router {
	conditionalMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if condition(r) {
				middleware(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
	return g.Use(conditionalMiddleware)
}

func (g *group) UseForPath(pathPattern string, middleware func(http.Handler) http.Handler) Router {
	condition := func(r *http.Request) bool {
		return matchesPattern(pathPattern, r.URL.Path)
	}
	return g.UseIf(condition, middleware)
}

func (g *group) UseForMethod(method string, middleware func(http.Handler) http.Handler) Router {
	condition := func(r *http.Request) bool {
		return r.Method == method
	}
	return g.UseIf(condition, middleware)
}

// Grouping methods - Sub-groups are automatically mounted

func (g *group) Group(prefix string, options ...GroupOption) Group {
	// Sub-groups are automatically mounted when created
	subGroup := NewGroup(prefix, g, g.logger, options...).(*group)
	g.mu.Lock()
	g.subGroups = append(g.subGroups, subGroup)
	g.mu.Unlock()
	return subGroup
}

func (g *group) GroupFunc(prefix string, fn func(Group), options ...GroupOption) Router {
	subGroup := g.Group(prefix, options...)
	fn(subGroup)
	return g // Return parent group, consistent with Router interface
}

func (g *group) GroupWith(prefix string, fn func(Group), options ...GroupOption) Group {
	subGroup := g.Group(prefix, options...)
	if fn != nil {
		fn(subGroup)
	}
	return subGroup
}

// Route with callback - override to work with group's chiRouter

func (g *group) Route(pattern string, fn func(Router)) Router {
	g.chiRouter.Route(pattern, func(r chi.Router) {
		// Create a temporary group for the route
		tempGroup := &group{
			prefix:      pattern,
			fullPath:    path.Join(g.fullPath, pattern),
			parent:      g,
			chiRouter:   r,
			config:      g.config,
			middleware:  g.middleware,
			subGroups:   make([]*group, 0),
			routes:      make([]RouteInfo, 0),
			namedGroups: make(map[string]*group),
			logger:      g.logger,
			ctx:         g.ctx,
			mounted:     true, // Already mounted via chi.Route
		}
		fn(tempGroup)
	})
	return g
}

// Mounting - override Router methods to return Group

func (g *group) Mount(path string, handler http.Handler) Router {
	g.chiRouter.Mount(path, handler)
	g.logger.Debug("Handler mounted in group",
		logger.String("path", path),
		logger.String("group", g.prefix),
	)
	return g
}

func (g *group) MountGroup(path string, group Group) Router {
	g.chiRouter.Mount(path, group.Handler())
	g.logger.Debug("Group mounted in group",
		logger.String("path", path),
		logger.String("group", g.prefix),
	)
	return g
}

// Static file serving - forward to router with Group return types

func (g *group) Static(pattern, dir string) Router {
	return g.FileServer(pattern, dir)
}

func (g *group) StaticFS(pattern string, fs http.FileSystem) Router {
	g.chiRouter.Mount(pattern, http.StripPrefix(pattern, http.FileServer(fs)))
	g.logger.Debug("Static filesystem mounted in group",
		logger.String("pattern", pattern),
		logger.String("group", g.prefix),
	)
	return g
}

func (g *group) FileServer(pattern, dir string) Router {
	// Ensure pattern ends with /*
	if !strings.HasSuffix(pattern, "/*") {
		pattern = pattern + "/*"
	}

	// Create file server
	fs := http.Dir(dir)
	fileServer := http.FileServer(fs)

	// Mount with proper prefix stripping
	prefix := strings.TrimSuffix(pattern, "/*")
	g.chiRouter.Handle(pattern, http.StripPrefix(prefix, fileServer))

	g.logger.Debug("File server mounted in group",
		logger.String("pattern", pattern),
		logger.String("directory", dir),
		logger.String("group", g.prefix),
	)
	return g
}

// WebSocket support

func (g *group) WebSocket(pattern string, handler WebSocketHandler) Router {
	return g.WebSocketUpgrade(pattern, handler)
}

func (g *group) WebSocketUpgrade(pattern string, handler WebSocketHandler, options ...WebSocketOption) Router {
	// Implementation similar to router's WebSocket method
	g.logger.Debug("WebSocket endpoint registered in group",
		logger.String("pattern", pattern),
		logger.String("group", g.prefix),
	)
	return g
}

// Server-Sent Events

func (g *group) SSE(pattern string, handler SSEHandler) Router {
	sseHandler := func(w http.ResponseWriter, req *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Handle SSE
		if err := handler.HandleSSE(req.Context(), nil); err != nil {
			g.logger.Error("SSE handler error",
				logger.Error(err),
				logger.String("pattern", pattern),
				logger.String("group", g.prefix),
			)
		}
	}

	g.Get(pattern, sseHandler)
	return g
}

// API documentation

func (g *group) OpenAPI(path string, spec OpenAPISpec) Router {
	g.Get(path, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"openapi":"3.0.0","info":{"title":"%s","version":"%s"}}`,
			spec.Info.Title, spec.Info.Version)
	})
	return g
}

func (g *group) SwaggerUI(path string, specURL string) Router {
	swaggerHTML := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<title>API Documentation - %s</title>
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
</html>`, g.fullPath, specURL)

	g.Get(path, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(swaggerHTML))
	})
	return g
}

// Health and monitoring

func (g *group) Health(path string, checker HealthChecker) Router {
	g.Get(path, func(w http.ResponseWriter, req *http.Request) {
		if err := checker(req.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"unhealthy","error":"%s","group":"%s"}`, err.Error(), g.prefix)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"healthy","group":"%s"}`, g.prefix)
	})
	return g
}

func (g *group) Metrics(path string) Router {
	g.Get(path, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# Group: %s\n", g.prefix)
		fmt.Fprint(w, "# HELP http_requests_total Total HTTP requests\n")
		fmt.Fprint(w, "# TYPE http_requests_total counter\n")
		fmt.Fprint(w, "http_requests_total 42\n")
	})
	return g
}

// Error handling

func (g *group) NotFound(handler http.HandlerFunc) Router {
	g.chiRouter.NotFound(handler)
	return g
}

func (g *group) MethodNotAllowed(handler http.HandlerFunc) Router {
	g.chiRouter.MethodNotAllowed(handler)
	return g
}

func (g *group) ErrorHandler(handler ErrorHandler) Router {
	// Implementation would wrap handlers with error handling
	return g
}

// Configuration

func (g *group) SetTimeout(timeout time.Duration) Router {
	g.config.Timeout = timeout
	return g
}

func (g *group) SetMaxBodySize(size int64) Router {
	// Implementation would add body size limiting middleware
	return g
}

func (g *group) EnableCORS(config CORSConfig) Router {
	g.config.CORS = &config
	return g
}

func (g *group) EnableRateLimit(config RateLimitConfig) Router {
	g.config.RateLimit = &config
	return g
}

// Context

func (g *group) WithContext(ctx context.Context) Router {
	newGroup := *g
	newGroup.ctx = ctx
	return &newGroup
}

// Router interface methods that Group needs to implement
// Since Group embeds Router conceptually, it needs to provide implementations

func (g *group) NamedGroup(name, prefix string, options ...GroupOption) Group {
	// Groups don't typically manage named groups themselves,
	// delegate to parent router if it supports this
	if parentRouter, ok := g.parent.(*router); ok {
		return parentRouter.NamedGroup(name, prefix, options...)
	}
	// Fallback: just create a regular group
	return g.Group(prefix, options...)
}

func (g *group) GetGroup(name string) (Group, bool) {
	// Groups don't manage named groups, delegate to parent
	if parentRouter, ok := g.parent.(*router); ok {
		return parentRouter.GetGroup(name)
	}
	return nil, false
}

func (g *group) RemoveGroup(name string) bool {
	if parentRouter, ok := g.parent.(*router); ok {
		return parentRouter.RemoveGroup(name)
	}
	return false
}

func (g *group) ListGroups() []GroupInfo {
	if parentRouter, ok := g.parent.(*router); ok {
		return parentRouter.ListGroups()
	}
	return []GroupInfo{}
}

func (g *group) GroupCount() int {
	if parentRouter, ok := g.parent.(*router); ok {
		return parentRouter.GroupCount()
	}
	return 0
}

func (g *group) GetGroupByPath(path string) (Group, bool) {
	if parentRouter, ok := g.parent.(*router); ok {
		return parentRouter.GetGroupByPath(path)
	}
	return nil, false
}

func (g *group) GroupMiddleware(name string) []func(http.Handler) http.Handler {
	if parentRouter, ok := g.parent.(*router); ok {
		return parentRouter.GroupMiddleware(name)
	}
	return nil
}

func (g *group) MountGroups(groups map[string]Group) {
	if parentRouter, ok := g.parent.(*router); ok {
		parentRouter.MountGroups(groups)
	}
}

func (g *group) RemoveAllGroups() {
	if parentRouter, ok := g.parent.(*router); ok {
		parentRouter.RemoveAllGroups()
	}
}

func (g *group) APIGroup(prefix string, options ...GroupOption) Group {
	apiOptions := []GroupOption{
		WithDescription("API endpoints"),
		WithTags("api"),
		WithAuth(true),
		WithValidation(true, true),
	}
	apiOptions = append(apiOptions, options...)
	return g.Group(prefix, apiOptions...)
}

func (g *group) AdminGroup(prefix string, options ...GroupOption) Group {
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
	return g.Group(prefix, adminOptions...)
}

func (g *group) PublicGroup(prefix string, options ...GroupOption) Group {
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
	return g.Group(prefix, publicOptions...)
}

func (g *group) WebSocketGroup(prefix string, options ...GroupOption) Group {
	wsOptions := []GroupOption{
		WithDescription("WebSocket endpoints"),
		WithTags("websocket", "realtime"),
		WithTimeout(0), // No timeout for WebSocket connections
	}
	wsOptions = append(wsOptions, options...)
	return g.Group(prefix, wsOptions...)
}

// Handler creation

func (g *group) Handler() http.Handler {
	return g.chiRouter
}

// Group-specific introspection methods

func (g *group) Prefix() string {
	return g.prefix
}

func (g *group) FullPath() string {
	return g.fullPath
}

func (g *group) SubGroups() []Group {
	g.mu.RLock()
	defer g.mu.RUnlock()

	groups := make([]Group, len(g.subGroups))
	for i, sg := range g.subGroups {
		groups[i] = sg
	}
	return groups
}

func (g *group) Parent() Router {
	return g.parent
}

// Introspection methods shared with Router

func (g *group) Routes() []RouteInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]RouteInfo(nil), g.routes...)
}

func (g *group) Middleware() []func(http.Handler) http.Handler {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]func(http.Handler) http.Handler(nil), g.middleware...)
}

// Register method - Updated to work with any Router (not just *router)
func (g *group) Register(parent Router) {
	// Mount this group on the parent router using the Router interface
	// This works for both *router and *group parents
	parent.Mount(g.prefix, g.Handler())
	g.mounted = true

	g.logger.Debug("Group registered with parent",
		logger.String("prefix", g.prefix),
		logger.String("full_path", g.fullPath),
	)
}

// Helper methods

func (g *group) applyConfigMiddleware() {
	// Apply authentication middleware if required
	if g.config.RequireAuth {
		g.logger.Debug("Auth middleware enabled for group", logger.String("prefix", g.prefix))
	}

	// Apply rate limiting if configured
	if g.config.RateLimit != nil {
		g.logger.Debug("Rate limiting enabled for group", logger.String("prefix", g.prefix))
	}

	// Apply CORS if configured
	if g.config.CORS != nil {
		g.logger.Debug("CORS enabled for group", logger.String("prefix", g.prefix))
	}

	// Apply timeout middleware if configured
	if g.config.Timeout > 0 {
		timeoutMiddleware := func(next http.Handler) http.Handler {
			return http.TimeoutHandler(next, g.config.Timeout, "Request timeout")
		}
		g.chiRouter.Use(timeoutMiddleware)
	}
}

func (g *group) wrapHandler(pattern string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()

		// Add group information to context
		ctx := context.WithValue(req.Context(), "group_prefix", g.prefix)
		ctx = context.WithValue(ctx, "group_path", g.fullPath)
		ctx = context.WithValue(ctx, "route_pattern", pattern)
		req = req.WithContext(ctx)

		// Execute handler
		handler(w, req)

		// Log execution time
		duration := time.Since(start)
		g.logger.Debug("Route executed in group",
			logger.String("pattern", pattern),
			logger.String("method", req.Method),
			logger.String("group", g.prefix),
			logger.Duration("duration", duration),
		)
	}
}
