package router

import (
	"slices"
	"time"
)

// --- Authentication Interceptors ---

// RequireAuth creates an interceptor that requires authentication.
// Checks for "auth" or "user" in the context (typically set by auth middleware).
func RequireAuth() Interceptor {
	return NewInterceptor("require-auth", func(ctx Context, route RouteInfo) InterceptorResult {
		if ctx.Get("auth") == nil && ctx.Get("user") == nil {
			return Block(Unauthorized("authentication required"))
		}

		return Allow()
	})
}

// RequireAuthProvider creates an interceptor that requires a specific auth provider.
// Checks the "auth.provider" context value.
func RequireAuthProvider(providerName string) Interceptor {
	return NewInterceptor("require-auth:"+providerName, func(ctx Context, route RouteInfo) InterceptorResult {
		authProvider := ctx.Get("auth.provider")
		if authProvider != providerName {
			return Block(Unauthorized("authentication via " + providerName + " required"))
		}

		return Allow()
	})
}

// --- Authorization Interceptors ---

// RequireScopes creates an interceptor that requires ALL specified scopes.
// Checks the "auth.scopes" context value (expected to be []string).
func RequireScopes(scopes ...string) Interceptor {
	return NewInterceptor("require-scopes", func(ctx Context, route RouteInfo) InterceptorResult {
		userScopes, ok := ctx.Get("auth.scopes").([]string)
		if !ok {
			return Block(Forbidden("no scopes available"))
		}

		scopeSet := make(map[string]bool)
		for _, s := range userScopes {
			scopeSet[s] = true
		}

		for _, required := range scopes {
			if !scopeSet[required] {
				return Block(Forbidden("missing required scope: " + required))
			}
		}

		return Allow()
	})
}

// RequireAnyScope creates an interceptor that requires ANY of the specified scopes.
// At least one scope must be present.
func RequireAnyScope(scopes ...string) Interceptor {
	return NewInterceptor("require-any-scope", func(ctx Context, route RouteInfo) InterceptorResult {
		userScopes, ok := ctx.Get("auth.scopes").([]string)
		if !ok {
			return Block(Forbidden("no scopes available"))
		}

		scopeSet := make(map[string]bool)
		for _, s := range userScopes {
			scopeSet[s] = true
		}

		for _, scope := range scopes {
			if scopeSet[scope] {
				return Allow()
			}
		}

		return Block(Forbidden("insufficient permissions"))
	})
}

// RequireRole creates an interceptor that requires ANY of the specified roles.
// Checks the "user.role" context value.
func RequireRole(roles ...string) Interceptor {
	return NewInterceptor("require-role", func(ctx Context, route RouteInfo) InterceptorResult {
		userRole := ctx.Get("user.role")
		if userRole == nil {
			return Block(Forbidden("no role assigned"))
		}

		for _, role := range roles {
			if userRole == role {
				return Allow()
			}
		}

		return Block(Forbidden("insufficient permissions"))
	})
}

// RequireAllRoles creates an interceptor that requires ALL specified roles.
// Checks the "user.roles" context value (expected to be []string).
func RequireAllRoles(roles ...string) Interceptor {
	return NewInterceptor("require-all-roles", func(ctx Context, route RouteInfo) InterceptorResult {
		userRoles, ok := ctx.Get("user.roles").([]string)
		if !ok {
			return Block(Forbidden("no roles assigned"))
		}

		roleSet := make(map[string]bool)
		for _, r := range userRoles {
			roleSet[r] = true
		}

		for _, required := range roles {
			if !roleSet[required] {
				return Block(Forbidden("missing required role: " + required))
			}
		}

		return Allow()
	})
}

// --- Tenant Interceptors ---

// TenantIsolation creates an interceptor that validates tenant access.
// Compares the tenant from the URL param with the user's tenant.
// Checks "user.tenantId" context value first, then falls back to the
// forge Scope's OrgID (from "forge:scope") for compatibility with the
// universal scope identity system.
func TenantIsolation(tenantParamName string) Interceptor {
	return NewInterceptor("tenant-isolation", func(ctx Context, route RouteInfo) InterceptorResult {
		requestTenantID := ctx.Param(tenantParamName)
		if requestTenantID == "" {
			return Allow() // No tenant in request, skip check
		}

		// Check legacy user.tenantId first
		if userTenantID := ctx.Get("user.tenantId"); userTenantID != nil {
			if requestTenantID != userTenantID {
				return Block(Forbidden("cross-tenant access denied"))
			}
			return Allow()
		}

		// Fallback: check forge Scope's OrgID (duck-typed to avoid circular import)
		type scopeWithOrg interface {
			OrgID() string
		}
		if scopeVal := ctx.Get("forge:scope"); scopeVal != nil {
			if s, ok := scopeVal.(scopeWithOrg); ok && s.OrgID() != "" {
				if requestTenantID != s.OrgID() {
					return Block(Forbidden("cross-tenant access denied"))
				}
				return Allow()
			}
		}

		return Block(Forbidden("tenant access denied"))
	})
}

