package router

// InterceptorResult represents the outcome of an interceptor execution.
// Interceptors can either allow the request to proceed, block it, or enrich the context.
type InterceptorResult struct {
	// Blocked indicates the request should not proceed
	Blocked bool

	// Error is returned when the interceptor blocks the request
	Error error

	// Values to merge into the context (enrichment)
	// These are merged with existing context values, not replaced
	Values map[string]any
}

// Allow returns a result that allows the request to proceed.
func Allow() InterceptorResult {
	return InterceptorResult{Blocked: false}
}

// AllowWithValues allows the request and enriches the context with values.
// Values are merged with existing context values.
func AllowWithValues(values map[string]any) InterceptorResult {
	return InterceptorResult{Blocked: false, Values: values}
}

// Block returns a result that blocks the request with an error.
func Block(err error) InterceptorResult {
	return InterceptorResult{Blocked: true, Error: err}
}

// BlockWithValues blocks the request but still provides values (useful for debugging/logging).
func BlockWithValues(err error, values map[string]any) InterceptorResult {
	return InterceptorResult{Blocked: true, Error: err, Values: values}
}

// InterceptorFunc is a function that inspects a request and decides
// whether to allow it to proceed.
//
// Interceptors receive:
//   - ctx: The forge Context with full request access
//   - route: Full RouteInfo for permission/config checking
//
// Return InterceptorResult using convenience functions:
//   - Allow() - proceed to handler
//   - AllowWithValues(map) - proceed and enrich context
//   - Block(err) - reject request with error
type InterceptorFunc func(ctx Context, route RouteInfo) InterceptorResult

// Interceptor represents a named interceptor with metadata.
// Named interceptors can be skipped on specific routes.
type Interceptor interface {
	// Name returns the unique identifier for this interceptor.
	// Used for skipping, debugging, and logging.
	Name() string

	// Intercept executes the interceptor logic.
	Intercept(ctx Context, route RouteInfo) InterceptorResult
}

// interceptorImpl wraps an InterceptorFunc with a name.
type interceptorImpl struct {
	name string
	fn   InterceptorFunc
}

func (i *interceptorImpl) Name() string {
	return i.name
}

func (i *interceptorImpl) Intercept(ctx Context, route RouteInfo) InterceptorResult {
	return i.fn(ctx, route)
}

// NewInterceptor creates a named interceptor from a function.
// Named interceptors can be skipped using WithSkipInterceptor.
//
// Example:
//
//	var RequireAdmin = NewInterceptor("require-admin", func(ctx Context, route RouteInfo) InterceptorResult {
//	    if ctx.Get("user.role") != "admin" {
//	        return Block(Forbidden("admin access required"))
//	    }
//	    return Allow()
//	})
func NewInterceptor(name string, fn InterceptorFunc) Interceptor {
	return &interceptorImpl{name: name, fn: fn}
}

// InterceptorFromFunc creates an anonymous interceptor from a function.
// Anonymous interceptors cannot be skipped by name.
// Use NewInterceptor for interceptors that may need to be skipped.
func InterceptorFromFunc(fn InterceptorFunc) Interceptor {
	return &interceptorImpl{name: "", fn: fn}
}
