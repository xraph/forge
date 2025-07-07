package middleware

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// Stack manages middleware execution order and composition
type Stack interface {
	// Middleware management
	Use(middleware ...Middleware) Stack
	UseWithPriority(priority int, middleware Middleware) Stack
	Remove(name string) Stack

	// Conditional middleware
	UseIf(condition func(*http.Request) bool, middleware Middleware) Stack
	UseForPath(pattern string, middleware Middleware) Stack
	UseForMethod(method string, middleware Middleware) Stack

	// Grouping
	Group(name string, middleware ...Middleware) Stack
	UseGroup(name string) Stack

	// Execution
	Handler(final http.Handler) http.Handler
	ServeHTTP(w http.ResponseWriter, r *http.Request, final http.Handler)

	// Introspection
	List() []MiddlewareInfo
	Count() int
	Has(name string) bool

	// Configuration
	SetTimeout(timeout time.Duration) Stack
	SetMaxBodySize(size int64) Stack
	EnableRecovery(enabled bool) Stack
}

// Middleware represents enhanced middleware with metadata
type Middleware interface {
	// Execution
	Handle(http.Handler) http.Handler

	// Metadata
	Name() string
	Priority() int
	Description() string

	// Lifecycle
	Initialize(ctx context.Context) error
	Shutdown(ctx context.Context) error

	// Configuration
	Configure(config map[string]interface{}) error
	IsEnabled() bool

	// Health
	Health(ctx context.Context) error
}

// MiddlewareInfo provides information about registered middleware
type MiddlewareInfo struct {
	Name        string                 `json:"name"`
	Priority    int                    `json:"priority"`
	Description string                 `json:"description"`
	Enabled     bool                   `json:"enabled"`
	Type        string                 `json:"type"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`

	// Statistics
	RequestCount int64         `json:"request_count"`
	ErrorCount   int64         `json:"error_count"`
	AvgDuration  time.Duration `json:"avg_duration"`
	LastUsed     time.Time     `json:"last_used"`
}

// stack implements the Stack interface
type stack struct {
	middlewares []middlewareEntry
	groups      map[string][]Middleware
	config      StackConfig
	stats       *StackStats
	mu          sync.RWMutex
	logger      logger.Logger
}

// middlewareEntry represents a middleware with its metadata
type middlewareEntry struct {
	middleware  Middleware
	priority    int
	condition   func(*http.Request) bool
	pathPattern string
	method      string
	group       string
	enabled     bool
	stats       middlewareStats
}

// middlewareStats tracks middleware performance
type middlewareStats struct {
	requestCount  int64
	errorCount    int64
	totalDuration time.Duration
	lastUsed      time.Time
	mu            sync.RWMutex
}

// StackConfig represents stack configuration
type StackConfig struct {
	DefaultTimeout time.Duration
	MaxBodySize    int64
	EnableRecovery bool
	EnableMetrics  bool
	EnableTracing  bool
	Logger         logger.Logger
}

// StackStats provides stack-wide statistics
type StackStats struct {
	TotalRequests   int64         `json:"total_requests"`
	TotalErrors     int64         `json:"total_errors"`
	TotalDuration   time.Duration `json:"total_duration"`
	MiddlewareCount int           `json:"middleware_count"`
	ActiveGroups    int           `json:"active_groups"`
	mu              sync.RWMutex
}

// NewStack creates a new middleware stack
func NewStack(config StackConfig) Stack {
	if config.Logger == nil {
		config.Logger = logger.GetGlobalLogger()
	}

	return &stack{
		middlewares: make([]middlewareEntry, 0),
		groups:      make(map[string][]Middleware),
		config:      config,
		stats:       &StackStats{},
		logger:      config.Logger,
	}
}

