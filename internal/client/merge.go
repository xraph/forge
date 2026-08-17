package client

import (
	"fmt"
	"sort"
)

// SourceKind records which document family a specification was parsed from.
type SourceKind int

const (
	// SourceUnknown is a spec built by something that did not say. It ranks
	// last, so it can never silently outrank a real REST document.
	SourceUnknown SourceKind = iota
	SourceOpenAPI
	SourceAsyncAPI
	SourceIntrospection
)

// mergeRank orders sources for a merge. OpenAPI and introspection are
// authoritative for shared types because they carry full request and response
// schemas; AsyncAPI fills only what is absent.
func mergeRank(k SourceKind) int {
	switch k {
	case SourceOpenAPI, SourceIntrospection:
		return 0
	case SourceAsyncAPI:
		return 1
	default:
		return 2
	}
}

// MergeSpecs combines parsed specifications into one.
//
// Sources are ordered by document kind, so that `--from-spec async.json
// --from-spec openapi.json` and its reverse produce identical output across
// differing kinds. Among sources that share a kind -- two OpenAPI documents,
// say -- the given argument order is preserved and decides precedence: a
// user listing two files has already expressed an order, and inventing a
// tiebreaker from their content would override that with something less
// predictable. This same-kind guarantee rests on sort.SliceStable below; if
// that is ever changed to sort.Slice, precedence among same-kind sources
// becomes unspecified and this comment goes stale.
//
// The result's RoutingTypes is left nil: resolveEntityFields is its only writer
// and rebuilds it from scratch, and merging two pre-built maps would break the
// invariant that RoutingTypes and Entities are disjoint. The caller must run
// resolveEntityFields on the result.
//
// Merging a single spec returns that spec unchanged, so the single-source path
// costs nothing and cannot drift from the multi-source one.
func MergeSpecs(specs ...*APISpec) *APISpec {
	ordered := make([]*APISpec, 0, len(specs))
	for _, s := range specs {
		if s != nil {
			ordered = append(ordered, s)
		}
	}

	switch len(ordered) {
	case 0:
		return nil
	case 1:
		return ordered[0]
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		return mergeRank(ordered[i].Kind) < mergeRank(ordered[j].Kind)
	})

	out := &APISpec{
		Info:     ordered[0].Info,
		Kind:     ordered[0].Kind,
		Schemas:  make(map[string]*Schema),
		Entities: make(map[string]*EntityRef),
	}

	seenServer := make(map[string]bool)
	seenTag := make(map[string]bool)
	seenScheme := make(map[string]bool)

	// Which source each kept schema and entity actually came from. The kept
	// definition is not always the first source's: the first source may be
	// silent about a name that two later sources then disagree about, and
	// reporting out.Kind there names a document that never mentioned it.
	schemaKind := make(map[string]SourceKind, len(ordered[0].Schemas))
	entityKind := make(map[string]SourceKind, len(ordered[0].Entities))

	for _, s := range ordered {
		out.Endpoints = append(out.Endpoints, s.Endpoints...)
		out.WebSockets = append(out.WebSockets, s.WebSockets...)
		out.SSEs = append(out.SSEs, s.SSEs...)
		out.WebTransports = append(out.WebTransports, s.WebTransports...)
		out.Warnings = append(out.Warnings, s.Warnings...)

		for _, srv := range s.Servers {
			if !seenServer[srv.URL] {
				seenServer[srv.URL] = true
				out.Servers = append(out.Servers, srv)
			}
		}
		for _, tag := range s.Tags {
			if !seenTag[tag.Name] {
				seenTag[tag.Name] = true
				out.Tags = append(out.Tags, tag)
			}
		}
		for _, sec := range s.Security {
			if !seenScheme[sec.Key] {
				seenScheme[sec.Key] = true
				out.Security = append(out.Security, sec)
			}
		}

		for name, schema := range s.Schemas {
			existing, taken := out.Schemas[name]
			if !taken {
				out.Schemas[name] = schema
				schemaKind[name] = s.Kind
				continue
			}
			if !sameSchemaShape(existing, schema) {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"schema %q is declared differently in two sources; keeping the %s definition (type %q) and ignoring the %s one (type %q)",
					name, kindName(schemaKind[name]), schemaType(existing), kindName(s.Kind), schemaType(schema)))
			}
		}
		for name, ent := range s.Entities {
			existing, taken := out.Entities[name]
			if !taken {
				out.Entities[name] = ent
				entityKind[name] = s.Kind
				continue
			}
			if existing.IDField != ent.IDField {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"entity %q has id field %q in the %s source and %q in the %s source; keeping %q",
					name, existing.IDField, kindName(entityKind[name]),
					ent.IDField, kindName(s.Kind), existing.IDField))
			}
		}

		if out.Streaming == nil {
			out.Streaming = s.Streaming
		}
	}

	// Duplicates are dropped, not merely reported. Every generator emits one
	// function (or one client class) per entry in these slices, so a route or
	// a stream declared by two sources -- a /health both documents carry, a
	// gateway document and the service document behind it, the same file
	// passed twice -- produces two definitions with the same name, and the Go
	// client does not compile. "The first declaration wins" has to actually
	// happen for the warning to be true.
	out.Endpoints = keepFirst(out.Endpoints, "route", routeKey, &out.Warnings)
	out.WebSockets = keepFirst(out.WebSockets, "websocket",
		func(ws WebSocketEndpoint) string { return streamKey(ws.ID, ws.Path) }, &out.Warnings)
	out.SSEs = keepFirst(out.SSEs, "sse stream",
		func(sse SSEEndpoint) string { return streamKey(sse.ID, sse.Path) }, &out.Warnings)
	out.WebTransports = keepFirst(out.WebTransports, "webtransport stream",
		func(wt WebTransportEndpoint) string { return streamKey(wt.ID, wt.Path) }, &out.Warnings)

	return out
}

