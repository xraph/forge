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
// NORMALIZATION DOES NOT DEPEND ON THIS FUNCTION AT ALL. resolveEntityFields
// gives `PageOrder` a routing row from pure reachability -- no policy, no guess
// -- so the orders inside a page land in the store keyed correctly whether or
// not anyone declares anything. What a declaration buys is only the cache
// CONTRACT: `ops.orderList.entity`, and the `Order:{id}` / `Order[]` tags.
//
// WHAT CHANGED, AND WHAT DID NOT. The argument above is still why this function
// wants a marker, and the marker still wins outright: a declaration names the
// property, so it resolves shapes inference will not touch -- a single-record
// `{data: Order, meta}` wrapper, or a page whose element type sits behind two
// candidate properties. What the argument turned out to get wrong is the price
// of refusing. Measured across four generated clients, four reads in five
// resolved nothing, because almost nothing declares. inferCollectionEnvelope is
// the fallback that case forced, and it carries the rest of the reasoning.
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

	entity := InferEntity(spec, target, spec.Schemas[target])
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
		if target == "" || InferEntity(spec, target, spec.Schemas[target]) == nil {
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

// inferCollectionEnvelope resolves an UNDECLARED wrapper to the entity carried
// by its sole array-of-entity property, for a read.
//
// WHY THIS IS INFERRED WHEN resolveEnvelopeEntity REFUSES TO INFER. The
// objection above is sound and still stands: `PageOrder{items: []Order, total}`
// and `OrderReport{topOrders: []Order, generatedAt}` are the same shape, so a
// rule keyed on shape tags the report as the collection. What has changed is
// the measured price of refusing. Across four generated clients roughly four
// reads in five resolved no entity at all, and the correlation with a missing
// entity was exact -- every one of them a list or an envelope. A mutation's
// `invalidates` only reaches a query that declares the matching tag in its own
// `provides`, so `membership.invite` invalidating `Invitation[]` reached
// nothing, and consumers kept the hand-written refetches the normalized cache
// exists to replace.
//
// So the two errors trade against each other, and they are not the same size.
// Tagging a report over orders costs a refetch of that report whenever an order
// changes -- and a report derived from orders is usually stale after one
// anyway. Refusing to tag a page of orders costs a list that never refreshes
// until someone writes the refetch by hand. That is the trade DeriveTags
// already makes for PATCH, in the same words: over-refetching is a performance
// defect a profiler finds, under-refetching is a stale row a user reports three
// weeks later. The route keeps `x-forge-no-entity` to remove the false edge,
// which is the direction an escape hatch should point.
//
// WHAT IS DELIBERATELY NOT WIDENED. Identity still comes from InferEntity
// alone, with its refusal on two identity-shaped fields intact. This function
// only reaches THROUGH a collection to a type that already resolves on its own,
// so it cannot key a record by a tenant discriminator -- the failure mode that
// would collide two tenants under one cache entry. Nothing here guesses an
// idField; a type that does not resolve stays untagged.
//
// Three further guards keep the rule narrow:
//
//   - The property must be an ARRAY of the entity. A wrapper carrying one
//     record -- a callback response holding the signed-in user -- is not a
//     collection read, and giving it the item tag would wire every write of
//     that type to an operation that never read it.
//   - Exactly one property may qualify, or the answer is a guess between two
//     membership claims. This is soleEntityProperty's rule, for its reason.
//   - Reads only. On a write, DeriveTags turns the same entity into
//     `invalidates`, so this rule applied to a POST would let a search
//     returning `{results: []Order}` evict every order list in the store. A
//     human may still declare that; shape alone may not assert it.
func inferCollectionEnvelope(spec *APISpec, method, rootName string) (*EntityRef, bool) {
	if rootName == "" || spec == nil || !isReadMethod(method) {
		return nil, false
	}

	root := spec.Schemas[rootName]
	if root == nil || root.Type != "object" {
		return nil, false
	}

	// A declared envelope was already resolved, warnings and all. `false` is a
	// deliberate "not an envelope" and a declaration that failed to resolve
	// asked the question explicitly and got a diagnostic; answering either by
	// shape would make the declaration not work.
	if _, declared := root.Extensions[envelopeExtension]; declared {
		return nil, false
	}

	entity, ok := soleCollectionEntity(spec, root)
	if !ok {
		return nil, false
	}

	return entity, true
}

// soleCollectionEntity returns the entity carried by the one array-typed
// property of root that resolves to one, or false when none or several do.
//
// Unlike the declared path this stays silent either way. A declaration is a
// question the developer asked and deserves an answer; shape is not, and
// warning on every read whose response happens to be an object would bury the
// warnings that name a real mistake.
func soleCollectionEntity(spec *APISpec, root *Schema) (*EntityRef, bool) {
	var found *EntityRef

	// Map order is not fixed and does not need to be: the answer is the sole
	// qualifying property or nothing, which is the same set whichever order the
	// loop runs in. The declared path sorts because it names its candidates in a
	// warning; this one emits none.
	for _, ps := range root.Properties {
		target, isList := namedTarget(ps, 0)
		if !isList || target == "" {
			continue
		}

		entity := InferEntity(spec, target, spec.Schemas[target])
		if entity == nil {
			continue
		}

		if found != nil {
			return nil, false // two candidate collections: which one is a guess
		}

		found = entity
	}

	return found, found != nil
}

// isReadMethod reports whether a method's derived contract is provides-only.
// Kept next to its caller rather than in tags.go because it exists to bound
// inference, not to classify methods: DeriveTags switches on the same two names
// and the two must not drift apart.
func isReadMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "GET", "HEAD":
		return true
	}

	return false
}
