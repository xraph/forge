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
// It also carries x-forge-stale-time on a sibling GET, since a number decoded from JSON
// arrives as float64 the same way an []any does, and numericExtension must cope with that
// the same way stringSlice copes with []any.
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
			"/orders/{id}": map[string]any{
				"get": map[string]any{
					"operationId":        "orderGet",
					"x-forge-stale-time": 30000,
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

	if spec.Endpoints[1].StaleTime != 30000 {
		t.Fatalf("StaleTime = %d, want 30000 — x-forge-stale-time was dropped by the JSON file path",
			spec.Endpoints[1].StaleTime)
	}
}

// x-forge-stream on a WebSocket channel must survive the file round trip into
// WebSocketEndpoint.StreamBindings. This exercises wiring site 3
// (convertWebSocketChannel), separately from site 4 below: the channel here
// references a server whose protocol is "wss", which is what routes it through
// the WebSocket branch of parseAsyncAPI rather than the SSE branch.
func TestSpecParserWebSocketStreamBindingsFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Orders Stream", "version": "1.0.0"},
		"servers": map[string]any{
			"main": map[string]any{
				"host":     "ws.example.com",
				"protocol": "wss",
			},
		},
		"channels": map[string]any{
			"orders": map[string]any{
				"address": "/orders",
				"servers": []any{
					map[string]any{"$ref": "#/servers/main"},
				},
				"messages": map[string]any{
					"orderUpdated": map[string]any{
						"payload": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id": map[string]any{"type": "string"},
							},
						},
					},
				},
				"x-forge-stream": []any{
					map[string]any{
						"message":     "orderUpdated",
						"entityType":  "Order",
						"intent":      "update",
						"invalidates": []any{"Order[]"},
					},
				},
			},
		},
		"operations": map[string]any{
			"sendOrderUpdate": map[string]any{
				"action":  "send",
				"channel": map[string]any{"$ref": "#/channels/orders"},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.WebSockets) != 1 {
		t.Fatalf("WebSockets = %d, want 1", len(spec.WebSockets))
	}

	bindings := spec.WebSockets[0].StreamBindings
	if len(bindings) != 1 {
		t.Fatalf("StreamBindings = %+v, want 1 entry — convertWebSocketChannel did not copy x-forge-stream", bindings)
	}

	b := bindings[0]
	if b.Message != "orderUpdated" || b.EntityType != "Order" || string(b.Intent) != "update" {
		t.Fatalf("StreamBindings[0] = %+v, want message/entityType/intent orderUpdated/Order/update", b)
	}

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "Order[]" {
		t.Fatalf("StreamBindings[0].Invalidates = %v, want [Order[]]", b.Invalidates)
	}
}

// x-forge-stream on an SSE channel must survive the file round trip into
// SSEEndpoint.StreamBindings. This exercises wiring site 4 (convertSSEChannel)
// separately from site 3 above: this channel declares no messages and no
// server reference, so detectWebSocketChannel routes it through the SSE
// branch of parseAsyncAPI instead of the WebSocket one.
func TestSpecParserSSEStreamBindingsFromFile(t *testing.T) {
	path := writeSpec(t, map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Notifications Stream", "version": "1.0.0"},
		"channels": map[string]any{
			"notifications": map[string]any{
				"address": "/notifications",
				"x-forge-stream": []any{
					map[string]any{
						"message":     "userJoined",
						"entityType":  "User",
						"intent":      "create",
						"invalidates": []any{"User[]"},
					},
				},
			},
		},
		"operations": map[string]any{
			"receiveNotifications": map[string]any{
				"action":  "receive",
				"channel": map[string]any{"$ref": "#/channels/notifications"},
			},
		},
	})

	spec, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.SSEs) != 1 {
		t.Fatalf("SSEs = %d, want 1", len(spec.SSEs))
	}

	bindings := spec.SSEs[0].StreamBindings
	if len(bindings) != 1 {
		t.Fatalf("StreamBindings = %+v, want 1 entry — convertSSEChannel did not copy x-forge-stream", bindings)
	}

	b := bindings[0]
	if b.Message != "userJoined" || b.EntityType != "User" || string(b.Intent) != "create" {
		t.Fatalf("StreamBindings[0] = %+v, want message/entityType/intent userJoined/User/create", b)
	}

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "User[]" {
		t.Fatalf("StreamBindings[0].Invalidates = %v, want [User[]]", b.Invalidates)
	}
}
