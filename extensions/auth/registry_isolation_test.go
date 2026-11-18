package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"

	"github.com/xraph/forge/internal/logger"
)

func okHandler(ctx forge.Context) error { return ctx.String(http.StatusOK, "ok") }

func serve(t *testing.T, mw forge.Middleware, next forge.Handler) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	_ = mw(next)(forge_http.NewContext(w, req, nil))

	return w
}

// A provider that returns the same *AuthContext for every request (a cached
// lookup, a per-key value) used to have MiddlewareAnd merge the second
// provider's scopes into it. After one AND request the API key permanently
// held "admin" and then passed an admin scope check on its own.
func TestMiddlewareAnd_DoesNotMutateProviderContext(t *testing.T) {
	reg := NewRegistry(nil, logger.NewTestLogger())

	keyCtx := &AuthContext{Subject: "svc-key", Scopes: []string{"read"}, Claims: map[string]any{"k": 1}}
	if err := reg.Register(&mockProvider{name: "apikey", schemeType: SecurityTypeAPIKey, authCtx: keyCtx}); err != nil {
		t.Fatal(err)
	}

	if err := reg.Register(&mockProvider{name: "jwt", schemeType: SecurityTypeHTTP, authFunc: func(context.Context, *http.Request) (*AuthContext, error) {
		return &AuthContext{Subject: "alice", Scopes: []string{"admin"}, Claims: map[string]any{"role": "admin"}}, nil
	}}); err != nil {
		t.Fatal(err)
	}

	var merged *AuthContext

	w := serve(t, reg.MiddlewareAnd("apikey", "jwt"), func(ctx forge.Context) error {
		merged, _ = GetAuthContext(ctx)

		return ctx.String(http.StatusOK, "ok")
	})
	if w.Code != http.StatusOK || merged == nil || !merged.HasScope("admin") {
		t.Fatalf("AND request: status %d, merged %+v", w.Code, merged)
	}

	if !slices.Equal(keyCtx.Scopes, []string{"read"}) || len(keyCtx.Claims) != 1 || keyCtx.ProviderName != "" {
		t.Fatalf("provider's context was modified: %+v", keyCtx)
	}

	if w := serve(t, reg.MiddlewareWithScopes("apikey", "admin"), okHandler); w.Code != http.StatusForbidden {
		t.Fatalf("key alone on an admin route: status %d, want 403", w.Code)
	}
}

func TestMiddleware_DoesNotWriteProviderNameIntoProviderContext(t *testing.T) {
	reg := NewRegistry(nil, logger.NewTestLogger())

	shared := &AuthContext{Subject: "svc"}
	_ = reg.Register(&mockProvider{name: "apikey", authCtx: shared})

	var fromStd *AuthContext

	w := serve(t, reg.Middleware("apikey"), func(ctx forge.Context) error {
		got, _ := GetAuthContext(ctx)
		fromStd, _ = FromContext(ctx.Context())

		if got.ProviderName != "apikey" {
			t.Errorf("ProviderName = %q, want apikey", got.ProviderName)
		}

		return ctx.String(http.StatusOK, "ok")
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}

	if shared.ProviderName != "" {
		t.Fatalf("registry wrote ProviderName into the provider's object: %q", shared.ProviderName)
	}

	if fromStd == nil || fromStd.Subject != "svc" {
		t.Fatal("auth context missing from the request context")
	}
}

// The 403 body used to echo the authorizer's error, which lists the scopes,
// roles or permissions the route requires.
func TestMiddlewareWithRequirement_ForbiddenBodyIsGeneric(t *testing.T) {
	reg := NewRegistry(nil, logger.NewTestLogger())
	_ = reg.Register(&mockProvider{name: "jwt", authCtx: &AuthContext{Subject: "u"}})

	w := serve(t, reg.MiddlewareWithRequirement(Requirement{
		Providers:   []string{"jwt"},
		Scopes:      []string{"billing:admin"},
		Roles:       []string{"superuser"},
		Permissions: []string{"secrets:read"},
	}), okHandler)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", w.Code)
	}

	body := w.Body.String()
	for _, leak := range []string{"billing:admin", "superuser", "secrets:read", "scopes", "roles"} {
		if strings.Contains(body, leak) {
			t.Fatalf("403 body %q reveals %q", body, leak)
		}
	}
}

func TestAuthContext_Clone(t *testing.T) {
	orig := &AuthContext{
		Subject:     "s",
		Scopes:      []string{"a"},
		Roles:       []string{"r"},
		Permissions: []string{"p"},
		Claims:      map[string]any{"c": 1},
		Metadata:    map[string]any{"m": 1},
	}

	c := orig.Clone()
	c.Scopes = append(c.Scopes[:1], "x")
	c.Scopes[0] = "changed"
	c.Roles[0] = "changed"
	c.Permissions[0] = "changed"
	c.Claims["new"] = true
	c.Metadata["new"] = true

	if orig.Scopes[0] != "a" || orig.Roles[0] != "r" || orig.Permissions[0] != "p" ||
		len(orig.Claims) != 1 || len(orig.Metadata) != 1 {
		t.Fatalf("clone shares state with the original: %+v", orig)
	}

	if (*AuthContext)(nil).Clone() != nil {
		t.Fatal("nil Clone should be nil")
	}
}
