package shared

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These mirror the JSON cases in extensions_marshal_test.go. They are not
// redundant with them: yaml.v3 never calls MarshalJSON/UnmarshalJSON, so the
// JSON tests say nothing at all about what a YAML document does.

// The method-less aliases below shed MarshalYAML/UnmarshalYAML, so encoding one
// produces exactly the bytes yaml.v3 would have produced if the type had no
// MarshalYAML method at all. That is the baseline the extension-free cases are
// pinned against -- a stronger and less brittle statement than a golden string,
// because it keeps holding when a field is added to the struct.
type (
	schemaNoYAMLMethods   Schema
	operationNoYAMLMethod Operation
	channelNoYAMLMethod   AsyncAPIChannel
)

func mustMarshalYAML(t *testing.T, v any) []byte {
	t.Helper()

	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	return data
}

func TestSchemaExtensionsMarshalInlineYAML(t *testing.T) {
	s := Schema{
		Type:        "string",
		Description: "an order number",
		Extensions:  map[string]any{"x-forge-id": true},
	}

	data := mustMarshalYAML(t, s)

	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// The extension must sit at the TOP LEVEL, not nested under "Extensions".
	if v, ok := decoded["x-forge-id"].(bool); !ok || !v {
		t.Fatalf("x-forge-id = %#v, want true at top level. Got document:\n%s", decoded["x-forge-id"], data)
	}

	if _, nested := decoded["Extensions"]; nested {
		t.Fatalf("Extensions leaked as a literal key:\n%s", data)
	}

	if decoded["type"] != "string" {
		t.Fatalf("type = %#v, want string — the marshaller dropped a normal field", decoded["type"])
	}

	if decoded["description"] != "an order number" {
		t.Fatalf("description was dropped:\n%s", data)
	}
}

func TestSchemaExtensionsRoundTripYAML(t *testing.T) {
	original := Schema{
		Type:       "object",
		Extensions: map[string]any{"x-forge-id": true, "x-other": "keep me"},
	}

	data := mustMarshalYAML(t, original)

	var back Schema
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if back.Type != "object" {
		t.Fatalf("Type = %q, want object", back.Type)
	}

	if v, _ := back.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("x-forge-id did not survive the YAML round trip: %#v", back.Extensions)
	}

	if back.Extensions["x-other"] != "keep me" {
		t.Fatalf("x-other did not survive: %#v", back.Extensions)
	}
}

// Extensions on a nested schema (a property) must survive too — this is the
// shape every real spec uses for x-forge-id, and it exercises the fact that
// yaml.Node.Encode still honours the nested type's own MarshalYAML.
func TestNestedSchemaExtensionsRoundTripYAML(t *testing.T) {
	original := Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"order_number": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
		},
	}

	data := mustMarshalYAML(t, original)

	if !strings.Contains(string(data), "x-forge-id") {
		t.Fatalf("nested extension never reached the document:\n%s", data)
	}

	var back Schema
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	prop := back.Properties["order_number"]
	if prop == nil {
		t.Fatalf("property order_number was lost:\n%s", data)
	}

	if v, _ := prop.Extensions["x-forge-id"].(bool); !v {
		t.Fatalf("nested x-forge-id did not survive: %#v", prop.Extensions)
	}
}

// A schema with no extensions must marshal byte-identically to how it did before
// MarshalYAML existed. Emitted specs are diffed in CI; merging through a map
// would reorder every key of every extension-free object in the document and
// produce a spurious diff on every run.
func TestSchemaWithoutExtensionsYAMLIsUnchanged(t *testing.T) {
	s := Schema{Type: "string", Description: "plain"}

	got := mustMarshalYAML(t, s)
	want := mustMarshalYAML(t, schemaNoYAMLMethods(s))

	if string(got) != string(want) {
		t.Fatalf("extension-free schema changed shape.\n got:\n%s\nwant:\n%s", got, want)
	}

	if strings.Contains(string(got), "Extensions") {
		t.Fatalf("empty Extensions leaked into output:\n%s", got)
	}
}

