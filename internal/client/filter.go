package client

import (
	"path"
	"sort"
	"strings"
)

// PathFilter selects which endpoints a generated client covers.
//
// It exists because a specification is usually larger than the API any one
// consumer talks to. A service that mounts an auth engine, an admin dashboard
// and its own domain routes publishes all three from one document, and a
// client generated over the whole thing buries the twenty endpoints a caller
// wants under the two hundred it must never touch.
//
// Filtering is a generation-time concern rather than a serving-time one: the
// server is right to publish everything it serves, and the client is right to
// bind only what it consumes.
type PathFilter struct {
	// Include keeps only the endpoints matching at least one pattern. Empty
	// means every endpoint is a candidate.
	Include []string

	// Exclude drops endpoints matching any pattern, and is applied after
	// Include so that a narrow exclusion can carve a hole in a broad include.
	Exclude []string
}

// FilterResult reports what a filter did, so a caller can say so rather than
// silently generating a smaller client than the operator expected.
type FilterResult struct {
	// KeptEndpoints and DroppedEndpoints count operations, not paths: one path
	// with a GET and a DELETE is two endpoints and they filter together.
	KeptEndpoints    int
	DroppedEndpoints int

	// KeptSchemas and DroppedSchemas count component schemas after pruning.
	KeptSchemas    int
	DroppedSchemas int

	// DroppedPaths lists the distinct paths removed, sorted, for reporting.
	DroppedPaths []string
}

// Empty reports whether the filter would do anything at all.
func (f PathFilter) Empty() bool {
	return len(f.Include) == 0 && len(f.Exclude) == 0
}

// Apply filters the spec in place and prunes schemas no surviving endpoint can
// reach.
//
// Pruning matters as much as the endpoint filter. Component schemas generate a
// type each, so a spec whose auth engine contributes a hundred and forty of
// them yields a types file that is mostly unreachable from the client's own
// surface — the endpoints look filtered while the types plainly are not.
func (s *APISpec) Apply(f PathFilter) FilterResult {
	result := FilterResult{}

	if f.Empty() {
		result.KeptEndpoints = len(s.Endpoints)
		result.KeptSchemas = len(s.Schemas)

		return result
	}

	kept := make([]Endpoint, 0, len(s.Endpoints))
	dropped := make(map[string]struct{})

	for _, endpoint := range s.Endpoints {
		if f.allows(endpoint.Path) {
			kept = append(kept, endpoint)

			continue
		}

		dropped[endpoint.Path] = struct{}{}
		result.DroppedEndpoints++
	}

	s.Endpoints = kept
	result.KeptEndpoints = len(kept)

	for p := range dropped {
		result.DroppedPaths = append(result.DroppedPaths, p)
	}

	sort.Strings(result.DroppedPaths)

	before := len(s.Schemas)
	s.pruneUnreachableSchemas()
	result.KeptSchemas = len(s.Schemas)
	result.DroppedSchemas = before - result.KeptSchemas

	return result
}

// allows reports whether a path survives the filter.
func (f PathFilter) allows(p string) bool {
	if len(f.Include) > 0 && !matchesAny(f.Include, p) {
		return false
	}

	return !matchesAny(f.Exclude, p)
}

func matchesAny(patterns []string, p string) bool {
	for _, pattern := range patterns {
		if matchPath(pattern, p) {
			return true
		}
	}

	return false
}

// matchPath matches a path against one pattern.
//
// Two forms are accepted, because operators reach for both and guessing wrong
// is a silently empty client:
//
//   - a path prefix: "/identity" matches "/identity" and "/identity/login" but
//     not "/identity-provider", since the boundary is a path separator rather
//     than a character count;
//   - a glob: "/api/*/health" matches through one segment, and a trailing
//     "/**" matches any depth. Plain path.Match is not enough on its own — its
//     "*" never crosses a separator, so "/api/*" would miss "/api/v1/models",
//     which is the pattern everyone writes first.
func matchPath(pattern, p string) bool {
	if pattern == "" {
		return false
	}

	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		// "/" alone: the root prefix, which is every path.
		return true
	}

	if pattern == p {
		return true
	}

	// Recursive glob: "/api/**" is the prefix form written explicitly.
	if base, ok := strings.CutSuffix(pattern, "/**"); ok {
		return p == base || strings.HasPrefix(p, base+"/")
	}

	// Prefix, on a segment boundary.
	if strings.HasPrefix(p, pattern+"/") {
		return true
	}

	if ok, err := path.Match(pattern, p); err == nil && ok {
		return true
	}

	return false
}

// pruneUnreachableSchemas drops component schemas that no remaining endpoint
// can reach, following $ref transitively through properties, items, the
// polymorphic combinators and additionalProperties.
func (s *APISpec) pruneUnreachableSchemas() {
	if len(s.Schemas) == 0 {
		return
	}

	reachable := make(map[string]struct{}, len(s.Schemas))

	var walk func(schema *Schema)

	walk = func(schema *Schema) {
		if schema == nil {
			return
		}

		if name := refName(schema.Ref); name != "" {
			if _, seen := reachable[name]; seen {
				// Already expanded. Stopping here is also what keeps a
				// self-referential schema — a tree node, a linked list — from
				// recursing forever.
				return
			}

			reachable[name] = struct{}{}
			walk(s.Schemas[name])
		}

		for _, prop := range schema.Properties {
			walk(prop)
		}

		walk(schema.Items)

		for _, sub := range schema.OneOf {
			walk(sub)
		}

		for _, sub := range schema.AnyOf {
			walk(sub)
		}

		for _, sub := range schema.AllOf {
			walk(sub)
		}

		if nested, ok := schema.AdditionalProperties.(*Schema); ok {
			walk(nested)
		}

		// A discriminator names schemas that no property references directly;
		// dropping them would leave a union that cannot resolve its variants.
		if schema.Discriminator != nil {
			for _, ref := range schema.Discriminator.Mapping {
				if name := refName(ref); name != "" {
					if _, seen := reachable[name]; !seen {
						reachable[name] = struct{}{}
						walk(s.Schemas[name])
					}
				}
			}
		}
	}

	for i := range s.Endpoints {
		endpoint := &s.Endpoints[i]

		for _, param := range endpoint.PathParams {
			walk(param.Schema)
		}

		for _, param := range endpoint.QueryParams {
			walk(param.Schema)
		}

		for _, param := range endpoint.HeaderParams {
			walk(param.Schema)
		}

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

	for name := range s.Schemas {
		if _, ok := reachable[name]; !ok {
			delete(s.Schemas, name)
		}
	}
}

func walkResponse(resp *Response, walk func(*Schema)) {
	if resp == nil {
		return
	}

	for _, media := range resp.Content {
		walk(media.Schema)
	}

	for _, header := range resp.Headers {
		walk(header.Schema)
	}
}

// refName extracts the component name from a local $ref, and returns "" for a
// remote or malformed one — which is not something to prune against.
func refName(ref string) string {
	const prefix = "#/components/schemas/"

	if !strings.HasPrefix(ref, prefix) {
		return ""
	}

	return strings.TrimPrefix(ref, prefix)
}
