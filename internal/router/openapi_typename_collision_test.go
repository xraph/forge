package router

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/router/testtypes/billing"
	"github.com/xraph/forge/internal/router/testtypes/shipping"
	"github.com/xraph/forge/internal/router/testtypes/warehouse"
	"github.com/xraph/forge/internal/shared"
)

type collisionEmptyRequest struct{}

// refTarget resolves the component a 200 response of the given path points at.
func refTarget(t *testing.T, spec *OpenAPISpec, path string) (string, *Schema) {
	t.Helper()

	item, ok := spec.Paths[path]
	require.True(t, ok, "path %s missing from spec", path)
	require.NotNil(t, item.Get, "GET operation missing for %s", path)

	resp, ok := item.Get.Responses["200"]
	require.True(t, ok, "200 response missing for %s", path)
	require.NotNil(t, resp.Content, "200 response for %s has no content", path)

	mt, ok := resp.Content["application/json"]
	require.True(t, ok, "application/json content missing for %s", path)
	require.NotNil(t, mt.Schema, "schema missing for %s", path)
	require.NotEmpty(t, mt.Schema.Ref, "expected a component $ref for %s", path)

	name := componentNameFromRef(mt.Schema.Ref)
	target, ok := spec.Components.Schemas[name]
	require.True(t, ok, "$ref %s for %s does not resolve to a component", mt.Schema.Ref, path)

	return name, target
}

func componentNameFromRef(ref string) string {
	const prefix = "#/components/schemas/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}

	return ref
}

func requireProperty(t *testing.T, schema *Schema, prop string) {
	t.Helper()
	require.NotNil(t, schema.Properties, "schema has no properties")
	require.Contains(t, schema.Properties, prop, "schema is missing property %q", prop)
}

// collectRefs walks the marshalled spec and returns every $ref it contains.
// Going through JSON keeps the walk honest: it sees exactly what a consumer of
// the document sees, including any corner of the spec this test never thought
// about.
func collectRefs(t *testing.T, spec *OpenAPISpec) []string {
	t.Helper()

	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	var doc any

	require.NoError(t, json.Unmarshal(raw, &doc))

	var (
		refs []string
		walk func(any)
	)

	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "$ref" {
					if s, ok := child.(string); ok {
						refs = append(refs, s)
					}

					continue
				}

				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}

	walk(doc)

	return refs
}

// requireEveryRefResolves is the regression guard for the rename pass: a
// component that moved while a reference to it did not would leave a dangling
// pointer, which is a worse failure than the collision it was fixing.
func requireEveryRefResolves(t *testing.T, spec *OpenAPISpec) {
	t.Helper()

	for _, ref := range collectRefs(t, spec) {
		require.True(t, strings.HasPrefix(ref, "#/components/schemas/"),
			"unexpected $ref target %q", ref)
		require.Contains(t, spec.Components.Schemas, componentNameFromRef(ref),
			"dangling $ref %q", ref)
	}
}

// componentNames returns the sorted component names of a spec.
func componentNames(spec *OpenAPISpec) []string {
	names := make([]string, 0, len(spec.Components.Schemas))
	for name := range spec.Components.Schemas {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// TestOpenAPI_CollidingTypeNames_BothSurvive pins the core bug: two types with the
// same bare name in different packages must both keep their own component schema.
func TestOpenAPI_CollidingTypeNames_BothSurvive(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Collision", Version: "1.0.0"}))

	require.NoError(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)
	require.NotNil(t, spec.Components)
	require.NotNil(t, spec.Components.Schemas)

	billingName, billingSchema := refTarget(t, spec, "/billing/invoice")
	shippingName, shippingSchema := refTarget(t, spec, "/shipping/invoice")

	require.NotEqual(t, billingName, shippingName,
		"colliding types share one component name: %s", billingName)

	// Each $ref must resolve to the shape of its own type.
	requireProperty(t, billingSchema, "invoice_number")
	requireProperty(t, billingSchema, "amount_cents")
	require.NotContains(t, billingSchema.Properties, "tracking_code")

	requireProperty(t, shippingSchema, "tracking_code")
	requireProperty(t, shippingSchema, "weight_kg")
	require.NotContains(t, shippingSchema.Properties, "invoice_number")

	// Constraint 2: neither side keeps the bare name when it is contested.
	require.NotContains(t, spec.Components.Schemas, "Invoice",
		"a contested bare name must not be handed to whichever type registered first")
}

// TestOpenAPI_NonCollidingTypeName_Unchanged is the compatibility guarantee:
// a type whose name is unique keeps its bare component name.
func TestOpenAPI_NonCollidingTypeName_Unchanged(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "NoCollision", Version: "1.0.0"}))

	require.NoError(t, r.GET("/warehouse/receipt",
		func(ctx shared.Context, req *collisionEmptyRequest) (*warehouse.Receipt, error) {
			return &warehouse.Receipt{}, nil
		}))
	require.NoError(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)

	name, schema := refTarget(t, spec, "/warehouse/receipt")
	require.Equal(t, "Receipt", name, "unique type names must not be rewritten")
	requireProperty(t, schema, "receipt_id")

	// The uncontested Invoice keeps its bare name too.
	invoiceName, _ := refTarget(t, spec, "/billing/invoice")
	require.Equal(t, "Invoice", invoiceName)
}

