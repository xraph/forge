package discovery

import (
	"testing"
	"time"

	"github.com/xraph/forge"
)

func TestForgeRoutesToRouteDescriptors(t *testing.T) {
	routes := []forge.RouteInfo{
		{
			Name:        "getUsers",
			Method:      "GET",
			Path:        "/api/users",
			Summary:     "List users",
			Description: "Returns all users",
			Tags:        []string{"users"},
			Metadata:    map[string]any{"public": true},
		},
		{
			Name:        "getUser",
			OperationID: "getUserById",
			Method:      "GET",
			Path:        "/api/users/{id}",
			Summary:     "Get user by ID",
			Tags:        []string{"users"},
			Deprecated:  true,
			Timeout:     5 * time.Second,
		},
		{
			Name:   "wsConnect",
			Method: "GET",
			Path:   "/ws",
			Tags:   []string{"websocket"},
		},
		{
			Name:   "graphqlEndpoint",
			Method: "POST",
			Path:   "/graphql",
			Metadata: map[string]any{
				"protocol": "graphql",
			},
		},
		// Internal route — should be filtered out
		{
			Name:   "farpManifest",
			Method: "GET",
			Path:   "/_farp/manifest",
		},
	}

	descriptors := forgeRoutesToRouteDescriptors(routes, farp.InternalPathRules())

	if len(descriptors) != 4 {
		t.Fatalf("expected 4 descriptors (internal route filtered), got %d", len(descriptors))
	}

	// Check first route: basic REST
	d := descriptors[0]
	if d.Path != "/api/users" {
		t.Errorf("path = %q, want /api/users", d.Path)
	}
	if len(d.Methods) != 1 || d.Methods[0] != "GET" {
		t.Errorf("methods = %v, want [GET]", d.Methods)
	}
	if d.Protocol != "rest" {
		t.Errorf("protocol = %q, want rest", d.Protocol)
	}
	if d.OperationID != "getUsers" {
		t.Errorf("operationID = %q, want getUsers (fallback from Name)", d.OperationID)
	}
	if !d.Public {
		t.Error("expected public=true from metadata")
	}
	if d.Metadata["summary"] != "List users" {
		t.Errorf("metadata[summary] = %v, want List users", d.Metadata["summary"])
	}

	// Check second route: explicit OperationID, deprecated, timeout
	d = descriptors[1]
	if d.OperationID != "getUserById" {
		t.Errorf("operationID = %q, want getUserById", d.OperationID)
	}
	if !d.Deprecated {
		t.Error("expected deprecated=true")
	}
	if d.Timeout != "5s" {
		t.Errorf("timeout = %q, want 5s", d.Timeout)
	}

	// Check third route: protocol from tag
	d = descriptors[2]
	if d.Protocol != "websocket" {
		t.Errorf("protocol = %q, want websocket", d.Protocol)
	}

	// Check fourth route: protocol from metadata
	d = descriptors[3]
	if d.Protocol != "graphql" {
		t.Errorf("protocol = %q, want graphql", d.Protocol)
	}
}

func TestForgeRoutesToRouteDescriptors_Empty(t *testing.T) {
	if descriptors := forgeRoutesToRouteDescriptors(nil, nil); descriptors != nil {
		t.Errorf("expected nil for nil input, got %v", descriptors)
	}
	if descriptors := forgeRoutesToRouteDescriptors([]forge.RouteInfo{}, nil); descriptors != nil {
		t.Errorf("expected nil for empty input, got %v", descriptors)
	}
}

func TestForgeRoutesToRouteDescriptors_AllInternal(t *testing.T) {
	routes := []forge.RouteInfo{
		{Method: "GET", Path: "/_farp/manifest"},
		{Method: "GET", Path: "/_farp/health"},
		{Method: "GET", Path: "/_farp/schemas/openapi"},
	}
	if descriptors := forgeRoutesToRouteDescriptors(routes, farp.InternalPathRules()); descriptors != nil {
		t.Errorf("expected nil when all routes are internal, got %v", descriptors)
	}
}
