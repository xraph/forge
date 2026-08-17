package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	forge_http "github.com/xraph/go-utils/http"
)

// newInterceptorTestContext builds a real Context (not the bare-bones
// mockInterceptorContext in interceptor_test.go, which only implements
// Get/Set and is never passed to an actual Interceptor). RequireRole and
// RequireAllRoles are exercised through Interceptor.Intercept, which expects
// the full Context, so the tests need the same forge_http.NewContext
// construction the isolation tests already use.
func newInterceptorTestContext() Context {
	return forge_http.NewContext(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil),
		nil,
	)
}

// TestRequireRole covers the ANY-of role check. It reads the subject's roles
// from "auth.subject.roles" (the key MiddlewareWithRequirement now publishes,
// renamed from "auth.roles" — see Ruling 1 in task-13) and falls back to the
// legacy "user.role" scalar for applications that still set it directly.
func TestRequireRole(t *testing.T) {
	t.Run("allows when auth.subject.roles contains a listed role", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.roles", []string{"admin", "editor"})

		result := RequireRole("admin", "owner").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow, got blocked: %v", result.Error)
		}
	})

	t.Run("denies when auth.subject.roles does not contain a listed role", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.roles", []string{"viewer"})

		result := RequireRole("admin").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block, got allow")
		}
	})

	t.Run("falls back to the legacy user.role scalar when auth.subject.roles is absent", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("user.role", "admin")

		result := RequireRole("admin").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow via legacy fallback, got blocked: %v", result.Error)
		}
	})

	t.Run("legacy user.role fallback still denies a mismatched role", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("user.role", "viewer")

		result := RequireRole("admin").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block via legacy fallback, got allow")
		}
	})

	t.Run("denies when neither key is set", func(t *testing.T) {
		ctx := newInterceptorTestContext()

		result := RequireRole("admin").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block when no role information is present")
		}
	})
}

// TestRequireAllRoles covers the ALL-of role check. It reads
// "auth.subject.roles" first and falls back to the legacy "user.roles"
// []string for applications that still set it directly.
func TestRequireAllRoles(t *testing.T) {
	t.Run("allows when auth.subject.roles contains every listed role", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.roles", []string{"admin", "editor", "viewer"})

		result := RequireAllRoles("admin", "editor").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow, got blocked: %v", result.Error)
		}
	})

	t.Run("denies when auth.subject.roles is missing one listed role", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.roles", []string{"admin"})

		result := RequireAllRoles("admin", "editor").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block, got allow")
		}
	})

	t.Run("falls back to the legacy user.roles slice when auth.subject.roles is absent", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("user.roles", []string{"admin", "editor"})

		result := RequireAllRoles("admin", "editor").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow via legacy fallback, got blocked: %v", result.Error)
		}
	})

	t.Run("legacy user.roles fallback still denies a missing role", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("user.roles", []string{"admin"})

		result := RequireAllRoles("admin", "editor").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block via legacy fallback, got allow")
		}
	})

	t.Run("denies when neither key is set", func(t *testing.T) {
		ctx := newInterceptorTestContext()

		result := RequireAllRoles("admin").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block when no role information is present")
		}
	})
}
