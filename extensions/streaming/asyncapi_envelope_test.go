package streaming

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// Every frame on this socket is a Message. A published payload schema naming a
// property the envelope does not have is not a documentation nit -- it describes
// a frame nothing can emit and no client can satisfy. The presence schema
// documented top-level status and custom_status, and the channel publish schema
// documented an action verb; both were unsatisfiable, and both went unnoticed
// because nothing checked the spec against the struct it claims to describe.
//
// This is that check.

// envelopeFields returns the JSON property names of the Message envelope.
func envelopeFields(t *testing.T) map[string]bool {
	t.Helper()

	fields := make(map[string]bool)

	typ := reflect.TypeOf(internal.Message{})
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}

		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			fields[name] = true
		}
	}

	if len(fields) == 0 {
		t.Fatal("Message has no JSON-tagged fields; the envelope check would pass vacuously")
	}

	return fields
}

// specForTest builds the published spec from a config.
func specForTest(t *testing.T, cfg Config) *forge.AsyncAPISpec {
	t.Helper()

	ext := NewExtensionWithConfig(cfg)
	if ext == nil {
		t.Fatal("NewExtensionWithConfig returned nil")
	}

	return ext.AsyncAPISpec()
}

// knownSpecGaps names payload schemas that do not describe the envelope and are
// not fixable by editing the schema alone. It is empty, and adding to it should
// be argued for rather than done: an entry here is a published lie with a note
// attached.
var knownSpecGaps = map[string]string{}

