package router

import (
	"context"
	"maps"
	"sync"
)

// parallelResult holds the result of a single interceptor execution.
type parallelResult struct {
	name   string
	result InterceptorResult
}

// Parallel executes interceptors concurrently and collects results.
// ALL interceptors must pass for the request to proceed.
// If any interceptor blocks, the first blocking error is returned.
// Values from all passing interceptors are merged.
//
// Example:
//
//	// Load user data concurrently
//	router.GET("/dashboard", handler,
//	    forge.WithInterceptor(
//	        Parallel(
//	            EnrichUser(loadUser),
//	            Enrich("prefs", loadPreferences),
//	            Enrich("notifications", loadNotifications),
//	        ),
//	    ),
//	)
func Parallel(interceptors ...Interceptor) Interceptor {
	return NewInterceptor("parallel", func(ctx Context, route RouteInfo) InterceptorResult {
		if len(interceptors) == 0 {
			return Allow()
		}

		results := make(chan parallelResult, len(interceptors))

		var wg sync.WaitGroup

		// Create a cancellable context for early termination
		execCtx, cancel := context.WithCancel(ctx.Context())
		defer cancel()

		for _, interceptor := range interceptors {
			wg.Add(1)

			go func(i Interceptor) {
				defer wg.Done()

				// Check if already cancelled
				select {
				case <-execCtx.Done():
					return
				default:
				}

				result := i.Intercept(ctx, route)
				results <- parallelResult{name: i.Name(), result: result}

				// If blocked, cancel other interceptors
				if result.Blocked {
					cancel()
				}
			}(interceptor)
		}

		// Close results channel when all done
		go func() {
			wg.Wait()
			close(results)
		}()

		// Collect results
		mergedValues := make(map[string]any)

		var firstBlockError error

		for pr := range results {
			if pr.result.Blocked {
				if firstBlockError == nil {
					firstBlockError = pr.result.Error
				}

				// Still collect values for debugging
				maps.Copy(mergedValues, pr.result.Values)
			} else {
				// Merge values from successful interceptors
				maps.Copy(mergedValues, pr.result.Values)
			}
		}

		if firstBlockError != nil {
			return BlockWithValues(firstBlockError, mergedValues)
		}

		if len(mergedValues) > 0 {
			return AllowWithValues(mergedValues)
		}

		return Allow()
	})
}

// ParallelAny executes interceptors concurrently.
// ANY one passing is sufficient to proceed.
// Returns as soon as one interceptor allows.
// This is useful for "any of these auth methods" scenarios.
//
// Example:
//
//	// Any authentication method works
//	router.GET("/api/data", handler,
//	    forge.WithInterceptor(
//	        ParallelAny(
//	            RequireAuthProvider("jwt"),
//	            RequireAuthProvider("api-key"),
//	            RequireAuthProvider("oauth2"),
//	        ),
//	    ),
//	)
func ParallelAny(interceptors ...Interceptor) Interceptor {
	return NewInterceptor("parallel-any", func(ctx Context, route RouteInfo) InterceptorResult {
		if len(interceptors) == 0 {
			return Allow()
		}

		type outcome struct {
			allowed bool
			result  parallelResult
		}

		results := make(chan outcome, len(interceptors))

		execCtx, cancel := context.WithCancel(ctx.Context())
		defer cancel()

		var wg sync.WaitGroup

		for _, interceptor := range interceptors {
			wg.Add(1)

			go func(i Interceptor) {
				defer wg.Done()

				select {
				case <-execCtx.Done():
					return
				default:
				}

				result := i.Intercept(ctx, route)
				results <- outcome{
					allowed: !result.Blocked,
					result:  parallelResult{name: i.Name(), result: result},
				}

				// If allowed, cancel others
				if !result.Blocked {
					cancel()
				}
			}(interceptor)
		}

		// Close results channel when all done
		go func() {
			wg.Wait()
			close(results)
		}()

		// Wait for first success or all failures
		var lastError error

		mergedValues := make(map[string]any)

		for o := range results {
			// Collect values regardless of outcome
			maps.Copy(mergedValues, o.result.result.Values)

			if o.allowed {
				// First success wins
				if len(mergedValues) > 0 {
					return AllowWithValues(mergedValues)
				}

				return Allow()
			}

			lastError = o.result.result.Error
		}

		// All failed
		if len(mergedValues) > 0 {
			return BlockWithValues(lastError, mergedValues)
		}

		return Block(lastError)
	})
}
