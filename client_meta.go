package forge

import "github.com/xraph/forge/internal/router"

// EntityDef declares how a type is identified in a client-side normalized cache.
//
// IDField is the JSON property name in the response body -- `id`,
// `order_number` -- not the Go field name.
type EntityDef = router.EntityDef

// ForgeEntity is implemented by types that override entity inference. The
// schema generator honours it, marking the declared id property so identity
// travels with the type rather than being repeated on every route.
//
// The marker beats the `id` name heuristic, so this is the way to say "the
// field named id is not what identifies this record". Declare identity once per
// type: this and the `forge:"id"` struct tag write the same marker, and using
// both on different fields resolves to no entity at all.
//
// Example:
//
//	type Order struct {
//	    Number string `json:"order_number"`
//	    Total  int    `json:"total"`
//	}
//
//	func (Order) ForgeEntity() forge.EntityDef {
//	    return forge.EntityDef{Type: "Order", IDField: "order_number"}
//	}
type ForgeEntity = router.ForgeEntity

// StreamIntent is what a stream message does to the cache.
type StreamIntent = router.StreamIntent

// StreamBinding binds one channel message to an entity type.
type StreamBinding = router.StreamBinding

// EmitsBuilder accumulates one stream binding.
type EmitsBuilder = router.EmitsBuilder

const (
	StreamUpsert = router.StreamUpsert
	StreamPatch  = router.StreamPatch
	StreamEvict  = router.StreamEvict
)

// Emits declares that a channel emits `message` carrying entity T.
//
// Example:
//
//	router.WebSocket("/ws/orders", handler,
//	    forge.WithStreamBinding(
//	        forge.Emits[Order]("order.created"),
//	        forge.Emits[Order]("order.updated"),
//	        forge.Emits[Order]("order.deleted"),
//	    ),
//	)
func Emits[T any](message string) *EmitsBuilder { return router.Emits[T](message) }

// WithEntity overrides inferred identity for this endpoint's response.
//
// IDField is the JSON property name, so it must match the key that appears in
// the response body -- `id`, not the Go field name `ID`. Generation warns when
// the named property is absent from the response schema, because as declared it
// would produce a cache key that never matches a record.
//
// Prefer implementing ForgeEntity on the type: identity is intrinsic to a type,
// and declaring it per route repeats it on every endpoint returning an Order.
// This option exists for types you cannot add a method to, and for the one
// endpoint whose response is identified differently from the rest.
//
// Example:
//
//	router.GET("/orders/{id}", getOrder,
//	    forge.WithEntity(forge.EntityDef{Type: "Order", IDField: "id"}),
//	)
func WithEntity(def EntityDef) RouteOption { return router.WithEntity(def) }

// WithoutEntity keeps this endpoint's response out of the normalized store.
// Use it for projections and snapshots that must not merge with the canonical
// record.
//
// Example:
//
//	router.GET("/orders/{id}/audit-snapshot", h, forge.WithoutEntity())
func WithoutEntity() RouteOption { return router.WithoutEntity() }

// WithInvalidates declares cross-entity invalidation effects.
// Same-entity invalidation is derived, so this is only for edges a reader
// would not predict.
//
// Example:
//
//	router.POST("/orders", createOrder,
//	    forge.WithInvalidates("Inventory[]", "Customer:{req.customerId}"),
//	)
func WithInvalidates(tags ...string) RouteOption { return router.WithInvalidates(tags...) }

// WithoutInvalidation suppresses a derived invalidation for endpoints that
// cannot change list membership.
func WithoutInvalidation(tags ...string) RouteOption {
	return router.WithoutInvalidation(tags...)
}

// WithStreamBinding declares which entity updates a channel emits.
//
// Example:
//
//	router.WebSocket("/ws/orders", handler,
//	    forge.WithStreamBinding(
//	        forge.Emits[Order]("order.created"),
//	        forge.Emits[Order]("order.updated"),
//	        forge.Emits[Order]("order.deleted"),
//	    ),
//	)
func WithStreamBinding(builders ...*EmitsBuilder) RouteOption {
	return router.WithStreamBinding(builders...)
}
