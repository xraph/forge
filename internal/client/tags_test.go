// internal/client/tags_test.go
package client

import (
	"reflect"
	"testing"
)

func TestDeriveTags(t *testing.T) {
	order := &EntityRef{Type: "Order", IDField: "id"}

	tests := []struct {
		name   string
		method string
		entity *EntityRef
		isList bool
		want   TagSet
	}{
		{
			name: "get one provides the item", method: "GET", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}},
		},
		{
			name: "get list provides item and collection", method: "GET", entity: order, isList: true,
			want: TagSet{Provides: []string{"Order:{id}", "Order[]"}},
		},
		{
			name: "post provides item and invalidates collection", method: "POST", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
		},
		{
			name: "patch invalidates the collection too", method: "PATCH", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
		},
		{
			name: "delete only invalidates", method: "DELETE", entity: order,
			want: TagSet{Invalidates: []string{"Order[]"}},
		},
		{
			name: "no entity means no tags", method: "POST", entity: nil,
			want: TagSet{},
		},
		{
			name: "method case is normalised", method: "post", entity: order,
			want: TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveTags(tt.method, tt.entity, tt.isList)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("DeriveTags = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestApplyTagOverrides(t *testing.T) {
	base := TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Order[]"}}

	got := ApplyTagOverrides(base, []string{"Inventory[]"}, []string{"Order[]"})

	want := TagSet{Provides: []string{"Order:{id}"}, Invalidates: []string{"Inventory[]"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplyTagOverrides = %+v, want %+v", got, want)
	}
}

// Output order must not depend on map iteration; generated files are diffed in CI.
func TestApplyTagOverridesIsSorted(t *testing.T) {
	got := ApplyTagOverrides(TagSet{}, []string{"Zebra[]", "Alpha[]", "Middle[]"}, nil)

	want := []string{"Alpha[]", "Middle[]", "Zebra[]"}
	if !reflect.DeepEqual(got.Invalidates, want) {
		t.Fatalf("Invalidates = %v, want %v", got.Invalidates, want)
	}
}

func TestApplyTagOverridesDeduplicates(t *testing.T) {
	base := TagSet{Invalidates: []string{"Order[]"}}

	got := ApplyTagOverrides(base, []string{"Order[]"}, nil)

	if len(got.Invalidates) != 1 {
		t.Fatalf("Invalidates = %v, want one entry", got.Invalidates)
	}
}
