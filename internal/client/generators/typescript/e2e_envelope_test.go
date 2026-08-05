package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// envelopeFixture is the shape this whole path exists for, written as a
// document: a paginated list, plus the two things that used to make one
// uncacheable.
//
// `GET /orders` returns `PageOrder{items: []Order, total, nextCursor}` --
// nothing about that response is an entity, and before this the operation got
// no entity, no tags and no normalization at all. `PageOrder` declares
// `x-forge-envelope: true`.
//
// `Order.shipment` reaches `Carrier` through `Shipment`, which is NOT an
// entity: it has no identity-shaped field, so it can never be a record, and the
// old field-map pass stopped dead at it and lost `Carrier` entirely.
//
// The component graph also cycles -- `Order.customer -> Customer.orders ->
// Order`, and `Order.parent -> Order` -- because the reachability passes now
// follow refs and a real document does exactly this.
//
// `PickPack` is useful (it reaches `Carrier`) and appears ONLY in a request
// body. Nothing will ever be walked through it, and it must not get a row.
//
// Driven through the real entry point -- a file on disk, SpecParser.ParseFile,
// Generator.Generate -- rather than a hand-built APISpec, for the reason
// e2e_entity_fields_test.go gives: a hand-built intermediate representation is
// how a generator gets tested against a shape production never produces.
const envelopeFixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/orders": {
      "get": {
        "operationId": "orders.list",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/PageOrder" } } } } }
      },
      "post": {
        "operationId": "orders.create",
        "requestBody": { "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/PickPack" } } } },
        "responses": { "201": { "description": "created", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Order" } } } } }
      }
    },
    "/reports/orders": {
      "get": {
        "operationId": "reports.orders",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/OrderReport" } } } } }
      }
    },
    "/customers/{id}": {
      "get": {
        "operationId": "customers.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Customer" } } } } }
      }
    },
    "/carriers/{id}": {
      "get": {
        "operationId": "carriers.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Carrier" } } } } }
      }
    }
  },
  "components": {
    "schemas": {
      "PageOrder": {
        "type": "object",
        "x-forge-envelope": true,
        "properties": {
          "items": { "type": "array", "items": { "$ref": "#/components/schemas/Order" } },
          "total": { "type": "integer" },
          "nextCursor": { "type": "string" }
        }
      },
      "OrderReport": {
        "type": "object",
        "properties": {
          "topOrders": { "type": "array", "items": { "$ref": "#/components/schemas/Order" } },
          "generatedAt": { "type": "string" }
        }
      },
      "Order": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "customer": { "$ref": "#/components/schemas/Customer" },
          "shipment": { "$ref": "#/components/schemas/Shipment" },
          "status": { "$ref": "#/components/schemas/OrderStatus" },
          "parent": { "$ref": "#/components/schemas/Order" }
        }
      },
      "Shipment": {
        "type": "object",
        "properties": {
          "carrier": { "$ref": "#/components/schemas/Carrier" },
          "weightKg": { "type": "number" }
        }
      },
      "Carrier": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "name": { "type": "string" }
        }
      },
      "Customer": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "orders": { "type": "array", "items": { "$ref": "#/components/schemas/Order" } }
        }
      },
      "PickPack": {
        "type": "object",
        "properties": { "carrier": { "$ref": "#/components/schemas/Carrier" } }
      },
      "OrderStatus": { "type": "string", "enum": ["open", "closed"] }
    }
  }
}`

// The feature, end to end. A paginated read gets the same cache contract a bare
// array gets, and the entity table gains rows for the types that only route.
//
// The paired assertion lives in packages/client-core/__tests__/envelope.test.ts,
// which feeds this exact table to `normalize` and checks the orders inside the
// page reach the store. Either half alone proves nothing: Go can emit a table
// the runtime does not read, and the runtime can read a table Go does not emit.
func TestGenerateFromSpecFileEmitsEnvelopeCacheContract(t *testing.T) {
	ops := envelopeOps(t)

	// The operation. `entity` names what is cached; `rootType` names what the
	// response document IS, and they are deliberately different here -- that
	// difference is the entire reason RootType exists.
	wantOp := `  'orders.list': {
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    rootType: 'PageOrder',
    provides: ['Order:{id}', 'Order[]'],
    invalidates: [],
  },`
	if !strings.Contains(ops, wantOp) {
		t.Fatalf("ops.ts is missing the enveloped list operation:\n%s\n\ngot:\n%s", wantOp, ops)
	}

	// The envelope's row: field edges, and no identity at all. A `PageOrder`
	// is never a record, and it used to need an idField naming a property no
	// payload carries in order to say so.
	if !strings.Contains(ops, `PageOrder: { fields: { items: 'Order' } },`) {
		t.Fatalf("ops.ts is missing the identity-free PageOrder row\n\n%s", ops)
	}

	// The transitive hop. `Order.shipment` is kept even though Shipment is not
	// an entity, because Carrier is reachable through it, and Shipment gets its
	// own identity-free row so the walk can continue.
	if !strings.Contains(ops, `Shipment: { fields: { carrier: 'Carrier' } },`) {
		t.Fatalf("ops.ts is missing the Shipment routing row\n\n%s", ops)
	}

	wantOrder := `Order: { idField: 'id', fields: { customer: 'Customer', parent: 'Order', shipment: 'Shipment' } }`
	if !strings.Contains(ops, wantOrder) {
		t.Fatalf("ops.ts is missing %q\n\n%s", wantOrder, ops)
	}
}

// What must NOT be in the table, which is the half a permissive rule gets
// wrong.
func TestGenerateFromSpecFileOmitsUnreachableAndUselessTypes(t *testing.T) {
	ops := envelopeOps(t)

	// PickPack reaches Carrier, so it is useful -- and no response is ever
	// walked through it, so it is not kept. Without the roots pass every
	// request body in the document would land in the runtime's table.
	if strings.Contains(ops, "PickPack") {
		t.Fatalf("ops.ts gave a row to a type only a request body mentions\n\n%s", ops)
	}

	// A named type with no entity anywhere beneath it stays out, exactly as
	// before: the runtime's only use for a row is to descend through it.
	if strings.Contains(ops, "OrderStatus") {
		t.Fatalf("ops.ts gave a row to a type with no entity beneath it\n\n%s", ops)
	}
}

// The decision the policy turns on, stated as a test.
//
// `OrderReport{topOrders: []Order, generatedAt: string}` is structurally
// IDENTICAL to `PageOrder{items: []Order, total: int}` -- one named non-entity
// type with exactly one array-of-entity property. The heuristic that would make
// PageOrder work would fire on this too, and assert that a report over some
// orders provides `Order[]`: the sentence "this response is the Order
// collection", which nobody wrote and which puts a false edge in the
// invalidation graph.
//
// So the report gets NO tags and NO entity. What it does get -- because
// normalization never depended on the policy -- is a routing row and a
// rootType, so the orders inside it still normalize and still share the store
// with every other view of those same orders.
func TestGenerateFromSpecFileRefusesToGuessAnEnvelope(t *testing.T) {
	ops := envelopeOps(t)

	wantOp := `  'reports.orders': {
    method: 'GET',
    path: '/reports/orders',
    rootType: 'OrderReport',
    provides: [],
    invalidates: [],
  },`
	if !strings.Contains(ops, wantOp) {
		t.Fatalf("undeclared wrapper did not resolve as expected:\n%s\n\ngot:\n%s", wantOp, ops)
	}

	if !strings.Contains(ops, `OrderReport: { fields: { topOrders: 'Order' } },`) {
		t.Fatalf("ops.ts is missing the OrderReport routing row\n\n%s", ops)
	}
}

// Regenerating the same file must produce the same bytes. CI byte-diffs
// generated output, and both new tables are built from Go maps.
func TestGenerateFromSpecFileEnvelopeTableIsDeterministic(t *testing.T) {
	path := writeSpecFile(t, "openapi.json", envelopeFixture)

	first := generateFromSpecFile(t, path)["src/ops.ts"]

	for i := 0; i < 20; i++ {
		if got := generateFromSpecFile(t, path)["src/ops.ts"]; got != first {
			t.Fatalf("ops.ts differs between runs\n\nfirst:\n%s\n\nlater:\n%s", first, got)
		}
	}
}

// A hand-built APISpec is banned in this file for the reason its fixture
// explains, so the warnings a malformed declaration produces are checked
// against a real document too.
func TestParseFileWarnsOnAmbiguousEnvelope(t *testing.T) {
	const fixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/feed": {
      "get": {
        "operationId": "feed.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Feed" } } } } }
      }
    },
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
    }
  },
  "components": {
    "schemas": {
      "Feed": {
        "type": "object",
        "x-forge-envelope": true,
        "properties": {
          "orders": { "type": "array", "items": { "$ref": "#/components/schemas/Order" } },
          "customers": { "type": "array", "items": { "$ref": "#/components/schemas/Customer" } }
        }
      },
      "Order": { "type": "object", "properties": { "id": { "type": "string" } } },
      "Customer": { "type": "object", "properties": { "id": { "type": "string" } } }
    }
  }
}`

	spec := parseEnvelopeSpec(t, fixture)

	joined := strings.Join(spec.Warnings, "\n")
	for _, want := range []string{"2 of its properties carry an entity", "customers, orders"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warning %q not raised; got: %s", want, joined)
		}
	}

	// Refusing means refusing: no entity, no tags. The response still
	// normalizes, which is why the row survives.
	if ep := endpointByPath(t, spec, "/feed"); ep.Entity != nil || len(ep.CacheTags.Provides) != 0 {
		t.Fatalf("ambiguous envelope still produced a cache contract: %+v", ep)
	}
}

