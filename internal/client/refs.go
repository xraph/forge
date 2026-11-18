package client

import (
	"fmt"
	"strings"
)

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

// ValidateRefs warns about every canonical component pointer in the surface
// this client ships that names a component the client does not carry.
//
// It runs after the path filter rather than instead of it, and over what
// survived rather than over what was reachable. Those are different sets and
// the difference is the whole reason this exists as a pass of its own:
//
//   - Reachability stops at the first undeclared name. A pointer at a
//     component nobody declared marks a name with no schema behind it and the
//     walk ends there, so a broken pointer buried in a component that no
//     endpoint reaches is invisible to it. With no filter set, that component
//     is emitted anyway, and the hole surfaces as generated code naming a type
//     that was never generated.
//   - Neither production caller even calls Apply when the filter is empty, so
//     anything hung off the filter reports nothing for the majority of runs.
//
// Every component schema is therefore a root here, alongside the endpoints and
// channels, and no root resolves its own pointers -- see walkInlineRefs. That
// is what makes the attribution honest: a pointer written in Order's `customer`
// property is reported against Order, not against whichever endpoint happens to
// return an Order three levels up.
//
// Appends rather than returns, because spec.Warnings is the channel the
// generators already copy onto GeneratedClient.Warnings and the command already
// prints. It assumes one call per generation, which is what Generator.Generate
// does; calling it twice over one specification would say everything twice.
func (s *APISpec) ValidateRefs() {
	dangling := make(map[string]string)

	s.walkSurface(func(origin string, schema *Schema) {
		walkInlineRefs(schema, func(ref string) {
			s.noteDanglingRef(dangling, ref, origin)
		})
	})

	s.warnDanglingRefs(dangling)
}

// walkSurface calls visit with every schema root the client surface carries,
// labelled with where it was written: each endpoint's parameters and bodies,
// each channel, the streaming extensions, and finally every component schema
// in name order.
//
// The order is a contract with ValidateRefs. A pointer written on an endpoint
// is better reported against the endpoint than against some component that
// repeats it, and first-writer-wins there makes that a question of order. In
// name order because map order would otherwise decide which of two components
// owns a pointer they both write.
func (s *APISpec) walkSurface(visit func(origin string, schema *Schema)) {
	origin := ""

	walk := func(schema *Schema) {
		visit(origin, schema)
	}

	walkParams := func(params []Parameter) {
		for _, param := range params {
			walk(param.Schema)
		}
	}

	for i := range s.Endpoints {
		endpoint := &s.Endpoints[i]

		// TrimSpace because a document can leave the method off, and a warning
		// that opens with a space reads like the line lost its subject.
		origin = strings.TrimSpace(endpoint.Method + " " + endpoint.Path)

		walkParams(endpoint.PathParams)
		walkParams(endpoint.QueryParams)
		walkParams(endpoint.HeaderParams)

		if endpoint.RequestBody != nil {
			for _, media := range endpoint.RequestBody.Content {
				walk(media.Schema)
			}
		}

		for _, resp := range endpoint.Responses {
			walkResponse(resp, walk)
		}

		walkResponse(endpoint.DefaultError, walk)
	}

	for i := range s.WebSockets {
		ws := &s.WebSockets[i]

		origin = "websocket " + ws.Path

		walkParams(ws.Parameters)
		walk(ws.SendSchema)
		walk(ws.ReceiveSchema)

		for _, schema := range ws.MessageTypes {
			walk(schema)
		}
	}

	for i := range s.SSEs {
		sse := &s.SSEs[i]

		origin = "sse " + sse.Path

		for _, schema := range sse.EventSchemas {
			walk(schema)
		}
	}

	for i := range s.WebTransports {
		wt := &s.WebTransports[i]

		origin = "webtransport " + wt.Path

		walkStream(wt.UniStreamSchema, walk)
		walkStream(wt.BiStreamSchema, walk)
		walk(wt.DatagramSchema)
	}

	origin = "the streaming extensions"

	s.walkStreamingFeatures(walk, walkParams)

	for _, name := range sortedKeys(s.Schemas) {
		origin = fmt.Sprintf("component schema %q", name)

		walk(s.Schemas[name])
	}
}

