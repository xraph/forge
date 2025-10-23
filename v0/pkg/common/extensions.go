package common

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type Middleware func(next http.Handler) http.Handler

// NamedMiddleware middleware when you need registration
type NamedMiddleware interface {
	Name() string
	// Priority returns the priority (lower = higher priority)
	Priority() int
	Handler() func(next http.Handler) http.Handler
}

// StatefulMiddleware extends Middleware with lifecycle management
type StatefulMiddleware interface {
	NamedMiddleware
	Initialize(container Container) error
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
}

// InstrumentedMiddleware extends Middleware with statistics
type InstrumentedMiddleware interface {
	NamedMiddleware

	// GetStats returns middleware statistics
	GetStats() MiddlewareStats
}

// MiddlewareStats contains statistics about middleware performance
type MiddlewareStats struct {
	Name           string        `json:"name"`
	Priority       int           `json:"priority"`
	Applied        bool          `json:"applied"`
	AppliedAt      time.Time     `json:"applied_at"`
	CallCount      int64         `json:"call_count"`
	ErrorCount     int64         `json:"error_count"`
	TotalLatency   time.Duration `json:"total_latency"`
	AverageLatency time.Duration `json:"average_latency"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LastError      string        `json:"last_error,omitempty"`
	Dependencies   []string      `json:"dependencies"`
	Status         string        `json:"status"`
}

// MiddlewareEntry represents a unified middleware container
type MiddlewareEntry struct {
	Name     string     `json:"name"`
	Priority int        `json:"priority"`
	Handler  Middleware `json:"-"`
	Type     string     `json:"type"` // "function", "named", "stateful"
	Instance interface{}
}

// NewMiddlewareEntry creates a MiddlewareEntry from any middleware type
func NewMiddlewareEntry(middleware interface{}) (*MiddlewareEntry, error) {
	switch m := middleware.(type) {
	case Middleware:
		return &MiddlewareEntry{
			Name:     fmt.Sprintf("func_middleware_%d", time.Now().UnixNano()),
			Priority: 50,
			Handler:  m,
			Type:     "function",
			Instance: m,
		}, nil

	case NamedMiddleware:
		return &MiddlewareEntry{
			Name:     m.Name(),
			Priority: m.Priority(),
			Handler:  m.Handler(),
			Type:     "named",
			Instance: m,
		}, nil

	case StatefulMiddleware:
		return &MiddlewareEntry{
			Name:     m.Name(),
			Priority: m.Priority(),
			Handler:  m.Handler(),
			Type:     "stateful",
			Instance: m,
		}, nil

	default:
		return nil, ErrInvalidConfig("middleware", fmt.Errorf("unsupported middleware type: %T", middleware))
	}
}

// ForgeContextMiddleware injects ForgeContext into requests
func ForgeContextMiddleware(container Container, logger Logger, metrics Metrics, config ConfigManager) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create ForgeContext
			forgeCtx := NewForgeContext(r.Context(), container, logger, metrics, config)
			forgeCtx = forgeCtx.WithRequest(r).WithResponseWriter(w)

			// Store in standard context
			ctx := WithForgeContext(r.Context(), forgeCtx)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}
