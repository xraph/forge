package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// These mirror spec_parser_client_meta_test.go and spec_parser_stream_entity_test.go
// for YAML sources. They are not duplicates: gopkg.in/yaml.v3 never consults
// MarshalJSON/UnmarshalJSON, so passing the JSON cases proved nothing whatsoever
// about a .yaml spec. Every one of these failed before shared gained
// MarshalYAML/UnmarshalYAML — the specs parsed cleanly and every x-forge-*
// extension in them was dropped on the floor.
//
// All of them drive client.NewSpecParser().ParseFile, the real entry point, against
// a real file on disk rather than a hand-built IR fixture.

// writeYAMLSpec renders a spec document as YAML and returns its path on disk.
func writeYAMLSpec(t *testing.T, name string, doc map[string]any) string {
	t.Helper()

	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	return path
}

// The headline case: a hand-written YAML document, exactly as a user would
// commit one, carrying x-forge-entity, x-forge-id and x-forge-invalidates. All
// three must reach the IR.
func TestSpecParserReadsForgeExtensionsFromHandWrittenYAML(t *testing.T) {
	const spec = `openapi: 3.0.0
info:
  title: Orders
  version: 1.0.0
components:
  schemas:
    Order:
      type: object
      properties:
        order_number:
          type: string
          x-forge-id: true
        total:
          type: integer
paths:
  /orders:
    post:
      operationId: orderCreate
      x-forge-invalidates:
        - Inventory[]
      responses:
        '201':
          description: created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
  /orders/{id}/snapshot:
    get:
      operationId: orderSnapshot
      x-forge-entity:
        type: Order
        idField: order_number
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
`

	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	parsed, err := NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Schema-level extension.
	order := parsed.Schemas["Order"]
	if order == nil {
		t.Fatalf("Schemas missing Order: %+v", parsed.Schemas)
	}

	if v, _ := order.Properties["order_number"].Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id did not survive the YAML path: %+v",
			order.Properties["order_number"].Extensions)
	}

	byPath := make(map[string]*Endpoint, len(parsed.Endpoints))
	for i := range parsed.Endpoints {
		byPath[parsed.Endpoints[i].Path] = &parsed.Endpoints[i]
	}

	// Operation-level extension: x-forge-invalidates.
	create := byPath["/orders"]
	if create == nil {
		t.Fatalf("endpoint /orders missing; got %+v", byPath)
	}

	var found bool

	for _, tag := range create.CacheTags.Invalidates {
		if tag == "Inventory[]" {
			found = true
		}
	}

	if !found {
		t.Fatalf("Invalidates = %v, want it to contain Inventory[] — x-forge-invalidates was dropped by YAML",
			create.CacheTags.Invalidates)
	}

	// Operation-level extension: x-forge-entity, an explicit declaration.
	snapshot := byPath["/orders/{id}/snapshot"]
	if snapshot == nil {
		t.Fatalf("endpoint /orders/{id}/snapshot missing; got %+v", byPath)
	}

	if snapshot.Entity == nil || snapshot.Entity.Type != "Order" || snapshot.Entity.IDField != "order_number" {
		t.Fatalf("Entity = %+v, want Order/order_number — x-forge-entity was dropped by YAML", snapshot.Entity)
	}

	// And the entity registry the browser runtime indexes by.
	if parsed.Entities["Order"] == nil {
		t.Fatalf("spec.Entities missing Order: %+v", parsed.Entities)
	}

	if len(parsed.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none — a YAML spec now carries its extensions", parsed.Warnings)
	}
}

func TestSpecParserResolvesEntityFromYAMLFile(t *testing.T) {
	path := writeYAMLSpec(t, "openapi.yaml", map[string]any{
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
		t.Fatalf("Entity = %+v, want Order/id — the YAML file path did not resolve identity", ep.Entity)
	}

	if len(ep.CacheTags.Provides) != 2 {
		t.Fatalf("Provides = %v, want item and collection for a list response", ep.CacheTags.Provides)
	}

	if spec.Entities["Order"] == nil {
		t.Fatalf("spec.Entities missing Order: %+v", spec.Entities)
	}
}

// An explicit opt-out must beat inference on the YAML path too.
func TestSpecParserHonoursNoEntityFromYAMLFile(t *testing.T) {
	path := writeYAMLSpec(t, "openapi.yaml", map[string]any{
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
		t.Fatalf("Entity = %+v, want nil — x-forge-no-entity was dropped by YAML",
			spec.Endpoints[0].Entity)
	}
}

// x-forge-stream on a WebSocket channel must survive the YAML file round trip
// into WebSocketEndpoint.StreamBindings.
func TestSpecParserWebSocketStreamBindingsFromYAMLFile(t *testing.T) {
	path := writeYAMLSpec(t, "asyncapi.yaml", map[string]any{
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
		t.Fatalf("StreamBindings = %+v, want 1 entry — x-forge-stream was dropped by YAML", bindings)
	}

	b := bindings[0]
	if b.Message != "orderUpdated" || b.EntityType != "Order" || string(b.Intent) != "update" {
		t.Fatalf("StreamBindings[0] = %+v, want message/entityType/intent orderUpdated/Order/update", b)
	}

	if len(b.Invalidates) != 1 || b.Invalidates[0] != "Order[]" {
		t.Fatalf("StreamBindings[0].Invalidates = %v, want [Order[]]", b.Invalidates)
	}
}

// x-forge-stream on an SSE channel must survive the YAML file round trip into
// SSEEndpoint.StreamBindings, and register the entity it names.
func TestSpecParserSSEStreamBindingsFromYAMLFile(t *testing.T) {
	path := writeYAMLSpec(t, "asyncapi.yaml", map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Orders Stream", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponent()},
		},
		// No messages and no server reference, so detectWebSocketChannel routes
		// this through the SSE branch of parseAsyncAPI rather than the WebSocket
		// one — the same shape the JSON SSE test uses.
		"channels": map[string]any{
			"orders": map[string]any{
				"address": "/orders",
				"x-forge-stream": []any{
					map[string]any{
						"message":    "orderUpdated",
						"entityType": "Order",
						"intent":     "upsert",
					},
				},
			},
		},
		"operations": map[string]any{
			"receiveOrderUpdate": map[string]any{
				"action":  "receive",
				"channel": map[string]any{"$ref": "#/channels/orders"},
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
		t.Fatalf("StreamBindings = %+v, want 1 entry — x-forge-stream was dropped by YAML", bindings)
	}

	entity := spec.Entities["Order"]
	if entity == nil {
		t.Fatalf("spec.Entities missing Order: %+v — the stream binding is the only reference to it",
			spec.Entities)
	}

	if entity.IDField != "id" {
		t.Fatalf("Entities[\"Order\"].IDField = %q, want \"id\"", entity.IDField)
	}
}

// A .yml spec must behave exactly like a .yaml one: ParseFile accepts both, and
// the extension path must not depend on which suffix was used.
func TestSpecParserReadsExtensionsFromYMLSuffix(t *testing.T) {
	path := writeYAMLSpec(t, "openapi.yml", map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{
				"Order": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"order_number": map[string]any{"type": "string", "x-forge-id": true},
					},
				},
			},
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

	if spec.Endpoints[0].Entity == nil || spec.Endpoints[0].Entity.IDField != "order_number" {
		t.Fatalf("Entity = %+v, want IDField order_number", spec.Endpoints[0].Entity)
	}
}