// TestOpenAPI_ThreeWayCollision covers more than two participants.
func TestOpenAPI_ThreeWayCollision(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "ThreeWay", Version: "1.0.0"}))

	require.NoError(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/warehouse/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*warehouse.Invoice, error) {
			return &warehouse.Invoice{}, nil
		}))

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)

	names := map[string]bool{}

	for path, prop := range map[string]string{
		"/billing/invoice":   "invoice_number",
		"/shipping/invoice":  "tracking_code",
		"/warehouse/invoice": "bin_location",
	} {
		name, schema := refTarget(t, spec, path)
		require.False(t, names[name], "component name %s reused for %s", name, path)

		names[name] = true

		requireProperty(t, schema, prop)
	}

	require.Len(t, names, 3)
	require.NotContains(t, spec.Components.Schemas, "Invoice")
}

// TestOpenAPI_CollisionResolution_IndependentOfRouteOrder is constraint 2: the
// names must depend on the set of types, not on which route was declared first.
func TestOpenAPI_CollisionResolution_IndependentOfRouteOrder(t *testing.T) {
	build := func(reverse bool) *OpenAPISpec {
		r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Order", Version: "1.0.0"}))

		bill := func() error {
			return r.GET("/billing/invoice",
				func(ctx shared.Context, req *collisionEmptyRequest) (*billing.Invoice, error) {
					return &billing.Invoice{}, nil
				})
		}
		ship := func() error {
			return r.GET("/shipping/invoice",
				func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
					return &shipping.Invoice{}, nil
				})
		}

		if reverse {
			bill, ship = ship, bill
		}

		require.NoError(t, bill())
		require.NoError(t, ship())

		return r.OpenAPISpec()
	}

	forward := build(false)
	backward := build(true)

	require.Equal(t, componentNames(forward), componentNames(backward),
		"component names must not depend on registration order")

	forwardName, _ := refTarget(t, forward, "/billing/invoice")
	backwardName, _ := refTarget(t, backward, "/billing/invoice")
	require.Equal(t, forwardName, backwardName)
}

// TestOpenAPI_ComponentNames_StableAcrossGenerations pins determinism across
// repeated generation of the same document. Specs get diffed and checked in.
func TestOpenAPI_ComponentNames_StableAcrossGenerations(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Stable", Version: "1.0.0"}))

	require.NoError(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/warehouse/receipt",
		func(ctx shared.Context, req *collisionEmptyRequest) (*warehouse.Receipt, error) {
			return &warehouse.Receipt{}, nil
		}))

	first := r.OpenAPISpec()
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)

	for range 5 {
		next := r.OpenAPISpec()
		require.Equal(t, componentNames(first), componentNames(next))

		nextJSON, err := json.Marshal(next)
		require.NoError(t, err)
		require.JSONEq(t, string(firstJSON), string(nextJSON))

		requireEveryRefResolves(t, next)
	}
}

