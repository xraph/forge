package router

// Golden-file tests for the generated OpenAPI and AsyncAPI documents.
//
// These are the only tests in the repository that compare a whole emitted
// document byte for byte. Everything else asserts one field at a time, which
// cannot see a change that adds, drops or reorders something nobody thought to
// assert on -- and three recent changes (component-name collision resolution,
// x-* extension hoisting on JSON marshal, and the new YAML marshallers) all
// rest on byte-stability of documents that were previously unguarded.
//
// To regenerate after an INTENTIONAL change:
//
//	go test ./internal/router -run TestGolden -update
//
// Then read the diff in testdata/ before committing it. A golden diff you
// cannot explain is a bug report, not a formatting nit.
//
// Nothing here normalises, sorts or post-processes the output: the bytes
// compared are exactly what encoding/json and gopkg.in/yaml.v3 hand a consumer
// (the JSON goldens are the indented form the generator's PrettyJSON config
// serves; indentation is a serialisation choice made by the caller, not a
// rewrite of the document).

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/xraph/forge/internal/router/testtypes/billing"
	"github.com/xraph/forge/internal/router/testtypes/shipping"
	"github.com/xraph/forge/internal/router/testtypes/warehouse"
	"github.com/xraph/forge/internal/shared"
)

// updateGolden regenerates the files under testdata/ instead of comparing
// against them. Without an update path, a legitimate change means hand-editing
// a large document, and people delete the test instead.
var updateGolden = flag.Bool("update", false, "update golden files")

// goldenGenerations is how many independently-built routers the determinism
// check compares. Fresh routers, not repeated calls on one: the generator
// caches per route-table revision, so calling OpenAPISpec() twice on the same
// router returns the same pointer and proves nothing.
const goldenGenerations = 4

// ---------------------------------------------------------------------------
// Fixture types.
//
// Each type below exists to pin one behaviour of the schema generator that a
// field-at-a-time assertion cannot see moving. Types colliding across packages
// come from internal/router/testtypes/{billing,shipping,warehouse}.
// ---------------------------------------------------------------------------

// goldenOrderStatus is an enum that names its own component (EnumNamer) and
// enumerates its own values (EnumValues). It pins enum component extraction and
// the fact that a user-pinned name survives the collision-resolution pass.
type goldenOrderStatus string

func (goldenOrderStatus) EnumValues() []any {
	return []any{"draft", "open", "closed"}
}

func (goldenOrderStatus) EnumComponentName() string { return "GoldenOrderStatus" }

func (s goldenOrderStatus) MarshalText() ([]byte, error) { return []byte(s), nil }

// goldenTimestamps is embedded from an UNEXPORTED-named type. reflect reports
// such a field as unexported (a field's name is its type name), but
// encoding/json still promotes its exported fields -- a divergence this repo
// specifically fixed, and one only a whole-document diff keeps fixed.
type goldenTimestamps struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	secret string //nolint:unused // present to prove genuinely unexported fields stay skipped
}

// GoldenAudit is embedded from an exported-named type: the ordinary promotion
// path, kept beside the unexported one so a regression in either is visible.
type GoldenAudit struct {
	Revision int `json:"revision"`
}

// goldenAddress is a plain nested struct, registered as its own component and
// referenced by $ref.
type goldenAddress struct {
	Street string `description:"Street address" json:"street"`
	City   string `json:"city"`
}

// goldenOrder is the workhorse response type: nested struct, both flavours of
// embedding, an enum property, and identity declared through the ForgeEntity
// interface rather than a tag. The interface names order_number, while the type
// also carries a property literally named `id` that is NOT the identity -- the
// exact case where an unhonoured ForgeEntity silently marks the wrong property.
type goldenOrder struct {
	goldenTimestamps
	GoldenAudit

	OrderNumber string            `json:"order_number"`
	ID          string            `json:"id"`
	Status      goldenOrderStatus `json:"status"`
	Address     goldenAddress     `json:"address"`
	Total       int               `description:"Total in minor units" json:"total"`
}

func (goldenOrder) ForgeEntity() EntityDef {
	return EntityDef{Type: "Order", IDField: "order_number"}
}

