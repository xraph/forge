package client_test

import (
	"context"
	"os"
	"path/filepath"
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

// restDoc's Order entity carries a "customer" property that $refs Customer --
// a type restDoc never defines. Customer only becomes a schema, and only
// becomes an entity, on the AsyncAPI side (see streamDoc). This is
// deliberate: resolving restDoc in isolation cannot mark Customer "useful"
// (resolveEntityFields only credits a $ref target when it is reachable to a
// KNOWN entity), so the customer edge on Order.Fields only survives
// resolution that runs after the two documents are merged. A version of this
// fixture where Customer lived in restDoc too would pass identically whether
// resolution ran per-document or once after merge, and would prove nothing
// about the split.
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
        customer:
          $ref: '#/components/schemas/Customer'
`

// streamDoc registers Customer as an entity purely through a stream binding
// (x-forge-stream), the same mechanism internal/client/spec_parser_stream_entity_test.go
// exercises directly. Customer's only schema definition, and its only route
// to spec.Entities, lives here -- restDoc never sees it.
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
    x-forge-stream:
      - message: orderUpdated
        entityType: Customer
        intent: upsert
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
    Customer:
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

// TestUnresolvedParseThenMergeCarriesNoSpuriousWarnings proves what the
// ParseFileUnresolved / MergeSpecs / resolve-once split actually buys: a
// cross-document entity edge resolves correctly, because resolution runs
// once over the union of both documents' schemas and entities rather than
// once per document.
//
// Order (a restDoc entity) has a "customer" property naming Customer, a type
// restDoc never defines and never registers as an entity -- Customer only
// exists, and only becomes an entity, on the streamDoc (AsyncAPI) side, via a
// stream binding. resolveEntityFields only keeps an edge whose target is
// reachable to a KNOWN entity (see internal/client/entity_fields.go's
// usefulTypes/keptTypes), so this edge is a genuine test of "does resolution
// see both documents' entities at once", not merely "does resolution run at
// all": resolving restDoc by itself cannot mark Customer useful, because
// restDoc's own spec.Entities has no Customer entry to reach. Only after
// MergeSpecs unions rest.Entities and stream.Entities does a single
// resolveEntityFields pass have enough information to keep Order.Fields
// carrying the customer -> Customer edge.
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

	if len(merged.Endpoints) == 0 {
		t.Errorf("merged spec lost its REST endpoints")
	}
	if len(merged.WebSockets) == 0 {
		t.Errorf("merged spec lost its stream endpoints")
	}

	order := merged.Entities["Order"]
	if order == nil {
		t.Fatalf("merged spec missing Entities[Order]")
	}
	if merged.Entities["Customer"] == nil {
		t.Fatalf("merged spec missing Entities[Customer] -- the stream-binding registration was lost in the merge")
	}
	if got := order.Fields["customer"]; got != "Customer" {
		t.Errorf(`Entities["Order"].Fields["customer"] = %q, want "Customer" -- this edge only survives`+
			" resolution that runs once over the merged whole; resolving restDoc alone cannot see Customer"+
			" as an entity at all, since restDoc never defines or registers it", got)
	}
}
