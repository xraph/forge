package client

import (
	"context"
	"strings"
	"testing"
)

// Every case here goes through SpecParser.ParseFile on a real file on disk.
// None of them hand-builds an APISpec: the classification is only as good as
// what the parser actually produces from a document a user wrote, and a test
// that assembles the intermediate representation itself agrees with the differ
// about a shape the parser may never emit.

// diffFiles parses two spec documents from disk and classifies them.
func diffFiles(t *testing.T, oldDoc, newDoc map[string]any) DiffReport {
	t.Helper()

	parser := NewSpecParser()

	oldSpec, err := parser.ParseFile(context.Background(), writeSpec(t, oldDoc))
	if err != nil {
		t.Fatalf("parse old spec: %v", err)
	}

	newSpec, err := parser.ParseFile(context.Background(), writeSpec(t, newDoc))
	if err != nil {
		t.Fatalf("parse new spec: %v", err)
	}

	return DiffSpecs(oldSpec, newSpec)
}

// requireChange asserts that exactly one bucket claims a change whose detail
// contains want, and returns it.
func requireChange(t *testing.T, report DiffReport, kind ChangeKind, want string) Change {
	t.Helper()

	for _, change := range report.Changes {
		if change.Kind == kind && strings.Contains(change.Detail, want) {
			return change
		}
	}

	t.Fatalf("no %s change whose detail contains %q\n%s", kind, want, formatReport(report))

	return Change{}
}

func requireNoChangeOfKind(t *testing.T, report DiffReport, kind ChangeKind) {
	t.Helper()

	for _, change := range report.Changes {
		if change.Kind == kind {
			t.Fatalf("unexpected %s change: %s -- %s\n%s", kind, change.Subject, change.Detail, formatReport(report))
		}
	}
}

func formatReport(report DiffReport) string {
	var sb strings.Builder

	for _, change := range report.Changes {
		sb.WriteString("  " + string(change.Kind) + " | " + change.Subject + " | " + change.Detail + "\n")
	}

	if sb.Len() == 0 {
		return "  (no changes)\n"
	}

	return sb.String()
}

// --- document builders -------------------------------------------------------
//
// Each returns a fresh map so a test can mutate one side without the other
// seeing it.

func jsonBody(schema map[string]any) map[string]any {
	return map[string]any{
		"content": map[string]any{
			"application/json": map[string]any{"schema": schema},
		},
	}
}

func orderComponentSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":    map[string]any{"type": "string"},
			"total": map[string]any{"type": "integer"},
		},
	}
}

// ordersDoc is the baseline both sides of most cases start from: a list
// endpoint (which makes Order an entity with an inferred id) and a create
// endpoint carrying a request body.
func ordersDoc() map[string]any {
	listResponse := jsonBody(map[string]any{
		"type":  "array",
		"items": map[string]any{"$ref": "#/components/schemas/Order"},
	})
	listResponse["description"] = "ok"

	createResponse := jsonBody(map[string]any{"$ref": "#/components/schemas/Order"})
	createResponse["description"] = "created"

	createRequest := jsonBody(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"total": map[string]any{"type": "integer"},
		},
	})
	createRequest["required"] = true

	return map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{"Order": orderComponentSchema()},
		},
		"paths": map[string]any{
			"/orders": map[string]any{
				"get": map[string]any{
					"operationId": "orderList",
					"responses":   map[string]any{"200": listResponse},
				},
				"post": map[string]any{
					"operationId": "orderCreate",
					"requestBody": createRequest,
					"responses":   map[string]any{"201": createResponse},
				},
			},
		},
	}
}

func paths(doc map[string]any) map[string]any {
	return doc["paths"].(map[string]any)
}

func operation(doc map[string]any, path, method string) map[string]any {
	return paths(doc)[path].(map[string]any)[method].(map[string]any)
}

func componentSchemas(doc map[string]any) map[string]any {
	return doc["components"].(map[string]any)["schemas"].(map[string]any)
}

func requestProperties(doc map[string]any, path, method string) map[string]any {
	op := operation(doc, path, method)
	body := op["requestBody"].(map[string]any)
	schema := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)

	return schema["properties"].(map[string]any)
}

// param builds one OpenAPI parameter object.
func param(name, in string, required bool, schema map[string]any) map[string]any {
	return map[string]any{
		"name":     name,
		"in":       in,
		"required": required,
		"schema":   schema,
	}
}