func TestOperationWithoutExtensionsYAMLIsUnchanged(t *testing.T) {
	op := Operation{Summary: "list orders", OperationID: "orderList"}

	got := mustMarshalYAML(t, op)
	want := mustMarshalYAML(t, operationNoYAMLMethod(op))

	if string(got) != string(want) {
		t.Fatalf("extension-free operation changed shape.\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAsyncAPIChannelWithoutExtensionsYAMLIsUnchanged(t *testing.T) {
	ch := AsyncAPIChannel{Address: "/orders", Title: "Orders"}

	got := mustMarshalYAML(t, ch)
	want := mustMarshalYAML(t, channelNoYAMLMethod(ch))

	if string(got) != string(want) {
		t.Fatalf("extension-free channel changed shape.\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Existing fields keep their original order when extensions ARE present: the
// extensions are appended after the object's own keys rather than the whole
// object being rebuilt from a map.
func TestSchemaWithExtensionsKeepsFieldOrderYAML(t *testing.T) {
	s := Schema{Type: "string", Description: "plain"}

	base := string(mustMarshalYAML(t, schemaNoYAMLMethods(s)))

	s.Extensions = map[string]any{"x-forge-id": true}
	got := string(mustMarshalYAML(t, s))

	if !strings.HasPrefix(got, base) {
		t.Fatalf("field order changed once extensions were added.\n got:\n%s\nwant prefix:\n%s", got, base)
	}

	if !strings.HasSuffix(got, "x-forge-id: true\n") {
		t.Fatalf("extension was not appended after the object's own fields:\n%s", got)
	}
}

// Only x- prefixed keys are hoisted. A non-extension key in the map is a caller
// error and must not be able to overwrite a real schema field.
func TestNonExtensionKeysAreNotHoistedYAML(t *testing.T) {
	s := Schema{Type: "string", Extensions: map[string]any{"type": "object", "x-ok": 1}}

	data := mustMarshalYAML(t, s)

	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["type"] != "string" {
		t.Fatalf("a non-x- extension key overwrote a real field:\n%s", data)
	}

	if decoded["x-ok"] == nil {
		t.Fatalf("x-ok was not hoisted:\n%s", data)
	}
}

// A map holding ONLY non-x- keys must take the untouched path, exactly as an
// empty map does — otherwise a caller error would silently reorder a document.
func TestOnlyNonExtensionKeysLeaveYAMLUnchanged(t *testing.T) {
	plain := Schema{Type: "string"}

	s := plain
	s.Extensions = map[string]any{"type": "object"}

	got := mustMarshalYAML(t, s)
	want := mustMarshalYAML(t, schemaNoYAMLMethods(plain))

	if string(got) != string(want) {
		t.Fatalf("a map with no x- keys changed the document.\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestOperationExtensionsMarshalInlineYAML(t *testing.T) {
	op := Operation{
		OperationID: "orderCreate",
		Extensions:  map[string]any{"x-forge-invalidates": []string{"Order[]"}},
	}

	data := mustMarshalYAML(t, op)

	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["x-forge-invalidates"] == nil {
		t.Fatalf("operation extension not hoisted:\n%s", data)
	}

	if _, nested := decoded["Extensions"]; nested {
		t.Fatalf("Extensions leaked as a literal key:\n%s", data)
	}
}

func TestOperationExtensionsRoundTripYAML(t *testing.T) {
	op := Operation{
		OperationID: "orderCreate",
		Summary:     "create an order",
		Extensions: map[string]any{
			"x-forge-entity": map[string]any{"type": "Order", "idField": "id"},
		},
	}

	data := mustMarshalYAML(t, op)

	var back Operation
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if back.OperationID != "orderCreate" || back.Summary != "create an order" {
		t.Fatalf("ordinary fields lost: %+v", back)
	}

	entity, ok := back.Extensions["x-forge-entity"].(map[string]any)
	if !ok {
		t.Fatalf("x-forge-entity = %#v, want map[string]any — the client parser type-asserts exactly this",
			back.Extensions["x-forge-entity"])
	}

	if entity["type"] != "Order" || entity["idField"] != "id" {
		t.Fatalf("x-forge-entity contents lost: %#v", entity)
	}
}

func TestAsyncAPIChannelExtensionsMarshalInlineYAML(t *testing.T) {
	ch := AsyncAPIChannel{
		Address:    "/orders",
		Extensions: map[string]any{"x-forge-stream": []any{"binding"}},
	}

	data := mustMarshalYAML(t, ch)

	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["x-forge-stream"] == nil {
		t.Fatalf("channel extension not hoisted:\n%s", data)
	}

	if _, nested := decoded["Extensions"]; nested {
		t.Fatalf("Extensions leaked as a literal key:\n%s", data)
	}
}

func TestAsyncAPIChannelExtensionsRoundTripYAML(t *testing.T) {
	ch := AsyncAPIChannel{
		Address: "/orders",
		Title:   "Orders",
		Extensions: map[string]any{
			"x-forge-stream": []any{
				map[string]any{"message": "orderUpdated", "entityType": "Order", "intent": "update"},
			},
		},
	}

	data := mustMarshalYAML(t, ch)

	var back AsyncAPIChannel
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if back.Address != "/orders" || back.Title != "Orders" {
		t.Fatalf("ordinary fields lost: %+v", back)
	}

	entries, ok := back.Extensions["x-forge-stream"].([]any)
	if !ok {
		t.Fatalf("x-forge-stream = %#v, want []any — streamBindings type-switches on exactly this",
			back.Extensions["x-forge-stream"])
	}

	if len(entries) != 1 {
		t.Fatalf("x-forge-stream = %#v, want one entry", entries)
	}

	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("x-forge-stream[0] = %#v, want map[string]any", entries[0])
	}

	if entry["message"] != "orderUpdated" || entry["entityType"] != "Order" {
		t.Fatalf("stream binding contents lost: %#v", entry)
	}
}

// An x- key already spelled out in the document is decoded into Extensions and,
// on the way back out, replaces that key rather than emitting it twice. A
// duplicate key makes the document invalid for strict YAML readers.
func TestHoistedExtensionDoesNotDuplicateAnExistingKeyYAML(t *testing.T) {
	s := Schema{Type: "string", Extensions: map[string]any{"x-forge-id": true}}

	data := mustMarshalYAML(t, s)

	if got := strings.Count(string(data), "x-forge-id:"); got != 1 {
		t.Fatalf("x-forge-id appears %d times, want 1:\n%s", got, data)
	}

	// Re-marshalling what we just read back must stay stable.
	var back Schema
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	again := mustMarshalYAML(t, back)
	if string(again) != string(data) {
		t.Fatalf("second marshal differs.\nfirst:\n%s\nsecond:\n%s", data, again)
	}
}
