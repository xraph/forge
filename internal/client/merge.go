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
			if !seenScheme[sec.Name] {
				seenScheme[sec.Name] = true
				out.Security = append(out.Security, sec)
			}
		}

		for name, schema := range s.Schemas {
			existing, taken := out.Schemas[name]
			if !taken {
				out.Schemas[name] = schema
				continue
			}
			if !sameSchemaShape(existing, schema) {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"schema %q is declared differently in two sources; keeping the %s definition (type %q) and ignoring the %s one (type %q)",
					name, kindName(out.Kind), schemaType(existing), kindName(s.Kind), schemaType(schema)))
			}
		}
		for name, ent := range s.Entities {
			existing, taken := out.Entities[name]
			if !taken {
				out.Entities[name] = ent
				continue
			}
			if existing.IDField != ent.IDField {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"entity %q has id field %q in the %s source and %q in the %s source; keeping %q",
					name, existing.IDField, kindName(out.Kind),
					ent.IDField, kindName(s.Kind), existing.IDField))
			}
		}

		if out.Streaming == nil {
			out.Streaming = s.Streaming
		}
	}

	seenRoute := make(map[string]bool)
	for _, ep := range out.Endpoints {
		key := ep.Method + " " + ep.Path
		if seenRoute[key] {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"route %q is declared in more than one source; the first declaration wins", key))
			continue
		}
		seenRoute[key] = true
	}

	return out
}

// sameSchemaShape reports whether two schemas describe the same thing closely
// enough that declaring both is not a conflict. It compares the structural
// fields only: descriptions and examples differ freely between a REST document
// and a stream document describing one type, and warning about those would
// train the reader to ignore the warning that matters.
func sameSchemaShape(a, b *Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type != b.Type || a.Format != b.Format || a.Nullable != b.Nullable {
		return false
	}
	if len(a.Properties) != len(b.Properties) || len(a.Required) != len(b.Required) {
		return false
	}
	for name, av := range a.Properties {
		bv, ok := b.Properties[name]
		if !ok || !sameSchemaShape(av, bv) {
			return false
		}
	}
	required := make(map[string]bool, len(a.Required))
	for _, r := range a.Required {
		required[r] = true
	}
	for _, r := range b.Required {
		if !required[r] {
			return false
		}
	}
	return sameSchemaShape(a.Items, b.Items)
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
