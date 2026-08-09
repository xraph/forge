package client_test

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func restSpec() *client.APISpec {
	return &client.APISpec{
		Kind:      client.SourceOpenAPI,
		Info:      client.APIInfo{Title: "Orders", Version: "1.0.0"},
		Servers:   []client.Server{{URL: "https://api.example.com"}},
		Endpoints: []client.Endpoint{{OperationID: "listOrders", Path: "/orders", Method: "GET"}},
		Schemas:   map[string]*client.Schema{"Order": {Type: "object"}},
		Entities:  map[string]*client.EntityRef{"Order": {Type: "Order", IDField: "id"}},
		Tags:      []client.Tag{{Name: "orders"}},
	}
}

func streamSpec() *client.APISpec {
	return &client.APISpec{
		Kind:       client.SourceAsyncAPI,
		Info:       client.APIInfo{Title: "Orders Streams", Version: "2.0.0"},
		Servers:    []client.Server{{URL: "wss://api.example.com"}},
		WebSockets: []client.WebSocketEndpoint{{Path: "/ws/orders"}},
		Schemas:    map[string]*client.Schema{"OrderEvent": {Type: "object"}},
		Tags:       []client.Tag{{Name: "orders"}},
	}
}

func TestMergeSpecsNilAndEmpty(t *testing.T) {
	if got := client.MergeSpecs(); got != nil {
		t.Fatalf("MergeSpecs() with no specs = %v, want nil", got)
	}
	if got := client.MergeSpecs(nil, nil); got != nil {
		t.Fatalf("MergeSpecs(nil, nil) = %v, want nil", got)
	}
}

func TestMergeSpecsSingleSpecIsIdentity(t *testing.T) {
	in := restSpec()
	got := client.MergeSpecs(in)
	if got != in {
		t.Fatalf("MergeSpecs(one) must return that same spec unchanged")
	}
}

func TestMergeSpecsUnionsEndpointsAndStreams(t *testing.T) {
	got := client.MergeSpecs(restSpec(), streamSpec())

	if len(got.Endpoints) != 1 || got.Endpoints[0].OperationID != "listOrders" {
		t.Errorf("Endpoints = %v, want the one REST endpoint", got.Endpoints)
	}
	if len(got.WebSockets) != 1 || got.WebSockets[0].Path != "/ws/orders" {
		t.Errorf("WebSockets = %v, want the one stream endpoint", got.WebSockets)
	}
	if len(got.Schemas) != 2 {
		t.Errorf("Schemas has %d entries, want 2 (Order, OrderEvent)", len(got.Schemas))
	}
	if len(got.Servers) != 2 {
		t.Errorf("Servers has %d entries, want 2 distinct URLs", len(got.Servers))
	}
	if len(got.Tags) != 1 {
		t.Errorf("Tags has %d entries, want 1 after dedup by name", len(got.Tags))
	}
	if got.Info.Title != "Orders" {
		t.Errorf("Info.Title = %q, want the OpenAPI document's title", got.Info.Title)
	}
	if got.RoutingTypes != nil {
		t.Errorf("RoutingTypes must be nil after merge; resolveEntityFields rebuilds it")
	}
}

func TestMergeSpecsOrdersByDocumentKindNotArgumentOrder(t *testing.T) {
	forward := client.MergeSpecs(restSpec(), streamSpec())
	reverse := client.MergeSpecs(streamSpec(), restSpec())

	if forward.Info.Title != reverse.Info.Title {
		t.Errorf("Info.Title differs by argument order: %q vs %q", forward.Info.Title, reverse.Info.Title)
	}
	if len(forward.Endpoints) != len(reverse.Endpoints) {
		t.Errorf("Endpoints count differs by argument order")
	}
	if forward.Servers[0].URL != reverse.Servers[0].URL {
		t.Errorf("Servers order differs by argument order: %q vs %q",
			forward.Servers[0].URL, reverse.Servers[0].URL)
	}
}

