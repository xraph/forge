package common

import (
	"reflect"
	"time"
)

// =============================================================================
// HANDLER DEFINITIONS
// =============================================================================

// HandlerOption defines options for route handlers
type HandlerOption func(*RouteHandlerInfo)

// RouteHandlerInfo contains information about a route handler
type RouteHandlerInfo struct {
	ServiceName    string
	ServiceType    reflect.Type
	HandlerFunc    interface{}
	Method         string
	Path           string
	Middleware     []any
	Dependencies   []string
	Config         interface{}
	Tags           map[string]string
	Opinionated    bool // For automatic schema generation
	RequestType    reflect.Type
	ResponseType   reflect.Type
	RegisteredAt   time.Time
	CallCount      int64
	LastCalled     time.Time
	AverageLatency time.Duration
	ErrorCount     int64
	LastError      error
}

// AddMiddleware safely adds middleware to RouteHandlerInfo
func (rhi *RouteHandlerInfo) AddMiddleware(middleware interface{}) error {
	entry, err := NewMiddlewareEntry(middleware)
	if err != nil {
		return err
	}
	rhi.Middleware = append(rhi.Middleware, entry)
	return nil
}

// GetMiddlewareHandlers returns just the handler functions
func (rhi *RouteHandlerInfo) GetMiddlewareHandlers() []Middleware {
	handlers := make([]Middleware, len(rhi.Middleware))
	for i, entry := range rhi.Middleware {
		me, err := NewMiddlewareEntry(entry)
		if err != nil {
			continue
		}
		handlers[i] = me.Handler
	}
	return handlers
}

// =============================================================================
// HANDLER TYPES
// =============================================================================

// OpinionatedHandler defines the signature for opinionated handlers with automatic schema generation
// Signature: func(ctx Context, req RequestType) (*ResponseType, error)
type OpinionatedHandler interface{}

// ServiceHandler defines the signature for service-aware handlers
// Signature: func(ctx Context, service ServiceType, req RequestType) (*ResponseType, error)
type ServiceHandler interface{}

// ControllerHandler defines the signature for controller handlers
// Signature: func(ctx Context) error (with manual response handling)
type ControllerHandler func(ctx Context) error

// WebSocketHandler defines the signature for WebSocket handlers
// Supported signatures:
// - func(ctx Context, conn Connection) error
// - func(conn Connection) error
type WebSocketHandler interface{}

// SSEHandler defines the signature for Server-Sent Events handlers
// Supported signatures:
// - func(ctx Context, conn Connection) error
// - func(conn Connection) error
type SSEHandler interface{}
