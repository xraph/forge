package router

import "testing"

// forgeIDPayload carries a forge:"id" tag on one field and an untagged sibling.
// SplitMessageComponents is the AsyncAPI-side payload builder that walks
// non-header fields independently of the OpenAPI schema generator, and until
// this test's fix it never checked the forge:"id" tag at all -- so a payload
// that only ever appeared on a WebSocket/SSE message never carried its
// identity marker into the generated manifest.
type forgeIDPayload struct {
	ID   string `forge:"id"  json:"id"`
	Name string `json:"name"`
}

// TestAsyncAPIPayloadMarksForgeIDTag verifies GAP 1 (tag half): a forge:"id"
// tagged field in an AsyncAPI message payload gets x-forge-id, and an
// untagged sibling gets no such key at all (absent, not false).
func TestAsyncAPIPayloadMarksForgeIDTag(t *testing.T) {
	_, payload := newTestAsyncAPISchemaGenerator().SplitMessageComponents(forgeIDPayload{})
	if payload == nil {
		t.Fatal("SplitMessageComponents returned no payload schema")
	}

	idProp, ok := payload.Properties["id"]
	if !ok {
		t.Fatalf("id missing from payload properties: %#v", payload.Properties)
	}

	if v, _ := idProp.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id = %#v on id, want true", idProp.Extensions["x-forge-id"])
	}

	nameProp, ok := payload.Properties["name"]
	if !ok {
		t.Fatalf("name missing from payload properties: %#v", payload.Properties)
	}

	if _, present := nameProp.Extensions["x-forge-id"]; present {
		t.Fatalf("x-forge-id was set on name, which carries no forge:\"id\" tag")
	}
}

// forgeIDEntityPayload implements ForgeEntity rather than using the struct
// tag, declaring its identity on order_number while also carrying a property
// literally named "id" that is NOT the identity -- the case ForgeEntity exists
// to override the name heuristic for.
type forgeIDEntityPayload struct {
	OrderNumber string `json:"order_number"`
	ID          string `json:"id"`
}

func (forgeIDEntityPayload) ForgeEntity() EntityDef {
	return EntityDef{Type: "Order", IDField: "order_number"}
}

// TestAsyncAPIPayloadMarksForgeEntityMethod verifies GAP 1 (ForgeEntity half):
// a payload type whose identity is declared via ForgeEntity gets the marker
// on the declared property, mirroring openapi_schema.go's applyForgeEntity.
func TestAsyncAPIPayloadMarksForgeEntityMethod(t *testing.T) {
	_, payload := newTestAsyncAPISchemaGenerator().SplitMessageComponents(forgeIDEntityPayload{})
	if payload == nil {
		t.Fatal("SplitMessageComponents returned no payload schema")
	}

	prop, ok := payload.Properties["order_number"]
	if !ok {
		t.Fatalf("order_number missing from payload properties: %#v", payload.Properties)
	}

	if v, _ := prop.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id = %#v on order_number, want true (ForgeEntity was not honoured)",
			prop.Extensions["x-forge-id"])
	}

	if _, present := payload.Properties["id"].Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id was set on id, which ForgeEntity did not name")
	}
}

// forgeIDHeaderPayload tags a HEADER field with forge:"id". Headers are not
// entity payloads -- a header happening to be named/tagged "id" is not the
// record's identity, and marking it would fabricate a false entity. This is
// the scoping decision from the task: only the two payload-walker sites
// (embedded-field path and main path) apply forge:"id"/ForgeEntity marking;
// the two header walkers must not.
type forgeIDHeaderPayload struct {
	TraceID string `forge:"id"  header:"X-Trace-Id"`
	Name    string `json:"name"`
}

// TestAsyncAPIHeaderForgeIDTagNotMarked deliberately asserts the negative:
// a forge:"id" tagged HEADER field must not get x-forge-id, since headers are
// out of scope for entity identity.
func TestAsyncAPIHeaderForgeIDTagNotMarked(t *testing.T) {
	gen := newTestAsyncAPISchemaGenerator()

	headers := gen.GenerateHeadersSchema(forgeIDHeaderPayload{})
	if headers == nil {
		t.Fatal("expected a headers schema for the X-Trace-Id field")
	}

	prop, ok := headers.Properties["X-Trace-Id"]
	if !ok {
		t.Fatalf("X-Trace-Id missing from headers properties: %#v", headers.Properties)
	}

	if _, present := prop.Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id was set on a HEADER field; header walkers must not mark identity")
	}

	// SplitMessageComponents routes the same struct through the header-aware
	// path (it has a header field), so also check the payload side directly.
	_, payload := gen.SplitMessageComponents(forgeIDHeaderPayload{})
	if payload == nil {
		t.Fatal("SplitMessageComponents returned no payload schema")
	}

	if _, present := payload.Properties["name"].Extensions["x-forge-id"]; present {
		t.Fatal("x-forge-id unexpectedly set on name")
	}
}
