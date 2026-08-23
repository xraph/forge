package client

import "testing"

func TestComponentRefNameIsStrict(t *testing.T) {
	cases := map[string]string{
		"#/components/schemas/Order":         "Order",
		"#/components/schemas/Order_Summary": "Order_Summary",
		"#/components/responses/Order":       "",
		"#/definitions/Order":                "",
		"https://example.com/schemas#/Order": "",
		"Order":                              "",
		"":                                   "",
	}

	for ref, want := range cases {
		if got := ComponentRefName(ref); got != want {
			t.Errorf("ComponentRefName(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestRefTargetNameTakesTheLastSegmentOfALocalPointer(t *testing.T) {
	cases := map[string]string{
		"#/components/schemas/Order":   "Order",
		"#/components/responses/Order": "Order",
		"#/definitions/Order":          "Order",
		"#/":                           "",

		// Remote pointers name nothing this document holds.
		"https://example.com/schemas#/Order": "",
		"other.yaml#/components/schemas/X":   "",
		"Order":                              "",
		"":                                   "",
	}

	for ref, want := range cases {
		if got := refTargetName(ref); got != want {
			t.Errorf("refTargetName(%q) = %q, want %q", ref, got, want)
		}
	}
}

// The reachability walk and the entity edge graph must agree about what a
// pointer names. They read the same documents, and while one was strict about
// the components/schemas prefix and the other took the last segment, a
// document written with any other pointer shape got edges to names the walk
// never marked -- so the walk pruned the row out from under a live edge.
func TestPruningAgreesWithTheEdgeGraphOnPointerShape(t *testing.T) {
	spec := &APISpec{
		Endpoints: []Endpoint{
			{
				Path:     "/shop/orders",
				Method:   "GET",
				RootType: "Order",
				Responses: map[int]*Response{
					200: {Content: map[string]*MediaType{
						"application/json": {Schema: &Schema{Ref: "#/definitions/Order"}},
					}},
				},
			},
			{Path: "/admin/tickets", Method: "GET"},
		},
		Schemas: map[string]*Schema{
			"Order": {Type: "object", Properties: map[string]*Schema{
				"id":       {Type: "string"},
				"customer": {Ref: "#/definitions/Customer"},
			}},
			"Customer": {Type: "object", Properties: map[string]*Schema{"id": {Type: "string"}}},
			"Orphan":   {Type: "object"},
		},
		Entities: map[string]*EntityRef{
			"Order":    {Type: "Order", IDField: "id", Fields: map[string]string{"customer": "Customer"}},
			"Customer": {Type: "Customer", IDField: "id"},
		},
	}

	spec.Apply(PathFilter{Include: []string{"/shop/**"}})

	for _, name := range []string{"Order", "Customer"} {
		if _, ok := spec.Entities[name]; !ok {
			t.Errorf("%s is reachable through a non-canonical pointer and was pruned", name)
		}

		if _, ok := spec.Schemas[name]; !ok {
			t.Errorf("%s's schema is reachable through a non-canonical pointer and was pruned", name)
		}
	}

	if _, ok := spec.Schemas["Orphan"]; ok {
		t.Error("Orphan is reachable from nothing and should still have been pruned")
	}
}
