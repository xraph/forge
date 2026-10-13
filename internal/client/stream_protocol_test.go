package client

import (
	"testing"
)

func TestAsyncAPIHonorsExplicitTransportBindings(t *testing.T) {
	for _, protocol := range []string{"http", "ws"} {
		t.Run(protocol, func(t *testing.T) {
			path := writeSpec(t, map[string]any{
				"asyncapi": "3.0.0", "info": map[string]any{"title": "Stream", "version": "1"},
				"channels": map[string]any{"updates": map[string]any{
					"address": "/updates", "bindings": map[string]any{protocol: map[string]any{"method": "GET"}},
					"messages": map[string]any{"updated": map[string]any{"payload": map[string]any{"type": "object"}}},
				}},
				"operations": map[string]any{"updates": map[string]any{"action": "receive", "channel": map[string]any{"$ref": "#/channels/updates"}}},
			})

			spec, err := NewSpecParser().ParseFile(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}

			if protocol == "http" && (len(spec.SSEs) != 1 || len(spec.WebSockets) != 0) {
				t.Fatalf("HTTP stream: %+v %+v", spec.SSEs, spec.WebSockets)
			}

			if protocol == "ws" && (len(spec.WebSockets) != 1 || len(spec.SSEs) != 0) {
				t.Fatalf("WebSocket stream: %+v %+v", spec.WebSockets, spec.SSEs)
			}
		})
	}
}
