package client

import "testing"

func strSchema() *Schema { return &Schema{Type: "string"} }

func TestInferEntity(t *testing.T) {
	tests := []struct {
		name    string
		typeNm  string
		schema  *Schema
		want    *EntityRef
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
			name:   "two identity fields refuse inference",
			typeNm: "Order",
			schema: &Schema{Type: "object", Properties: map[string]*Schema{
				"id":           strSchema(),
				"order_number": {Type: "string", Extensions: map[string]any{"x-forge-id": true}},
			}},
			want: nil,
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
			case tt.want != nil && (*got != *tt.want):
				t.Fatalf("InferEntity = %+v, want %+v", got, tt.want)
			}
		})
	}
}