// goldenOrderRequest carries one parameter of every `in` the generator emits,
// so a change to parameter ordering, requiredness or enum-by-reference shows up.
type goldenOrderRequest struct {
	OrderID string            `description:"Order identifier" path:"orderId"`
	Status  goldenOrderStatus `query:"status"`
	Verbose bool              `optional:"true"                query:"verbose"`
	TraceID string            `header:"X-Trace-ID"            optional:"true"`
}

// goldenCreateOrder is a request BODY (no param tags at all), registered as its
// own component. `forge:"id"` marks identity by struct tag -- the other half of
// the identity story from ForgeEntity above.
type goldenCreateOrder struct {
	Reference string        `forge:"id"     json:"reference"`
	Address   goldenAddress `json:"address"`
	Total     int           `json:"total"`
}

// goldenSummary is a projection: identity by tag, but the route declares
// WithoutEntity(), so x-forge-no-entity must appear on the operation while
// x-forge-id still appears on the schema property.
type goldenSummary struct {
	SummaryID string `forge:"id"    json:"summary_id"`
	Orders    int    `json:"orders"`
}

// goldenWorkspace + goldenPage[T] pin generic component naming, which is where
// a naming change is most likely to go unnoticed.
type goldenWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type goldenPage[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// Stream payloads. Separate types from the REST ones so a channel schema
// leaking into the REST document (or vice versa) is visible.
type goldenOrderCommand struct {
	Action  string `json:"action"`
	OrderID string `json:"order_id"`
}

type goldenOrderEvent struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type goldenNotification struct {
	Kind string `enum:"info,warning,error" json:"kind"`
	Text string `json:"text"`
}

type goldenEmptyRequest struct{}

// goldenLegacyQueryRequest is a request struct carrying nothing but a query
// parameter, used by a route that does NOT declare WithRequestSchema. That
// combination is the legacy extraction path, and it is the one shape the rest
// of this fixture never exercised: every other legacy route here uses
// goldenEmptyRequest, and the only query-bearing route declares
// WithRequestSchema and so takes the unified path.
//
// Under the legacy path the whole struct went to GenerateSchema, which knows
// only about json tags -- so `query:"cursor"` was folded into the request BODY
// as a property named after the Go field, and no query parameter was emitted at
// all. A GET therefore carried a required body and silently lost its parameter.
// This type exists so the golden pins that boundary in both directions.
type goldenLegacyQueryRequest struct {
	Cursor string `query:"cursor"`
	Limit  int    `optional:"true" query:"limit"`
}

// goldenTemplatedQueryRequest is the same shape as goldenLegacyQueryRequest but
// is used on a route whose URL carries a {placeholder} the struct never
// mentions. It pins the union of the two sources of path parameters: the struct
// declares none, so `tenantId` can only come from the URL template.
//
// Routing plain handlers through the unified branch skipped
// extractPathParameters, which is the only thing that reads the template --
// leaving `{tenantId}` in the path with no parameter object declaring it, which
// OpenAPI 3.1 path templating forbids and which drops the argument from any
// generated client.
type goldenTemplatedQueryRequest struct {
	Cursor string `optional:"true" query:"cursor"`
}

// goldenPageCursor is a query-parameter base meant to be embedded, the ordinary
// way a codebase shares pagination across request types.
type goldenPageCursor struct {
	Cursor string `optional:"true" query:"cursor"`
}

// goldenEmbeddedQueryRequest reaches the legacy path's fold-query-into-body
// defect through an embed: its own fields carry only a json tag, and the query
// tag lives on the embedded goldenPageCursor. hasUnifiedTags inspects only
// top-level fields, so the gate says "no unified tags" and the whole struct --
// embedded field included -- goes to GenerateSchema, which promotes Cursor into
// the request BODY as a required property named after the Go field and emits no
// query parameter.
//
// extractUnifiedRequestComponents itself handles embedding correctly (its
// field.Anonymous branch recurses); it was only the gate that did not.
type goldenEmbeddedQueryRequest struct {
	goldenPageCursor

	Name string `json:"name"`
}

// goldenOptionalBody is a request body every one of whose properties is
// optional, so the generated component carries no `required` array at all. It
// exists to pin requestBody.required, which must be true regardless: a body
// that exists must be sent, even as {}. Deriving requestBody.required from
// schema.required flips this route to false and nothing else in the fixture
// notices, which is exactly how that error survived a golden run.
type goldenOptionalBody struct {
	Note  string `json:"note,omitempty"`
	Stars int    `json:"stars,omitempty"`
}

// goldenMixedTagRequest carries a query parameter AND a json body field on a
// plain handler, so it takes the unified branch by way of hasUnifiedTags. It
// pins the one shape where that reroute changes an otherwise-correct document:
// the body is emitted inline rather than as a named $ref component, because the
// unified extractor builds an anonymous object out of the body fields it
// selects. The content is right; the type name is what a client generator
// loses.
type goldenMixedTagRequest struct {
	Mode  string `optional:"true" query:"mode"`
	Title string `json:"title"`
}

// ---------------------------------------------------------------------------
// Fixture router.
// ---------------------------------------------------------------------------

// buildGoldenRouter returns a router carrying the whole fixture surface. It is
// called once per determinism iteration, so every call must build an
// independent router -- no package-level state, no shared maps.
func buildGoldenRouter(t *testing.T) Router {
	t.Helper()

	r := NewRouter(
		WithOpenAPI(OpenAPIConfig{
			Title:       "Forge Golden Fixture",
			Description: "Fixture API pinned by internal/router/openapi_golden_test.go",
			Version:     "1.0.0",
			Servers: []OpenAPIServer{
				{URL: "https://api.example.com", Description: "Production"},
			},
		}),
		WithAsyncAPI(AsyncAPIConfig{
			Title:       "Forge Golden Fixture Streams",
			Description: "Fixture channels pinned by internal/router/openapi_golden_test.go",
			Version:     "1.0.0",
			Servers: map[string]*shared.AsyncAPIServer{
				"production": {Host: "api.example.com", Protocol: "wss"},
			},
		}),
	)

	// Path + query + header params, an enum parameter, a nested struct, both
	// embedding flavours, and ForgeEntity-declared identity.
	must(t, r.GET("/orders/{orderId}",
		func(ctx shared.Context, req *goldenOrderRequest) (*goldenOrder, error) {
			return &goldenOrder{}, nil
		},
		WithSummary("Fetch one order"),
		WithTags("orders"),
		// The unified request schema is what splits path/query/header tags into
		// parameters; without it the whole struct becomes a request body and
		// the query and header declarations vanish silently.
		WithRequestSchema(&goldenOrderRequest{}),
	))

	// Request body component, plus the two operation extensions a route can
	// declare positively: x-forge-entity and x-forge-invalidates.
	must(t, r.POST("/orders",
		func(ctx shared.Context, req *goldenCreateOrder) (*goldenOrder, error) {
			return &goldenOrder{}, nil
		},
		WithSummary("Create an order"),
		WithTags("orders"),
		WithEntity(EntityDef{Type: "Order", IDField: "order_number"}),
		WithInvalidates("Order[]", "Dashboard"),
	))

	// The opt-out extension, on a response whose schema still carries x-forge-id.
	must(t, r.GET("/reports/summary",
		func(ctx shared.Context, req *goldenEmptyRequest) (*goldenSummary, error) {
			return &goldenSummary{}, nil
		},
		WithSummary("Order summary projection"),
		WithoutEntity(),
	))

	// Two same-named types from different packages: the contested name must be
	// qualified on BOTH sides and must not be handed to whoever registered first.
	must(t, r.GET("/billing/invoice",
		func(ctx shared.Context, req *goldenEmptyRequest) (*billing.Invoice, error) {
			return &billing.Invoice{}, nil
		}))
	must(t, r.GET("/shipping/invoice",
		func(ctx shared.Context, req *goldenEmptyRequest) (*shipping.Invoice, error) {
			return &shipping.Invoice{}, nil
		}))

	// The compatibility half of the same guarantee: an uncontested name stays
	// bare, byte-identical to what a pre-collision-resolution build emitted.
	must(t, r.GET("/warehouse/receipt",
		func(ctx shared.Context, req *goldenEmptyRequest) (*warehouse.Receipt, error) {
			return &warehouse.Receipt{}, nil
		}))

	// A plain handler -- no WithRequestSchema -- whose request struct is nothing
	// but query parameters. See goldenLegacyQueryRequest: this is the legacy
	// extraction path, and the only route in the fixture that takes it with
	// anything other than an empty request struct.
	must(t, r.GET("/audit/events",
		func(ctx shared.Context, req *goldenLegacyQueryRequest) (*goldenSummary, error) {
			return &goldenSummary{}, nil
		},
		WithSummary("List audit events"),
	))

	// Same legacy shape, but with a {placeholder} in the URL that the request
	// struct does not declare: the path parameter can only come from the
	// template, so this pins the union of the two sources.
	must(t, r.GET("/tenants/{tenantId}/items",
		func(ctx shared.Context, req *goldenTemplatedQueryRequest) (*goldenSummary, error) {
			return &goldenSummary{}, nil
		},
		WithSummary("List tenant items"),
	))

	// A plain handler whose query tag lives on an EMBEDDED struct: the same
	// fold-into-body defect GET /audit/events had, reached through an embed.
	must(t, r.POST("/reports/{reportId}/runs",
		func(ctx shared.Context, req *goldenEmbeddedQueryRequest) (*goldenSummary, error) {
			return &goldenSummary{}, nil
		},
		WithSummary("Start a report run"),
	))

	// A body whose every property is optional. requestBody.required must still
	// be true -- see goldenOptionalBody.
	must(t, r.POST("/feedback",
		func(ctx shared.Context, req *goldenOptionalBody) (*goldenSummary, error) {
			return &goldenSummary{}, nil
		},
		WithSummary("Submit feedback"),
	))

	// A plain handler mixing a query tag with a body field: takes the unified
	// branch, and its body is inline rather than a named component.
	must(t, r.POST("/notes",
		func(ctx shared.Context, req *goldenMixedTagRequest) (*goldenSummary, error) {
			return &goldenSummary{}, nil
		},
		WithSummary("Create a note"),
	))

	// Generic instantiation: component naming for parameterised types.
	must(t, r.GET("/workspaces",
		func(ctx shared.Context, req *goldenEmptyRequest) (*goldenPage[goldenWorkspace], error) {
			return &goldenPage[goldenWorkspace]{}, nil
		},
		WithSummary("List workspaces"),
	))

	// WebSocket channel: send/receive message pair plus x-forge-stream bindings
	// covering an upsert (created) and an evict (deleted).
	must(t, r.WebSocket("/ws/orders/{roomId}",
		func(ctx Context, conn Connection) error { return nil },
		WithWebSocketMessages(goldenOrderCommand{}, goldenOrderEvent{}),
		WithName("orders"),
		WithSummary("Order room"),
		WithStreamBinding(
			Emits[goldenOrder]("order.created"),
			Emits[goldenOrder]("order.deleted"),
		),
	))

	// SSE channel: receive-only, with a patch-intent binding so the third
	// StreamIntent is represented too.
	must(t, r.EventStream("/sse/notifications",
		func(ctx Context, stream Stream) error { return nil },
		WithSSEMessages(map[string]any{"notification": goldenNotification{}}),
		WithName("notifications"),
		WithSummary("Notification feed"),
		WithStreamBinding(Emits[goldenOrder]("order.progressed")),
	))

	return r
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("registering fixture route: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Serialisation.
// ---------------------------------------------------------------------------

func marshalJSONGolden(t *testing.T, doc any) []byte {
	t.Helper()

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshalling document to JSON: %v", err)
	}

	return append(raw, '\n')
}

func marshalYAMLGolden(t *testing.T, doc any) []byte {
	t.Helper()

	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling document to YAML: %v", err)
	}

	return raw
}

// goldenDocuments builds one router and returns its four serialisations, keyed
// by golden filename.
func goldenDocuments(t *testing.T) map[string][]byte {
	t.Helper()

	r := buildGoldenRouter(t)

	openAPI := r.OpenAPISpec()
	if openAPI == nil {
		t.Fatal("OpenAPISpec() returned nil; the fixture router did not enable OpenAPI")
	}

	asyncAPI := r.AsyncAPISpec()
	if asyncAPI == nil {
		t.Fatal("AsyncAPISpec() returned nil; the fixture router did not enable AsyncAPI")
	}

	return map[string][]byte{
		"openapi.json":  marshalJSONGolden(t, openAPI),
		"openapi.yaml":  marshalYAMLGolden(t, openAPI),
		"asyncapi.json": marshalJSONGolden(t, asyncAPI),
		"asyncapi.yaml": marshalYAMLGolden(t, asyncAPI),
	}
}

// ---------------------------------------------------------------------------
// Tests.
// ---------------------------------------------------------------------------

// TestGoldenSpecsAreDeterministic runs BEFORE any golden comparison matters: a
// nondeterministic generator makes the golden test fail intermittently, and an
// intermittently-failing golden test gets deleted rather than believed.
//
// Each iteration builds a brand-new router, so this exercises generation rather
// than re-reading the document the generator cached for a route-table revision.
func TestGoldenSpecsAreDeterministic(t *testing.T) {
	first := goldenDocuments(t)

	for run := 2; run <= goldenGenerations; run++ {
		next := goldenDocuments(t)

		for name, want := range first {
			got, ok := next[name]
			if !ok {
				t.Fatalf("run %d produced no %s", run, name)
			}

			if string(want) != string(got) {
				t.Errorf("%s is not deterministic: run 1 and run %d differ\n%s",
					name, run, diffReport(want, got, "run 1", fmt.Sprintf("run %d", run)))
			}
		}
	}
}

// TestGoldenSpecs is the byte diff itself.
func TestGoldenSpecs(t *testing.T) {
	docs := goldenDocuments(t)

	names := []string{"openapi.json", "openapi.yaml", "asyncapi.json", "asyncapi.yaml"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			compareGolden(t, name, docs[name])
		})
	}
}

