package shared

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaExtensionsMarshalInline(t *testing.T) {
	s := Schema{
		Type:        "string",
		Description: "an order number",
		Extensions:  map[string]any{"x-forge-id": true},
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// The extension must sit at the TOP LEVEL, not nested under "Extensions".
	if v, ok := decoded["x-forge-id"].(bool); !ok || !v {
		t.Fatalf("x-forge-id = %#v, want true at top level. Got object: %s", decoded["x-forge-id"], data)
	}

	if _, nested := decoded["Extensions"]; nested {
		t.Fatalf("Extensions leaked as a literal key: %s", data)
	}

	// Ordinary fields must survive the custom marshaller.
	if decoded["type"] != "string" {
		t.Fatalf("type = %#v, want string — the marshaller dropped a normal field", decoded["type"])
	}

	if decoded["description"] != "an order number" {
		t.Fatalf("description was dropped: %s", data)
	}
}

func TestSchemaExtensionsRoundTrip(t *testing.T) {
	original := Schema{
		Type:       "object",
		Extensions: map[string]any{"x-forge-id": true, "x-other": "keep me"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back Schema
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if back.Type != "object" {
		t.Fatalf("Type = %q, want object", back.Type)
	}

	if v, _ := back.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id did not survive the round trip: %#v", back.Extensions)
	}

	if back.Extensions["x-other"] != "keep me" {
		t.Fatalf("x-other did not survive: %#v", back.Extensions)
	}
}

// A schema with no extensions must marshal byte-identically to how it did before this
// change. Generated specs are diffed in CI; reordering every schema in the document would
// produce a spurious diff on every run and train everyone to ignore the drift check.
func TestSchemaWithoutExtensionsIsUnchanged(t *testing.T) {
	s := Schema{Type: "string", Description: "plain"}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if strings.Contains(string(data), "Extensions") {
		t.Fatalf("empty Extensions leaked into output: %s", data)
	}

	if !strings.HasPrefix(string(data), `{"type":"string"`) {
		t.Fatalf("field order changed for an extension-free schema: %s", data)
	}
}

// Only x- prefixed keys are hoisted. A non-extension key in the map is a caller error and
// must not be able to overwrite a real schema field.
func TestNonExtensionKeysAreNotHoisted(t *testing.T) {
	s := Schema{Type: "string", Extensions: map[string]any{"type": "object", "x-ok": 1}}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["type"] != "string" {
		t.Fatalf("a non-x- extension key overwrote a real field: %s", data)
	}

	if decoded["x-ok"] == nil {
		t.Fatalf("x-ok was not hoisted: %s", data)
	}
}

func TestOperationExtensionsMarshalInline(t *testing.T) {
	op := Operation{Extensions: map[string]any{"x-forge-invalidates": []string{"Order[]"}}}

	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["x-forge-invalidates"] == nil {
		t.Fatalf("operation extension not hoisted: %s", data)
	}
}

func TestAsyncAPIChannelExtensionsMarshalInline(t *testing.T) {
	ch := AsyncAPIChannel{Extensions: map[string]any{"x-forge-stream": []any{"binding"}}}

	data, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["x-forge-stream"] == nil {
		t.Fatalf("channel extension not hoisted: %s", data)
	}
}
