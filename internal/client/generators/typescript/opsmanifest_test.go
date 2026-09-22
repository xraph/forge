// internal/client/generators/typescript/opsmanifest_test.go
package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func manifestSpec() *client.APISpec {
	return &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				ID: "orderList", Method: "GET", Path: "/orders",
				Entity:    &client.EntityRef{Type: "Order", IDField: "id"},
				CacheTags: client.TagSet{Provides: []string{"Order:{id}", "Order[]"}},
			},
			{
				ID: "orderCreate", Method: "POST", Path: "/orders",
				Entity:    &client.EntityRef{Type: "Order", IDField: "id"},
				CacheTags: client.TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
			},
		},
		Entities: map[string]*client.EntityRef{
			"Order": {Type: "Order", IDField: "id"},
		},
	}
}

func TestOpsManifestContainsOperations(t *testing.T) {
	out := manifestText(manifestSpec(), client.GeneratorConfig{})

	for _, want := range []string{
		"orderList",
		"orderCreate",
		`method: 'POST'`,
		`path: '/orders'`,
		`provides: ['Order:{id}', 'Order[]']`,
		`invalidates: ['Order[]']`,
		`entity: 'Order'`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ops.ts missing %q\n\n%s", want, out)
		}
	}
}

func TestOpsManifestContainsEntities(t *testing.T) {
	out := manifestText(manifestSpec(), client.GeneratorConfig{})

	if !strings.Contains(out, `'Order': { idField: 'id' }`) {
		t.Fatalf("ops.ts missing entity table\n\n%s", out)
	}
}

// Generated output is diffed by CI; map iteration must not reach the file.
func TestOpsManifestIsDeterministic(t *testing.T) {
	gen := NewOpsManifestGenerator()

	first := gen.Generate(manifestSpec(), client.GeneratorConfig{})
	for i := 0; i < 50; i++ {
		if got := gen.Generate(manifestSpec(), client.GeneratorConfig{}); got != first {
			t.Fatal("ops.ts differs between runs: a map is being iterated unsorted")
		}
	}
}

// fieldMapSpec is manifestSpec plus the property-to-typename edges the
// resolver fills in. `Order` reaches a Customer directly and a list of
// LineItems through an array; `LineItem` reaches nothing.
func fieldMapSpec() *client.APISpec {
	spec := manifestSpec()
	spec.Entities = map[string]*client.EntityRef{
		"Order": {Type: "Order", IDField: "id", Fields: map[string]string{
			"customer": "Customer",
			"items":    "LineItem",
			"parent":   "Order",
		}},
		"Customer": {Type: "Customer", IDField: "id"},
		"LineItem": {Type: "LineItem", IDField: "sku"},
	}

	return spec
}

func TestOpsManifestEmitsFieldMap(t *testing.T) {
	out := manifestText(fieldMapSpec(), client.GeneratorConfig{})

	want := `'Order': { idField: 'id', fields: { 'customer': 'Customer', 'items': 'LineItem', 'parent': 'Order' } }`
	if !strings.Contains(out, want) {
		t.Fatalf("ops.ts missing %q\n\n%s", want, out)
	}

	// The runtime's EntityMeta types `fields` as optional; an entity with no
	// entity-typed property must not carry an empty object.
	if !strings.Contains(out, `'LineItem': { idField: 'sku' },`) {
		t.Fatalf("ops.ts did not omit an empty field map\n\n%s", out)
	}

	if strings.Contains(out, "fields: {  }") || strings.Contains(out, "fields: {}") {
		t.Fatalf("ops.ts emitted an empty field map\n\n%s", out)
	}
}

// The declared interface has to admit the property the table now carries, or
// the `satisfies` clause fails to compile in the consuming repository.
func TestOpsManifestDeclaresFieldsOnEntityMeta(t *testing.T) {
	out := manifestText(fieldMapSpec(), client.GeneratorConfig{})

	if !strings.Contains(out, "readonly fields?: Readonly<Record<string, string>>;") {
		t.Fatalf("ops.ts EntityMeta does not declare fields\n\n%s", out)
	}
}

// EntityRef.Fields is a Go map, and this file is byte-diffed by CI: an
// unsorted walk over it reports a change on every regeneration.
func TestOpsManifestFieldMapIsDeterministic(t *testing.T) {
	gen := NewOpsManifestGenerator()

	first := gen.Generate(fieldMapSpec(), client.GeneratorConfig{})
	for i := 0; i < 50; i++ {
		if got := gen.Generate(fieldMapSpec(), client.GeneratorConfig{}); got != first {
			t.Fatal("ops.ts differs between runs: EntityRef.Fields is being iterated unsorted")
		}
	}
}

