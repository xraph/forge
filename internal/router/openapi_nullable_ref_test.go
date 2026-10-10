package router

import (
	"encoding/json"
	"testing"
)

type quotaNullRef struct {
	Remaining *int64 `json:"remaining" nullable:"true"`
}

type nullableSource struct {
	Quota    *quotaNullRef `description:"Current quota" json:"quota" nullable:"true"`
	Ordinary *quotaNullRef `json:"ordinary,omitempty"`
}

func TestNullableNamedRefKeepsNullAtFieldBoundary(t *testing.T) {
	components := make(map[string]*Schema)
	g := newSchemaGenerator(components, nil)

	schema, err := g.GenerateSchema(nullableSource{})
	if err != nil {
		t.Fatal(err)
	}

	quota := schema.Properties["quota"]

	if quota.Ref != "" || len(quota.OneOf) != 2 || quota.OneOf[0].Ref != "#/components/schemas/quotaNullRef" || quota.OneOf[1].Type != "object" || !quota.OneOf[1].Nullable || len(quota.OneOf[1].Enum) != 1 || quota.OneOf[1].Enum[0] != nil || quota.Description != "Current quota" {
		t.Fatalf("invalid nullable wrapper: %+v", quota)
	}

	if ordinary := schema.Properties["ordinary"]; ordinary.Ref != "#/components/schemas/quotaNullRef" || len(ordinary.OneOf) > 0 {
		t.Fatal("ordinary reference changed", ordinary)
	}

	if components["quotaNullRef"].Nullable || !components["quotaNullRef"].Properties["remaining"].Nullable {
		t.Fatal("field nullability leaked into component")
	}

	raw, err := json.Marshal(quota)
	if err != nil {
		t.Fatal(err)
	}

	var wire map[string]any

	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}

	if _, hasRef := wire["$ref"]; hasRef {
		t.Fatal("ignored ref siblings emitted")
	}
}