// withParams attaches a parameter list to an operation.
func withParams(doc map[string]any, path, method string, params ...map[string]any) map[string]any {
	list := make([]any, 0, len(params))
	for _, p := range params {
		list = append(list, p)
	}

	operation(doc, path, method)["parameters"] = list

	return doc
}

func stringSchema() map[string]any { return map[string]any{"type": "string"} }

// --- parameters --------------------------------------------------------------
//
// Path, query and header parameters are request inputs exactly as much as body
// fields are. They went undiffed in the first cut of this differ, which meant
// adding a required query parameter -- the single most routine breaking change
// there is, and a hard signature break for a generated client -- reported "no
// changes" and exited 0. A gate that greenlights that is worse than no gate.

func TestDiffAddedRequiredQueryParameterIsBreaking(t *testing.T) {
	newDoc := withParams(ordersDoc(), "/orders", "get", param("tenant", "query", true, stringSchema()))

	report := diffFiles(t, ordersDoc(), newDoc)

	change := requireChange(t, report, ChangeBreakingAPI, `added required query parameter "tenant"`)
	if change.Subject != "GET /orders" {
		t.Fatalf("subject = %q, want GET /orders", change.Subject)
	}
}

func TestDiffAddedOptionalQueryParameterIsCompatible(t *testing.T) {
	newDoc := withParams(ordersDoc(), "/orders", "get", param("cursor", "query", false, stringSchema()))

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeCompatible, `added optional query parameter "cursor"`)
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffRemovedQueryParameterIsBreaking(t *testing.T) {
	oldDoc := withParams(ordersDoc(), "/orders", "get", param("status", "query", false, stringSchema()))

	report := diffFiles(t, oldDoc, ordersDoc())

	requireChange(t, report, ChangeBreakingAPI, `removed query parameter "status"`)
}

func TestDiffParameterBecomingRequiredIsBreaking(t *testing.T) {
	oldDoc := withParams(ordersDoc(), "/orders", "get", param("tenant", "query", false, stringSchema()))
	newDoc := withParams(ordersDoc(), "/orders", "get", param("tenant", "query", true, stringSchema()))

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeBreakingAPI, `query parameter "tenant" became required`)
}

func TestDiffParameterBecomingOptionalIsCompatible(t *testing.T) {
	oldDoc := withParams(ordersDoc(), "/orders", "get", param("tenant", "query", true, stringSchema()))
	newDoc := withParams(ordersDoc(), "/orders", "get", param("tenant", "query", false, stringSchema()))

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeCompatible, `query parameter "tenant" became optional`)
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffNarrowedParameterTypeIsBreaking(t *testing.T) {
	oldDoc := withParams(ordersDoc(), "/orders", "get",
		param("limit", "query", false, map[string]any{"type": "number"}))
	newDoc := withParams(ordersDoc(), "/orders", "get",
		param("limit", "query", false, map[string]any{"type": "integer"}))

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeBreakingAPI, `query parameter "limit": type narrowed number -> integer`)
}

func TestDiffWidenedParameterTypeIsCompatible(t *testing.T) {
	oldDoc := withParams(ordersDoc(), "/orders", "get",
		param("limit", "query", false, map[string]any{"type": "integer"}))
	newDoc := withParams(ordersDoc(), "/orders", "get",
		param("limit", "query", false, map[string]any{"type": "number"}))

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeCompatible, `query parameter "limit": type widened integer -> number`)
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

// A parameter is identified by location AND name. Moving "tenant" from the
// query string to a header is a removal plus an addition, not "no change" --
// and a differ that keyed on the name alone would compare a query parameter
// against a header and report nothing at all.
func TestDiffParameterLocationIsPartOfItsIdentity(t *testing.T) {
	oldDoc := withParams(ordersDoc(), "/orders", "get", param("tenant", "query", true, stringSchema()))
	newDoc := withParams(ordersDoc(), "/orders", "get", param("tenant", "header", true, stringSchema()))

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeBreakingAPI, `removed query parameter "tenant"`)
	requireChange(t, report, ChangeBreakingAPI, `added required header parameter "tenant"`)
}

// Two parameters that share a name in different locations must not collapse
// into one, or every endpoint carrying both would report a permanent spurious
// change.
func TestDiffSameNameInTwoLocationsIsNotSpuriousChange(t *testing.T) {
	build := func() map[string]any {
		return withParams(ordersDoc(), "/orders", "get",
			param("tenant", "query", true, stringSchema()),
			param("tenant", "header", true, stringSchema()))
	}

	report := diffFiles(t, build(), build())

	if len(report.Changes) != 0 {
		t.Fatalf("identical specs with a name shared across locations produced changes:\n%s", formatReport(report))
	}
}

