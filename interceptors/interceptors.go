// Package interceptors provides pre-built interceptors for forge routes.
//
// Interceptors run before the handler and make allow/block decisions.
// They differ from middleware: middleware wraps the handler chain (before/after),
// while interceptors only run before the handler with a simple allow/block/enrich decision.
//
// Usage:
//
//	import "github.com/xraph/forge/interceptors"
//
//	router.GET("/admin/users", handler,
//	    interceptors.WithInterceptor(
//	        interceptors.RequireAuth(),
//	        interceptors.RequireRole("admin"),
//	    ),
//	)
package interceptors

import (
	"time"

	"github.com/xraph/forge/internal/router"
)

// ---------------------------------------------------------------------------
// Core Types
// ---------------------------------------------------------------------------

// Interceptor represents a named interceptor with metadata.
type Interceptor = router.Interceptor

// InterceptorFunc is a function that inspects a request and decides
// whether to allow it to proceed.
type InterceptorFunc = router.InterceptorFunc

// InterceptorResult represents the outcome of an interceptor execution.
type InterceptorResult = router.InterceptorResult

// Context is the forge request context.
type Context = router.Context

// RouteInfo provides route information for inspection.
type RouteInfo = router.RouteInfo

// RouteOption configures a route.
type RouteOption = router.RouteOption

// GroupOption configures a route group.
type GroupOption = router.GroupOption

// RateLimitResult contains rate limit check results.
type RateLimitResult = router.RateLimitResult

// ---------------------------------------------------------------------------
// Result Helpers
// ---------------------------------------------------------------------------

var (
	// Allow returns a result that allows the request to proceed.
	Allow = router.Allow

	// AllowWithValues allows the request and enriches the context with values.
	AllowWithValues = router.AllowWithValues

	// Block returns a result that blocks the request with an error.
	Block = router.Block

	// BlockWithValues blocks the request but still provides values.
	BlockWithValues = router.BlockWithValues
)

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

var (
	// NewInterceptor creates a named interceptor from a function.
	NewInterceptor = router.NewInterceptor

	// InterceptorFromFunc creates an anonymous interceptor from a function.
	InterceptorFromFunc = router.InterceptorFromFunc
)

// ---------------------------------------------------------------------------
// Route / Group Options
// ---------------------------------------------------------------------------

// WithInterceptor adds interceptors to a route.
func WithInterceptor(i ...Interceptor) RouteOption {
	return router.WithInterceptor(i...)
}

// WithSkipInterceptor skips named interceptors for a route.
func WithSkipInterceptor(names ...string) RouteOption {
	return router.WithSkipInterceptor(names...)
}

// WithGroupInterceptor adds interceptors to all routes in a group.
func WithGroupInterceptor(i ...Interceptor) GroupOption {
	return router.WithGroupInterceptor(i...)
}

// WithGroupSkipInterceptor skips named interceptors for all routes in a group.
func WithGroupSkipInterceptor(names ...string) GroupOption {
	return router.WithGroupSkipInterceptor(names...)
}

// ---------------------------------------------------------------------------
// Authentication Interceptors
// ---------------------------------------------------------------------------

var (
	// RequireAuth requires authentication (checks "auth" or "user" in context).
	RequireAuth = router.RequireAuth

	// RequireAuthProvider requires a specific auth provider.
	RequireAuthProvider = router.RequireAuthProvider
)

// ---------------------------------------------------------------------------
// Authorization Interceptors
// ---------------------------------------------------------------------------

var (
	// RequireScopes requires ALL specified scopes.
	RequireScopes = router.RequireScopes

	// RequireAnyScope requires ANY of the specified scopes.
	RequireAnyScope = router.RequireAnyScope

	// RequireRole requires ANY of the specified roles.
	RequireRole = router.RequireRole

	// RequireAllRoles requires ALL specified roles.
	RequireAllRoles = router.RequireAllRoles
)

// ---------------------------------------------------------------------------
// Tenant Interceptors
// ---------------------------------------------------------------------------

var (
	// TenantIsolation validates tenant access from URL param against user context.
	TenantIsolation = router.TenantIsolation
)

// ---------------------------------------------------------------------------
// Feature Flag Interceptors
// ---------------------------------------------------------------------------