// --- Feature Flag Interceptors ---

// FeatureFlag creates an interceptor that checks if a feature is enabled.
// The checker function determines if the feature is enabled for the current request.
func FeatureFlag(flagName string, checker func(ctx Context, flag string) bool) Interceptor {
	return NewInterceptor("feature-flag:"+flagName, func(ctx Context, route RouteInfo) InterceptorResult {
		if !checker(ctx, flagName) {
			return Block(NotFound("feature not available"))
		}

		return Allow()
	})
}

// FeatureFlagFromContext creates an interceptor that checks a feature flag from context.
// Expects a "feature-flags" map[string]bool in context.
func FeatureFlagFromContext(flagName string) Interceptor {
	return NewInterceptor("feature-flag:"+flagName, func(ctx Context, route RouteInfo) InterceptorResult {
		flags, ok := ctx.Get("feature-flags").(map[string]bool)
		if !ok {
			return Block(NotFound("feature not available"))
		}

		if !flags[flagName] {
			return Block(NotFound("feature not available"))
		}

		return Allow()
	})
}

// --- Enrichment Interceptors ---

// Enrich creates an interceptor that enriches the context with values.
// The loader function is called to fetch data to inject.
func Enrich(name string, loader func(ctx Context, route RouteInfo) (map[string]any, error)) Interceptor {
	return NewInterceptor("enrich:"+name, func(ctx Context, route RouteInfo) InterceptorResult {
		values, err := loader(ctx, route)
		if err != nil {
			return Block(InternalError(err))
		}

		return AllowWithValues(values)
	})
}

// EnrichUser creates an interceptor that loads user data into context under "user" key.
func EnrichUser(loader func(ctx Context) (any, error)) Interceptor {
	return NewInterceptor("enrich:user", func(ctx Context, route RouteInfo) InterceptorResult {
		user, err := loader(ctx)
		if err != nil {
			return Block(InternalError(err))
		}

		return AllowWithValues(map[string]any{"user": user})
	})
}

// EnrichFromService creates an interceptor that loads data from a DI service.
func EnrichFromService[T any](serviceName string, loader func(ctx Context, svc T) (map[string]any, error)) Interceptor {
	return NewInterceptor("enrich:"+serviceName, func(ctx Context, route RouteInfo) InterceptorResult {
		svc, err := ctx.Resolve(serviceName)
		if err != nil {
			return Block(InternalError(err))
		}

		typedSvc, ok := svc.(T)
		if !ok {
			return Block(InternalError(nil))
		}

		values, err := loader(ctx, typedSvc)
		if err != nil {
			return Block(InternalError(err))
		}

		return AllowWithValues(values)
	})
}

// --- Metadata-Based Interceptors ---

// RequireMetadata creates an interceptor that checks route metadata.
func RequireMetadata(key string, expectedValue any) Interceptor {
	return NewInterceptor("require-metadata:"+key, func(ctx Context, route RouteInfo) InterceptorResult {
		if route.Metadata == nil {
			return Block(Forbidden("access denied"))
		}

		value, exists := route.Metadata[key]
		if !exists || value != expectedValue {
			return Block(Forbidden("access denied"))
		}

		return Allow()
	})
}

// RequireTag creates an interceptor that checks if route has a specific tag.
func RequireTag(tag string) Interceptor {
	return NewInterceptor("require-tag:"+tag, func(ctx Context, route RouteInfo) InterceptorResult {
		if slices.Contains(route.Tags, tag) {
			return Allow()
		}

		return Block(Forbidden("access denied"))
	})
}

// --- Rate Limiting Interceptors ---

// RateLimitResult contains rate limit check results.
type RateLimitResult struct {
	Allowed   bool
	Remaining int
	ResetAt   time.Time
}

// RateLimit creates a rate limit interceptor.
// The checker function should return the rate limit status for the given key.
// Rate limit info is added to context for response headers.
func RateLimit(keyName string, checker func(ctx Context, key string) RateLimitResult) Interceptor {
	return NewInterceptor("rate-limit:"+keyName, func(ctx Context, route RouteInfo) InterceptorResult {
		result := checker(ctx, keyName)

		// Enrich with rate limit info for response headers
		values := map[string]any{
			"ratelimit.remaining": result.Remaining,
			"ratelimit.reset":     result.ResetAt,
		}

		if !result.Allowed {
			return InterceptorResult{
				Blocked: true,
				Error:   NewHTTPError(429, "rate limit exceeded"),
				Values:  values,
			}
		}

		return AllowWithValues(values)
	})
}

