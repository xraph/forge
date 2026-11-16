package client

import (
	"strings"
	"testing"
)

func eventStream() map[int]*Response {
	return map[int]*Response{200: {Content: map[string]*MediaType{
		"text/event-stream; charset=utf-8": {Schema: &Schema{Type: "string"}},
	}}}
}

func TestResolveEndpointStream(t *testing.T) {
	cases := []struct {
		name      string
		ext       map[string]any
		responses map[int]*Response
		want      string
	}{
		{"marked sse", map[string]any{"x-forge-protocol": "sse"}, nil, StreamSSE},
		{"marked websocket", map[string]any{"x-forge-protocol": "websocket"}, nil, StreamWebSocket},
		{"marked webtransport", map[string]any{"x-forge-protocol": "webtransport"}, nil, StreamWebTransport},
		{"event-stream body", nil, eventStream(), StreamSSE},
		{"upgrade only", nil, map[int]*Response{101: {}}, StreamWebSocket},
		{"json body", nil, map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json": {},
		}}}, ""},
		{"negotiated json or stream", nil, map[int]*Response{200: {Content: map[string]*MediaType{
			"application/json":  {},
			"text/event-stream": {},
		}}}, ""},
		{"bare 200", nil, map[int]*Response{200: {Description: "Success"}}, ""},
		{"101 beside a 200", nil, map[int]*Response{101: {}, 200: {}}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := &APISpec{}
			ep := &Endpoint{Method: "GET", Path: "/x", Responses: tc.responses}

			resolveEndpointStream(spec, ep, tc.ext)

			if ep.Stream != tc.want {
				t.Errorf("Stream = %q, want %q", ep.Stream, tc.want)
			}

			if len(spec.Warnings) != 0 {
				t.Errorf("unexpected warnings: %v", spec.Warnings)
			}
		})
	}
}

func TestResolveEndpointStreamWarnsOnAnUnusableMarker(t *testing.T) {
	for _, raw := range []any{"grpc", true} {
		spec := &APISpec{}
		ep := &Endpoint{Method: "GET", Path: "/logs", Responses: eventStream()}

		resolveEndpointStream(spec, ep, map[string]any{"x-forge-protocol": raw})

		if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], "x-forge-protocol") ||
			!strings.Contains(spec.Warnings[0], "GET /logs") {
			t.Errorf("%v: warnings = %v, want one naming the extension and the endpoint", raw, spec.Warnings)
		}

		// The marker is dropped, not the endpoint's other evidence.
		if ep.Stream != StreamSSE {
			t.Errorf("%v: Stream = %q, want sse from the response", raw, ep.Stream)
		}
	}
}

func TestSeparateStreamEndpoints(t *testing.T) {
	spec := &APISpec{
		Endpoints: []Endpoint{
			{Method: "GET", Path: "/orders"},
			// Described by AsyncAPI too, under the router's raw path syntax.
			{Method: "GET", Path: "/instances/{id}/logs/stream", Stream: StreamSSE},
			// Only OpenAPI knows about these.
			{
				Method: "GET", Path: "/admin/health/stream", Stream: StreamSSE,
				Summary: "Health", Tags: []string{"admin"},
				Security: []SecurityRequirement{{SchemeName: "bearer"}},
			},
			{Method: "GET", Path: "/ws/lobby", OperationID: "lobbySocket", Stream: StreamWebSocket},
			{Method: "GET", Path: "/wt", Stream: StreamWebTransport},
			// Only OpenAPI knows about it, and its path has a parameter the
			// stream clients would not substitute.
			{Method: "GET", Path: "/instances/{id}/usage/stream", Stream: StreamSSE},
		},
		SSEs: []SSEEndpoint{{ID: "logs", Path: "/instances/:id/logs/stream"}},
	}

	spec.SeparateStreamEndpoints()

	if len(spec.Endpoints) != 1 || spec.Endpoints[0].Path != "/orders" {
		t.Fatalf("Endpoints = %+v, want only /orders", spec.Endpoints)
	}

	if len(spec.SSEs) != 2 {
		t.Fatalf("SSEs = %+v, want the AsyncAPI one plus the health stream", spec.SSEs)
	}

	if spec.SSEs[0].ID != "logs" {
		t.Errorf("the channel AsyncAPI described was replaced: %+v", spec.SSEs[0])
	}

	health := spec.SSEs[1]
	if health.Path != "/admin/health/stream" || health.ID != "admin-health-stream" ||
		health.Summary != "Health" || len(health.Security) != 1 || len(health.Tags) != 1 {
		t.Errorf("health stream = %+v, want the operation's path, a path-derived id, summary, tags and security", health)
	}

	if len(spec.WebSockets) != 1 || spec.WebSockets[0].ID != "lobbySocket" {
		t.Errorf("WebSockets = %+v, want lobbySocket", spec.WebSockets)
	}

	if len(spec.WebTransports) != 1 || spec.WebTransports[0].Path != "/wt" {
		t.Errorf("WebTransports = %+v, want /wt", spec.WebTransports)
	}

	if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], "GET /instances/{id}/usage/stream") {
		t.Errorf("warnings = %v, want one naming the parameterized stream", spec.Warnings)
	}

	// Idempotent.
	spec.SeparateStreamEndpoints()

	if len(spec.Endpoints) != 1 || len(spec.SSEs) != 2 || len(spec.WebSockets) != 1 || len(spec.Warnings) != 1 {
		t.Errorf("a second pass changed the spec: %d endpoints, %d SSEs, %d sockets, %d warnings",
			len(spec.Endpoints), len(spec.SSEs), len(spec.WebSockets), len(spec.Warnings))
	}
}
