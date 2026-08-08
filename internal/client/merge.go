package client

import "sort"

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
			if _, taken := out.Schemas[name]; !taken {
				out.Schemas[name] = schema
			}
		}
		for name, ent := range s.Entities {
			if _, taken := out.Entities[name]; !taken {
				out.Entities[name] = ent
			}
		}

		if out.Streaming == nil {
			out.Streaming = s.Streaming
		}
	}

	return out
}