// keepFirst drops every element whose identity a previous element already
// claimed, warning once per dropped element. Order among the survivors is
// unchanged, so the merge order established above still decides precedence.
func keepFirst[T any](in []T, noun string, identity func(T) string, warnings *[]string) []T {
	if len(in) < 2 {
		return in
	}

	seen := make(map[string]bool, len(in))
	kept := make([]T, 0, len(in))

	for _, item := range in {
		key := identity(item)
		if seen[key] {
			*warnings = append(*warnings, fmt.Sprintf(
				"%s %q is declared in more than one source; the first declaration wins", noun, key))

			continue
		}

		seen[key] = true

		kept = append(kept, item)
	}

	return kept
}

// routeKey identifies a REST endpoint by the thing that makes it addressable:
// its method and path.
func routeKey(ep Endpoint) string {
	return ep.Method + " " + ep.Path
}

// streamKey identifies a stream endpoint (WebSocket, SSE, WebTransport) by its
// declared id, falling back to its address when a document gives none.
//
// The id, not the address, because the id is what every generator turns into
// the name of the generated client -- so an id claimed twice is exactly the
// collision that fails to compile. Keying on the address alone would instead
// drop one of two channels that legitimately share an address, which a single
// AsyncAPI document is allowed to declare (channels are keyed by channel name,
// and the parser keeps both); dropping one only once a second document is
// present would make the merge path disagree with the single-source path.
func streamKey(id, path string) string {
	if id != "" {
		return id
	}

	return path
}