func TestMergeSpecsSameKindPrecedenceFollowsArgumentOrder(t *testing.T) {
	first := func() *client.APISpec {
		return &client.APISpec{
			Kind:    client.SourceOpenAPI,
			Info:    client.APIInfo{Title: "First"},
			Schemas: map[string]*client.Schema{"Order": {Type: "object"}},
		}
	}
	second := func() *client.APISpec {
		return &client.APISpec{
			Kind:    client.SourceOpenAPI,
			Info:    client.APIInfo{Title: "Second"},
			Schemas: map[string]*client.Schema{"Order": {Type: "string"}},
		}
	}

	forward := client.MergeSpecs(first(), second())
	if forward.Schemas["Order"].Type != "object" {
		t.Errorf("first-passed source must win: got %q, want %q", forward.Schemas["Order"].Type, "object")
	}
	if forward.Info.Title != "First" {
		t.Errorf("Info.Title = %q, want %q", forward.Info.Title, "First")
	}

	reverse := client.MergeSpecs(second(), first())
	if reverse.Schemas["Order"].Type != "string" {
		t.Errorf("first-passed source must win when reversed: got %q, want %q", reverse.Schemas["Order"].Type, "string")
	}
}

func hasWarningContaining(warnings []string, substr string) bool {
	return warningContaining(warnings, substr) != ""
}

// warningContaining returns the first warning mentioning substr, or "" when
// none does -- for assertions about a warning's wording, not merely its
// existence.
func warningContaining(warnings []string, substr string) string {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return w
		}
	}

	return ""
}

