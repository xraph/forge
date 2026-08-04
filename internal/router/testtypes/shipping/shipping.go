// Package shipping provides test fixture types whose names deliberately collide
// with types in sibling fixture packages, so OpenAPI component-name collision
// handling can be exercised from the router tests.
package shipping

// Note is a nested type whose name collides with its sibling packages'.
type Note struct {
	Courier string `json:"courier"`
}

// Invoice is the shipping flavour of a colliding type name.
type Invoice struct {
	TrackingCode string  `json:"tracking_code"`
	WeightKg     float64 `json:"weight_kg"`
	Note         Note    `json:"note"`
}