// DefaultStack creates a stack with sensible defaults
func DefaultStack(logger logger.Logger) Stack {
	config := StackConfig{
		DefaultTimeout: 30 * time.Second,
		MaxBodySize:    10 * 1024 * 1024, // 10MB
		EnableRecovery: true,
		EnableMetrics:  true,
		EnableTracing:  true,
		Logger:         logger,
	}

	s := NewStack(config)

	// Add default middleware in order of execution
	s.UseWithPriority(100, NewRequestIDMiddleware())
	s.UseWithPriority(200, NewLoggingMiddleware(logger))
	s.UseWithPriority(300, NewRecoveryMiddleware(logger))
	s.UseWithPriority(400, NewCORSMiddleware(DefaultCORSConfig()))
	s.UseWithPriority(500, NewSecurityMiddleware(DefaultSecurityConfig()))

	return s
}

// Use adds middleware to the stack
func (s *stack) Use(middleware ...Middleware) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range middleware {
		entry := middlewareEntry{
			middleware: m,
			priority:   m.Priority(),
			enabled:    m.IsEnabled(),
		}
		s.middlewares = append(s.middlewares, entry)
	}

	s.sortMiddleware()
	return s
}

// UseWithPriority adds middleware with specific priority
func (s *stack) UseWithPriority(priority int, middleware Middleware) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := middlewareEntry{
		middleware: middleware,
		priority:   priority,
		enabled:    middleware.IsEnabled(),
	}
	s.middlewares = append(s.middlewares, entry)

	s.sortMiddleware()
	return s
}

// Remove removes middleware by name
func (s *stack) Remove(name string) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, entry := range s.middlewares {
		if entry.middleware.Name() == name {
			s.middlewares = append(s.middlewares[:i], s.middlewares[i+1:]...)
			break
		}
	}

	return s
}

// UseIf adds conditional middleware
func (s *stack) UseIf(condition func(*http.Request) bool, middleware Middleware) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := middlewareEntry{
		middleware: middleware,
		priority:   middleware.Priority(),
		condition:  condition,
		enabled:    middleware.IsEnabled(),
	}
	s.middlewares = append(s.middlewares, entry)

	s.sortMiddleware()
	return s
}

// UseForPath adds path-specific middleware
func (s *stack) UseForPath(pattern string, middleware Middleware) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := middlewareEntry{
		middleware:  middleware,
		priority:    middleware.Priority(),
		pathPattern: pattern,
		enabled:     middleware.IsEnabled(),
	}
	s.middlewares = append(s.middlewares, entry)

	s.sortMiddleware()
	return s
}

// UseForMethod adds method-specific middleware
func (s *stack) UseForMethod(method string, middleware Middleware) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := middlewareEntry{
		middleware: middleware,
		priority:   middleware.Priority(),
		method:     method,
		enabled:    middleware.IsEnabled(),
	}
	s.middlewares = append(s.middlewares, entry)

	s.sortMiddleware()
	return s
}

// Group creates a named group of middleware
func (s *stack) Group(name string, middleware ...Middleware) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.groups[name] = middleware
	return s
}

// UseGroup applies a named group of middleware
func (s *stack) UseGroup(name string) Stack {
	s.mu.RLock()
	group, exists := s.groups[name]
	s.mu.RUnlock()

	if !exists {
		s.logger.Warn("Middleware group not found", logger.String("group", name))
		return s
	}

	return s.Use(group...)
}

// Handler creates the final middleware chain handler
func (s *stack) Handler(final http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ServeHTTP(w, r, final)
	})
}

// ServeHTTP executes the middleware chain
func (s *stack) ServeHTTP(w http.ResponseWriter, r *http.Request, final http.Handler) {
	start := time.Now()

	// Create execution context
	ctx := r.Context()
	ctx = context.WithValue(ctx, "middleware_start", start)
	r = r.WithContext(ctx)

	// Build applicable middleware chain
	applicable := s.getApplicableMiddleware(r)

	// Create the chain
	handler := final
	for i := len(applicable) - 1; i >= 0; i-- {
		entry := applicable[i]
		if entry.enabled {
			// Wrap with stats tracking
			handler = s.wrapWithStats(entry, handler)
		}
	}

	// Execute the chain
	defer func() {
		duration := time.Since(start)
		s.updateStats(duration, nil)
	}()

	handler.ServeHTTP(w, r)
}

