package router

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/internal/router/testtypes/billing"
	"github.com/xraph/forge/internal/router/testtypes/shipping"
)

func TestPointerArrayItemsAdmitNull(t *testing.T) {
	for name, value := range map[string]any{
		"slice":             []*float64{},
		"array":             [2]*float64{},
		"multiple pointers": []**float64{},
	} {
		t.Run(name, func(t *testing.T) {
			g := newSchemaGenerator(map[string]*Schema{}, nil)

			schema, err := g.GenerateSchema(value)
			if err != nil || schema.Type != "array" || schema.Nullable || schema.Items.Type != "number" || !schema.Items.Nullable {
				t.Fatalf("pointer items lost nullability: %+v, %v", schema, err)
			}
		})
	}

	var row struct {
		Values []*float64   `json:"values"`
		Plain  []float64    `json:"plain"`
		Nested [][]*float64 `json:"nested"`
		Dates  []*time.Time `json:"dates"`
	}

	g := newSchemaGenerator(map[string]*Schema{}, nil)

	schema, err := g.GenerateSchema(row)
	if err != nil {
		t.Fatal(err)
	}

	if !schema.Properties["values"].Items.Nullable || schema.Properties["plain"].Items.Nullable || !schema.Properties["nested"].Items.Items.Nullable || schema.Properties["nested"].Items.Nullable {
		t.Fatal("array nullability leaked across item boundaries")
	}

	if dates := schema.Properties["dates"].Items; !dates.Nullable || dates.Type != "string" || dates.Format != "date-time" {
		t.Fatal("nullable date format lost", dates)
	}
}

func TestPointerArrayItemsPreserveComponentBoundaries(t *testing.T) {
	var row struct {
		Nullable    []*quotaNullRef    `json:"nullable"`
		Fixed       [2]*quotaNullRef   `json:"fixed"`
		Ordinary    []quotaNullRef     `json:"ordinary"`
		States      []*TestQueryStatus `json:"states"`
		PlainStates []TestQueryStatus  `json:"plain_states"`
	}

	components := map[string]*Schema{}
	g := newSchemaGenerator(components, nil)

	schema, err := g.GenerateSchema(row)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"nullable", "fixed", "states"} {
		item := schema.Properties[name].Items
		if item.Ref != "" || len(item.OneOf) != 2 || item.OneOf[0].Ref == "" || !item.OneOf[1].Nullable || len(item.OneOf[1].Enum) != 1 || item.OneOf[1].Enum[0] != nil {
			t.Fatal("nullable item reference wrapper missing", name, item)
		}

		raw, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}

		var wire map[string]any
		if json.Unmarshal(raw, &wire) != nil || wire["$ref"] != nil {
			t.Fatal("nullable item emitted ignored reference siblings")
		}
	}

	for _, name := range []string{"ordinary", "plain_states"} {
		if item := schema.Properties[name].Items; item.Ref == "" || len(item.OneOf) != 0 || item.Nullable {
			t.Fatal("nonpointer reference changed", name, item)
		}
	}

	for name, component := range components {
		if component.Nullable || len(component.OneOf) != 0 {
			t.Fatal("item nullability leaked into shared component", name)
		}
	}
}

func TestPointerArrayItemsTrackComponentRenames(t *testing.T) {
	var row struct {
		Billing  []*billing.Invoice  `json:"billing"`
		Shipping []*shipping.Invoice `json:"shipping"`
	}

	components := map[string]*Schema{}
	g := newSchemaGenerator(components, nil)

	schema, err := g.GenerateSchema(row)
	if err != nil {
		t.Fatal(err)
	}

	g.finalizeComponentNames()

	refs := map[string]bool{}

	for _, name := range []string{"billing", "shipping"} {
		item := schema.Properties[name].Items
		ref := item.OneOf[0].Ref

		component := strings.TrimPrefix(ref, componentRefPrefix)
		if component == "Invoice" || components[component] == nil || refs[ref] {
			t.Fatal("nullable item lost renamed component reference", ref)
		}

		refs[ref] = true
	}
}