// CheckTypeNames refuses a specification whose schema names cannot be written
// as type names in the target language.
//
// Generators emit a component schema's name, and the last segment of every
// pointer at one, verbatim wherever the type is declared or used. A name is
// text from the specification, and the specification is not always written by
// the person running the generator: a name such as
// "X {} ; globalThis.PWN=1; interface Y" became running code in the generated
// TypeScript client. Rejecting the name is safer than rewriting it, because a
// rewrite has to be applied identically at every site that prints the name,
// and one missed site is the same injection again.
//
// valid decides what the language accepts as an identifier. lang only labels
// the error.
func (s *APISpec) CheckTypeNames(lang string, valid func(string) bool) error {
	bad := make(map[string]string)

	note := func(name, origin string) {
		if name == "" || valid(name) {
			return
		}

		if _, seen := bad[name]; !seen {
			bad[name] = origin
		}
	}

	for _, name := range sortedKeys(s.Schemas) {
		note(name, fmt.Sprintf("component schema %q", name))
	}

	s.walkSurface(func(origin string, schema *Schema) {
		walkInlineRefs(schema, func(ref string) {
			if ref == "" {
				return
			}

			note(ref[strings.LastIndex(ref, "/")+1:], origin)
		})
	})

	if len(bad) == 0 {
		return nil
	}

	lines := make([]string, 0, len(bad))
	for _, name := range sortedKeys(bad) {
		lines = append(lines, fmt.Sprintf("%q (from %s)", name, bad[name]))
	}

	return fmt.Errorf("cannot generate a %s client: these schema names are not valid %s identifiers, rename them in the specification: %s",
		lang, lang, strings.Join(lines, ", "))
}

// noteDanglingRef records a pointer that asserts a component schema exists and
// is wrong, against the first root to write it.
//
// Only ComponentRefName's shape -- "#/components/schemas/X" -- makes that
// assertion, which is why the check is strict here while refTargetName beside
// it stays permissive. A pointer into another section of the document is legal
// and its last segment is still the name the entity edge graph will use, so
// refTargetName resolves it on purpose (see its doc comment above). Widening
// this to every local pointer that resolves to no component would warn about
// "#/components/responses/Error" in every document that declares a shared
// response, which is how a real warning gets learned as noise.
//
// The two lines below are ResolveSchemaRef's rule, which is what makes this
// free of false positives rather than merely conservative: it warns exactly
// when the resolver the generators call would hand them a nil schema. Keep them
// in step -- a divergence here reports pointers that resolve fine, or stays
// quiet about ones that do not.
func (s *APISpec) noteDanglingRef(dangling map[string]string, ref, origin string) {
	name := ComponentRefName(ref)
	if name == "" {
		return
	}

	if _, carried := s.Schemas[name]; carried {
		return
	}

	// First writer wins. The roots are visited in a fixed order -- endpoints,
	// channels, then components by name -- so which one owns the line is a
	// decision rather than an accident of map iteration.
	if _, noted := dangling[name]; noted {
		return
	}

	dangling[name] = origin
}

// warnDanglingRefs reports the unresolvable pointers as one warning each,
// sorted by the name they name.
//
// A warning rather than a refusal, for two reasons. The document is invalid, so
// refusing it would be defensible -- but this package generates a client from
// whatever a running service published, and half a client an operator can see
// the hole in beats a command that exits one and calls a specification it did
// not write malformed. And the fix is not local: a pointer at a component
// nobody declared is usually a rename applied to the component key and not to
// the reference, which the author repairs in the service.
//
// Silence was the actual defect. Under a path filter the reachability walk
// marks the undeclared name, finds nothing behind it and stops, so
// pruneUnreachable drops every schema and entity row that pointer was the only
// route to -- which, when it is an endpoint's only response, is all of them.
// Apply reports that as "0/10 schemas, 0/5 entity rows", and the client still
// compiles, because an empty entity table is well-formed. It is
// indistinguishable from a service with genuinely nothing to cache.
//
// Sorted because the warnings are printed to an operator and a report that
// reshuffles itself between runs is noise -- the same reason MergeSpecs sorts
// its own conflict lines.
func (s *APISpec) warnDanglingRefs(dangling map[string]string) {
	for _, name := range sortedKeys(dangling) {
		s.Warnings = append(s.Warnings, fmt.Sprintf(
			"%s references %q, which names no component schema this client carries; no type is generated for that name, and any type or entity row reachable only through it is dropped with it",
			dangling[name], componentRefPrefix+name))
	}
}
