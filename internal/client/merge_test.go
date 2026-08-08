package client_test

import (
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
