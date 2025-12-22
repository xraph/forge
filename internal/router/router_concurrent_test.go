package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRouter_ConcurrentGroupAccess verifies that groups share the same mutex
// and can safely handle concurrent route registration and middleware addition.
func TestRouter_ConcurrentGroupAccess(t *testing.T) {
	router := NewRouter()

	var wg sync.WaitGroup
	routeCount := 50

	// Create multiple groups concurrently
	groups := make([]Router, 3)
	for i := 0; i < 3; i++ {
		groups[i] = router.Group("/api/v" + string(rune('1'+i)))
	}

	// Concurrently add middleware to different groups
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func(g Router, idx int) {
			defer wg.Done()
			g.Use(func(next Handler) Handler {
				return func(ctx Context) error {
					return next(ctx)
				}
			})
		}(groups[i], i)
	}
	wg.Wait()

	// Concurrently register routes to different groups with unique paths
	wg.Add(routeCount)
	for i := 0; i < routeCount; i++ {
		go func(idx int) {
			defer wg.Done()
			groupIdx := idx % 3
			g := groups[groupIdx]

			// Use unique path for each goroutine
			path := fmt.Sprintf("/test-%d", idx)
			err := g.GET(path, func(ctx Context) error {
				return ctx.String(200, "ok")
			})
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Verify all routes were registered
	routes := router.Routes()
	require.Equal(t, routeCount, len(routes), "All routes should be registered")

	// Verify routes can handle concurrent requests
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/test1", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
		}()
	}
	wg.Wait()
}

// TestRouter_ConcurrentUseAndRegister verifies thread safety when
// Use() and route registration happen concurrently.
func TestRouter_ConcurrentUseAndRegister(t *testing.T) {
	router := NewRouter()

	var wg sync.WaitGroup
	middlewareCount := 10
	routeCount := 20

	// Add middleware concurrently
	wg.Add(middlewareCount)
	for i := 0; i < middlewareCount; i++ {
		go func(idx int) {
			defer wg.Done()
			router.Use(func(next Handler) Handler {
				return func(ctx Context) error {
					return next(ctx)
				}
			})
		}(i)
	}

	// Register routes concurrently while middleware is being added with unique paths
	wg.Add(routeCount)
	for i := 0; i < routeCount; i++ {
		go func(idx int) {
			defer wg.Done()
			// Use unique path for each goroutine
			path := fmt.Sprintf("/test-%d", idx)
			err := router.GET(path, func(ctx Context) error {
				return ctx.String(200, "ok")
			})
			require.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Verify all routes were registered
	routes := router.Routes()
	require.Equal(t, routeCount, len(routes), "All routes should be registered")
}

// TestRouter_NestedGroupsConcurrent tests concurrent access to nested groups
func TestRouter_NestedGroupsConcurrent(t *testing.T) {
	router := NewRouter()

	var wg sync.WaitGroup

	// Create nested groups concurrently
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			defer wg.Done()

			// Create first level group
			g1 := router.Group("/api")
			g1.Use(func(next Handler) Handler {
				return func(ctx Context) error {
					return next(ctx)
				}
			})

			// Create nested group
			g2 := g1.Group("/v1")
			g2.Use(func(next Handler) Handler {
				return func(ctx Context) error {
					return next(ctx)
				}
			})

			// Register route with unique path
			err := g2.GET(fmt.Sprintf("/test-%d", idx), func(ctx Context) error {
				return ctx.String(200, "ok")
			})
			require.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Verify routes were registered
	routes := router.Routes()
	require.Equal(t, 5, len(routes), "Should have 5 routes registered")
}

// TestRouter_GroupMutexSharing verifies that parent and child groups
// share the same mutex by checking they can safely access shared state.
func TestRouter_GroupMutexSharing(t *testing.T) {
	router := NewRouter()
	group := router.Group("/api")

	// Both router and group should share the same mutex
	// This test verifies no data race occurs when accessing routes
	var wg sync.WaitGroup

	// Writer goroutines (register routes)
	wg.Add(10)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			defer wg.Done()
			err := router.GET(fmt.Sprintf("/root-%d", idx), func(ctx Context) error {
				return ctx.String(200, "ok")
			})
			require.NoError(t, err)
		}(i)

		go func(idx int) {
			defer wg.Done()
			err := group.GET(fmt.Sprintf("/child-%d", idx), func(ctx Context) error {
				return ctx.String(200, "ok")
			})
			require.NoError(t, err)
		}(i)
	}

	// Reader goroutines (access routes)
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			routes := router.Routes()
			_ = routes // Just access, don't need to check
		}()
	}

	wg.Wait()

	// Verify all routes were registered
	routes := router.Routes()
	require.Equal(t, 10, len(routes), "Should have 5 root + 5 child routes")
}