// --- one-sided bodies --------------------------------------------------------

// Deleting a response's entire content block means the client stops getting a
// payload. This used to be skipped outright by an early return, so it printed
// "no changes" and exited 0.
func TestDiffRemovedResponseBodyIsReported(t *testing.T) {
	newDoc := ordersDoc()
	response := operation(newDoc, "/orders", "get")["responses"].(map[string]any)["200"].(map[string]any)

	delete(response, "content")

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeUnknown, "schema removed")
}

// The exact reported repro: a body with no fields at all, so no field-level
// comparison can stand in for the missing body-root check. Before the fix this
// pair produced an empty report.
func TestDiffRemovedFieldlessResponseBodyIsTheOnlyChange(t *testing.T) {
	build := func(withBody bool) map[string]any {
		response := map[string]any{"description": "ok"}
		if withBody {
			response["content"] = map[string]any{
				"text/plain": map[string]any{"schema": map[string]any{"type": "string"}},
			}
		}

		return map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]any{"title": "Health", "version": "1.0.0"},
			"paths": map[string]any{
				"/health": map[string]any{
					"get": map[string]any{
						"operationId": "healthGet",
						"responses":   map[string]any{"200": response},
					},
				},
			},
		}
	}

	report := diffFiles(t, build(true), build(false))

	if len(report.Changes) != 1 {
		t.Fatalf("want exactly one change for a vanished body, got:\n%s", formatReport(report))
	}

	requireChange(t, report, ChangeUnknown, "schema removed")
}

func TestDiffAddedRequestBodyIsReported(t *testing.T) {
	oldDoc := ordersDoc()
	delete(operation(oldDoc, "/orders", "post"), "requestBody")

	report := diffFiles(t, oldDoc, ordersDoc())

	requireChange(t, report, ChangeUnknown, "schema added where there was none")
}

// --- COMPATIBLE --------------------------------------------------------------

func TestDiffAddedEndpointIsCompatible(t *testing.T) {
	newDoc := ordersDoc()
	paths(newDoc)["/invoices"] = map[string]any{
		"get": map[string]any{
			"operationId": "invoiceList",
			"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
		},
	}

	report := diffFiles(t, ordersDoc(), newDoc)

	change := requireChange(t, report, ChangeCompatible, "added endpoint")
	if change.Subject != "GET /invoices" {
		t.Fatalf("subject = %q, want GET /invoices", change.Subject)
	}

	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffAddedOptionalRequestFieldIsCompatible(t *testing.T) {
	newDoc := ordersDoc()
	requestProperties(newDoc, "/orders", "post")["note"] = map[string]any{"type": "string"}

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeCompatible, `added optional request field "note"`)
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffAddedResponseFieldIsCompatible(t *testing.T) {
	newDoc := ordersDoc()
	componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any)["currency"] = map[string]any{"type": "string"}

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeCompatible, `added response 200 field "[].currency"`)
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffWidenedTypeIsCompatible(t *testing.T) {
	newDoc := ordersDoc()
	componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any)["total"] = map[string]any{"type": "number"}

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeCompatible, "type widened integer -> number")
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

// --- BREAKING (API) ----------------------------------------------------------

func TestDiffRemovedEndpointIsBreaking(t *testing.T) {
	oldDoc := ordersDoc()
	paths(oldDoc)["/invoices"] = map[string]any{
		"get": map[string]any{
			"operationId": "invoiceList",
			"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
		},
	}

	report := diffFiles(t, oldDoc, ordersDoc())

	change := requireChange(t, report, ChangeBreakingAPI, "removed endpoint")
	if change.Subject != "GET /invoices" {
		t.Fatalf("subject = %q, want GET /invoices", change.Subject)
	}
}

func TestDiffAddedRequiredRequestFieldIsBreaking(t *testing.T) {
	newDoc := ordersDoc()

	op := operation(newDoc, "/orders", "post")
	schema := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	schema["properties"].(map[string]any)["currency"] = map[string]any{"type": "string"}
	schema["required"] = []any{"currency"}

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeBreakingAPI, `added required request field "currency"`)
}

