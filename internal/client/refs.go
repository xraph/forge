package client

import "strings"

// componentRefPrefix is the canonical local pointer this package emits, and
// the only shape StripPrefix rebuilds a rewritten reference into.
const componentRefPrefix = "#/components/schemas/"

// ComponentRefName returns the component name a canonical schema pointer names,
// and "" for anything else.
//
// Strict on purpose. Its callers are the ones that must not guess: resolving a
// reference against spec.Schemas, and deciding whether a codec exists for a
// body. A pointer into some other part of the document is not a component
// schema, and answering with its last path segment would hand back a name that
// happens to collide with one.
//
// Exported because the TypeScript generator asks the same question and used to
// answer it with its own copy of this constant. Two implementations of one
// rule is how the two drift.
func ComponentRefName(ref string) string {
	name, ok := strings.CutPrefix(ref, componentRefPrefix)
	if !ok {
		return ""
	}

	return name
}

// refTargetName is ComponentRefName's permissive twin: the last segment of any
// local pointer, and "" for a remote or empty one.
//
// The two exist separately because reachability and resolution want opposite
// failure modes, and for a while they had them by accident rather than by
// decision. The entity edge graph names a property's type with this rule, so a
// document written with any other pointer shape gets edges built to a name a
// strict walk would never mark, and the row behind that name is pruned out
// from under an edge that still points at it. Nothing in this repository emits
// such a pointer today, so nothing was hitting it; the divergence was a defect
// waiting for the first hand-written specification that did.
//
// Being over-inclusive is the safe direction. A pointer into another section
// whose last segment happens to match a component name keeps a schema nothing
// reads, which costs bytes. Under-including drops a row something reads, which
// costs a cache entry that never matches and says nothing about why.
func refTargetName(ref string) string {
	if !strings.HasPrefix(ref, "#/") {
		return ""
	}

	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}

	return ""
}