func TestStaleTimeIsEmittedWhenDeclared(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{{
		ID: "orderList", Method: "GET", Path: "/orders", StaleTime: 30000,
	}}}

	out := manifestText(spec, client.GeneratorConfig{})

	if !strings.Contains(out, "staleTime: 30000,") {
		t.Fatalf("ops.ts missing staleTime\n\n%s", out)
	}
}

func TestStaleTimeIsOmittedWhenUndeclared(t *testing.T) {
	out := manifestText(manifestSpec(), client.GeneratorConfig{})

	// The interface declares `staleTime?: number` unconditionally (see
	// generateMeta), so the bare substring "staleTime" is always present.
	// What must stay absent for an undeclared endpoint is the VALUE line --
	// "staleTime: <ms>," -- which is bundle weight on a lookup that always
	// misses. The "?" in the interface's "staleTime?:" keeps this check from
	// matching the type declaration.
	if strings.Contains(out, "staleTime: ") {
		t.Fatalf("ops.ts emitted a staleTime value for a spec that declared none\n\n%s", out)
	}
}

func TestStaleTimeReachesThePerOperationModule(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{{
		ID: "orderList", Method: "GET", Path: "/orders", StaleTime: 30000,
	}}}

	files := NewOpsManifestGenerator().GenerateModules(spec, client.GeneratorConfig{})

	var found bool
	for name, content := range files {
		if strings.HasPrefix(name, "src/ops/") && strings.Contains(content, "staleTime: 30000,") {
			found = true
		}
	}

	if !found {
		t.Fatal("ops.ts and the split modules must not disagree about an operation")
	}
}

func TestOpsManifestEscapesHostileValues(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{{
		ID: "x", Method: "GET", Path: `/orders'; evil()//`,
	}}}

	out := manifestText(spec, client.GeneratorConfig{})

	if strings.Contains(out, `'/orders'; evil()//'`) {
		t.Fatalf("unescaped quote broke out of the string literal\n\n%s", out)
	}
}

// declaredTagSpec is an API whose wire is snake_case and whose routes declare
// cross-entity templates in that wire spelling, exactly as WithInvalidates
// asks for them on the server: the JSON property name, not the Go field.
//
// It exercises every source a template can name. `customer_id` is a body
// property on the create, a response property on the order, and a path
// parameter on the fetch; `shop_id` is a query parameter; `external_id` sits
// one $ref hop into the response.
func declaredTagSpec() *client.APISpec {
	order := &client.EntityRef{Type: "Order", IDField: "id"}

	return &client.APISpec{
		Schemas: map[string]*client.Schema{
			"Order": {Type: "object", Properties: map[string]*client.Schema{
				"id":          {Type: "string"},
				"customer_id": {Type: "string"},
				"customer":    {Ref: "#/components/schemas/Customer"},
			}},
			"Customer": {Type: "object", Properties: map[string]*client.Schema{
				"id":          {Type: "string"},
				"external_id": {Type: "string"},
			}},
			"CreateOrderRequest": {Type: "object", Properties: map[string]*client.Schema{
				"customer_id": {Type: "string"},
			}},
		},
		Endpoints: []client.Endpoint{
			{
				ID: "orderCreate", Method: "POST", Path: "/orders",
				Entity: order,
				QueryParams: []client.Parameter{
					{Name: "shop_id", In: "query", Schema: &client.Schema{Type: "string"}},
				},
				RequestBody: &client.RequestBody{Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/CreateOrderRequest"}},
				}},
				Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
				}}},
				CacheTags: client.TagSet{
					Provides: []string{"Order:{id}"},
					Invalidates: []string{
						"Customer:{req.customer_id}",
						"Customer:{res.customer_id}",
						"Customer:{customer_id}",
						"Ledger:{res.customer.external_id}",
						"Shop:{req.shop_id}",
						"Order[]",
					},
				},
			},
			{
				ID: "orderFetch", Method: "GET", Path: "/customers/{customer_id}/orders/{id}",
				Entity: order,
				PathParams: []client.Parameter{
					{Name: "customer_id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}},
					{Name: "id", In: "path", Required: true, Schema: &client.Schema{Type: "string"}},
				},
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
				}}},
				CacheTags: client.TagSet{Provides: []string{"Order:{id}", "Customer:{customer_id}"}},
			},
		},
		Entities: map[string]*client.EntityRef{"Order": order},
	}
}