// TestOpenAPI_CollidingRequestBodies covers the request-body registration site.
func TestOpenAPI_CollidingRequestBodies(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Requests", Version: "1.0.0"}))

	require.NoError(t, r.POST("/billing/invoice",
		func(ctx shared.Context, req *billing.Invoice) (*warehouse.Receipt, error) {
			return &warehouse.Receipt{}, nil
		}))
	require.NoError(t, r.POST("/shipping/invoice",
		func(ctx shared.Context, req *shipping.Invoice) (*warehouse.Receipt, error) {
			return &warehouse.Receipt{}, nil
		}))

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)

	body := func(path string) *Schema {
		item, ok := spec.Paths[path]
		require.True(t, ok, "path %s missing", path)
		require.NotNil(t, item.Post)
		require.NotNil(t, item.Post.RequestBody)

		mt, ok := item.Post.RequestBody.Content["application/json"]
		require.True(t, ok)
		require.NotNil(t, mt.Schema)
		require.NotEmpty(t, mt.Schema.Ref)

		target, ok := spec.Components.Schemas[componentNameFromRef(mt.Schema.Ref)]
		require.True(t, ok, "dangling request body $ref %s", mt.Schema.Ref)

		return target
	}

	requireProperty(t, body("/billing/invoice"), "invoice_number")
	requireProperty(t, body("/shipping/invoice"), "tracking_code")
	requireEveryRefResolves(t, spec)
}

// TestOpenAPI_CollidingNestedTypes covers types that become components because
// another struct references them, rather than because a route returns them.
func TestOpenAPI_CollidingNestedTypes(t *testing.T) {
	type ledger struct {
		Billed  *billing.Invoice    `json:"billed"`
		Shipped *shipping.Invoice   `json:"shipped"`
		Stored  []warehouse.Invoice `json:"stored"`
	}

	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Nested", Version: "1.0.0"}))

	require.NoError(t, r.GET("/ledger",
		func(ctx shared.Context, req *collisionEmptyRequest) (*ledger, error) {
			return &ledger{}, nil
		}))

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)

	_, ledgerSchema := refTarget(t, spec, "/ledger")

	resolve := func(prop string) *Schema {
		field := ledgerSchema.Properties[prop]
		require.NotNil(t, field, "missing property %q", prop)

		ref := field.Ref
		if ref == "" && field.Items != nil {
			ref = field.Items.Ref
		}

		require.NotEmpty(t, ref, "property %q is not a component reference", prop)

		target, ok := spec.Components.Schemas[componentNameFromRef(ref)]
		require.True(t, ok, "dangling $ref %s", ref)

		return target
	}

	requireProperty(t, resolve("billed"), "invoice_number")
	requireProperty(t, resolve("shipped"), "tracking_code")
	requireProperty(t, resolve("stored"), "bin_location")

	require.NotContains(t, spec.Components.Schemas, "Invoice")
	requireEveryRefResolves(t, spec)
}

// TestOpenAPI_CollisionIsReported pins requirement 4: a qualification is never
// silent. The report names every colliding type and the name it received.
func TestOpenAPI_CollisionIsReported(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Report", Version: "1.0.0"}))

	require.NoError(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))
	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))

	impl, ok := r.(*router)
	require.True(t, ok)

	gen, ok := impl.openAPIGenerator.(*openAPIGenerator)
	require.True(t, ok)

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)

	reports := gen.schemas.nameCollisions
	// One line per contested bare name: Invoice, and the Note nested in both.
	require.Len(t, reports, 2)

	var invoiceReport string

	for _, report := range reports {
		if strings.Contains(report, `name "Invoice"`) {
			invoiceReport = report
		}
	}

	require.NotEmpty(t, invoiceReport, "no report for the contested Invoice name: %v", reports)

	billingName, _ := refTarget(t, spec, "/billing/invoice")
	shippingName, _ := refTarget(t, spec, "/shipping/invoice")

	for _, want := range []string{
		"testtypes/billing.Invoice",
		"testtypes/shipping.Invoice",
		billingName,
		shippingName,
	} {
		require.Contains(t, invoiceReport, want)
	}

	// Regenerating must not repeat the report on every request for the spec.
	r.OpenAPISpec()
	require.Len(t, gen.schemas.nameCollisions, 2)
}

// TestOpenAPI_CollisionIntroducedAfterFirstGeneration covers a route registered
// after the document has already been generated once. The component built by
// the earlier pass must not be reused with references that predate the rename.
func TestOpenAPI_CollisionIntroducedAfterFirstGeneration(t *testing.T) {
	type wrapper struct {
		Billed *billing.Invoice `json:"billed"`
	}

	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Late", Version: "1.0.0"}))

	require.NoError(t, r.GET("/wrapper",
		func(ctx shared.Context, req *collisionEmptyRequest) (*wrapper, error) {
			return &wrapper{}, nil
		}))

	first := r.OpenAPISpec()
	require.Contains(t, first.Components.Schemas, "Invoice")
	requireEveryRefResolves(t, first)

	// A colliding type arrives only now.
	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))

	second := r.OpenAPISpec()
	requireEveryRefResolves(t, second)
	require.NotContains(t, second.Components.Schemas, "Invoice")

	_, wrapperSchema := refTarget(t, second, "/wrapper")
	billed := wrapperSchema.Properties["billed"]
	require.NotNil(t, billed)
	require.NotEmpty(t, billed.Ref)

	target, ok := second.Components.Schemas[componentNameFromRef(billed.Ref)]
	require.True(t, ok, "nested $ref %s went stale across regeneration", billed.Ref)
	requireProperty(t, target, "invoice_number")
}

