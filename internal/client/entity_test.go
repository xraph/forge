package client

import "testing"

func strSchema() *Schema { return &Schema{Type: "string"} }

func TestInferEntity(t *testing.T) {
	tests := []struct {
		name   string
		typeNm string
		schema *Schema
		want   *EntityRef
	}{
		{
			name:   "object with id is an entity",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id":    strSchema(),
				"total": {Type: "integer"},
			}},
			want: &EntityRef{Type: "Order", IDField: "id"},
		},
		{
			name:   "integer id is accepted",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id": {Type: "integer"},
			}},
			want: &EntityRef{Type: "Order", IDField: "id"},
		},
		{
			name:   "tenant_id alone is not identity",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"tenant_id": strSchema(),
			}},
			want: nil,
		},
		{
			name:   "forge id extension wins over name",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"order_number": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
			}},
			want: &EntityRef{Type: "Order", IDField: "order_number"},
		},
		{
			// The case ForgeEntity and `forge:"id"` exist for: the type has an
			// `id`, but it is not the identity. A declaration beats the name
			// heuristic outright, so this resolves to the marked field. It used
			// to count two identity fields and refuse, which meant marking a
			// field made the type stop being an entity instead of start being
			// one.
			name:   "explicit marker wins over a property named id",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id":           strSchema(),
				"order_number": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
			}},
			want: &EntityRef{Type: "Order", IDField: "order_number"},
		},
		{
			// Self-contradictory input: two fields each declared as the one
			// identity. There is no heuristic left that would not be overruling
			// a deliberate declaration, so refuse.
			name:   "two explicit markers refuse inference",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"order_number": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
				"uuid":         {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
			}},
			want: nil,
		},
		{
			// Nothing declared, two name matches (the name test is
			// case-insensitive). The original refusal, unchanged: guessing here
			// keys two records to one cache entry.
			name:   "two unmarked identity-named fields refuse inference",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id": strSchema(),
				"ID": strSchema(),
			}},
			want: nil,
		},
		{
			// A marker on a field that cannot serve as a cache key is not a
			// declaration at all, so the name rule still applies.
			name:   "marker on a non-identity-shaped field falls through to the name rule",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id":       strSchema(),
				"metadata": {Type: "object", Extensions: map[string]any{"x-forge-id": true}},
			}},
			want: &EntityRef{Type: "Order", IDField: "id"},
		},
		{
			name:   "object id is not identity-shaped",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id": {Type: "object"},
			}},
			want: nil,
		},
		{
			name:   "unnamed schema is never an entity",
			typeNm: "",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{"id": strSchema()}},
			want:   nil,
		},
		{
			name:   "non-object is never an entity",
			typeNm: "Order",
			schema: &Schema{Type: "array"},
			want:   nil,
		},
		{
			name:   "nil schema is safe",
			typeNm: "Order",
			schema: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferEntity(tt.typeNm, tt.schema)

			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("InferEntity = %+v, want nil", got)
			case tt.want != nil && got == nil:
				t.Fatalf("InferEntity = nil, want %+v", tt.want)
			// Compared field by field rather than by struct equality:
			// EntityRef now carries a map (Fields), which InferEntity never
			// populates -- that is resolveEntityFields' job, once the whole
			// spec is known -- and a struct holding a map is not comparable.
			case tt.want != nil && (got.Type != tt.want.Type || got.IDField != tt.want.IDField):
				t.Fatalf("InferEntity = %+v, want %+v", got, tt.want)
			}
		})
	}
}