func compareGolden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("creating testdata directory: %v", err)
		}

		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		t.Logf("updated %s (%d bytes)", path, len(got))

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v\n\nIf this file is missing, create it with:\n    %s",
			path, err, updateCommand())
	}

	if string(want) == string(got) {
		return
	}

	t.Errorf("generated document does not match %s\n%s\n\nIf this change is intentional, regenerate with:\n    %s",
		path, diffReport(want, got, "golden", "generated"), updateCommand())
}

func updateCommand() string {
	return "go test ./internal/router -run TestGolden -update"
}

// ---------------------------------------------------------------------------
// Diff reporting.
//
// "output differs" makes people regenerate blindly, which defeats the point of
// having a golden file at all. This prints the first differing line with
// surrounding context, and a count of how many lines differ overall.
// ---------------------------------------------------------------------------

const diffContextLines = 4

func diffReport(want, got []byte, wantLabel, gotLabel string) string {
	wantLines := strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
	gotLines := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")

	first := -1

	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		if lineAt(wantLines, i) != lineAt(gotLines, i) {
			first = i

			break
		}
	}

	var b strings.Builder

	fmt.Fprintf(&b, "  %s: %d lines, %d bytes\n", wantLabel, len(wantLines), len(want))
	fmt.Fprintf(&b, "  %s: %d lines, %d bytes\n", gotLabel, len(gotLines), len(got))

	if first < 0 {
		b.WriteString("  (contents match line for line; the difference is in trailing bytes)\n")

		return b.String()
	}

	differing := 0

	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		if lineAt(wantLines, i) != lineAt(gotLines, i) {
			differing++
		}
	}

	fmt.Fprintf(&b, "  first difference at line %d (%d differing lines in total)\n\n", first+1, differing)

	start := max(first-diffContextLines, 0)
	end := min(first+diffContextLines+1, max(len(wantLines), len(gotLines)))

	for i := start; i < end; i++ {
		w, g := lineAt(wantLines, i), lineAt(gotLines, i)
		if w == g {
			fmt.Fprintf(&b, "   %5d  %s\n", i+1, w)

			continue
		}

		if i < len(wantLines) {
			fmt.Fprintf(&b, "  -%5d  %s\n", i+1, w)
		}

		if i < len(gotLines) {
			fmt.Fprintf(&b, "  +%5d  %s\n", i+1, g)
		}
	}

	return b.String()
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}

	return ""
}