func TestMergeSpecsIdenticalRedeclarationIsSilent(t *testing.T) {
	a := restSpec()
	b := streamSpec()
	// Same name, structurally identical: the normal case, not a conflict.
	b.Schemas["Order"] = &client.Schema{Type: "object"}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("identical redeclaration must not warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingSchemaShape(t *testing.T) {
	a := restSpec()
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "string"} // genuinely different

	got := client.MergeSpecs(a, b)

	if got.Schemas["Order"].Type != "object" {
		t.Errorf("Schemas[Order].Type = %q, want the OpenAPI shape %q",
			got.Schemas["Order"].Type, "object")
	}
	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing schema shape must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingEntityIDField(t *testing.T) {
	a := restSpec()
	b := streamSpec()
	b.Entities = map[string]*client.EntityRef{
		"Order": {Type: "Order", IDField: "orderId"},
	}

	got := client.MergeSpecs(a, b)

	if got.Entities["Order"].IDField != "id" {
		t.Errorf("Entities[Order].IDField = %q, want the OpenAPI value %q",
			got.Entities["Order"].IDField, "id")
	}
	if !hasWarningContaining(got.Warnings, "orderId") {
		t.Errorf("differing IDField must warn naming both values, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDuplicateRoute(t *testing.T) {
	a := restSpec()
	b := restSpec()
	b.Info.Title = "Second"

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "GET /orders") {
		t.Errorf("duplicate path+method must warn, got %v", got.Warnings)
	}
}

// The warning above says the first declaration wins; this is the assertion
// that it does. Leaving both endpoints in place makes every generator emit
// the operation twice -- two `func (c *Client) OrderList(...)` in the Go
// client, which does not compile.
func TestMergeSpecsDropsDuplicateRoutes(t *testing.T) {
	a := restSpec()
	b := restSpec()
	b.Info.Title = "Second"
	b.Endpoints[0].OperationID = "listOrdersAgain"

	got := client.MergeSpecs(a, b)

	if len(got.Endpoints) != 1 {
		t.Fatalf("Endpoints has %d entries, want 1 -- the duplicate route must be dropped, got %+v",
			len(got.Endpoints), got.Endpoints)
	}
	if got.Endpoints[0].OperationID != "listOrders" {
		t.Errorf("kept endpoint is %q, want the first source's %q",
			got.Endpoints[0].OperationID, "listOrders")
	}
}

// The same class of duplicate, on the stream side: two AsyncAPI documents
// declaring the same channel produce two generated stream clients with one
// name between them.
func TestMergeSpecsDropsDuplicateStreamEndpoints(t *testing.T) {
	a := streamSpec()
	a.WebSockets = []client.WebSocketEndpoint{{ID: "orderEvents", Path: "/ws/orders"}}
	a.SSEs = []client.SSEEndpoint{{ID: "orderFeed", Path: "/sse/orders"}}
	a.WebTransports = []client.WebTransportEndpoint{{ID: "orderBulk", Path: "/wt/orders"}}

	b := streamSpec()
	b.Info.Title = "Second"
	// Same declared ids, different addresses: the id is what names the
	// generated client, so this is the collision, not the address.
	b.WebSockets = []client.WebSocketEndpoint{{ID: "orderEvents", Path: "/ws/v2/orders"}}
	b.SSEs = []client.SSEEndpoint{{ID: "orderFeed", Path: "/sse/v2/orders"}}
	b.WebTransports = []client.WebTransportEndpoint{{ID: "orderBulk", Path: "/wt/v2/orders"}}

	got := client.MergeSpecs(a, b)

	if len(got.WebSockets) != 1 || got.WebSockets[0].Path != "/ws/orders" {
		t.Errorf("WebSockets = %+v, want only the first declaration", got.WebSockets)
	}
	if len(got.SSEs) != 1 || got.SSEs[0].Path != "/sse/orders" {
		t.Errorf("SSEs = %+v, want only the first declaration", got.SSEs)
	}
	if len(got.WebTransports) != 1 || got.WebTransports[0].Path != "/wt/orders" {
		t.Errorf("WebTransports = %+v, want only the first declaration", got.WebTransports)
	}

	for _, id := range []string{"orderEvents", "orderFeed", "orderBulk"} {
		if !hasWarningContaining(got.Warnings, id) {
			t.Errorf("dropping %q must warn, got %v", id, got.Warnings)
		}
	}
}

// A stream endpoint with no declared id falls back to its address, so two
// documents describing the same anonymous channel still collapse.
func TestMergeSpecsDropsDuplicateUnnamedStreamEndpoints(t *testing.T) {
	got := client.MergeSpecs(streamSpec(), streamSpec())

	if len(got.WebSockets) != 1 {
		t.Fatalf("WebSockets has %d entries, want 1, got %+v", len(got.WebSockets), got.WebSockets)
	}
	if !hasWarningContaining(got.Warnings, "/ws/orders") {
		t.Errorf("dropping an unnamed duplicate channel must warn, got %v", got.Warnings)
	}
}

// Two distinctly named channels that happen to share an address are two
// endpoints, not one. A single AsyncAPI document may declare them (channels
// are keyed by channel name, not by address) and the parser keeps both, so
// the merge must not start dropping one the moment a second document exists.
func TestMergeSpecsKeepsDistinctStreamsSharingAnAddress(t *testing.T) {
	a := streamSpec()
	a.WebSockets = []client.WebSocketEndpoint{{ID: "orderCreated", Path: "/ws/orders"}}

	b := streamSpec()
	b.Info.Title = "Second"
	b.WebSockets = []client.WebSocketEndpoint{{ID: "orderCancelled", Path: "/ws/orders"}}

	got := client.MergeSpecs(a, b)

	if len(got.WebSockets) != 2 {
		t.Errorf("WebSockets has %d entries, want both distinctly named channels: %+v",
			len(got.WebSockets), got.WebSockets)
	}
}

// The collision warning must name the source the KEPT definition actually
// came from. The first source is silent about Feed here, so the definition
// that wins comes from an AsyncAPI document -- reporting the merge result's
// own kind would call it the OpenAPI one.
func TestMergeSpecsWarningNamesTheSourceTheKeptSchemaCameFrom(t *testing.T) {
	rest := restSpec() // OpenAPI, and says nothing about Feed

	first := streamSpec()
	first.Schemas["Feed"] = &client.Schema{Type: "object"}

	second := streamSpec()
	second.Info.Title = "Second"
	second.Schemas["Feed"] = &client.Schema{Type: "string"}

	got := client.MergeSpecs(rest, first, second)

	warning := warningContaining(got.Warnings, "Feed")
	if warning == "" {
		t.Fatalf("differing Feed schema must warn, got %v", got.Warnings)
	}
	if !strings.Contains(warning, "keeping the AsyncAPI definition") {
		t.Errorf("warning = %q, want it to name AsyncAPI as the source of the kept definition", warning)
	}
}

// The same, for the entity id-field collision warning.
func TestMergeSpecsWarningNamesTheSourceTheKeptEntityCameFrom(t *testing.T) {
	rest := restSpec() // OpenAPI, and registers no Feed entity

	first := streamSpec()
	first.Entities = map[string]*client.EntityRef{"Feed": {Type: "Feed", IDField: "id"}}

	second := streamSpec()
	second.Info.Title = "Second"
	second.Entities = map[string]*client.EntityRef{"Feed": {Type: "Feed", IDField: "feedId"}}

	got := client.MergeSpecs(rest, first, second)

	warning := warningContaining(got.Warnings, "Feed")
	if warning == "" {
		t.Fatalf("differing Feed id field must warn, got %v", got.Warnings)
	}
	if !strings.Contains(warning, `"id" in the AsyncAPI source`) {
		t.Errorf("warning = %q, want it to attribute the kept id field to the AsyncAPI source", warning)
	}
}

func TestMergeSpecsWarnsOnDifferingEnum(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{"open", "closed"}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{"open", "closed", "archived"}}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing enum values must warn, got %v", got.Warnings)
	}
}