// RateLimitByIP creates a rate limit interceptor keyed by client IP.
func RateLimitByIP(checker func(ctx Context, ip string) RateLimitResult) Interceptor {
	return NewInterceptor("rate-limit:ip", func(ctx Context, route RouteInfo) InterceptorResult {
		// Get client IP (simplified - in production, parse X-Forwarded-For, etc.)
		clientIP := ctx.Request().RemoteAddr

		result := checker(ctx, clientIP)

		values := map[string]any{
			"ratelimit.remaining": result.Remaining,
			"ratelimit.reset":     result.ResetAt,
		}

		if !result.Allowed {
			return InterceptorResult{
				Blocked: true,
				Error:   NewHTTPError(429, "rate limit exceeded"),
				Values:  values,
			}
		}

		return AllowWithValues(values)
	})
}

// --- IP/Network Interceptors ---

// AllowIPs creates an interceptor that only allows specific IP addresses.
func AllowIPs(allowedIPs ...string) Interceptor {
	ipSet := make(map[string]bool)
	for _, ip := range allowedIPs {
		ipSet[ip] = true
	}

	return NewInterceptor("allow-ips", func(ctx Context, route RouteInfo) InterceptorResult {
		clientIP := ctx.Request().RemoteAddr
		// Note: In production, parse X-Forwarded-For, X-Real-IP, etc.

		if !ipSet[clientIP] {
			return Block(Forbidden("IP not allowed"))
		}

		return Allow()
	})
}

// DenyIPs creates an interceptor that blocks specific IP addresses.
func DenyIPs(deniedIPs ...string) Interceptor {
	ipSet := make(map[string]bool)
	for _, ip := range deniedIPs {
		ipSet[ip] = true
	}

	return NewInterceptor("deny-ips", func(ctx Context, route RouteInfo) InterceptorResult {
		clientIP := ctx.Request().RemoteAddr

		if ipSet[clientIP] {
			return Block(Forbidden("IP blocked"))
		}

		return Allow()
	})
}

// --- Time-Based Interceptors ---

// TimeWindow creates an interceptor that only allows requests during specific hours.
// Hours are in 24-hour format (0-23) in the specified timezone.
func TimeWindow(startHour, endHour int, location *time.Location) Interceptor {
	return NewInterceptor("time-window", func(ctx Context, route RouteInfo) InterceptorResult {
		now := time.Now().In(location)
		hour := now.Hour()

		if hour < startHour || hour >= endHour {
			return Block(NewHTTPError(503, "service unavailable during this time"))
		}

		return Allow()
	})
}

// Maintenance creates an interceptor that blocks requests during maintenance.
// The checker function returns true if maintenance mode is active.
func Maintenance(checker func() bool) Interceptor {
	return NewInterceptor("maintenance", func(ctx Context, route RouteInfo) InterceptorResult {
		if checker() {
			return Block(NewHTTPError(503, "service under maintenance"))
		}

		return Allow()
	})
}

// --- Validation Interceptors ---

// RequireHeader creates an interceptor that requires specific headers to be present.
func RequireHeader(headers ...string) Interceptor {
	return NewInterceptor("require-header", func(ctx Context, route RouteInfo) InterceptorResult {
		for _, header := range headers {
			if ctx.Header(header) == "" {
				return Block(BadRequest("missing required header: " + header))
			}
		}

		return Allow()
	})
}

// RequireContentType creates an interceptor that requires a specific Content-Type.
func RequireContentType(contentTypes ...string) Interceptor {
	return NewInterceptor("require-content-type", func(ctx Context, route RouteInfo) InterceptorResult {
		ct := ctx.Header("Content-Type")

		if slices.Contains(contentTypes, ct) {
			return Allow()
		}

		return Block(NewHTTPError(415, "unsupported media type"))
	})
}

// --- Audit/Logging Interceptors ---

// AuditLog creates an interceptor that logs access attempts.
// The logger function is called with audit information.
func AuditLog(logger func(ctx Context, route RouteInfo, timestamp time.Time)) Interceptor {
	return NewInterceptor("audit-log", func(ctx Context, route RouteInfo) InterceptorResult {
		timestamp := time.Now()

		// Log the access
		logger(ctx, route, timestamp)

		// Enrich context with audit info
		return AllowWithValues(map[string]any{
			"audit.timestamp": timestamp,
			"audit.route":     route.Path,
			"audit.method":    route.Method,
		})
	})
}

// --- Custom Interceptor Helpers ---

// FromFunc creates an anonymous interceptor from a simple function.
// Equivalent to InterceptorFromFunc but with a clearer name.
func FromFunc(fn InterceptorFunc) Interceptor {
	return InterceptorFromFunc(fn)
}

// Named wraps an anonymous interceptor with a name.
// Useful for making interceptors skippable.
func Named(name string, interceptor Interceptor) Interceptor {
	return NewInterceptor(name, func(ctx Context, route RouteInfo) InterceptorResult {
		return interceptor.Intercept(ctx, route)
	})
}
