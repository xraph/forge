package typescript

import (
	"strings"
	"testing"
)

// entityFieldsFixture is the feature, written as a document.
//
// An `Order` embeds a `Customer` and a list of `LineItem`s, carries a status
// whose type is named but is not an entity, and points at a sibling `Order`.
// Every one of those is a decision the field-map resolver has to make, and
// this file drives all of them through the real entry point -- a spec on disk,
// SpecParser.ParseFile, Generator.Generate -- rather than through a hand-built
// APISpec, because a hand-built intermediate representation is exactly how a
// generator gets tested against a shape production never produces.
const entityFieldsFixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/orders/{id}": {
      "get": {
        "operationId": "orders.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Order" } } } } }
      }
    },
    "/customers/{id}": {
      "get": {
        "operationId": "customers.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Customer" } } } } }
      }
    },
    "/line-items": {
      "get": {
        "operationId": "lineItems.list",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "type": "array", "items": { "$ref": "#/components/schemas/LineItem" } } } } } }
      }
    }
  },
  "components": {
    "schemas": {
      "Order": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "customer": { "$ref": "#/components/schemas/Customer" },
          "items": { "type": "array", "items": { "$ref": "#/components/schemas/LineItem" } },
          "status": { "$ref": "#/components/schemas/OrderStatus" },
          "parent": { "$ref": "#/components/schemas/Order" },
          "audit": { "type": "object", "properties": { "by": { "type": "string" } } }
        }
      },
      "Customer": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "orders": { "type": "array", "items": { "$ref": "#/components/schemas/Order" } }
        }
      },
      "LineItem": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "qty": { "type": "integer" }
        }
      },
      "OrderStatus": { "type": "string", "enum": ["open", "closed"] }
    }
  }
}`

// The whole feature, end to end: a real file on disk produces an entity table
// whose `Order` names the type of what its `customer` property contains.
//
// The paired assertion lives in packages/client-core/__tests__/generated-fields.test.ts,
// which feeds this exact table to `normalize` and checks that `Customer:c-3`
// reaches the store. Either half alone proves nothing: Go can emit a map the
// runtime does not read, and the runtime can read a map Go does not emit.
func TestGenerateFromSpecFileEmitsEntityFieldMap(t *testing.T) {
	files := generateFromSpecFile(t, writeSpecFile(t, "openapi.json", entityFieldsFixture))

	if _, ok := files["src/ops.ts"]; !ok {
		t.Fatal("src/ops.ts was not generated")
	}

	ops := ClientManifestText(files)

	want := `'Order': { idField: 'id', fields: { 'customer': 'Customer', 'items': 'LineItem', 'parent': 'Order' } }`
	if !strings.Contains(ops, want) {
		t.Fatalf("ops.ts is missing %q\n\n%s", want, ops)
	}

	// The back edge, from the type registered by a different operation.
	if !strings.Contains(ops, `'Customer': { idField: 'id', fields: { 'orders': 'Order' } }`) {
		t.Fatalf("ops.ts is missing the Customer -> Order edge\n\n%s", ops)
	}

	// A leaf entity carries no `fields` key at all.
	if !strings.Contains(ops, `'LineItem': { idField: 'id' },`) {
		t.Fatalf("ops.ts did not omit an empty field map for LineItem\n\n%s", ops)
	}

	// `status` names OrderStatus, which is not an entity, and `audit` is
	// anonymous. Neither is an edge the runtime could follow.
	for _, unwanted := range []string{"OrderStatus", "audit:"} {
		if strings.Contains(ops, unwanted) {
			t.Fatalf("ops.ts recorded an unusable edge %q\n\n%s", unwanted, ops)
		}
	}
}

// Regenerating the same file must produce the same bytes. CI byte-diffs
// generated output, and EntityRef.Fields is a Go map.
func TestGenerateFromSpecFileEntityFieldMapIsDeterministic(t *testing.T) {
	path := writeSpecFile(t, "openapi.json", entityFieldsFixture)

	first := ClientManifestText(generateFromSpecFile(t, path))

	for i := 0; i < 20; i++ {
		if got := ClientManifestText(generateFromSpecFile(t, path)); got != first {
			t.Fatalf("ops.ts differs between runs\n\nfirst:\n%s\n\nlater:\n%s", first, got)
		}
	}
}