// getApplicableMiddleware filters middleware based on request
func (s *stack) getApplicableMiddleware(r *http.Request) []middlewareEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var applicable []middlewareEntry

	for _, entry := range s.middlewares {
		if s.isApplicable(entry, r) {
			applicable = append(applicable, entry)
		}
	}

	return applicable
}

// isApplicable checks if middleware should be applied to request
func (s *stack) isApplicable(entry middlewareEntry, r *http.Request) bool {
	if !entry.enabled {
		return false
	}

	// Check condition
	if entry.condition != nil && !entry.condition(r) {
		return false
	}

	// Check path pattern
	if entry.pathPattern != "" {
		// Simple pattern matching - could be enhanced with regex
		if !matchPattern(entry.pathPattern, r.URL.Path) {
			return false
		}
	}

	// Check method
	if entry.method != "" && entry.method != r.Method {
		return false
	}

	return true
}

// wrapWithStats wraps middleware execution with statistics tracking
func (s *stack) wrapWithStats(entry middlewareEntry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		defer func() {
			duration := time.Since(start)
			entry.stats.update(duration, nil)
		}()

		// Execute middleware
		entry.middleware.Handle(next).ServeHTTP(w, r)
	})
}

// sortMiddleware sorts middleware by priority (lower numbers execute first)
func (s *stack) sortMiddleware() {
	sort.Slice(s.middlewares, func(i, j int) bool {
		return s.middlewares[i].priority < s.middlewares[j].priority
	})
}

// List returns information about all middleware
func (s *stack) List() []MiddlewareInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info := make([]MiddlewareInfo, len(s.middlewares))
	for i, entry := range s.middlewares {
		entry.stats.mu.RLock()
		avgDuration := time.Duration(0)
		if entry.stats.requestCount > 0 {
			avgDuration = time.Duration(int64(entry.stats.totalDuration) / entry.stats.requestCount)
		}

		info[i] = MiddlewareInfo{
			Name:         entry.middleware.Name(),
			Priority:     entry.priority,
			Description:  entry.middleware.Description(),
			Enabled:      entry.enabled,
			RequestCount: entry.stats.requestCount,
			ErrorCount:   entry.stats.errorCount,
			AvgDuration:  avgDuration,
			LastUsed:     entry.stats.lastUsed,
		}
		entry.stats.mu.RUnlock()
	}

	return info
}

// Count returns the number of middleware in the stack
func (s *stack) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.middlewares)
}

// Has checks if middleware with given name exists
func (s *stack) Has(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, entry := range s.middlewares {
		if entry.middleware.Name() == name {
			return true
		}
	}
	return false
}

// Configuration methods

func (s *stack) SetTimeout(timeout time.Duration) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.DefaultTimeout = timeout
	return s
}

func (s *stack) SetMaxBodySize(size int64) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.MaxBodySize = size
	return s
}

func (s *stack) EnableRecovery(enabled bool) Stack {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.EnableRecovery = enabled
	return s
}

// Helper methods

func (s *stack) updateStats(duration time.Duration, err error) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()

	s.stats.TotalRequests++
	s.stats.TotalDuration += duration
	if err != nil {
		s.stats.TotalErrors++
	}
	s.stats.MiddlewareCount = len(s.middlewares)
	s.stats.ActiveGroups = len(s.groups)
}

func (stats *middlewareStats) update(duration time.Duration, err error) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.requestCount++
	stats.totalDuration += duration
	stats.lastUsed = time.Now()
	if err != nil {
		stats.errorCount++
	}
}

