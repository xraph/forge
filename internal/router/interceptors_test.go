package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	forge_http "github.com/xraph/go-utils/http"
)

// newInterceptorTestContext builds a real Context (not the bare-bones
// mockInterceptorContext in interceptor_test.go, which only implements
// Get/Set and is never passed to an actual Interceptor). The authorization
// interceptors are exercised through Interceptor.Intercept, which expects the
// full Context, so the tests need the same forge_http.NewContext construction
// the isolation tests already use.
func newInterceptorTestContext() Context {
	return forge_http.NewContext(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil),
		nil,
	)
}

// TestRequireScopes covers the ALL-of scope check. It reads the subject's
// scopes from "auth.subject.scopes" (the key MiddlewareWithRequirement
// publishes, renamed off "auth.scopes" because that string is route metadata
// for the scopes a route REQUIRES) and falls back to the deprecated
// "auth.scopes" for applications that publish it from their own middleware.
func TestRequireScopes(t *testing.T) {
	t.Run("allows when auth.subject.scopes contains every listed scope", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.scopes", []string{"read:users", "write:users", "admin"})

		result := RequireScopes("read:users", "write:users").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow, got blocked: %v", result.Error)
		}
	})

	t.Run("denies when auth.subject.scopes is missing one listed scope", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.scopes", []string{"read:users"})

		result := RequireScopes("read:users", "write:users").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block, got allow")
		}
	})

	t.Run("falls back to the legacy auth.scopes slice when auth.subject.scopes is absent", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.scopes", []string{"read:users", "write:users"})

		result := RequireScopes("read:users", "write:users").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow via legacy fallback, got blocked: %v", result.Error)
		}
	})

	t.Run("legacy auth.scopes fallback still denies a missing scope", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.scopes", []string{"read:users"})

		result := RequireScopes("read:users", "write:users").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block via legacy fallback, got allow")
		}
	})

	t.Run("auth.subject.scopes wins over a stale legacy auth.scopes", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.scopes", []string{"read:users"})
		ctx.Set("auth.scopes", []string{"read:users", "write:users"})

		result := RequireScopes("write:users").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected the new key to take precedence over the legacy one")
		}
	})

	t.Run("denies when neither key is set", func(t *testing.T) {
		ctx := newInterceptorTestContext()

		result := RequireScopes("read:users").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block when no scope information is present")
		}
	})
}

// TestRequireAnyScope covers the ANY-of scope check, over the same two keys
// RequireScopes reads.
func TestRequireAnyScope(t *testing.T) {
	t.Run("allows when auth.subject.scopes contains a listed scope", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.scopes", []string{"read:users"})

		result := RequireAnyScope("read:users", "admin").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow, got blocked: %v", result.Error)
		}
	})

	t.Run("denies when auth.subject.scopes contains no listed scope", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.scopes", []string{"read:users"})

		result := RequireAnyScope("write:users", "admin").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block, got allow")
		}
	})

	t.Run("falls back to the legacy auth.scopes slice when auth.subject.scopes is absent", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.scopes", []string{"admin"})

		result := RequireAnyScope("write:users", "admin").Intercept(ctx, RouteInfo{})

		if result.Blocked {
			t.Fatalf("expected allow via legacy fallback, got blocked: %v", result.Error)
		}
	})

	t.Run("legacy auth.scopes fallback still denies an unlisted scope", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.scopes", []string{"read:users"})

		result := RequireAnyScope("write:users", "admin").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block via legacy fallback, got allow")
		}
	})

	t.Run("auth.subject.scopes wins over a stale legacy auth.scopes", func(t *testing.T) {
		ctx := newInterceptorTestContext()
		ctx.Set("auth.subject.scopes", []string{"read:users"})
		ctx.Set("auth.scopes", []string{"admin"})

		result := RequireAnyScope("admin").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected the new key to take precedence over the legacy one")
		}
	})

	t.Run("denies when neither key is set", func(t *testing.T) {
		ctx := newInterceptorTestContext()

		result := RequireAnyScope("read:users").Intercept(ctx, RouteInfo{})

		if !result.Blocked {
			t.Fatal("expected block when no scope information is present")
		}
	})
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
