package client

import "strings"

// InferEntity reports how a named schema is identified, or nil when the schema
// is not an entity.
//
// Refusing is the important half. A schema carrying two identity-shaped fields
// is ambiguous, and picking one collides two records under a single cache key.
// Where that second field is a tenant discriminator the result is a data leak
// wearing a caching bug's clothes, so ambiguity returns nil and the developer
// declares the identity explicitly.
func InferEntity(name string, schema *Schema) *EntityRef {
	if name == "" || schema == nil || schema.Type != "object" {
		return nil
	}

	found := ""

	for prop, ps := range schema.Properties {
		if !isIdentityField(prop, ps) {
			continue
		}

		if found != "" {
			return nil // ambiguous
		}

		found = prop
	}

	if found == "" {
		return nil
	}

	return &EntityRef{Type: name, IDField: found}
}

// isIdentityField reports whether a property identifies its containing object.
//
// The name test is exact rather than suffixed: `tenant_id` ends in "id" but
// identifies a tenant, not this record.
func isIdentityField(prop string, s *Schema) bool {
	if s == nil || !isIdentityType(s) {
		return false
	}

	if v, ok := s.Extensions["x-forge-id"].(bool); ok && v {
		return true
	}

	return strings.EqualFold(prop, "id")
}

// isIdentityType reports whether a schema can serve as a cache key component.
func isIdentityType(s *Schema) bool {
	return s.Type == "string" || s.Type == "integer"
}
