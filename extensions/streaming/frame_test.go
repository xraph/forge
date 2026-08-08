package streaming_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/streaming"
)

// The Go half of the wire-contract proof.
//
// packages/client-core/__tests__/streaming.test.ts is the other half: it drives
// the same envelope through the real StreamBinder and asserts what the bytes
// do. This half asserts the bytes. Both are required, and for the reason the
// defect they pin demonstrates -- each side had a suite, each suite tested that
// side against its own idea of the envelope, and a frame neither would accept
// from the other passed both.

// TestEventMessageWireShape pins the field names and values the client decodes.
//
// Asserted through the marshalled JSON rather than the struct, because the
// struct tags are the contract and a rename that kept the field names would be
// invisible to an assertion on the fields.
func TestEventMessageWireShape(t *testing.T) {
	msg := streaming.NewEventMessage("order.created", map[string]any{"id": 9})
	msg.ID = "msg-1"
	msg.ChannelID = "orders"
	msg.UserID = "u-1"

	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The two names, and which is which. Reversing them is the defect.
	if got := wire["event"]; got != "order.created" {
		t.Errorf("event = %v, want the domain name order.created", got)
	}

	if got := wire["type"]; got != streaming.MessageTypeMessage {
		t.Errorf("type = %v, want the transport kind %q", got, streaming.MessageTypeMessage)
	}

	// The payload field. The client reads `data`; `payload` is the other shape
	// in circulation and this envelope must not be spelling it.
	data, ok := wire["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want the payload object", wire["data"])
	}

	if data["id"] != float64(9) {
		t.Errorf("data.id = %v, want 9", data["id"])
	}

	if _, exists := wire["payload"]; exists {
		t.Error("envelope carries a payload field; the client reads data")
	}

	// The channel, in the spelling the decoder looks for. Note that this is a
	// logical subscription id and not the endpoint path a manifest binding is
	// keyed on, which is why the client does not surface it as the frame's
	// channel unless an application supplies a mapping.
	if got := wire["channel_id"]; got != "orders" {
		t.Errorf("channel_id = %v, want orders", got)
	}
}

// TestTransportKindsMirrorTheConstants fails when a MessageType* constant is
// declared and not added to TransportKinds.
//
// The failure is the point. An unmirrored kind reaches the client as a frame
// name no binding claims and is reported as an unknown message on every channel
// that emits it -- a quiet, permanent warning for something working exactly as
// designed. The constants are parsed out of internal/streaming.go rather than
// copied here, because a copy is not a check: it agrees with whatever it was
// last edited to agree with.
func TestTransportKindsMirrorTheConstants(t *testing.T) {
	declared := declaredMessageTypes(t)

	kinds := streaming.TransportKinds()

	if !slices.Equal(slices.Sorted(slices.Values(kinds)), slices.Sorted(slices.Values(declared))) {
		t.Fatalf("TransportKinds() = %v, want %v", kinds, declared)
	}

	// The literal set the TypeScript decoder holds. Copied from
	// packages/client-core/src/streaming.ts; if this fails, that file is the
	// other edit the change needs.
	mirrored := []string{"message", "presence", "typing", "system", "join", "leave", "error"}

	if !slices.Equal(slices.Sorted(slices.Values(kinds)), slices.Sorted(slices.Values(mirrored))) {
		t.Errorf(
			"TransportKinds() = %v, but packages/client-core/src/streaming.ts holds %v",
			kinds, mirrored,
		)
	}
}

// messageTypesIn reads every MessageType* constant declared in one file.
func messageTypesIn(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var declared []string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "MessageType") || i >= len(value.Values) {
					continue
				}

				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}

				declared = append(declared, unquoted)
			}
		}
	}

	return declared
}

// declaredMessageTypes reads the constants out of the file that declares them,
// rather than restating them here.
//
// A hand-written copy was the first version, and it asserted nothing: it changed
// only when somebody edited this test, so the comparison was between two copies
// of the same list and a newly declared kind passed both. Parsing the source is
// the only spelling in which "a constant was added and TransportKinds was not"
// is a detectable event.
func declaredMessageTypes(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("internal", "streaming.go")
	declared := messageTypesIn(t, path)

	if len(declared) == 0 {
		t.Fatalf("no MessageType* constants found in %s; the parse found nothing to check", path)
	}

	return declared
}

// TestMessageTypesInFindsEveryDeclaredConstant is the proof that
// declaredMessageTypes would notice a newly declared kind.
//
// Asserted against a fixture rather than by temporarily editing
// internal/streaming.go: that file is shared with another workstream, and a
// proof that requires mutating somebody else's file is a proof that will one
// day be left half-applied.
func TestMessageTypesInFindsEveryDeclaredConstant(t *testing.T) {
	got := messageTypesIn(t, filepath.Join("testdata", "constants_fixture.go"))

	want := []string{"message", "ack"}

	if !slices.Equal(got, want) {
		t.Errorf("messageTypesIn(fixture) = %v, want %v", got, want)
	}
}

func TestIsTransportKind(t *testing.T) {
	if !streaming.IsTransportKind(streaming.MessageTypePresence) {
		t.Error("presence is a reserved transport kind")
	}

	// The case a producer needs the check for: a domain name that happens to
	// collide with a reserved kind cannot be bound on the client.
	if streaming.IsTransportKind("order.created") {
		t.Error("order.created is a domain event, not a transport kind")
	}
}

// TestNewEventMessageLeavesIdentityAlone pins the deliberate omission. A helper
// that invented an ID would be inventing deduplication and history semantics
// that belong to the producer.
func TestNewEventMessageLeavesIdentityAlone(t *testing.T) {
	msg := streaming.NewEventMessage("order.created", nil)

	if msg.ID != "" || msg.UserID != "" || msg.RoomID != "" || msg.ChannelID != "" {
		t.Errorf("constructor filled a routing or identity field: %+v", msg)
	}

	if msg.Timestamp.IsZero() {
		t.Error("timestamp is zero; a frame without one marshals as a wrong answer")
	}
}