func TestDiffRemovedResponseFieldIsBreaking(t *testing.T) {
	newDoc := ordersDoc()
	delete(componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any), "total")

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeBreakingAPI, `removed response 200 field "[].total"`)
}

func TestDiffNarrowedTypeIsBreaking(t *testing.T) {
	oldDoc := ordersDoc()
	componentSchemas(oldDoc)["Order"].(map[string]any)["properties"].(map[string]any)["total"] = map[string]any{"type": "number"}

	report := diffFiles(t, oldDoc, ordersDoc())

	requireChange(t, report, ChangeBreakingAPI, "type narrowed number -> integer")
}

// --- BREAKING (CACHE) --------------------------------------------------------
//
// None of the four cases below changes a single byte on the wire. An API-only
// differ reports every one of them as "no change", which is exactly why they
// have their own column: the defect they cause shows up as a stale or
// duplicated record several screens away from the rename that caused it.

func TestDiffEntityTypenameChangeIsCacheBreaking(t *testing.T) {
	newDoc := ordersDoc()

	schemas := componentSchemas(newDoc)
	schemas["PurchaseOrder"] = schemas["Order"]

	delete(schemas, "Order")

	for _, method := range []string{"get", "post"} {
		op := operation(newDoc, "/orders", method)
		for _, resp := range op["responses"].(map[string]any) {
			replaceRef(resp.(map[string]any), "#/components/schemas/PurchaseOrder")
		}
	}

	report := diffFiles(t, ordersDoc(), newDoc)

	change := requireChange(t, report, ChangeBreakingCache, "entity typename changed Order -> PurchaseOrder")
	if change.Old != "Order" || change.New != "PurchaseOrder" {
		t.Fatalf("old/new = %q/%q, want Order/PurchaseOrder", change.Old, change.New)
	}

	// The HTTP contract is untouched: same paths, same methods, same fields.
	// If this ever starts reporting an API break, the cache column has stopped
	// being the thing that distinguishes this differ.
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)

	requireChange(t, report, ChangeBreakingCache, "entity type Order is gone")
}

func TestDiffEntityIDFieldChangeIsCacheBreaking(t *testing.T) {
	newDoc := ordersDoc()
	props := componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any)
	props["order_number"] = map[string]any{"type": "string", "x-forge-id": true}

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeBreakingCache, "id field changed id -> order_number")
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffEntityBecomingNonEntityIsCacheBreaking(t *testing.T) {
	newDoc := ordersDoc()
	operation(newDoc, "/orders", "get")["x-forge-no-entity"] = true

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeBreakingCache, "no longer an entity")
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffNonEntityBecomingEntityIsCacheBreaking(t *testing.T) {
	oldDoc := ordersDoc()
	operation(oldDoc, "/orders", "get")["x-forge-no-entity"] = true

	report := diffFiles(t, oldDoc, ordersDoc())

	requireChange(t, report, ChangeBreakingCache, "is now an entity")
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func TestDiffRemovedInvalidationTagIsCacheBreaking(t *testing.T) {
	oldDoc := ordersDoc()
	operation(oldDoc, "/orders", "post")["x-forge-invalidates"] = []any{"Inventory[]"}

	report := diffFiles(t, oldDoc, ordersDoc())

	change := requireChange(t, report, ChangeBreakingCache, `invalidates tag "Inventory[]" removed`)
	if change.Subject != "POST /orders" {
		t.Fatalf("subject = %q, want POST /orders", change.Subject)
	}

	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

// A renamed invalidation tag is a removal plus an addition. The removal is the
// half that matters: the write stops refetching the collection it changed.
func TestDiffRenamedInvalidationTagIsCacheBreaking(t *testing.T) {
	oldDoc := ordersDoc()
	operation(oldDoc, "/orders", "post")["x-forge-invalidates"] = []any{"Inventory[]"}

	newDoc := ordersDoc()
	operation(newDoc, "/orders", "post")["x-forge-invalidates"] = []any{"StockLevel[]"}

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeBreakingCache, `invalidates tag "Inventory[]" removed`)
	requireChange(t, report, ChangeCompatible, `invalidates tag "StockLevel[]" added`)
}

func TestDiffStreamBindingEntityChangeIsCacheBreaking(t *testing.T) {
	oldDoc := streamDoc("Order")
	newDoc := streamDoc("PurchaseOrder")

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeBreakingCache, "entity type changed Order -> PurchaseOrder")
	requireNoChangeOfKind(t, report, ChangeBreakingAPI)
}

