package client

import (
	"reflect"
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

// TestRouteToEndpointReadsAuthorization is the regression test for F2: this
// fallback path (used whenever router.OpenAPISpec() returns nil) built
// Security from "auth.providers"/"auth.scopes" but never populated
// endpoint.Authorization at all, so CollectRoles and CollectPermissions came
// back empty and the generated client silently lost its Role and Permission
// unions whenever OpenAPI was disabled.
//
// WithAnyRole and WithAllPermissions (internal/router/router_auth_opts.go)
// write "auth.roles" and "auth.permissions". This asserts routeToEndpoint
// reads them and produces the exact same *Authorization that
// resolveEndpointAuthz produces for the equivalent x-forge-authz extension --
// the two IR builders must not disagree about what a route declared.
func TestRouteToEndpointReadsAuthorization(t *testing.T) {
	i := &Introspector{}

	ep := i.routeToEndpoint(routeInfoForTest(map[string]any{
		"auth.roles":       []string{"editor", "admin"},
		"auth.permissions": []string{"users:write"},
	}))

	want := resolveEndpointAuthz(map[string]any{
		"x-forge-authz": map[string]any{
			"roles":       []any{"editor", "admin"},
			"permissions": []any{"users:write"},
		},
	})

	if !reflect.DeepEqual(ep.Authorization, want) {
		t.Errorf("Authorization = %+v, want %+v", ep.Authorization, want)
	}
}

// TestRouteToEndpointAuthorizationAbsentWhenNothingDeclared mirrors
// resolveEndpointAuthz's own contract: nil rather than an empty
// &Authorization{} when nothing was declared, so an unguarded endpoint never
// looks guarded on this path either.
func TestRouteToEndpointAuthorizationAbsentWhenNothingDeclared(t *testing.T) {
	i := &Introspector{}

	ep := i.routeToEndpoint(routeInfoForTest(map[string]any{}))

	if ep.Authorization != nil {
		t.Errorf("Authorization = %+v, want nil", ep.Authorization)
	}
}
