package typescript

import (
	"context"
	"slices"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestGenerationIsDeterministic(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			first, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
			if err != nil {
				t.Fatal(err)
			}

			for i := 1; i < 12; i++ {
				next, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
				if err != nil {
					t.Fatal(err)
				}

				if len(next.Files) != len(first.Files) {
					t.Fatalf("run %d: file count changed: %d != %d", i, len(next.Files), len(first.Files))
				}

				for name, content := range first.Files {
					if next.Files[name] != content {
						t.Fatalf("run %d: %s differs from run 0", i, name)
					}
				}

				// Warnings are gathered from map-keyed walks across several
				// generators and concatenated, so their order is only stable
				// because each generator sorts before returning. Comparing
				// Files alone would let a dropped sort regress silently --
				// the output would stay byte-identical while the warnings a
				// user sees reordered between runs.
				if !slices.Equal(next.Warnings, first.Warnings) {
					t.Fatalf("run %d: warnings differ from run 0:\n got: %v\nwant: %v", i, next.Warnings, first.Warnings)
				}
			}
		})
	}
}

// TestWarningOrderIsDeterministic is the real guard for warning ordering.
//
// The Warnings check inside TestGenerationIsDeterministic is nearly vacuous
// on its own: across the whole 12-fixture corpus exactly ONE warning is
// produced (by `allof`), so that loop compares empty slices eleven times and
// a single-element slice once -- and a one-element slice has no order to get
// wrong. It is kept there to cover future fixtures, not because it proves
// anything today.
//
// This test builds a spec that deliberately warns from every generator that
// has a warnings channel -- REST, codecs, WebSocket, SSE and WebTransport --
// so the concatenated slice is long enough for an ordering regression to
// show.
//
// Be precise about what it does and does not catch. Warning order is
// currently deterministic TWICE OVER: every site that appends a warning
// already walks its map through sortedKeys (e.g. sse.go's
// sortedKeys(sse.EventSchemas)), and each generator then sorts again before
// returning. So removing one of those sort.Strings calls does NOT make this
// test flap -- verified by replacing sse.go's with a no-op comparator and
// watching 8 consecutive runs stay green.
//
// What it does guard is a FUTURE generator that appends warnings from a raw
// `for k := range someMap` without sorting. That is the realistic
// regression, since the existing sorts make it easy to assume ordering is
// handled and reach for a bare range. Without this test the generated files
// would stay byte-identical and nothing else in the suite would notice.
func TestWarningOrderIsDeterministic(t *testing.T) {
	inline := func() *client.Schema {
		// Inline (non-$ref) schemas cannot resolve to a codec id, which is
		// what makes each generator emit its "will not be renamed" warning.
		return &client.Schema{Type: "object", Properties: map[string]*client.Schema{
			"some_field": {Type: "string"},
		}}
	}

	spec := &client.APISpec{
		Info: client.APIInfo{Title: "WarnAPI", Version: "1"},
		Schemas: map[string]*client.Schema{
			// Undiscriminated union -> codec-table warning.
			"Zeta":  {OneOf: []*client.Schema{{Ref: "#/components/schemas/Leaf"}}},
			"Alpha": {OneOf: []*client.Schema{{Ref: "#/components/schemas/Leaf"}}},
			"Mid":   {OneOf: []*client.Schema{{Ref: "#/components/schemas/Leaf"}}},
			"Leaf":  {Type: "object", Properties: map[string]*client.Schema{"leaf_value": {Type: "string"}}},
		},
		Endpoints: []client.Endpoint{{
			Method: "POST", Path: "/inline", OperationID: "inline.create",
			RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
				"application/json": {Schema: inline()},
			}},
			Responses: map[int]*client.Response{200: {Description: "ok", Content: map[string]*client.MediaType{
				"application/json": {Schema: inline()},
			}}},
		}},
		WebSockets: []client.WebSocketEndpoint{{
			ID: "ws.chat", Path: "/ws",
			SendSchema: inline(), ReceiveSchema: inline(),
		}},
		SSEs: []client.SSEEndpoint{{
			ID: "sse.feed", Path: "/sse",
			EventSchemas: map[string]*client.Schema{"zulu": inline(), "alfa": inline(), "mike": inline()},
		}},
		WebTransports: []client.WebTransportEndpoint{
			{ID: "wt.zulu", Path: "/wt-z", DatagramSchema: inline()},
			{ID: "wt.alfa", Path: "/wt-a", DatagramSchema: inline()},
		},
	}

	cfg := baseConfig()
	cfg.IncludeStreaming = true

	first, err := NewGenerator().Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Guard against this test silently becoming vacuous the way the corpus
	// check is: if a refactor stops emitting these warnings, fail loudly
	// rather than comparing two empty slices forever.
	if len(first.Warnings) < 6 {
		t.Fatalf("expected this spec to produce several warnings across generators, got %d: %v",
			len(first.Warnings), first.Warnings)
	}

	for i := 1; i < 12; i++ {
		next, err := NewGenerator().Generate(context.Background(), spec, cfg)
		if err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(next.Warnings, first.Warnings) {
			t.Fatalf("run %d: warning order is not deterministic:\n got: %v\nwant: %v",
				i, next.Warnings, first.Warnings)
		}
	}
}
