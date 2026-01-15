package router

import (
	"slices"
	"testing"
)

// TestRouter_NestedGroupInheritance tests that nested groups inherit parent group metadata.
func TestRouter_NestedGroupInheritance(t *testing.T) {
	router := NewRouter()

	// Create parent group with metadata
	parentGroup := router.Group("/parent",
		WithGroupMetadata("key1", "value1"),
		WithGroupMetadata("exclude", true),
		WithGroupTags("parent-tag"),
	)

	// Create nested group without any options
	childGroup := parentGroup.Group("/child")

	// Register routes on nested group
	err := childGroup.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	if err != nil {
		t.Fatalf("failed to register route: %v", err)
	}

	// Get registered routes
	routes := router.Routes()
	if len(routes) == 0 {
		t.Fatal("no routes registered")
	}

	// Find our test route
	var testRoute *RouteInfo

	for i := range routes {
		if routes[i].Path == "/parent/child/test" {
			testRoute = &routes[i]

			break
		}
	}

	if testRoute == nil {
		t.Fatal("test route not found")
	}

	// Verify metadata was inherited from parent group
	if testRoute.Metadata["key1"] != "value1" {
		t.Errorf("expected metadata key1=value1, got %v", testRoute.Metadata["key1"])
	}

	if testRoute.Metadata["exclude"] != true {
		t.Errorf("expected metadata exclude=true, got %v", testRoute.Metadata["exclude"])
	}

	// Verify tags were inherited
	found := slices.Contains(testRoute.Tags, "parent-tag")

	if !found {
		t.Errorf("expected tag 'parent-tag' to be inherited, got tags: %v", testRoute.Tags)
	}
}

// TestRouter_NestedGroupOverride tests that nested groups can override parent metadata.
func TestRouter_NestedGroupOverride(t *testing.T) {
	router := NewRouter()

	// Create parent group with metadata
	parentGroup := router.Group("/parent",
		WithGroupMetadata("key1", "parent-value"),
		WithGroupMetadata("key2", "parent-only"),
	)

	// Create nested group that overrides key1
	childGroup := parentGroup.Group("/child",
		WithGroupMetadata("key1", "child-value"),
		WithGroupMetadata("key3", "child-only"),
	)

	// Register route on nested group
	err := childGroup.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	if err != nil {
		t.Fatalf("failed to register route: %v", err)
	}

	// Get registered routes
	routes := router.Routes()

	var testRoute *RouteInfo

	for i := range routes {
		if routes[i].Path == "/parent/child/test" {
			testRoute = &routes[i]

			break
		}
	}

	if testRoute == nil {
		t.Fatal("test route not found")
	}

	// Verify child overrode key1
	if testRoute.Metadata["key1"] != "child-value" {
		t.Errorf("expected metadata key1=child-value (overridden), got %v", testRoute.Metadata["key1"])
	}

	// Verify parent's key2 was inherited
	if testRoute.Metadata["key2"] != "parent-only" {
		t.Errorf("expected metadata key2=parent-only (inherited), got %v", testRoute.Metadata["key2"])
	}

	// Verify child's key3 was set
	if testRoute.Metadata["key3"] != "child-only" {
		t.Errorf("expected metadata key3=child-only, got %v", testRoute.Metadata["key3"])
	}
}

// TestRouter_SchemaExcludeInheritance tests that schema exclusion is inherited by nested groups.
func TestRouter_SchemaExcludeInheritance(t *testing.T) {
	router := NewRouter()

	// Create parent group with schema exclusion
	parentGroup := router.Group("/internal",
		WithGroupMetadata("openapi.exclude", true),
		WithGroupMetadata("asyncapi.exclude", true),
		WithGroupMetadata("orpc.exclude", true),
	)

	// Create nested group without any options
	childGroup := parentGroup.Group("/admin")

	// Create deeply nested group
	grandchildGroup := childGroup.Group("/users")

	// Register routes at different nesting levels
	err := parentGroup.GET("/status", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	if err != nil {
		t.Fatalf("failed to register parent route: %v", err)
	}

	err = childGroup.GET("/config", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	if err != nil {
		t.Fatalf("failed to register child route: %v", err)
	}

	err = grandchildGroup.GET("/list", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	if err != nil {
		t.Fatalf("failed to register grandchild route: %v", err)
	}

	// Get all routes
	routes := router.Routes()

	// Verify all routes have exclusion metadata
	for i := range routes {
		route := &routes[i]

		if route.Metadata["openapi.exclude"] != true {
			t.Errorf("route %s: expected openapi.exclude=true, got %v", route.Path, route.Metadata["openapi.exclude"])
		}

		if route.Metadata["asyncapi.exclude"] != true {
			t.Errorf("route %s: expected asyncapi.exclude=true, got %v", route.Path, route.Metadata["asyncapi.exclude"])
		}

		if route.Metadata["orpc.exclude"] != true {
			t.Errorf("route %s: expected orpc.exclude=true, got %v", route.Path, route.Metadata["orpc.exclude"])
		}
	}
}
