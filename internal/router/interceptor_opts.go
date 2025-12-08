package router

// --- Route option implementations ---

type interceptorOpt struct {
	interceptors []Interceptor
}

func (o *interceptorOpt) Apply(cfg *RouteConfig) {
	cfg.Interceptors = append(cfg.Interceptors, o.interceptors...)
}

type skipInterceptorOpt struct {
	names []string
}

func (o *skipInterceptorOpt) Apply(cfg *RouteConfig) {
	if cfg.SkipInterceptors == nil {
		cfg.SkipInterceptors = make(map[string]bool)
	}

	for _, name := range o.names {
		cfg.SkipInterceptors[name] = true
	}
}

// --- Group option implementations ---

type groupInterceptorOpt struct {
	interceptors []Interceptor
}

func (o *groupInterceptorOpt) Apply(cfg *GroupConfig) {
	cfg.Interceptors = append(cfg.Interceptors, o.interceptors...)
}

type groupSkipInterceptorOpt struct {
	names []string
}

func (o *groupSkipInterceptorOpt) Apply(cfg *GroupConfig) {
	if cfg.SkipInterceptors == nil {
		cfg.SkipInterceptors = make(map[string]bool)
	}

	for _, name := range o.names {
		cfg.SkipInterceptors[name] = true
	}
}

// --- Constructors ---

// WithInterceptor adds interceptors to a route.
// Interceptors run in order before the handler.
// If any interceptor blocks, subsequent interceptors and the handler are not executed.
//
// Interceptors are different from middleware:
//   - Middleware wraps the handler chain (before/after)
//   - Interceptors run before the handler only, with a simple allow/block decision
//   - Interceptors can enrich the context with values
//   - Interceptors can be skipped by name
//
// Example:
//
//	router.POST("/admin/users", handler,
//	    forge.WithInterceptor(RequireAdmin, RateLimitCheck),
//	)
func WithInterceptor(interceptors ...Interceptor) RouteOption {
	return &interceptorOpt{interceptors: interceptors}
}

// WithSkipInterceptor skips named interceptors for this route.
// Useful for excluding specific group-level interceptors.
// Only named interceptors (created with NewInterceptor) can be skipped.
//
// Example:
//
//	adminAPI := router.Group("/admin",
//	    forge.WithGroupInterceptor(RequireAdmin),
//	)
//
//	// Skip admin check for health endpoint
//	adminAPI.GET("/health", healthHandler,
//	    forge.WithSkipInterceptor("require-admin"),
//	)
func WithSkipInterceptor(names ...string) RouteOption {
	return &skipInterceptorOpt{names: names}
}

// WithGroupInterceptor adds interceptors to all routes in a group.
// Group interceptors run before route-specific interceptors.
// These interceptors are inherited by nested groups.
//
// Example:
//
//	adminAPI := router.Group("/admin",
//	    forge.WithGroupInterceptor(RequireAuth(), RequireAdmin),
//	)
//
//	// All routes in this group require auth and admin role
//	adminAPI.GET("/users", listUsers)
//	adminAPI.DELETE("/users/:id", deleteUser)
func WithGroupInterceptor(interceptors ...Interceptor) GroupOption {
	return &groupInterceptorOpt{interceptors: interceptors}
}

// WithGroupSkipInterceptor skips named interceptors for all routes in a group.
// Useful for excluding parent group interceptors from a nested group.
//
// Example:
//
//	api := router.Group("/api",
//	    forge.WithGroupInterceptor(RateLimitInterceptor),
//	)
//
//	// Nested group without rate limiting
//	internal := api.Group("/internal",
//	    forge.WithGroupSkipInterceptor("rate-limit"),
//	)
func WithGroupSkipInterceptor(names ...string) GroupOption {
	return &groupSkipInterceptorOpt{names: names}
}