// TestOpenAPI_NestedComponentSurvivesLateCollision is the same scenario one
// level deeper: the renamed component is referenced only from inside another
// component's schema, which is exactly the reference a per-document rewrite
// would miss if schemas built for an earlier document were reused.
func TestOpenAPI_NestedComponentSurvivesLateCollision(t *testing.T) {
	type holder struct {
		Billed *billing.Invoice `json:"billed"`
	}

	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "LateNested", Version: "1.0.0"}))

	require.NoError(t, r.GET("/holder",
		func(ctx shared.Context, req *collisionEmptyRequest) (*holder, error) {
			return &holder{}, nil
		}))

	first := r.OpenAPISpec()
	require.Contains(t, first.Components.Schemas, "Note")
	requireEveryRefResolves(t, first)

	require.NoError(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *collisionEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))

	second := r.OpenAPISpec()
	requireEveryRefResolves(t, second)
	require.NotContains(t, second.Components.Schemas, "Note")

	_, holderSchema := refTarget(t, second, "/holder")
	invoice := second.Components.Schemas[componentNameFromRef(holderSchema.Properties["billed"].Ref)]
	require.NotNil(t, invoice)

	note := invoice.Properties["note"]
	require.NotNil(t, note)

	target, ok := second.Components.Schemas[componentNameFromRef(note.Ref)]
	require.True(t, ok, "nested $ref %s went stale across regeneration", note.Ref)
	requireProperty(t, target, "memo")
}

// TestOpenAPI_ExplicitNameClaimedTwiceIsReported covers the one collision this
// scheme cannot resolve: two types explicitly named the same thing. The name is
// the user's, so it is honoured as given -- but not silently.
func TestOpenAPI_ExplicitNameClaimedTwiceIsReported(t *testing.T) {
	type firstResp struct {
		ETag string          `header:"ETag"`
		Body billing.Invoice `body:""       json:"body" schema:"Shared"`
	}

	type secondResp struct {
		ETag string           `header:"ETag"`
		Body shipping.Invoice `body:""       json:"body" schema:"Shared"`
	}

	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Pinned", Version: "1.0.0"}))

	require.NoError(t, r.GET("/first", func(ctx Context) error { return nil },
		WithResponseSchema(200, "first", firstResp{})))
	require.NoError(t, r.GET("/second", func(ctx Context) error { return nil },
		WithResponseSchema(200, "second", secondResp{})))

	impl, ok := r.(*router)
	require.True(t, ok)

	gen, ok := impl.openAPIGenerator.(*openAPIGenerator)
	require.True(t, ok)

	spec := r.OpenAPISpec()
	require.NotNil(t, spec)
	require.Contains(t, spec.Components.Schemas, "Shared")
	requireEveryRefResolves(t, spec)

	var conflict string

	for _, report := range gen.schemas.nameCollisions {
		if strings.Contains(report, `"Shared"`) {
			conflict = report
		}
	}

	require.NotEmpty(t, conflict, "an unresolvable explicit-name clash must be reported: %v",
		gen.schemas.nameCollisions)
	require.Contains(t, conflict, "testtypes/billing.Invoice")
	require.Contains(t, conflict, "testtypes/shipping.Invoice")
}

func TestSanitizeComponentName(t *testing.T) {
	cases := map[string]string{
		"github.com/xraph/forge/internal/router.Invoice": "github_com_xraph_forge_internal_router_Invoice",
		"github.com/go-chi/chi/v5.Mux":                   "github_com_go_chi_chi_v5_Mux",
		"Invoice":                                        "Invoice",
	}

	for in, want := range cases {
		require.Equal(t, want, sanitizeComponentName(in))
		require.Regexp(t, `^[a-zA-Z0-9._-]+$`, sanitizeComponentName(in),
			"component names must match the OpenAPI charset")
	}
}
