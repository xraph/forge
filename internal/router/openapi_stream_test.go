package router

import (
	"testing"

	"github.com/xraph/forge/internal/shared"
)

type streamProbeRequest struct{}

type streamProbeResponse struct {
	ID string `json:"id"`
}

// A streaming route used to reach OpenAPI as `GET -> 200 Success`, which no
// reader can tell from a REST read. The client generator bound every one of
// them as a query; see xraph/forge#106.
func TestStreamingRoutesAreMarkedInOpenAPI(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Streams", Version: "1.0.0"}))

	if err := r.SSE("/logs/stream", func(ctx Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if err := r.EventStream("/events", func(ctx Context, s Stream) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if err := r.WebSocket("/ws", func(ctx Context, c Connection) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if err := r.GET("/orders", func(ctx shared.Context, req *streamProbeRequest) (*streamProbeResponse, error) {
		return &streamProbeResponse{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	spec := r.OpenAPISpec()
	if spec == nil {
		t.Fatal("no OpenAPI document")
	}

	for _, path := range []string{"/logs/stream", "/events"} {
		op := spec.Paths[path].Get
		if op == nil {
			t.Fatalf("%s: no GET operation", path)
		}

		if got := op.Extensions["x-forge-protocol"]; got != "sse" {
			t.Errorf("%s: x-forge-protocol = %v, want sse", path, got)
		}

		resp := op.Responses["200"]
		if resp == nil || resp.Content["text/event-stream"] == nil {
			t.Errorf("%s: 200 does not answer text/event-stream: %+v", path, resp)
		} else if len(resp.Content) != 1 {
			t.Errorf("%s: 200 declares %d media types, want only text/event-stream", path, len(resp.Content))
		}
	}

	ws := spec.Paths["/ws"].Get
	if got := ws.Extensions["x-forge-protocol"]; got != "websocket" {
		t.Errorf("/ws: x-forge-protocol = %v, want websocket", got)
	}

	if _, ok := ws.Responses["101"]; !ok || len(ws.Responses) != 1 {
		t.Errorf("/ws: responses = %v, want only 101", ws.Responses)
	}

	rest := spec.Paths["/orders"].Get
	if _, marked := rest.Extensions["x-forge-protocol"]; marked {
		t.Error("/orders: an ordinary GET is marked as a stream")
	}
}

// A schema a route declared for its 200 describes the events, so it survives
// under the stream's media type instead of being replaced.
func TestMarkStreamOperationKeepsADeclaredEventSchema(t *testing.T) {
	declared := &Schema{Ref: "#/components/schemas/LogLine"}
	op := &Operation{Responses: map[string]*Response{
		"200": {Description: "Log lines", Content: map[string]*MediaType{
			"application/json": {Schema: declared},
		}},
	}}

	markStreamOperation(op, KindSSE)

	resp := op.Responses["200"]
	if resp.Description != "Log lines" {
		t.Errorf("description = %q, want the declared one kept", resp.Description)
	}

	media := resp.Content["text/event-stream"]
	if media == nil || media.Schema != declared {
		t.Errorf("content = %+v, want the declared schema under text/event-stream", resp.Content)
	}

	if _, ok := resp.Content["application/json"]; ok {
		t.Error("application/json is still declared on a stream")
	}
}

// A WebSocket route that declared its own responses keeps them: only the
// generated default is rewritten to 101.
func TestMarkStreamOperationLeavesDeclaredWebSocketResponses(t *testing.T) {
	op := &Operation{Responses: map[string]*Response{
		"200": {Description: "Upgraded"},
	}}

	markStreamOperation(op, KindWebSocket)

	if _, ok := op.Responses["200"]; !ok {
		t.Error("a declared 200 was replaced")
	}

	if op.Extensions["x-forge-protocol"] != "websocket" {
		t.Errorf("x-forge-protocol = %v, want websocket", op.Extensions["x-forge-protocol"])
	}
}