var (
	// FeatureFlag checks if a feature is enabled using a checker function.
	FeatureFlag = router.FeatureFlag

	// FeatureFlagFromContext checks a feature flag from the "feature-flags" context map.
	FeatureFlagFromContext = router.FeatureFlagFromContext
)

// ---------------------------------------------------------------------------
// Enrichment Interceptors
// ---------------------------------------------------------------------------

var (
	// Enrich enriches the context with values from a loader function.
	Enrich = router.Enrich

	// EnrichUser loads user data into context under the "user" key.
	EnrichUser = router.EnrichUser
)

// EnrichFromService loads data from a DI service into context.
func EnrichFromService[T any](serviceName string, loader func(ctx Context, svc T) (map[string]any, error)) Interceptor {
	return router.EnrichFromService[T](serviceName, loader)
}

// ---------------------------------------------------------------------------
// Metadata-Based Interceptors
// ---------------------------------------------------------------------------

var (
	// RequireMetadata checks that a route metadata key matches an expected value.
	RequireMetadata = router.RequireMetadata

	// RequireTag checks if a route has a specific tag.
	RequireTag = router.RequireTag
)

// ---------------------------------------------------------------------------
// Rate Limiting Interceptors
// ---------------------------------------------------------------------------

var (
	// RateLimit creates a rate limit interceptor with a custom checker.
	RateLimit = router.RateLimit

	// RateLimitByIP creates a rate limit interceptor keyed by client IP.
	RateLimitByIP = router.RateLimitByIP
)

// ---------------------------------------------------------------------------
// IP / Network Interceptors
// ---------------------------------------------------------------------------

var (
	// AllowIPs only allows specific IP addresses.
	AllowIPs = router.AllowIPs

	// DenyIPs blocks specific IP addresses.
	DenyIPs = router.DenyIPs
)

// ---------------------------------------------------------------------------
// Time-Based Interceptors
// ---------------------------------------------------------------------------

// TimeWindow only allows requests during specific hours.
func TimeWindow(startHour, endHour int, location *time.Location) Interceptor {
	return router.TimeWindow(startHour, endHour, location)
}

var (
	// Maintenance blocks requests when maintenance mode is active.
	Maintenance = router.Maintenance
)

// ---------------------------------------------------------------------------
// Validation Interceptors
// ---------------------------------------------------------------------------

var (
	// RequireHeader requires specific headers to be present.
	RequireHeader = router.RequireHeader

	// RequireContentType requires a specific Content-Type.
	RequireContentType = router.RequireContentType
)

// ---------------------------------------------------------------------------
// Audit / Logging Interceptors
// ---------------------------------------------------------------------------

// AuditLog logs access attempts via a logger function.
func AuditLog(logger func(ctx Context, route RouteInfo, timestamp time.Time)) Interceptor {
	return router.AuditLog(logger)
}

// ---------------------------------------------------------------------------
// Custom Interceptor Helpers
// ---------------------------------------------------------------------------

var (
	// FromFunc creates an anonymous interceptor from a simple function.
	FromFunc = router.FromFunc

	// Named wraps an anonymous interceptor with a name.
	Named = router.Named
)

// ---------------------------------------------------------------------------
// Combinator Interceptors
// ---------------------------------------------------------------------------

var (
	// And combines interceptors — ALL must pass (short-circuits on first block).
	And = router.And

	// Or combines interceptors — ANY one passing is sufficient.
	Or = router.Or

	// Not inverts an interceptor's decision.
	Not = router.Not

	// When conditionally executes an interceptor based on a predicate.
	When = router.When

	// Unless conditionally skips an interceptor based on a predicate.
	Unless = router.Unless

	// IfMetadata conditionally executes an interceptor based on route metadata.
	IfMetadata = router.IfMetadata

	// IfTag conditionally executes an interceptor based on route tags.
	IfTag = router.IfTag

	// ChainInterceptors combines multiple interceptors into a single one (alias for And).
	ChainInterceptors = router.ChainInterceptors
)

// ---------------------------------------------------------------------------
// Parallel Interceptors
// ---------------------------------------------------------------------------

var (
	// Parallel executes interceptors concurrently — ALL must pass.
	Parallel = router.Parallel

	// ParallelAny executes interceptors concurrently — ANY one passing is sufficient.
	ParallelAny = router.ParallelAny
)