func streamDoc(entityType string) map[string]any {
	return map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Orders Stream", "version": "1.0.0"},
		"channels": map[string]any{
			"orders": map[string]any{
				"address": "/orders",
				"x-forge-stream": []any{
					map[string]any{
						"message":     "orderUpdated",
						"entityType":  entityType,
						"intent":      "update",
						"invalidates": []any{entityType + "[]"},
					},
				},
			},
		},
		"operations": map[string]any{
			"receiveOrders": map[string]any{
				"action":  "receive",
				"channel": map[string]any{"$ref": "#/channels/orders"},
			},
		},
	}
}

// --- UNKNOWN -----------------------------------------------------------------

// A change the differ cannot prove is a widening or a narrowing is reported as
// UNKNOWN. Guessing here is worse than declining: a differ that misclassifies
// quietly is one nobody can gate on.
func TestDiffUnclassifiableTypeChangeIsUnknown(t *testing.T) {
	newDoc := ordersDoc()
	componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any)["total"] = map[string]any{
		"type":       "object",
		"properties": map[string]any{"amount": map[string]any{"type": "integer"}},
	}

	report := diffFiles(t, ordersDoc(), newDoc)

	requireChange(t, report, ChangeUnknown, "neither a widening nor a narrowing")
}

func TestDiffFormatReplacementIsUnknown(t *testing.T) {
	oldDoc := ordersDoc()
	componentSchemas(oldDoc)["Order"].(map[string]any)["properties"].(map[string]any)["id"] = map[string]any{
		"type": "string", "format": "uuid",
	}

	newDoc := ordersDoc()
	componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any)["id"] = map[string]any{
		"type": "string", "format": "email",
	}

	report := diffFiles(t, oldDoc, newDoc)

	requireChange(t, report, ChangeUnknown, "neither contains the other")
}

// --- no change and determinism -----------------------------------------------

func TestDiffIdenticalSpecsReportNothing(t *testing.T) {
	report := diffFiles(t, ordersDoc(), ordersDoc())

	if len(report.Changes) != 0 {
		t.Fatalf("identical specs produced changes:\n%s", formatReport(report))
	}

	if report.HasBreaking() || report.HasUnknown() {
		t.Fatalf("identical specs must be neither breaking nor unknown: %+v", report.Summary)
	}
}

// The report is pasted into pull requests and diffed against previous runs. Go
// randomizes map iteration, so an unsorted report would churn on every run and
// train everyone to ignore it.
func TestDiffOutputIsDeterministic(t *testing.T) {
	newDoc := ordersDoc()
	props := componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any)
	props["currency"] = map[string]any{"type": "string"}
	props["total"] = map[string]any{"type": "number"}

	paths(newDoc)["/invoices"] = map[string]any{
		"get": map[string]any{
			"operationId": "invoiceList",
			"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
		},
	}

	first := formatReport(diffFiles(t, ordersDoc(), newDoc))

	for i := 0; i < 8; i++ {
		again := formatReport(diffFiles(t, ordersDoc(), newDoc))
		if again != first {
			t.Fatalf("run %d differs from the first:\n--- first ---\n%s--- again ---\n%s", i, first, again)
		}
	}
}

// replaceRef rewrites the $ref of a response's JSON schema, following an array
// wrapper when there is one.
func replaceRef(response map[string]any, ref string) {
	content, ok := response["content"].(map[string]any)
	if !ok {
		return
	}

	media, ok := content["application/json"].(map[string]any)
	if !ok {
		return
	}

	schema, ok := media["schema"].(map[string]any)
	if !ok {
		return
	}

	if items, ok := schema["items"].(map[string]any); ok {
		items["$ref"] = ref

		return
	}

	schema["$ref"] = ref
}

// The blind spot this closed. `diffSpecEntities` used to compare Type and
// IDField only, so an edge appearing or vanishing was invisible to the very
// tool whose job is flagging cache-metadata changes.
//
// Losing `Order.customer -> Customer` changes no response byte and silently
// stops every nested Customer from being normalized: the runtime descends with
// no typename, writes no record, and a mutation invalidating `Customer:{id}`
// stops reaching the view showing it. That surfaces as a stale name on screen
// weeks later, with nothing in the diff to point at.
func TestDiffEntityFieldEdgeRemovalIsCacheBreaking(t *testing.T) {
	oldDoc := ordersWithCustomerDoc()

	newDoc := ordersWithCustomerDoc()
	delete(componentSchemas(newDoc)["Order"].(map[string]any)["properties"].(map[string]any), "customer")

	report := diffFiles(t, oldDoc, newDoc)

	change := requireChange(t, report, ChangeBreakingCache, "normalization field edges changed")
	if change.Old != "customer: Customer" || change.New != "" {
		t.Fatalf("old/new = %q/%q, want the edge on the old side only", change.Old, change.New)
	}

	// The HTTP contract change is real here (a response property is gone), but
	// the cache break is the one this assertion is about: it is reported as its
	// own change rather than folded into the field removal.
	if change.Category != CategoryEntity {
		t.Fatalf("category = %q, want %q", change.Category, CategoryEntity)
	}
}

