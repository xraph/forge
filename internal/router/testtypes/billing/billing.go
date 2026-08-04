// Package billing provides test fixture types whose names deliberately collide
// with types in sibling fixture packages, so OpenAPI component-name collision
// handling can be exercised from the router tests.
package billing

// Note is a nested type whose name collides with its sibling packages'.
type Note struct {
	Memo string `json:"memo"`
}

// Invoice is the billing flavour of a colliding type name.
type Invoice struct {
	InvoiceNumber string `json:"invoice_number"`
	AmountCents   int    `json:"amount_cents"`
	Note          Note   `json:"note"`
}
