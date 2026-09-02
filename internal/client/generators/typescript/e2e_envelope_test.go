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
    "/reports/orders/archived": {
      "get": {
        "operationId": "reports.archived",
        "x-forge-no-entity": true,
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
	//
	// `responseCodec` names the codec the RUNTIME must decode this response
	// with. It is the envelope's own id, not the entity's: decode walks the
	// document that actually arrived, and PageOrder's own `items` edge is what
	// carries it into Order. The default config is TypeScript, hence camel,
	// hence codecsNeeded -- so it is emitted here.
	wantOp := `  'orders.list': {
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    rootType: 'PageOrder',
    provides: ['Order:{id}', 'Order[]'],
    invalidates: [],
    responseCodec: 'PageOrder',
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
//
// Scoped to the ENTITIES table specifically, not to the whole file. A typename
// can legitimately appear elsewhere in ops.ts now: `bodyCodec: 'PickPack'`
// names the codec that encodes the create-order request body, which is a
// statement about wire encoding and says nothing about whether the runtime
// ever normalizes through PickPack. Asserting over the whole file would
// conflate the two and fail on a correct manifest.
func TestGenerateFromSpecFileOmitsUnreachableAndUselessTypes(t *testing.T) {
	ops := envelopeOps(t)
	table := entitiesTable(t, ops)

	// PickPack reaches Carrier, so it is useful -- and no response is ever
	// walked through it, so it is not kept. Without the roots pass every
	// request body in the document would land in the runtime's table.
	if strings.Contains(table, "PickPack") {
		t.Fatalf("ops.ts gave a row to a type only a request body mentions\n\n%s", ops)
	}

	// A named type with no entity anywhere beneath it stays out, exactly as
	// before: the runtime's only use for a row is to descend through it.
	if strings.Contains(table, "OrderStatus") {
		t.Fatalf("ops.ts gave a row to a type with no entity beneath it\n\n%s", ops)
	}

	// The other side of the same coin: PickPack IS named as a request-body
	// codec, because a type the runtime never normalizes through is still a
	// type the wire has to be encoded for. Pinned so the scoping above cannot
	// quietly start passing because the codec id disappeared too.
	if !strings.Contains(ops, "bodyCodec: 'PickPack',") {
		t.Fatalf("ops.ts dropped the request-body codec for PickPack\n\n%s", ops)
	}
}

// entitiesTable returns just the `export const entities = { ... }` block of an
// ops.ts, so an assertion about the runtime's normalization table cannot be
// satisfied -- or broken -- by an unrelated mention of the same typename
// somewhere else in the file.
func entitiesTable(t *testing.T, ops string) string {
	t.Helper()

	const open = "export const entities = {"

	const close = "} as const satisfies Record<string, EntityMeta>;"

	start := strings.Index(ops, open)
	if start < 0 {
		t.Fatalf("ops.ts has no entities table\n\n%s", ops)
	}

	end := strings.Index(ops[start:], close)
	if end < 0 {
		t.Fatalf("ops.ts entities table is unterminated\n\n%s", ops)
	}

	return ops[start : start+end+len(close)]
}

// The decision the policy turns on, stated as a test -- and the direction it
// now runs.
//
// `OrderReport{topOrders: []Order, generatedAt: string}` is structurally
// IDENTICAL to `PageOrder{items: []Order, total: int}`. Nothing in the document
// separates a page of the collection from a projection over it, so a rule keyed
// on shape tags both, and this report gets `Order[]`: the sentence "this
// response is the Order collection", which nobody wrote.
//
// That edge is accepted deliberately, because the two errors are not the same
// size. Tagging the report costs a refetch of a report that was derived from
// orders and is stale after one anyway. Refusing to tag the page cost four
// reads in five across four measured clients, each one a list that never
// refreshes until somebody hand-writes the refetch -- which is the cache model
// the normalized store exists to replace. It is the trade DeriveTags already
// makes for PATCH, in the same words.
//
// The escape points the right way now: a report that is not a view of the
// collection says so with `x-forge-no-entity`, which the next test drives.
//
// Asserted against the parsed document rather than the emitted manifest: this
// is a statement about resolution, and it should not fail when the generator
// changes how it lays out files.
func TestParseFileInfersAnUndeclaredCollectionEnvelope(t *testing.T) {
	spec := parseEnvelopeSpec(t, envelopeFixture)
	ep := endpointByPath(t, spec, "/reports/orders")

	if ep.Entity == nil || ep.Entity.Type != "Order" || ep.Entity.IDField != "id" {
		t.Fatalf("Entity = %+v, want Order/id", ep.Entity)
	}

	if ep.RootType != "OrderReport" {
		t.Fatalf("RootType = %q, want OrderReport", ep.RootType)
	}

	if got := strings.Join(ep.CacheTags.Provides, ","); got != "Order:{id},Order[]" {
		t.Fatalf("provides = %q, want the contract a bare []Order gets", got)
	}

	// Routing never asked the policy anything, and still does not.
	if f := spec.RoutingTypes["OrderReport"].Fields; f["topOrders"] != "Order" {
		t.Fatalf("OrderReport routing row lost its edge: %+v", f)
	}
}

// The escape from the edge above, driven through a real document.
//
// Inference can be wrong about what an endpoint MEANS -- that is the whole
// content of the OrderReport argument -- so an operation that is not a view of
// the collection has to be able to say so, and saying so has to remove the tag
// rather than soften it. `x-forge-no-entity` takes the response out entirely,
// rootType included, so the runtime does not descend into it either.
func TestParseFileOptOutBeatsInference(t *testing.T) {
	ep := endpointByPath(t, parseEnvelopeSpec(t, envelopeFixture), "/reports/orders/archived")

	if ep.Entity != nil || ep.RootType != "" {
		t.Fatalf("opt-out left Entity=%+v RootType=%q", ep.Entity, ep.RootType)
	}

	if len(ep.CacheTags.Provides) != 0 {
		t.Fatalf("provides = %v, want none", ep.CacheTags.Provides)
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