// The enum test above differs in LENGTH, so sameEnum's length check alone
// decides it and the elementwise comparison is never load-bearing. These two
// enums are the same length and differ only in a value, which nothing but the
// per-element comparison can catch.
func TestMergeSpecsWarnsOnSameLengthEnumWithADifferentValue(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{"open", "closed"}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{"open", "cancelled"}}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("an enum value that differs at the same length must warn, got %v", got.Warnings)
	}
}

// The case sameScalar's defer/recover guard exists for: an enum element that
// is itself a slice (JSON allows it, and a document is free to write one).
// Comparing two of those with == panics, so this both proves the guard is
// there -- a panic fails the test -- and that a difference underneath it is
// still reported rather than swallowed into "these are the same".
func TestMergeSpecsWarnsOnDifferingNonComparableEnumElements(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{[]any{"open"}}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{[]any{"closed"}}}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing non-comparable enum elements must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsIdenticalEnumIsSilent(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{"open", "closed"}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "string", Enum: []any{"open", "closed"}}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("identical enum values must not warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingRef(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Ref: "#/components/schemas/OrderV1"}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Ref: "#/components/schemas/OrderV2"}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing $ref must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsIdenticalRefIsSilent(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Ref: "#/components/schemas/OrderV1"}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Ref: "#/components/schemas/OrderV1"}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("identical $ref must not warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingAdditionalProperties(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "object", AdditionalProperties: false}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "object", AdditionalProperties: true}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing AdditionalProperties must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsIdenticalAdditionalPropertiesIsSilent(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "object", AdditionalProperties: false}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "object", AdditionalProperties: false}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("identical AdditionalProperties must not warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingOneOfLength(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{OneOf: []*client.Schema{{Type: "string"}}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{OneOf: []*client.Schema{{Type: "string"}, {Type: "integer"}}}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing OneOf length must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsIdenticalOneOfIsSilent(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{OneOf: []*client.Schema{{Type: "string"}, {Type: "integer"}}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{OneOf: []*client.Schema{{Type: "string"}, {Type: "integer"}}}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("identical OneOf must not warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnDifferingDiscriminatorPropertyName(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "object", Discriminator: &client.Discriminator{PropertyName: "type"}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "object", Discriminator: &client.Discriminator{PropertyName: "kind"}}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("differing Discriminator.PropertyName must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsIdenticalDiscriminatorIsSilent(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "object", Discriminator: &client.Discriminator{PropertyName: "type"}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "object", Discriminator: &client.Discriminator{PropertyName: "type"}}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("identical Discriminator must not warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsWarnsOnRequiredDuplicateCountMismatch(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "object", Required: []string{"x", "y"}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "object", Required: []string{"x", "x"}}

	got := client.MergeSpecs(a, b)

	if !hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("required lists of equal length but differing duplicate counts must warn, got %v", got.Warnings)
	}
}

func TestMergeSpecsRequiredOrderIsIgnored(t *testing.T) {
	a := restSpec()
	a.Schemas["Order"] = &client.Schema{Type: "object", Required: []string{"x", "y"}}
	b := streamSpec()
	b.Schemas["Order"] = &client.Schema{Type: "object", Required: []string{"y", "x"}}

	got := client.MergeSpecs(a, b)

	if hasWarningContaining(got.Warnings, "Order") {
		t.Errorf("required lists with the same members in a different order must not warn, got %v", got.Warnings)
	}
}
