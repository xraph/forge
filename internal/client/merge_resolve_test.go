package client_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// writeSpec writes content to a temp file with the given name and returns its path.
func writeSpec(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

const restDoc = `
openapi: 3.1.0
info:
  title: Orders
  version: 1.0.0
paths:
  /orders:
    get:
      operationId: listOrders
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
components:
  schemas:
    Order:
      type: object
      x-forge-entity:
        idField: id
      properties:
        id:
          type: string
`

const streamDoc = `
asyncapi: 3.0.0
info:
  title: Orders Streams
  version: 1.0.0
channels:
  orders:
    address: /ws/orders
    messages:
      orderUpdated:
        payload:
          $ref: '#/components/schemas/OrderEvent'
operations:
  orderUpdated:
    action: receive
    channel:
      $ref: '#/channels/orders'
components:
  schemas:
    OrderEvent:
      type: object
      properties:
        id:
          type: string
`

func TestParseFileUnresolvedLeavesRoutingTypesUnbuilt(t *testing.T) {
	p := client.NewSpecParser()
	path := writeSpec(t, "openapi.yaml", restDoc)

	spec, err := p.ParseFileUnresolved(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFileUnresolved: %v", err)
	}
	if spec.RoutingTypes != nil {
		t.Errorf("RoutingTypes = %v, want nil before resolution", spec.RoutingTypes)
	}
	if spec.Kind != client.SourceOpenAPI {
		t.Errorf("Kind = %v, want SourceOpenAPI", spec.Kind)
	}
}

func TestParseFileStillResolves(t *testing.T) {
	p := client.NewSpecParser()
	path := writeSpec(t, "openapi.yaml", restDoc)

	spec, err := p.ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if spec.Entities["Order"] == nil {
		t.Fatalf("Entities[Order] missing after ParseFile")
	}
	if spec.Kind != client.SourceOpenAPI {
		t.Errorf("Kind = %v, want SourceOpenAPI", spec.Kind)
	}
}

func TestUnresolvedParseThenMergeCarriesNoSpuriousWarnings(t *testing.T) {
	p := client.NewSpecParser()
	restPath := writeSpec(t, "openapi.yaml", restDoc)
	streamPath := writeSpec(t, "asyncapi.yaml", streamDoc)

	rest, err := p.ParseFileUnresolved(context.Background(), restPath)
	if err != nil {
		t.Fatalf("parse rest: %v", err)
	}
	stream, err := p.ParseFileUnresolved(context.Background(), streamPath)
	if err != nil {
		t.Fatalf("parse stream: %v", err)
	}

	merged := client.MergeSpecs(rest, stream)
	client.ResolveEntityFieldsForTest(merged)

	for _, w := range merged.Warnings {
		if strings.Contains(w, "no schema describes") {
			t.Errorf("merged spec carries a spurious unresolved-entity warning: %q", w)
		}
	}
	if len(merged.Endpoints) == 0 {
		t.Errorf("merged spec lost its REST endpoints")
	}
	if len(merged.WebSockets) == 0 {
		t.Errorf("merged spec lost its stream endpoints")
	}
}
