package router

import (
	"encoding/json"
	"testing"
)

// A json.RawMessage is written out as the JSON it holds, which is usually an
// object, so it has to read as any JSON. Reading it as a string makes every
// strict client refuse the response.
func TestRawMessageIsAnyJSON(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	type event struct {
		Data     json.RawMessage   `json:"data"`
		Optional *json.RawMessage  `json:"optional,omitempty"`
		Many     []json.RawMessage `json:"many"`
	}

	schema, err := gen.GenerateSchema(&event{})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"data", "optional"} {
		field, ok := schema.Properties[name]
		if !ok {
			t.Fatalf("no %q property", name)
		}

		if field.Type != "" || field.Format != "" {
			t.Errorf("%s: want any JSON (no type, no format), got type %q format %q", name, field.Type, field.Format)
		}
	}

	many := schema.Properties["many"]
	if many == nil || many.Type != "array" || many.Items == nil || many.Items.Type != "" {
		t.Errorf("many: want an array of any JSON, got %+v", many)
	}
}
