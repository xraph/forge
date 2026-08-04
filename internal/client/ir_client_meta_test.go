package client

import "testing"

func TestEndpointCarriesEntityAndCacheTags(t *testing.T) {
	ep := Endpoint{
		Method: "GET",
		Path:   "/orders/{id}",
		Tags:   []string{"orders"}, // OpenAPI tags, unrelated to cache tags
		Entity: &EntityRef{Type: "Order", IDField: "id"},
		CacheTags: TagSet{
			Provides: []string{"Order:{id}"},
		},
	}

	if ep.Entity.Type != "Order" {
		t.Fatalf("Entity.Type = %q, want Order", ep.Entity.Type)
	}

	if len(ep.CacheTags.Provides) != 1 || ep.CacheTags.Provides[0] != "Order:{id}" {
		t.Fatalf("CacheTags.Provides = %v, want [Order:{id}]", ep.CacheTags.Provides)
	}

	// OpenAPI tags and cache tags must remain independent fields.
	if len(ep.Tags) != 1 || ep.Tags[0] != "orders" {
		t.Fatalf("Tags = %v, want [orders]", ep.Tags)
	}
}

func TestStreamBindingIntents(t *testing.T) {
	b := StreamBinding{
		Message:     "order.created",
		EntityType:  "Order",
		Intent:      StreamUpsert,
		Invalidates: []string{"Order[]"},
	}

	if b.Intent != "upsert" {
		t.Fatalf("StreamUpsert = %q, want upsert", b.Intent)
	}
}
