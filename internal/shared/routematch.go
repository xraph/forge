package shared

import "context"

type routeMatchKey struct{}

// RouteMatch carries the matched route pattern back up the middleware chain.
//
// Global middleware runs before the router matches a route, so it cannot read
// the pattern on the way in. It puts a RouteMatch on the context, the router
// calls Match just before the route's handler runs, and the middleware reads
// it once the handler returns.
//
// The holder has no locking. It assumes every middleware between the one that
// installed it and the router calls next on the request goroutine. Middleware
// that runs next on another goroutine, as middleware.Timeout does, breaks
// that: Establish and its callbacks then race with the outer middleware
// reading Established, and on a timeout the outer middleware finishes the
// request while the stream is still opening. Stream routes must not sit
// behind such middleware.
type RouteMatch struct {
	Pattern string
	Kind    RouteKind // KindHTTP unless the router stamped a stream kind

	established bool
	onEstablish []func(status int)
	onMatch     []func(pattern string)

	// inline backs onEstablish for the usual case, tracing and HTTP metrics
	// registering one callback each, so a request doesn't allocate for them.
	// inlineMatch does the same for onMatch, where profiling registers one.
	inline      [2]func(status int)
	inlineMatch [1]func(pattern string)
}

// OnEstablished registers fn to run when the router reports a stream route as
// set up: after a WebSocket or WebTransport upgrade, or once an SSE stream is
// writable. Middleware registers before passing the request on. Everything runs
// on the request goroutine, so there is no locking.
func (m *RouteMatch) OnEstablished(fn func(status int)) {
	if m == nil {
		return
	}

	if m.onEstablish == nil {
		m.onEstablish = m.inline[:0]
	}

	m.onEstablish = append(m.onEstablish, fn)
}

// Establish runs the registered callbacks once. Later calls do nothing.
func (m *RouteMatch) Establish(status int) {
	if m == nil || m.established {
		return
	}

	m.established = true

	for _, fn := range m.onEstablish {
		fn(status)
	}
}

// Established reports whether Establish has run.
func (m *RouteMatch) Established() bool {
	return m != nil && m.established
}

// OnMatched registers fn to run when the router stamps the matched route,
// before the route's own middleware runs. Like OnEstablished, everything runs
// on the request goroutine, so there is no locking.
func (m *RouteMatch) OnMatched(fn func(pattern string)) {
	if m == nil {
		return
	}

	if m.onMatch == nil {
		m.onMatch = m.inlineMatch[:0]
	}

	m.onMatch = append(m.onMatch, fn)
}

// Match records the matched route and runs the OnMatched callbacks. The router
// calls it once per request, just before the route's handler chain.
func (m *RouteMatch) Match(pattern string, kind RouteKind) {
	if m == nil {
		return
	}

	m.Pattern, m.Kind = pattern, kind

	for _, fn := range m.onMatch {
		fn(pattern)
	}
}

// PatternOrEmpty returns the stamped pattern, or "" on a nil holder.
func (m *RouteMatch) PatternOrEmpty() string {
	if m == nil {
		return ""
	}

	return m.Pattern
}

// NewRouteMatch always puts a fresh holder on ctx, hiding any holder an outer
// request left there. The outermost middleware for a request calls this, so a
// handler that dispatches a second request through the router with its own
// context (an RPC bridge, say) can't rename the outer request's span. Inner
// middleware calls EnsureRouteMatch to share the holder.
func NewRouteMatch(ctx context.Context) (context.Context, *RouteMatch) {
	m := &RouteMatch{}

	return context.WithValue(ctx, routeMatchKey{}, m), m
}

// EnsureRouteMatch returns the holder already on ctx, or adds a new one.
func EnsureRouteMatch(ctx context.Context) (context.Context, *RouteMatch) {
	if m, ok := ctx.Value(routeMatchKey{}).(*RouteMatch); ok {
		return ctx, m
	}

	m := &RouteMatch{}

	return context.WithValue(ctx, routeMatchKey{}, m), m
}

// RouteMatchFromContext returns the holder on ctx, or nil when there is none.
func RouteMatchFromContext(ctx context.Context) *RouteMatch {
	m, _ := ctx.Value(routeMatchKey{}).(*RouteMatch)

	return m
}
