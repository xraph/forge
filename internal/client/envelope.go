package client

import (
	"fmt"
	"sort"
	"strings"
)

// envelopeExtension marks a component schema as a wrapper around the entity it
// carries, rather than a record in its own right.
//
// It sits on the SCHEMA, not on the operation, because envelope-ness is
// intrinsic to the type: `PageOrder` is a page of orders no matter which of the
// eleven endpoints returns it, and declaring it once per type is the same
// choice `x-forge-id` already makes for identity. An operation that wants out
// has `x-forge-no-entity`, which already wins over everything here.
//
// Two spellings are accepted:
//
//	x-forge-envelope: true      resolve the sole entity-typed property
//	x-forge-envelope: "items"   that property carries the entity
//
// The first is the ergonomic form for the shape this exists to serve, where
// there is exactly one such property and naming it is noise. It REFUSES rather
// than picks when a schema has none or several, for the reason InferEntity
// refuses on two identity-shaped fields. The second is the escape for the
// schema where the automatic answer is wrong or absent.
const envelopeExtension = "x-forge-envelope"

// resolveEnvelopeEntity reports the entity a declared envelope carries, and
// whether it carries a collection of them.
//
// WHY A MARKER AND NOT A HEURISTIC. The tempting rule is "a named non-entity
// type with exactly one array-of-entity property is an envelope". It is
// tempting because it is nearly always right, and it must still be refused,
// because `PageOrder{items: []Order, total: int}` and
// `OrderReport{topOrders: []Order, generatedAt: string}` are the same shape.
// Nothing in the document distinguishes a page of the collection from a
// projection over it -- the difference is what the endpoint MEANS, and the
// heuristic would be inferring meaning from structure.
//
// What that inference would cost is specifically the tag claim. Deriving tags
// from a guessed envelope makes the report endpoint assert `provides:
// ['Order[]']`, which is the sentence "this response is the Order collection".
// The invalidation graph then carries an edge nobody wrote, and every order
// mutation refetches a report that was never a view of the collection. That is
// the exact class of false edge `WithoutEntity()` exists to let a developer
// remove, and adding it by inference while offering an escape hatch from it is
// backwards.
//
// The cost of refusing is much smaller than it looks, and that asymmetry is the
// decision. NORMALIZATION DOES NOT DEPEND ON THIS FUNCTION AT ALL.
// resolveEntityFields gives `PageOrder` a routing row from pure reachability --
// no policy, no guess -- so the orders inside a page land in the store keyed
// correctly whether or not anyone declares anything. What a declaration buys is
// only the cache CONTRACT: `ops.orderList.entity`, and the `Order:{id}` /
// `Order[]` tags. So the guess would have bought the part that is safe to infer
// nothing extra, and the part that is unsafe to infer everything.
func resolveEnvelopeEntity(spec *APISpec, ep *Endpoint, rootName string) (*EntityRef, bool) {
	if rootName == "" || spec == nil {
		return nil, false
	}

	root := spec.Schemas[rootName]
	if root == nil {
		return nil, false
	}

	marker, declared := root.Extensions[envelopeExtension]
	if !declared {
		return nil, false
	}

	prop, ok := envelopeProperty(spec, ep, rootName, root, marker)
	if !ok {
		return nil, false
	}

	target, isList := namedTarget(root.Properties[prop], 0)

	entity := InferEntity(target, spec.Schemas[target])
	if entity == nil {
		// Resolved against the component schema rather than against
		// spec.Entities, which would make the answer depend on whether some
		// other endpoint returning a bare Order had already been walked. It
		// also means a paginated list can be the ONLY endpoint an entity ever
		// appears through, which for a collection resource is the normal case.
		spec.Warnings = append(spec.Warnings, fmt.Sprintf(
			"client: %s %s returns %s, declared %s on property %q, but %q is not an entity"+
				" (no identity-shaped field, or an ambiguous one). This response will normalize"+
				" but provide no cache tags.",
			ep.Method, ep.Path, rootName, envelopeExtension, prop, target))

		return nil, false
	}

	return entity, isList
}

