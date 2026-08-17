package client

import (
	"testing"

	"github.com/xraph/forge/internal/router"
)

// routeInfoForTest builds the minimal router.RouteInfo literal routeToEndpoint
// needs, with the given metadata attached.
func routeInfoForTest(metadata map[string]any) router.RouteInfo {
	return router.RouteInfo{
		Method:   "GET",
		Path:     "/test",
		Metadata: metadata,
	}
}

// TestRouteToEndpointReadsAuthProviders is the regression test for a key
// mismatch: this site read route.Metadata["auth"], which nothing writes.
// WithRequiredAuth writes "auth.providers", "auth.scopes" and "auth.mode"
// (internal/router/router_auth_opts.go). The result was that an app with
// OpenAPI disabled generated a client with no auth at all, with no warning.
func TestRouteToEndpointReadsAuthProviders(t *testing.T) {
	i := &Introspector{}

	ep := i.routeToEndpoint(routeInfoForTest(map[string]any{
		"auth.providers": []string{"jwt"},
		"auth.scopes":    []string{"write:users", "admin"},
		"auth.mode":      "or",
	}))

	if len(ep.Security) != 1 {
		t.Fatalf("Security = %d requirements, want 1: %+v", len(ep.Security), ep.Security)
	}

	if ep.Security[0].SchemeName != "jwt" {
		t.Errorf("SchemeName = %q, want \"jwt\"", ep.Security[0].SchemeName)
	}

	// Sorted, so the generated capability files stay byte-stable.
	want := []string{"admin", "write:users"}
	if len(ep.Security[0].Scopes) != len(want) {
		t.Fatalf("Scopes = %v, want %v", ep.Security[0].Scopes, want)
	}

	for idx, scope := range want {
		if ep.Security[0].Scopes[idx] != scope {
			t.Errorf("Scopes[%d] = %q, want %q", idx, ep.Security[0].Scopes[idx], scope)
		}
	}
}

// TestRouteToEndpointNoAuthMetadata confirms an unguarded route stays
// unguarded rather than gaining an empty requirement.
func TestRouteToEndpointNoAuthMetadata(t *testing.T) {
	i := &Introspector{}

	ep := i.routeToEndpoint(routeInfoForTest(map[string]any{}))

	if len(ep.Security) != 0 {
		t.Errorf("Security = %+v, want empty", ep.Security)
	}
}