// A template names a property of the wire document, because that is what the
// server-side declaration can see. The runtime resolves it against a body the
// caller built in TypeScript and a response the codec has already decoded, so
// under camelCase naming the wire spelling names nothing and the tag
// silently invalidates nothing. The manifest has to rename the placeholder
// through the same schema-aware rename the entities table and the derived
// item tag already go through.
//
// Path and query parameters are NOT renamed: the transport substitutes them by
// their wire name, and that is the key the caller supplies them under.
func TestOpsManifestRenamesDeclaredTemplatesThroughTheSchema(t *testing.T) {
	out := manifestText(declaredTagSpec(), client.GeneratorConfig{Language: "typescript"})

	for _, want := range []string{
		// body property, explicit and bare
		`'Customer:{req.customerId}'`,
		`'Customer:{customerId}'`,
		// response property, explicit
		`'Customer:{res.customerId}'`,
		// one $ref hop into the response
		`'Ledger:{res.customer.externalId}'`,
		// a query parameter keeps its wire name
		`'Shop:{req.shop_id}'`,
		// a path parameter keeps its wire name, even bare
		`provides: ['Order:{id}', 'Customer:{customer_id}']`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ops.ts missing %q", want)
		}
	}

	for _, stale := range []string{
		`{req.customer_id}`,
		`{res.customer_id}`,
		`{res.customer.external_id}`,
	} {
		if strings.Contains(out, stale) {
			t.Errorf("ops.ts still carries wire-cased template %q", stale)
		}
	}

	if t.Failed() {
		t.Logf("\n%s", out)
	}
}

// Under preserve the rename is the identity, so the file the generator wrote
// before this existed is the file it writes now.
func TestOpsManifestLeavesDeclaredTemplatesAloneUnderPreserve(t *testing.T) {
	out := manifestText(declaredTagSpec(), client.GeneratorConfig{
		Language: "typescript", FieldNaming: client.NamingPreserve,
	})

	for _, want := range []string{
		`'Customer:{req.customer_id}'`,
		`'Customer:{res.customer_id}'`,
		`'Ledger:{res.customer.external_id}'`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ops.ts missing %q\n\n%s", want, out)
		}
	}
}

// streamSpec is one channel emitting Orders, with the Order component present
// so the entity has a codec to decode through.
func streamSpec() *client.APISpec {
	return &client.APISpec{
		Schemas: map[string]*client.Schema{
			"Order": {Type: "object", Properties: map[string]*client.Schema{
				"id":          {Type: "string"},
				"customer_id": {Type: "string"},
			}},
		},
		WebSockets: []client.WebSocketEndpoint{{
			ID: "orders", Path: "/ws/orders",
			StreamBindings: []client.StreamBinding{
				{Message: "order.created", EntityType: "Order", Intent: client.StreamUpsert, Invalidates: []string{"Order[]"}},
				{Message: "audit.logged", EntityType: "AuditEvent", Intent: client.StreamPatch},
			},
		}},
		Entities: map[string]*client.EntityRef{"Order": {Type: "Order", IDField: "id"}},
	}
}

// A frame is normalized against an entities table that names client-side
// fields, so under a renaming configuration the row has to carry the entity's
// codec. A binding whose entity no component describes has no codec to carry.
func TestOpsManifestStreamsDecodeThroughTheEntityCodec(t *testing.T) {
	out := manifestText(streamSpec(), client.GeneratorConfig{Language: "typescript"})

	for _, want := range []string{
		"import { CODECS, decode } from './codecs';",
		"decode: (payload: unknown) => decode(payload, CODECS['Order']),",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ops.ts missing %q", want)
		}
	}

	if strings.Contains(out, "CODECS['AuditEvent']") {
		t.Errorf("ops.ts decodes an entity no component describes")
	}

	if t.Failed() {
		t.Logf("\n%s", out)
	}
}

// Under preserve the wire shape is the client shape, and the table is what it
// was before decode existed.
func TestOpsManifestStreamsCarryNoDecodeUnderPreserve(t *testing.T) {
	out := manifestText(streamSpec(), client.GeneratorConfig{
		Language: "typescript", FieldNaming: client.NamingPreserve,
	})

	if strings.Contains(out, "decode") {
		t.Fatalf("ops.ts carries a decode under preserve:\n%s", out)
	}
}

// A WebTransport endpoint's bindings reach the streams table like a socket's.
func TestOpsManifestStreamsIncludeWebTransport(t *testing.T) {
	spec := streamSpec()
	spec.WebTransports = []client.WebTransportEndpoint{{
		ID: "ticks", Path: "/wt/ticks",
		StreamBindings: []client.StreamBinding{
			{Message: "order.ticked", EntityType: "Order", Intent: client.StreamPatch},
		},
	}}

	out := manifestText(spec, client.GeneratorConfig{})

	for _, want := range []string{"channel: '/wt/ticks'", "message: 'order.ticked'"} {
		if !strings.Contains(out, want) {
			t.Errorf("ops.ts missing %q\n\n%s", want, out)
		}
	}
}
