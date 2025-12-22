package router

// executeInterceptors runs all interceptors for a route.
// Returns an error if any interceptor blocks, nil otherwise.
// Skips interceptors listed in skipSet.
// Merges values into context as interceptors complete.
func executeInterceptors(
	ctx Context,
	route RouteInfo,
	interceptors []Interceptor,
	skipSet map[string]bool,
) error {
	for _, interceptor := range interceptors {
		// Check if this interceptor should be skipped
		name := interceptor.Name()
		if name != "" && skipSet != nil && skipSet[name] {
			continue
		}

		result := interceptor.Intercept(ctx, route)

		// Merge values into context (regardless of block status for debugging/logging)
		for k, v := range result.Values {
			ctx.Set(k, v)
		}

		if result.Blocked {
			return result.Error
		}
	}

	return nil
}

// combineSkipSets merges multiple skip sets into one.
func combineSkipSets(sets ...map[string]bool) map[string]bool {
	combined := make(map[string]bool)

	for _, set := range sets {
		for name := range set {
			combined[name] = true
		}
	}

	return combined
}

// combineInterceptors merges group and route interceptors in proper order.
// Group interceptors run first, then route-specific interceptors.
func combineInterceptors(groupInterceptors, routeInterceptors []Interceptor) []Interceptor {
	if len(groupInterceptors) == 0 {
		return routeInterceptors
	}

	if len(routeInterceptors) == 0 {
		return groupInterceptors
	}

	combined := make([]Interceptor, 0, len(groupInterceptors)+len(routeInterceptors))
	combined = append(combined, groupInterceptors...)
	combined = append(combined, routeInterceptors...)

	return combined
}

// InterceptorChain represents a chain of interceptors with skip configuration.
// This is used internally by the router to manage interceptors for a route.
type InterceptorChain struct {
	interceptors []Interceptor
	skipSet      map[string]bool
}

// NewInterceptorChain creates a new interceptor chain.
func NewInterceptorChain(interceptors []Interceptor, skipSet map[string]bool) *InterceptorChain {
	return &InterceptorChain{
		interceptors: interceptors,
		skipSet:      skipSet,
	}
}

// Execute runs all interceptors in the chain.
// Returns an error if any interceptor blocks.
func (c *InterceptorChain) Execute(ctx Context, route RouteInfo) error {
	return executeInterceptors(ctx, route, c.interceptors, c.skipSet)
}

// Empty returns true if the chain has no interceptors.
func (c *InterceptorChain) Empty() bool {
	return len(c.interceptors) == 0
}

// Len returns the number of interceptors in the chain.
func (c *InterceptorChain) Len() int {
	return len(c.interceptors)
}
