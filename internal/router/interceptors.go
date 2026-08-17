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

// subjectScopes returns the scopes the authenticated subject holds.
//
// It reads "auth.subject.scopes" — the []string the auth extension's
// MiddlewareWithRequirement publishes from AuthContext.Scopes (see
// extensions/auth/registry.go). That key is deliberately not "auth.scopes":
// this package already uses "auth.scopes" as ROUTE metadata for the scopes a
// route requires (set by WithRequiredAuth and WithGroupRequiredScopes in
// router_auth_opts.go, read by the OpenAPI generator and the client
// generator). Reusing the same string for "scopes the route needs" and "scopes
// the subject has" would make the two opposite meanings collide under one
// name, even though they live in different maps and never actually clash at
// runtime.
//
// internal/router cannot import extensions/auth — that package imports forge,
// and forge's router wiring lives here, so the import would cycle. The
// contract is therefore a plain []string passed through the context rather
// than a typed value.
func subjectScopes(ctx Context) ([]string, bool) {
	if scopes, ok := ctx.Get("auth.subject.scopes").([]string); ok {
		return scopes, true
	}

	// Deprecated fallback: "auth.scopes" was the key these interceptors read
	// before the rename. Nothing in this repository ever published it into the
	// request context, so any application relying on it is setting the key
	// itself from its own middleware. Keep honoring it so those callers see no
	// behavior change, and remove once nothing in the wild still sets it.
	// Replacement: "auth.subject.scopes".
	scopes, ok := ctx.Get("auth.scopes").([]string)

	return scopes, ok
}

// RequireScopes creates an interceptor that requires ALL specified scopes.
//
// It reads the subject's scopes via subjectScopes: "auth.subject.scopes"
// first, then the deprecated "auth.scopes".
func RequireScopes(scopes ...string) Interceptor {
	return NewInterceptor("require-scopes", func(ctx Context, route RouteInfo) InterceptorResult {
		userScopes, ok := subjectScopes(ctx)
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
//
// Like RequireScopes, it reads the subject's scopes via subjectScopes:
// "auth.subject.scopes" first, then the deprecated "auth.scopes".
func RequireAnyScope(scopes ...string) Interceptor {
	return NewInterceptor("require-any-scope", func(ctx Context, route RouteInfo) InterceptorResult {
		userScopes, ok := subjectScopes(ctx)
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
//
// It reads the subject's roles from "auth.subject.roles" — the []string the
// auth extension's MiddlewareWithRequirement publishes from AuthContext.Roles
// (see extensions/auth/registry.go). That key is deliberately not
// "auth.roles": this package already uses "auth.roles" as ROUTE metadata for
// the roles a route requires (set by WithAnyRole in router_auth_opts.go, read
// by the OpenAPI generator). Reusing the same string for "roles the route
// needs" and "roles the subject has" would make the two opposite meanings
// collide under one name, even though they live in different maps and never
// actually clash at runtime.
//
// internal/router cannot import extensions/auth — that package imports forge,
// and forge's router wiring lives here, so the import would cycle. The
// contract is therefore a plain []string passed through the context rather
// than a typed value.
func RequireRole(roles ...string) Interceptor {
	return NewInterceptor("require-role", func(ctx Context, route RouteInfo) InterceptorResult {
		if subjectRoles, ok := ctx.Get("auth.subject.roles").([]string); ok {
			for _, role := range subjectRoles {
				if slices.Contains(roles, role) {
					return Allow()
				}
			}

			return Block(Forbidden("insufficient permissions"))
		}

		// Deprecated fallback: some applications set "user.role" (a scalar)
		// directly instead of going through the auth extension's middleware.
		// Keep honoring it so those callers see no behavior change. Remove
		// once nothing in the wild still sets this key.
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
//
// Like RequireRole, it reads "auth.subject.roles" first — the subject's roles
// as published by the auth extension's MiddlewareWithRequirement — and falls
// back to the legacy per-interceptor key for callers that set it themselves.
func RequireAllRoles(roles ...string) Interceptor {
	return NewInterceptor("require-all-roles", func(ctx Context, route RouteInfo) InterceptorResult {
		userRoles, ok := ctx.Get("auth.subject.roles").([]string)
		if !ok {
			// Deprecated fallback: "user.roles" was this interceptor's own
			// legacy key, set by applications that never adopted the auth
			// extension's context contract. Keep honoring it so those callers
			// see no behavior change. Remove once nothing in the wild still
			// sets this key.
			userRoles, ok = ctx.Get("user.roles").([]string)
			if !ok {
				return Block(Forbidden("no roles assigned"))
			}
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
