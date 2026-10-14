package shared

import "context"

type routeMatchKey struct{}

// RouteMatch carries the matched route pattern back up the middleware chain.
//
// Global middleware runs before the router matches a route, so it cannot read
// the pattern on the way in. It puts a RouteMatch on the context, the router
// fills Pattern just before the route's handler runs, and the middleware reads
// it once the handler returns.
type RouteMatch struct {
	Pattern string
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