// The same break arriving from the other side. A NEW edge lifts records that
// used to live inline into their own keyspace, so a persisted store written by
// the old client holds the same data in a shape the new one will not look for.
func TestDiffEntityFieldEdgeAdditionIsCacheBreaking(t *testing.T) {
	oldDoc := ordersWithCustomerDoc()
	delete(componentSchemas(oldDoc)["Order"].(map[string]any)["properties"].(map[string]any), "customer")

	report := diffFiles(t, oldDoc, ordersWithCustomerDoc())

	change := requireChange(t, report, ChangeBreakingCache, "normalization field edges changed")
	if change.Old != "" || change.New != "customer: Customer" {
		t.Fatalf("old/new = %q/%q, want the edge on the new side only", change.Old, change.New)
	}
}

// A routing type carries no identity, so its edges are the only thing it has --
// and they are load-bearing in exactly the way an entity's are. A `PageOrder`
// row losing `items: Order` stops every paginated read from normalizing
// anything, without touching a response schema, a tag, or an entity.
func TestDiffRoutingTypeFieldEdgeChangeIsCacheBreaking(t *testing.T) {
	oldDoc := envelopedOrdersDoc()

	newDoc := envelopedOrdersDoc()
	componentSchemas(newDoc)["PageOrder"].(map[string]any)["properties"].(map[string]any)["items"] =
		map[string]any{"type": "array", "items": map[string]any{"type": "string"}}

	report := diffFiles(t, oldDoc, newDoc)

	change := requireChange(t, report, ChangeBreakingCache, "normalization field edges changed")
	if change.Subject != "routing type PageOrder" {
		t.Fatalf("subject = %q, want the routing type", change.Subject)
	}
}

// An unchanged document reports no field-edge change. Field maps are Go maps
// and this report is read by humans deciding whether to ship; a differ that
// cries on every run is one nobody reads.
func TestDiffIdenticalSpecsReportNoFieldEdgeChange(t *testing.T) {
	report := diffFiles(t, envelopedOrdersDoc(), envelopedOrdersDoc())

	for _, change := range report.Changes {
		if strings.Contains(change.Detail, "field edges") {
			t.Fatalf("identical specs reported %+v", change)
		}
	}
}

// ordersWithCustomerDoc is ordersDoc plus a nested Customer entity, so there is
// a field edge to lose.
func ordersWithCustomerDoc() map[string]any {
	doc := ordersDoc()

	schemas := componentSchemas(doc)
	schemas["Customer"] = map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}},
	}

	schemas["Order"].(map[string]any)["properties"].(map[string]any)["customer"] =
		map[string]any{"$ref": "#/components/schemas/Customer"}

	customerResponse := jsonBody(map[string]any{"$ref": "#/components/schemas/Customer"})
	customerResponse["description"] = "ok"

	paths(doc)["/customers/{id}"] = map[string]any{
		"get": map[string]any{
			"operationId": "customerGet",
			"responses":   map[string]any{"200": customerResponse},
		},
	}

	return doc
}

// envelopedOrdersDoc returns the orders list through a declared envelope.
func envelopedOrdersDoc() map[string]any {
	doc := ordersDoc()

	componentSchemas(doc)["PageOrder"] = map[string]any{
		"type":             "object",
		"x-forge-envelope": true,
		"properties": map[string]any{
			"items": map[string]any{
				"type":  "array",
				"items": map[string]any{"$ref": "#/components/schemas/Order"},
			},
			"total": map[string]any{"type": "integer"},
		},
	}

	listResponse := jsonBody(map[string]any{"$ref": "#/components/schemas/PageOrder"})
	listResponse["description"] = "ok"

	operation(doc, "/orders", "get")["responses"] = map[string]any{"200": listResponse}

	return doc
}
