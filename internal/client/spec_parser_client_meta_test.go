package client

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeSpec marshals a minimal OpenAPI document carrying x-forge extensions and returns its
// path on disk.
func writeSpec(t *testing.T, doc map[string]any) string {
	t.Helper()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	return path
}

func orderComponent() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":    map[string]any{"type": "string"},
			"total": map[string]any{"type": "integer"},
		},
	}
}

func TestSpecParserResolvesEntityFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"paths": map[string]any{
			"/orders": map[string]any{
				"get": map[string]any{
					"operationId": "orderList",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "ok",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type":  "array",
										"items": map[string]any{"$ref": "#/components/schemas/Order"},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.Endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(spec.Endpoints))
	}

	ep := spec.Endpoints[0]

	if ep.Entity == nil || ep.Entity.Type != "Order" || ep.Entity.IDField != "id" {
		t.Fatalf("Entity = %+v, want Order/id — the file path did not resolve identity", ep.Entity)
	}

	if len(ep.CacheTags.Provides) != 2 {
		t.Fatalf("Provides = %v, want item and collection for a list response", ep.CacheTags.Provides)
	}

	if spec.Entities["Order"] == nil {
		t.Fatalf("spec.Entities missing Order: %+v", spec.Entities)
	}
}

// x-forge-id must survive the file round trip. This is the case that silently degrades: the
// endpoint still generates, it just stops being an entity.
func TestSpecParserCarriesForgeIDExtension(t *testing.T) {
	component := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"order_number": map[string]any{"type": "string", "x-forge-id": true},
		},
	}

	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": component},
		},
		"paths": map[string]any{
			"/orders/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "orderGet",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "ok",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Order"},
								},
							},
						},
					},
				},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if spec.Schemas["Order"].Properties["order_number"].Extensions["x-forge-id"] != true {
		t.Fatalf("x-forge-id did not survive convertSchema: %+v",
			spec.Schemas["Order"].Properties["order_number"].Extensions)
	}

	if spec.Endpoints[0].Entity == nil || spec.Endpoints[0].Entity.IDField != "order_number" {
		t.Fatalf("Entity = %+v, want IDField order_number", spec.Endpoints[0].Entity)
	}
}

// An explicit opt-out on the file path must beat inference, same as on the live path.
func TestSpecParserHonoursNoEntityFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"paths": map[string]any{
			"/orders/{id}/snapshot": map[string]any{
				"get": map[string]any{
					"operationId":       "orderSnapshot",
					"x-forge-no-entity": true,
					"responses": map[string]any{
						"200": map[string]any{
							"description": "ok",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Order"},
								},
							},
						},
					},
				},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if spec.Endpoints[0].Entity != nil {
		t.Fatalf("Entity = %+v, want nil — a projection must not be normalized",
			spec.Endpoints[0].Entity)
	}
}

// Cross-entity declarations arrive as []any from JSON, not []string. stringSlice must cope.
func TestSpecParserReadsInvalidatesFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		"paths": map[string]any{
			"/orders": map[string]any{
				"post": map[string]any{
					"operationId":         "orderCreate",
					"x-forge-invalidates": []any{"Inventory[]"},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Order"},
								},
							},
						},
					},
				},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	inv := spec.Endpoints[0].CacheTags.Invalidates

	var found bool

	for _, tag := range inv {
		if tag == "Inventory[]" {
			found = true
		}
	}

	if !found {
		t.Fatalf("Invalidates = %v, want it to contain Inventory[] — []any was not coerced", inv)
	}
}
