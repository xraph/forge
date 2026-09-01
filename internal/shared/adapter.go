package shared

import (
	"net/http"

	"github.com/xraph/forge/internal/pathspec"
)

// ExtendedAdapter is the optional wide interface a routing backend may
// implement on top of RouterAdapter.
//
// Detection happens once, in NewRouter, via a single type assertion. A backend
// that does not implement this keeps working exactly as before, which is why
// chi and httprouter need no changes.
type ExtendedAdapter interface {
	RouterAdapter

	// Capabilities reports what this backend actually honors. Forge consults
	// it before relying on any wide behavior, and warns or errors when a
	// route needs something the backend cannot do.
	Capabilities() Capabilities

	// Configure applies router-level matcher settings. It is called once, at
	// construction, before any route is registered. Calling it after the
	// first route must return an error.
	Configure(MatcherConfig) error

	// HandleRoute registers a route from its parsed form. It returns an error
	// for a conflicting registration, which is what the plain Handle method
	// has no way to express.
	HandleRoute(RouteSpec) error
}

// Capabilities reports which wide behaviors a backend honors.
//
// The zero value means "supports nothing wide", which is the correct
// assumption for any adapter forge cannot type-assert to ExtendedAdapter.
// Every field must therefore be safe when false.
type Capabilities struct {
	// MethodNotAllowed means the backend can distinguish "no such path" from
	// "wrong method for this path" and will invoke the 405 handler.
	MethodNotAllowed bool

	// Constraints means the backend enforces Segment.Constraint during
	// matching. When false, forge erases constraints before rendering and
	// checks whether that erasure collides two routes into one.
	Constraints bool

	// ConflictDetection means HandleRoute returns an error for an ambiguous
	// registration rather than silently accepting it.
	ConflictDetection bool

	// TypedParams means the backend publishes parameters through the pooled
	// carrier rather than a map under the legacy string key.
	TypedParams bool

	// AnyMethod means the backend honors an empty RouteSpec.Method as "every
	// method", including verbs outside the common set.
	AnyMethod bool
}

// MatcherConfig carries router-level settings to a wide backend.
//
// The zero value describes forge's complete default behavior, so a backend
// that ignores configuration entirely is still correct.
type MatcherConfig struct {
	// NotFound is invoked when no route matches. Nil means the backend uses
	// its own default.
	NotFound http.Handler

	// MethodNotAllowed is invoked when a path matches but the method does
	// not. The backend sets the Allow header before invoking it. Nil means
	// the backend falls back to NotFound.
	MethodNotAllowed http.Handler
}

// RouteSpec is a route in the form a wide backend receives it.
type RouteSpec struct {
	// Method is the HTTP method. An empty string means every method, which is
	// how a mount registers without enumerating verbs.
	Method string

	// Pattern is the parsed path. The backend never parses a string.
	Pattern pathspec.Pattern

	// Handler is the fully wrapped handler: middleware, interceptors and
	// panic recovery are already applied.
	Handler http.Handler

	// Kind classifies the route. The zero value is KindHTTP.
	Kind RouteKind
}

// RouteKind classifies a route by connection lifetime.
//
// It replaces the "route.type" metadata string that four call sites used to
// write and two used to read with a `.(string)` assertion. The string version
// caused a real bug: the timeout check compared against "sse" and "websocket"
// only, so WebTransport sessions were given a handler deadline.
type RouteKind uint8

const (
	// KindHTTP is an ordinary request/response route. It is the zero value.
	KindHTTP RouteKind = iota
	KindSSE
	KindWebSocket
	KindWebTransport
)

// String returns the name previously stored in "route.type". The AsyncAPI
// generator reads these values, so they are a compatibility contract.
func (k RouteKind) String() string {
	switch k {
	case KindSSE:
		return "sse"
	case KindWebSocket:
		return "websocket"
	case KindWebTransport:
		return "webtransport"
	case KindHTTP:
		return "http"
	}

	return "http"
}

// LongLived reports whether a connection on this route outlives the handler
// call in the way a request/response exchange does not.
//
// Two behaviors hang off this: pooled parameters are not recycled for such
// routes, and the per-handler timeout is skipped.
func (k RouteKind) LongLived() bool { return k != KindHTTP }