// matchPattern performs simple pattern matching
func matchPattern(pattern, path string) bool {
	// Simple implementation - could be enhanced with more sophisticated matching
	if pattern == "*" {
		return true
	}

	if pattern == path {
		return true
	}

	// Simple prefix matching
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}

	return false
}

// BaseMiddleware provides a base implementation for middleware
type BaseMiddleware struct {
	name        string
	priority    int
	description string
	enabled     bool
	config      map[string]interface{}
	logger      logger.Logger
}

// NewBaseMiddleware creates a new base middleware
func NewBaseMiddleware(name string, priority int, description string) *BaseMiddleware {
	return &BaseMiddleware{
		name:        name,
		priority:    priority,
		description: description,
		enabled:     true,
		config:      make(map[string]interface{}),
		logger:      logger.GetGlobalLogger(),
	}
}

func (bm *BaseMiddleware) Name() string        { return bm.name }
func (bm *BaseMiddleware) Priority() int       { return bm.priority }
func (bm *BaseMiddleware) Description() string { return bm.description }
func (bm *BaseMiddleware) IsEnabled() bool     { return bm.enabled }

func (bm *BaseMiddleware) Initialize(ctx context.Context) error {
	return nil
}

func (bm *BaseMiddleware) Shutdown(ctx context.Context) error {
	return nil
}

func (bm *BaseMiddleware) Configure(config map[string]interface{}) error {
	bm.config = config
	return nil
}

func (bm *BaseMiddleware) Health(ctx context.Context) error {
	return nil
}

// Utility functions

// ChainMiddleware creates a middleware chain from multiple middleware
func ChainMiddleware(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		handler := final
		for i := len(middlewares) - 1; i >= 0; i-- {
			handler = middlewares[i](handler)
		}
		return handler
	}
}

// ConditionalMiddleware applies middleware only if condition is met
func ConditionalMiddleware(condition func(*http.Request) bool, middleware func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if condition(r) {
				middleware(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// MiddlewareFunc adapts a regular middleware function to the Middleware interface
type MiddlewareFunc struct {
	*BaseMiddleware
	handler func(http.Handler) http.Handler
}

// NewMiddlewareFunc creates a Middleware from a function
func NewMiddlewareFunc(name string, priority int, description string, handler func(http.Handler) http.Handler) Middleware {
	return &MiddlewareFunc{
		BaseMiddleware: NewBaseMiddleware(name, priority, description),
		handler:        handler,
	}
}

func (mf *MiddlewareFunc) Handle(next http.Handler) http.Handler {
	return mf.handler(next)
}

// Priority constants for common middleware
const (
	PriorityRequestID   = 100
	PriorityLogging     = 200
	PriorityRecovery    = 300
	PriorityMetrics     = 350
	PriorityTracing     = 375
	PriorityCORS        = 400
	PrioritySecurity    = 500
	PriorityCompression = 600
	PriorityAuth        = 700
	PriorityRateLimit   = 800
	PriorityBodyLimit   = 900
	PriorityApplication = 1000
)

// Common configuration helpers

// DefaultStackConfig returns a default stack configuration
func DefaultStackConfig() StackConfig {
	return StackConfig{
		DefaultTimeout: 30 * time.Second,
		MaxBodySize:    10 * 1024 * 1024, // 10MB
		EnableRecovery: true,
		EnableMetrics:  true,
		EnableTracing:  true,
		Logger:         logger.GetGlobalLogger(),
	}
}

// ProductionStackConfig returns a production-optimized stack configuration
func ProductionStackConfig() StackConfig {
	config := DefaultStackConfig()
	config.EnableMetrics = true
	config.EnableTracing = true
	return config
}

// DevelopmentStackConfig returns a development-optimized stack configuration
func DevelopmentStackConfig() StackConfig {
	config := DefaultStackConfig()
	config.EnableMetrics = false
	config.EnableTracing = false
	return config
}