// sameSchemaShape reports whether two schemas describe the same thing closely
// enough that declaring both is not a conflict. It compares every field that
// changes what a generated client can express: Type, Format, Nullable, Ref,
// Properties, Required, Items, Enum, AdditionalProperties, OneOf, AnyOf,
// AllOf, and Discriminator.
//
// Description and Example are deliberately excluded: prose and illustrative
// values differ freely between a REST document and a stream document
// describing one type, and warning about those would train the reader to
// ignore the warning that matters. Extensions is also excluded: a differing
// x-forge-entity id field is already caught by the entity IDField check in
// this function's caller (MergeSpecs), and comparing extensions here would
// double-report it.
func sameSchemaShape(a, b *Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type != b.Type || a.Format != b.Format || a.Nullable != b.Nullable || a.Ref != b.Ref {
		return false
	}
	if len(a.Properties) != len(b.Properties) {
		return false
	}
	for name, av := range a.Properties {
		bv, ok := b.Properties[name]
		if !ok || !sameSchemaShape(av, bv) {
			return false
		}
	}
	if !sameRequired(a.Required, b.Required) {
		return false
	}
	if !sameSchemaShape(a.Items, b.Items) {
		return false
	}
	if !sameEnum(a.Enum, b.Enum) {
		return false
	}
	if !sameAdditionalProperties(a.AdditionalProperties, b.AdditionalProperties) {
		return false
	}
	if !sameSchemaSlice(a.OneOf, b.OneOf) {
		return false
	}
	if !sameSchemaSlice(a.AnyOf, b.AnyOf) {
		return false
	}
	if !sameSchemaSlice(a.AllOf, b.AllOf) {
		return false
	}
	return sameDiscriminator(a.Discriminator, b.Discriminator)
}

// sameRequired reports whether two Required lists name the same fields the
// same number of times each, ignoring order. Set membership alone would call
// ["x","y"] and ["x","x"] equal; counting occurrences on both sides catches
// the duplicate.
func sameRequired(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, r := range a {
		counts[r]++
	}
	for _, r := range b {
		counts[r]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// sameEnum compares two Enum slices elementwise, in order -- an enum is a
// closed, ordered set of allowed values, so a genuine narrowing or widening
// (`[open, closed]` vs `[open, closed, archived]`) is exactly the disagreement
// this check exists to catch. Elements come from decoded JSON and are
// ordinarily comparable scalars, but an element that is itself a slice or map
// is not comparable with ==; sameScalar treats that case as a difference
// rather than letting == panic.
func sameEnum(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameScalar(a[i], b[i]) {
			return false
		}
	}
	return true
}

// sameScalar reports whether two `any` values are equal, treating a
// non-comparable dynamic type (slice, map, func smuggled into an any) as
// unequal instead of letting == panic.
func sameScalar(a, b any) (eq bool) {
	defer func() {
		if recover() != nil {
			eq = false
		}
	}()
	return a == b
}

// sameAdditionalProperties compares the two shapes AdditionalProperties can
// hold in this IR: a bool (allow/forbid extra properties) or a *Schema
// (extra properties must match it). A bool and a *Schema are never the same
// shape, and nil is only equal to nil.
func sameAdditionalProperties(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aSchema, aIsSchema := a.(*Schema)
	bSchema, bIsSchema := b.(*Schema)
	if aIsSchema || bIsSchema {
		return aIsSchema && bIsSchema && sameSchemaShape(aSchema, bSchema)
	}
	aBool, aIsBool := a.(bool)
	bBool, bIsBool := b.(bool)
	return aIsBool && bIsBool && aBool == bBool
}

// sameSchemaSlice compares two polymorphism branches (OneOf, AnyOf, AllOf)
// pairwise, in order, after a length check.
func sameSchemaSlice(a, b []*Schema) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameSchemaShape(a[i], b[i]) {
			return false
		}
	}
	return true
}

// sameDiscriminator compares a polymorphism discriminator's property name
// and its full value-to-schema mapping.
func sameDiscriminator(a, b *Discriminator) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.PropertyName != b.PropertyName {
		return false
	}
	if len(a.Mapping) != len(b.Mapping) {
		return false
	}
	for k, v := range a.Mapping {
		if bv, ok := b.Mapping[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func schemaType(s *Schema) string {
	if s == nil {
		return "<nil>"
	}
	return s.Type
}

func kindName(k SourceKind) string {
	switch k {
	case SourceOpenAPI:
		return "OpenAPI"
	case SourceAsyncAPI:
		return "AsyncAPI"
	case SourceIntrospection:
		return "introspected"
	default:
		return "unknown"
	}
}