// envelopeProperty resolves the declaration to the property carrying the
// entity.
func envelopeProperty(
	spec *APISpec, ep *Endpoint, rootName string, root *Schema, marker any,
) (string, bool) {
	switch m := marker.(type) {
	case string:
		return namedEnvelopeProperty(spec, ep, rootName, root, m)

	case bool:
		// `false` is a deliberate "this is not an envelope", which matters for
		// a type whose schema is assembled from a shared base that sets it.
		if !m {
			return "", false
		}

		return soleEntityProperty(spec, ep, rootName, root)
	}

	spec.Warnings = append(spec.Warnings, fmt.Sprintf(
		"client: %s %s returns %s, whose %s is %T; expected true or a property name."+
			" This response will normalize but provide no cache tags.",
		ep.Method, ep.Path, rootName, envelopeExtension, marker))

	return "", false
}

// namedEnvelopeProperty validates the explicit `x-forge-envelope: "items"`
// spelling.
func namedEnvelopeProperty(
	spec *APISpec, ep *Endpoint, rootName string, root *Schema, prop string,
) (string, bool) {
	ps, ok := root.Properties[prop]
	if !ok {
		have := make([]string, 0, len(root.Properties))
		for name := range root.Properties {
			have = append(have, name)
		}

		sort.Strings(have)

		spec.Warnings = append(spec.Warnings, fmt.Sprintf(
			"client: %s %s returns %s, whose %s names property %q, which the schema does not"+
				" have (has: %s). This response will normalize but provide no cache tags.",
			ep.Method, ep.Path, rootName, envelopeExtension, prop, strings.Join(have, ", ")))

		return "", false
	}

	if target, _ := namedTarget(ps, 0); target == "" {
		spec.Warnings = append(spec.Warnings, fmt.Sprintf(
			"client: %s %s returns %s, whose %s names property %q, which does not resolve to one"+
				" named type (an inline object has no name to key a cache entry by)."+
				" This response will normalize but provide no cache tags.",
			ep.Method, ep.Path, rootName, envelopeExtension, prop))

		return "", false
	}

	return prop, true
}

// soleEntityProperty resolves the `x-forge-envelope: true` spelling to the one
// property whose type is an entity.
//
// Candidates are collected and then counted rather than short-circuited on the
// second hit, so the warning names every candidate in sorted order. Go
// randomises map iteration and spec.Warnings is surfaced by the generators, so
// a message built as the loop happened to run would differ between two parses
// of one file -- the same churn sortedPathKeys exists to prevent.
func soleEntityProperty(
	spec *APISpec, ep *Endpoint, rootName string, root *Schema,
) (string, bool) {
	candidates := make([]string, 0, len(root.Properties))

	for prop, ps := range root.Properties {
		target, _ := namedTarget(ps, 0)
		if target == "" || InferEntity(target, spec.Schemas[target]) == nil {
			continue
		}

		candidates = append(candidates, prop)
	}

	sort.Strings(candidates)

	switch len(candidates) {
	case 1:
		return candidates[0], true

	case 0:
		spec.Warnings = append(spec.Warnings, fmt.Sprintf(
			"client: %s %s returns %s, declared %s, but no property of it resolves to an entity."+
				" This response will normalize but provide no cache tags.",
			ep.Method, ep.Path, rootName, envelopeExtension))

	default:
		spec.Warnings = append(spec.Warnings, fmt.Sprintf(
			"client: %s %s returns %s, declared %s, but %d of its properties carry an entity"+
				" (%s). Which one the response is a page of cannot be guessed from shape;"+
				" name it as %s: \"<property>\". This response will normalize but provide no"+
				" cache tags.",
			ep.Method, ep.Path, rootName, envelopeExtension, len(candidates),
			strings.Join(candidates, ", "), envelopeExtension))
	}

	return "", false
}
