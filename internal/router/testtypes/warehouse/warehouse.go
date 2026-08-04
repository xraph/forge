// Package warehouse provides test fixture types whose names deliberately collide
// with types in sibling fixture packages, so OpenAPI component-name collision
// handling can be exercised from the router tests.
package warehouse

// Invoice is the warehouse flavour of a colliding type name.
type Invoice struct {
	BinLocation string `json:"bin_location"`
	Pallets     int    `json:"pallets"`
}

// Receipt is a type whose name does not collide with anything else.
type Receipt struct {
	ReceiptID string `json:"receipt_id"`
}

// Gadget is a type whose inferred component name is contested by names the user
// pins explicitly -- a `schema:"Gadget"` struct tag, or an EnumNamer returning
// "Gadget" -- rather than by another inferred name.
type Gadget struct {
	SerialNumber string `json:"serial_number"`
	Slots        int    `json:"slots"`
}
