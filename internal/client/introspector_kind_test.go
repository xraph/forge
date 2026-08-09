package client_test

import (
	"testing"

	"github.com/xraph/forge/internal/client"
)

// An introspected spec must rank with OpenAPI, not below AsyncAPI: it is
// authoritative for REST in the same way, so a merge that put it second would
// let a stream document's schema definition win over the live router's.
func TestIntrospectionRanksWithOpenAPI(t *testing.T) {
	introspected := &client.APISpec{
		Kind:      client.SourceIntrospection,
		Info:      client.APIInfo{Title: "Live"},
		Endpoints: []client.Endpoint{{OperationID: "listOrders", Path: "/orders", Method: "GET"}},
		Schemas:   map[string]*client.Schema{"Order": {Type: "object"}},
	}
	stream := &client.APISpec{
		Kind:       client.SourceAsyncAPI,
		Info:       client.APIInfo{Title: "Streams"},
		WebSockets: []client.WebSocketEndpoint{{Path: "/ws/orders"}},
		Schemas:    map[string]*client.Schema{"Order": {Type: "string"}},
	}

	got := client.MergeSpecs(stream, introspected)

	if got.Info.Title != "Live" {
		t.Errorf("Info.Title = %q, want the introspected title", got.Info.Title)
	}
	if got.Schemas["Order"].Type != "object" {
		t.Errorf("Schemas[Order].Type = %q, want the introspected shape", got.Schemas["Order"].Type)
	}
}
