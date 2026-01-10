package router

import "maps"

import "slices"

// And combines interceptors sequentially - ALL must pass.
// Executes in order, short-circuits on first block.
// Values from all interceptors are merged into context as they complete.
//
// Example:
//
//	router.POST("/admin/users", handler,
//	    forge.WithInterceptor(
//	        And(RequireAuth(), RequireRole("admin"), RequireScopes("write:users")),
//	    ),
//	)
func And(interceptors ...Interceptor) Interceptor {
	return NewInterceptor("and", func(ctx Context, route RouteInfo) InterceptorResult {
		mergedValues := make(map[string]any)

		for _, i := range interceptors {
			result := i.Intercept(ctx, route)

			// Merge values into context immediately (for subsequent interceptors)
			for k, v := range result.Values {
				ctx.Set(k, v)
				mergedValues[k] = v
			}

			if result.Blocked {
				return result
			}
		}

		if len(mergedValues) > 0 {
			return AllowWithValues(mergedValues)
		}

		return Allow()
	})
}

// Or combines interceptors sequentially - ANY one passing is sufficient.
// Executes in order, short-circuits on first success.
//
// Example:
//
//	router.GET("/api/data", handler,
//	    forge.WithInterceptor(
//	        Or(RequireAuth(), RequireAPIKey()),
//	    ),
//	)
func Or(interceptors ...Interceptor) Interceptor {
	return NewInterceptor("or", func(ctx Context, route RouteInfo) InterceptorResult {
		var lastError error

		mergedValues := make(map[string]any)

		for _, i := range interceptors {
			result := i.Intercept(ctx, route)

			// Collect values regardless of outcome
			maps.Copy(mergedValues, result.Values)

			if !result.Blocked {
				// Apply enrichment values to context
				for k, v := range result.Values {
					ctx.Set(k, v)
				}

				if len(mergedValues) > 0 {
					return AllowWithValues(mergedValues)
				}

				return Allow()
			}

			lastError = result.Error
		}

		if len(mergedValues) > 0 {
			return BlockWithValues(lastError, mergedValues)
		}

		return Block(lastError)
	})
}

// Not inverts an interceptor's decision.
// If the interceptor blocks, Not allows. If it allows, Not blocks with the provided error.
//
// Example:
//
//	// Block if user IS an admin (inverse check)
//	var BlockAdmins = Not(RequireRole("admin"), Forbidden("admins cannot access this"))
func Not(interceptor Interceptor, blockError error) Interceptor {
	name := "not"
	if interceptor.Name() != "" {
		name = "not:" + interceptor.Name()
	}

	return NewInterceptor(name, func(ctx Context, route RouteInfo) InterceptorResult {
		result := interceptor.Intercept(ctx, route)

		if result.Blocked {
			// If blocked, allow (but don't carry over error values)
			return Allow()
		}

		return Block(blockError)
	})
}

// When conditionally executes an interceptor based on a predicate.
// If the predicate returns false, the interceptor is skipped (allows).
//
// Example:
//
//	// Only require bulk scope for bulk operations
//	router.POST("/api/resource", handler,
//	    forge.WithInterceptor(
//	        When(func(ctx Context, route RouteInfo) bool {
//	            return ctx.Header("X-Bulk-Operation") == "true"
//	        }, RequireScopes("bulk:write")),
//	    ),
//	)
func When(predicate func(ctx Context, route RouteInfo) bool, interceptor Interceptor) Interceptor {
	name := "when"
	if interceptor.Name() != "" {
		name = "when:" + interceptor.Name()
	}

	return NewInterceptor(name, func(ctx Context, route RouteInfo) InterceptorResult {
		if predicate(ctx, route) {
			return interceptor.Intercept(ctx, route)
		}

		return Allow()
	})
}

// Unless conditionally skips an interceptor based on a predicate.
// If the predicate returns true, the interceptor is skipped (allows).
// This is the inverse of When.
//
// Example:
//
//	// Skip auth for requests with valid API key header
//	router.GET("/api/data", handler,
//	    forge.WithInterceptor(
//	        Unless(func(ctx Context, route RouteInfo) bool {
//	            return ctx.Header("X-API-Key") != ""
//	        }, RequireAuth()),
//	    ),
//	)
func Unless(predicate func(ctx Context, route RouteInfo) bool, interceptor Interceptor) Interceptor {
	name := "unless"
	if interceptor.Name() != "" {
		name = "unless:" + interceptor.Name()
	}

	return NewInterceptor(name, func(ctx Context, route RouteInfo) InterceptorResult {
		if predicate(ctx, route) {
			return Allow()
		}

		return interceptor.Intercept(ctx, route)
	})
}

// IfMetadata conditionally executes an interceptor based on route metadata.
// If the metadata key doesn't exist or doesn't match the expected value, skips the interceptor.
//
// Example:
//
//	// Only run rate limiting on routes marked for it
//	router.GET("/api/data", handler,
//	    forge.WithMetadata("rate-limit", true),
//	    forge.WithInterceptor(
//	        IfMetadata("rate-limit", true, RateLimitInterceptor),
//	    ),
//	)
func IfMetadata(key string, expectedValue any, interceptor Interceptor) Interceptor {
	name := "if-metadata:" + key
	if interceptor.Name() != "" {
		name = "if-metadata:" + key + ":" + interceptor.Name()
	}

	return NewInterceptor(name, func(ctx Context, route RouteInfo) InterceptorResult {
		if route.Metadata == nil {
			return Allow()
		}

		value, exists := route.Metadata[key]
		if !exists || value != expectedValue {
			return Allow()
		}

		return interceptor.Intercept(ctx, route)
	})
}

// IfTag conditionally executes an interceptor based on route tags.
// If the route doesn't have the specified tag, skips the interceptor.
//
// Example:
//
//	// Only audit routes tagged as "sensitive"
//	router.POST("/api/payment", handler,
//	    forge.WithTags("sensitive"),
//	    forge.WithInterceptor(
//	        IfTag("sensitive", AuditLogInterceptor),
//	    ),
//	)
func IfTag(tag string, interceptor Interceptor) Interceptor {
	name := "if-tag:" + tag
	if interceptor.Name() != "" {
		name = "if-tag:" + tag + ":" + interceptor.Name()
	}

	return NewInterceptor(name, func(ctx Context, route RouteInfo) InterceptorResult {
		if slices.Contains(route.Tags, tag) {
			return interceptor.Intercept(ctx, route)
		}

		return Allow()
	})
}

// ChainInterceptors combines multiple interceptors into a single interceptor.
// All interceptors are executed in order; all must pass.
// This is equivalent to And() but more explicit about sequential execution.
func ChainInterceptors(interceptors ...Interceptor) Interceptor {
	return And(interceptors...)
}
