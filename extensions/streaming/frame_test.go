package streaming_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
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

// TestLifecycleMessageWireShape pins the rule that a lifecycle name never
// occupies the domain-event slot.
//
// Asserted through the marshalled JSON for the same reason
// TestEventMessageWireShape is: the struct tags are the contract, and an `event`
// key reappearing on this envelope -- whether from the constructor filling
// Event or from a tag rename pointing some other field at it -- is exactly the
// regression that turns every heartbeat into an unknown-message report on the
// client. Reading the fields rather than the bytes would see only half of that.
func TestLifecycleMessageWireShape(t *testing.T) {
	msg := streaming.NewLifecycleMessage(streaming.MessageTypeSystem, "ping")
	msg.ID = "ping-1"

	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The whole of the rule. Event is omitempty, so an empty one is an absent
	// key -- and absent is what keeps the frame in the client's transport branch
	// where the reserved-kind filter can drop it silently.
	if got, exists := wire["event"]; exists {
		t.Errorf("envelope carries event = %v; a lifecycle name must not claim the binding key", got)
	}

	if got := wire["type"]; got != streaming.MessageTypeSystem {
		t.Errorf("type = %v, want the reserved kind %q", got, streaming.MessageTypeSystem)
	}

	// The name, still retrievable -- dropping it would have been the cheap fix
	// and would have cost a client the ability to tell a kick from an idle
	// sweep. The key is spelled literally here as well as through the constant,
	// so that changing the constant's value is a wire change this test reports.
	metadata, ok := wire["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %v, want the lifecycle object", wire["metadata"])
	}

	if got := metadata["lifecycle"]; got != "ping" {
		t.Errorf("metadata.lifecycle = %v, want ping", got)
	}

	if streaming.LifecycleMetadataKey != "lifecycle" {
		t.Errorf(
			"LifecycleMetadataKey = %q, but the wire key consumers read is %q",
			streaming.LifecycleMetadataKey, "lifecycle",
		)
	}

	if got := metadata[streaming.LifecycleMetadataKey]; got != "ping" {
		t.Errorf("metadata[%s] = %v, want ping", streaming.LifecycleMetadataKey, got)
	}

	// The caller's fields survive construction, and the omissions match
	// NewEventMessage's: identity and routing are the producer's.
	if wire["id"] != "ping-1" {
		t.Errorf("id = %v, want ping-1", wire["id"])
	}

	if msg.Timestamp.IsZero() {
		t.Error("timestamp is zero; a frame without one marshals as a wrong answer")
	}

	if msg.UserID != "" || msg.RoomID != "" || msg.ChannelID != "" {
		t.Errorf("constructor filled a routing or identity field: %+v", msg)
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

	// The set the TypeScript decoder actually holds, read from the file that
	// holds it. A copy pinned here would only catch this side drifting; the
	// direction that matters as much is the client's set gaining a kind that
	// Go never reserved.
	mirrored, present := mirroredTransportKinds(t)
	if !present {
		t.Skip("packages/client-core is not present; nothing to mirror against")
	}

	if !slices.Equal(slices.Sorted(slices.Values(kinds)), slices.Sorted(slices.Values(mirrored))) {
		t.Errorf(
			"TransportKinds() = %v, but packages/client-core/src/streaming.ts holds %v",
			kinds, mirrored,
		)
	}
}

// transportKindsLiteral matches the TRANSPORT_KINDS declaration in
// packages/client-core/src/streaming.ts and captures the body of its Set.
//
// A regexp rather than a TypeScript parse, and the narrowness is deliberate: it
// matches one declaration whose exact text is a few lines away in a file this
// repository owns. If that declaration is ever rewritten into a form this does
// not match, the helper reports no kinds and the test fails loudly rather than
// passing on an empty comparison -- see the length check below.
var transportKindsLiteral = regexp.MustCompile(`(?s)TRANSPORT_KINDS[^=]*=\s*new Set\(\[(.*?)\]\)`)

var quotedKind = regexp.MustCompile(`'([^']*)'`)

// mirroredTransportKinds reads the set the TypeScript decoder actually holds.
//
// Returns false when the client package is not present. This module is
// publishable on its own, and a consumer who fetched it without the repository
// around it has no packages/ directory -- skipping there is correct, whereas
// failing would make the module untestable outside its own tree.
//
// The os.ReadFile below is invisible to the Go test cache: streaming.ts is not
// a Go source file, so it is not part of this package's build graph, and the
// cache has no reason to know its contents changed. A developer who edits only
// streaming.ts and runs a plain `go test ./...` afterward can get a stale
// cached PASS from before the edit and never see the drift this test exists to
// catch -- there is no code this function can add to force Go to invalidate on
// a file outside that graph. This is accepted rather than engineered around:
// CI always runs from a cold cache, so it never observes the stale result, and
// a developer chasing this specific check locally can force it with
// `go test -count=1`.
func mirroredTransportKinds(t *testing.T) ([]string, bool) {
	t.Helper()

	path := filepath.Join("..", "..", "packages", "client-core", "src", "streaming.ts")

	source, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}

		t.Fatalf("read %s: %v", path, err)
	}

	block := transportKindsLiteral.FindSubmatch(source)
	if block == nil {
		t.Fatalf("no TRANSPORT_KINDS set found in %s; the decoder's reserved kinds could not be read", path)
	}

	var kinds []string

	for _, match := range quotedKind.FindAllSubmatch(block[1], -1) {
		kinds = append(kinds, string(match[1]))
	}

	if len(kinds) == 0 {
		t.Fatalf("TRANSPORT_KINDS in %s parsed to nothing", path)
	}

	return kinds, true
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
