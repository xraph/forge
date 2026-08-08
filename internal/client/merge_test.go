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
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
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