// The escape from that refusal: name the property.
func TestParseFileHonoursNamedEnvelopeProperty(t *testing.T) {
	const fixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/feed": {
      "get": {
        "operationId": "feed.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Feed" } } } } }
      }
    },
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
    }
  },
  "components": {
    "schemas": {
      "Feed": {
        "type": "object",
        "x-forge-envelope": "orders",
        "properties": {
          "orders": { "type": "array", "items": { "$ref": "#/components/schemas/Order" } },
          "customers": { "type": "array", "items": { "$ref": "#/components/schemas/Customer" } }
        }
      },
      "Order": { "type": "object", "properties": { "id": { "type": "string" } } },
      "Customer": { "type": "object", "properties": { "id": { "type": "string" } } }
    }
  }
}`

	spec := parseEnvelopeSpec(t, fixture)

	ep := endpointByPath(t, spec, "/feed")
	if ep.Entity == nil || ep.Entity.Type != "Order" {
		t.Fatalf("named envelope property did not resolve to Order: %+v", ep.Entity)
	}

	if ep.RootType != "Feed" {
		t.Fatalf("RootType = %q, want Feed", ep.RootType)
	}

	if got := strings.Join(ep.CacheTags.Provides, ","); got != "Order:{id},Order[]" {
		t.Fatalf("provides = %q, want the item and collection tags", got)
	}

	// Both arrays still route, because routing never asked the policy anything.
	if f := spec.RoutingTypes["Feed"].Fields; f["orders"] != "Order" || f["customers"] != "Customer" {
		t.Fatalf("Feed routing row lost an edge: %+v", f)
	}
}

// A single-record wrapper -- `{data: Order, meta: {...}}` -- is an envelope
// too, and provides only the item tag. Deriving `Order[]` from it would tell
// the invalidation graph that one order is the whole collection.
func TestParseFileEnvelopeAroundOneRecordIsNotAList(t *testing.T) {
	const fixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/orders/{id}": {
      "get": {
        "operationId": "orders.get",
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/OrderResponse" } } } } }
      }
    }
  },
  "components": {
    "schemas": {
      "OrderResponse": {
        "type": "object",
        "x-forge-envelope": true,
        "properties": {
          "data": { "$ref": "#/components/schemas/Order" },
          "requestId": { "type": "string" }
        }
      },
      "Order": { "type": "object", "properties": { "id": { "type": "string" } } }
    }
  }
}`

	ep := endpointByPath(t, parseEnvelopeSpec(t, fixture), "/orders/{id}")

	if got := strings.Join(ep.CacheTags.Provides, ","); got != "Order:{id}" {
		t.Fatalf("provides = %q, want only the item tag", got)
	}
}

func envelopeOps(t *testing.T) string {
	t.Helper()

	files := generateFromSpecFile(t, writeSpecFile(t, "openapi.json", envelopeFixture))

	ops, ok := files["src/ops.ts"]
	if !ok {
		t.Fatal("src/ops.ts was not generated")
	}

	return ops
}

// endpointByPath finds one endpoint by path. Endpoints come out in sorted path
// order, so indexing by position breaks the moment a fixture gains a path that
// sorts earlier -- which is exactly what happened to the first draft of these
// fixtures.
func endpointByPath(t *testing.T, spec *client.APISpec, path string) *client.Endpoint {
	t.Helper()

	for i := range spec.Endpoints {
		if spec.Endpoints[i].Path == path {
			return &spec.Endpoints[i]
		}
	}

	t.Fatalf("no endpoint for path %q", path)

	return nil
}

func parseEnvelopeSpec(t *testing.T, fixture string) *client.APISpec {
	t.Helper()

	spec, err := client.NewSpecParser().ParseFile(t.Context(), writeSpecFile(t, "openapi.json", fixture))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	return spec
}
