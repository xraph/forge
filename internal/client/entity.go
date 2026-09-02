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
func InferEntity(spec *APISpec, name string, schema *Schema) *EntityRef {
	props := EntityProperties(spec, schema)
	if name == "" || len(props) == 0 {
		return nil
	}

	if marked := soleMatch(props, isMarkedIdentityField); marked != "" {
		return &EntityRef{Type: name, IDField: marked}
	}

	if anyMatch(props, isMarkedIdentityField) {
		return nil // two or more explicit, contradictory declarations
	}

	named := soleMatch(props, isNamedIdentityField)
	if named == "" {
		return nil
	}

	return &EntityRef{Type: name, IDField: named}
}

// EntityProperties returns the properties a schema effectively carries, or nil
// when the schema is not an object at all.
//
// Reading schema.Properties directly -- which is what every caller here used
// to do -- answers a narrower question than the one they are asking, and the
// gap between the two costs a whole service's cache metadata.
//
// TWO LEGAL SPELLINGS OF "AN OBJECT WITH AN id" WERE INVISIBLE.
//
// An allOf composition owns no properties of its own. `Composed: {allOf:
// [{$ref: Base}, {properties: {name}}]}` has an empty Properties map and, as
// documents in the wild almost always write it, no `type` either. The
// TypeScript emitter resolves that shape completely -- flattenAllOfLayers
// walks the members, and types.ts gets `type Composed = Base & {name?: string}`
// with every field on it -- so a document composing its responses this way
// generated full types and full codecs beside an entity table with nothing in
// it. Every read from that service became an opaque document in the consumer's
// cache, and no output said so.
//
// A schema can also declare `properties` and omit `type`. OpenAPI does not
// require the keyword; `properties` implies an object, and plenty of
// generators leave it off. The old `schema.Type != "object"` test read that as
// "not an object" and refused.
//
// ONLY allOf COMPOSES. oneOf and anyOf are a choice between shapes, not a sum
// of them, and merging their members' properties would describe a payload no
// response ever carries -- an identity inferred from it would key records by a
// field half of them do not have. A property whose TYPE is a oneOf wrapper is a
// different question, and namedSchemaTarget already answers it.
//
// The first definition of a property name wins. Members of one allOf
// redeclaring a property with different types is a contradictory document
// rather than a shape to reconcile, and identity resolution needs one answer
// per name; taking the first makes the answer stable across runs.
//
// spec may be nil, which costs only the $ref hops: a composition of inline
// members still resolves.
func EntityProperties(spec *APISpec, schema *Schema) map[string]*Schema {
	if !isObjectShaped(schema) {
		return nil
	}

	out := make(map[string]*Schema, len(schema.Properties))
	collectProperties(spec, schema, out, make(map[*Schema]bool), 0)

	if len(out) == 0 {
		return nil
	}

	return out
}

// isObjectShaped reports whether a schema describes an object, allowing for the
// two spellings that omit the keyword: properties without `type`, and an allOf
// composition that declares neither.
//
// A schema naming some OTHER type is still refused. `type: "array"` with
// properties hanging off it is a malformed document, and reading it as an
// object would put an array in the store under a record's key.
func isObjectShaped(schema *Schema) bool {
	if schema == nil {
		return false
	}

	if schema.Type == "object" {
		return true
	}

	if schema.Type != "" {
		return false
	}

	return len(schema.Properties) > 0 || len(schema.AllOf) > 0
}

// collectProperties merges schema's own properties and those of every allOf
// member into out, resolving a member's $ref through spec.
//
// Termination is this function's job, for the same reason it is
// resolveEntityFields': a component graph really does cycle, and `allOf:
// [{$ref: Self}]` is a document a parser will hand over without complaint. The
// visited set covers the shared-pointer case and the depth bound covers a
// hand-built Schema value whose members point back at each other, which no
// parser produces but a test can.
func collectProperties(spec *APISpec, schema *Schema, out map[string]*Schema, seen map[*Schema]bool, depth int) {
	if schema == nil || seen[schema] || depth > maxCompositionDepth {
		return
	}

	seen[schema] = true

	for prop, ps := range schema.Properties {
		if _, taken := out[prop]; !taken {
			out[prop] = ps
		}
	}

	for _, member := range schema.AllOf {
		collectProperties(spec, resolveComposed(spec, member), out, seen, depth+1)
	}
}

// resolveComposed follows an allOf member that is a bare $ref to the component
// it names, and returns anything else untouched.
func resolveComposed(spec *APISpec, member *Schema) *Schema {
	if member == nil || spec == nil || member.Ref == "" {
		return member
	}

	if target := spec.Schemas[ComponentRefName(member.Ref)]; target != nil {
		return target
	}

	return member
}

// maxCompositionDepth bounds how deep an allOf chain is followed. Real
// documents nest one or two levels; the bound is here so a cyclic hand-built
// Schema is a refusal rather than a stack overflow.
const maxCompositionDepth = 16

// soleMatch returns the one property satisfying pred, or "" when none or more
// than one does.
func soleMatch(props map[string]*Schema, pred func(string, *Schema) bool) string {
	found := ""

	for prop, ps := range props {
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
func anyMatch(props map[string]*Schema, pred func(string, *Schema) bool) bool {
	for prop, ps := range props {
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
