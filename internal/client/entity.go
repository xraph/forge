package client

import "strings"

// InferEntity reports how a named schema is identified, or nil when the schema
// is not an entity.
//
// Resolution is two passes, and the order between them is the whole point.
//
// An EXPLICIT marker wins outright. A property carrying x-forge-id was declared
// as the identity by a human -- through the `forge:"id"` struct tag or through a
// type's ForgeEntity method -- and a declaration must beat a heuristic. Without
// this precedence the documented reason to reach for those mechanisms ("two
// fields are both identity-shaped and inference refuses to guess") did not
// work: a schema with an `id` property AND a marked `uuid` property counted two
// identity fields and resolved to nothing, so marking a field made the type stop
// being an entity rather than start being one.
//
// Only when nothing is marked does the `id` name heuristic apply, with its
// exactly-one guard unchanged.
//
// Refusing is the important half of both passes. A schema carrying two
// identity-shaped fields is ambiguous, and picking one collides two records
// under a single cache key. Where that second field is a tenant discriminator
// the result is a data leak wearing a caching bug's clothes, so ambiguity
// returns nil and the developer declares the identity explicitly.
//
// Two EXPLICIT markers refuse for a sharper reason: that input is
// self-contradictory. The developer named two different fields as the one
// identity, and choosing between two deliberate declarations is worse than
// declining -- there is no heuristic left to fall back on that would not be
// overruling somebody on purpose.
func InferEntity(name string, schema *Schema) *EntityRef {
	if name == "" || schema == nil || schema.Type != "object" {
		return nil
	}

	if marked := soleMatch(schema, isMarkedIdentityField); marked != "" {
		return &EntityRef{Type: name, IDField: marked}
	}

	if anyMatch(schema, isMarkedIdentityField) {
		return nil // two or more explicit, contradictory declarations
	}

	named := soleMatch(schema, isNamedIdentityField)
	if named == "" {
		return nil
	}

	return &EntityRef{Type: name, IDField: named}
}

// soleMatch returns the one property satisfying pred, or "" when none or more
// than one does.
func soleMatch(schema *Schema, pred func(string, *Schema) bool) string {
	found := ""

	for prop, ps := range schema.Properties {
		if !pred(prop, ps) {
			continue
		}

		if found != "" {
			return ""
		}

		found = prop
	}

	return found
}

// anyMatch reports whether any property satisfies pred. Paired with soleMatch
// it distinguishes "none matched" from "several matched", which have to be
// handled differently: the first falls through to the next pass, the second
// refuses.
func anyMatch(schema *Schema, pred func(string, *Schema) bool) bool {
	for prop, ps := range schema.Properties {
		if pred(prop, ps) {
			return true
		}
	}

	return false
}

// isMarkedIdentityField reports whether a property was explicitly declared as
// the identity, via the `forge:"id"` struct tag or a ForgeEntity method.
func isMarkedIdentityField(_ string, s *Schema) bool {
	if s == nil || !isIdentityType(s) {
		return false
	}

	v, ok := s.Extensions["x-forge-id"].(bool)

	return ok && v
}

// isNamedIdentityField reports whether a property identifies its containing
// object by name alone.
//
// The name test is exact rather than suffixed: `tenant_id` ends in "id" but
// identifies a tenant, not this record.
func isNamedIdentityField(prop string, s *Schema) bool {
	if s == nil || !isIdentityType(s) {
		return false
	}

	return strings.EqualFold(prop, "id")
}

// isIdentityType reports whether a schema can serve as a cache key component.
func isIdentityType(s *Schema) bool {
	return s.Type == "string" || s.Type == "integer"
}