func TestAsyncAPIPayloadsMatchTheMessageEnvelope(t *testing.T) {
	fields := envelopeFields(t)

	spec := specForTest(t, DefaultConfig())

	if len(spec.Channels) == 0 {
		t.Fatal("spec has no channels; the check would pass vacuously")
	}

	checked := 0

	for channelName, channel := range spec.Channels {
		for msgName, msg := range channel.Messages {
			if msg.Payload == nil {
				continue
			}

			if reason, skip := knownSpecGaps[msgName]; skip {
				t.Logf("skipping %s/%s: %s", channelName, msgName, reason)

				continue
			}

			checked++

			for prop := range msg.Payload.Properties {
				if !fields[prop] {
					t.Errorf("%s/%s documents property %q, which is not a field on the Message envelope; "+
						"no producer can emit it and a client validating against this schema rejects every real frame",
						channelName, msgName, prop)
				}
			}

			// A required property that is not documented is a schema that
			// contradicts itself.
			for _, req := range msg.Payload.Required {
				if _, ok := msg.Payload.Properties[req]; !ok {
					t.Errorf("%s/%s requires property %q that it does not document", channelName, msgName, req)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("no payloads were checked")
	}
}

// TestAsyncAPIPresencePayloadMatchesTheInboundHandler pins the specific shape the
// presence fix settled on: the status rides in data as a string, which is what
// handleMessage parses and what keeps presence symmetric with the typing
// indicator's boolean.
func TestAsyncAPIPresencePayloadMatchesTheInboundHandler(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnablePresence = true

	spec := specForTest(t, cfg)

	channel, ok := spec.Channels["presence"]
	if !ok {
		t.Fatal("spec has no presence channel")
	}

	msg, ok := channel.Messages["PresenceUpdate"]
	if !ok {
		t.Fatal("presence channel has no PresenceUpdate message")
	}

	data, ok := msg.Payload.Properties["data"]
	if !ok {
		t.Fatal("PresenceUpdate does not document data, where the status rides")
	}

	if data.Type != "string" {
		t.Errorf("data.Type = %q, want string — handleMessage parses presence data as msg.Data.(string)", data.Type)
	}

	if len(data.Enum) == 0 {
		t.Error("data has no enum; the presence statuses were documented before and should not be lost")
	}

	for _, gone := range []string{"status", "custom_status"} {
		if _, present := msg.Payload.Properties[gone]; present {
			t.Errorf("PresenceUpdate still documents top-level %q, which the envelope has no field for", gone)
		}
	}
}

// TestAsyncAPIOperationsReferenceRealMessages catches a dangling $ref: an
// operation pointing at a message that was removed, or renamed, or never
// existed. A spec with a broken ref fails validation in every downstream
// generator, and nothing here would otherwise notice.
func TestAsyncAPIOperationsReferenceRealMessages(t *testing.T) {
	spec := specForTest(t, DefaultConfig())

	if len(spec.Operations) == 0 {
		t.Fatal("spec has no operations; the check would pass vacuously")
	}

	for opName, op := range spec.Operations {
		for _, ref := range op.Messages {
			// "#/channels/<channel>/messages/<message>"
			parts := strings.Split(strings.TrimPrefix(ref.Ref, "#/"), "/")
			if len(parts) != 4 || parts[0] != "channels" || parts[2] != "messages" {
				t.Errorf("operation %q has ref %q in an unrecognised form", opName, ref.Ref)

				continue
			}

			channel, ok := spec.Channels[parts[1]]
			if !ok {
				t.Errorf("operation %q references channel %q, which the spec does not define", opName, parts[1])

				continue
			}

			if _, ok := channel.Messages[parts[3]]; !ok {
				t.Errorf("operation %q references message %q on channel %q, which does not exist",
					opName, parts[3], parts[1])
			}
		}
	}
}

// TestAsyncAPIInboundOperationsAreDispatchable is the guard that would have
// caught the Subscribe operation.
//
// handleMessage dispatches on Message.Type, so a client-to-server frame the
// server can act on must name a type it recognises. Subscribe documented an
// action verb and no type at all: the server parsed it into a Message with an
// empty Type, fell through the switch, and discarded it silently while the
// client waited for events that were never routed to it.
//
// Any inbound operation that cannot state a known type is describing something
// the server does not implement.
func TestAsyncAPIInboundOperationsAreDispatchable(t *testing.T) {
	known := make(map[string]bool)
	for _, kind := range TransportKinds() {
		known[kind] = true
	}

	spec := specForTest(t, DefaultConfig())

	checked := 0

	for opName, op := range spec.Operations {
		if op.Action != "send" {
			continue // server-to-client; the client is free to route it as it likes
		}

		for _, ref := range op.Messages {
			parts := strings.Split(strings.TrimPrefix(ref.Ref, "#/"), "/")
			if len(parts) != 4 {
				continue // shape is covered by TestAsyncAPIOperationsReferenceRealMessages
			}

			channel, ok := spec.Channels[parts[1]]
			if !ok {
				continue
			}

			msg, ok := channel.Messages[parts[3]]
			if !ok || msg.Payload == nil {
				continue
			}

			checked++

			typeProp, ok := msg.Payload.Properties["type"]
			if !ok {
				t.Errorf("inbound operation %q (message %q) documents no type property, "+
					"so handleMessage cannot dispatch it — the server would discard the frame silently",
					opName, parts[3])

				continue
			}

			if len(typeProp.Enum) == 0 {
				t.Errorf("inbound operation %q (message %q) documents type without an enum, "+
					"so a client cannot tell which value the server dispatches on", opName, parts[3])

				continue
			}

			for _, v := range typeProp.Enum {
				kind, ok := v.(string)
				if !ok {
					t.Errorf("inbound operation %q (message %q) has non-string type enum value %#v",
						opName, parts[3], v)

					continue
				}

				if !known[kind] {
					t.Errorf("inbound operation %q (message %q) documents type %q, "+
						"which is not one of the transport kinds the server recognises (%v)",
						opName, parts[3], kind, TransportKinds())
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("no inbound operations were checked")
	}
}

// TestAsyncAPIPayloadsAreValidJSON is a cheap guard that the spec still
// serialises, since it is published rather than only read in Go.
func TestAsyncAPIPayloadsAreValidJSON(t *testing.T) {
	spec := specForTest(t, DefaultConfig())

	if _, err := json.Marshal(spec); err != nil {
		t.Fatalf("AsyncAPI spec does not marshal: %v", err)
	}
}
