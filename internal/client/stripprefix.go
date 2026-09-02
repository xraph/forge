package client

import (
	"fmt"
	"sort"
	"strings"
)

// StripPrefix removes leading service prefixes from every generated identifier
// in spec: schema names, operation ids, entity typenames, cache tags and the
// $refs that point at any of them.
//
// A gateway that fronts several services publishes their routes as one
// document, and disambiguates by prefixing everything it merges -- `Studio_`,
// `Portal_`. That prefix is doing real work in the merged document and none at
// all in a client generated from one service's slice of it, where it survives
// as `Studio_ProviderResponse` inside a package already called studio-client,
// and as the stutter in `useStudioStudioCatalogProvidersList`.
//
// This takes a SET of prefixes, not one, because a service does not only
// describe its own types. An auth service that fronts the others re-describes
// what it fronts, so identity's document declares `Portal_WorkspaceResponse`
// alongside portal's own `WorkspaceResponse`. Stripping only the client's own
// prefix leaves those two names for one record, and a consumer that unions the
// generated entity tables to make a record fetched through two clients one
// cache entry gets two entries instead, neither invalidating the other. That
// failure is silent: a collision guard looks for one name carrying two shapes,
// and this is two names carrying one.
//
// This runs over the whole spec rather than inside each emitter because the
// names have to move together. types.ts, ops.ts, hooks.ts, rest.ts and
// codecs.ts all derive their identifiers from these same fields, so a rename
// applied in one emitter and not another produces a client whose hook names a
// type its own types.ts does not export.
//
// Order matters: call this AFTER the path filter, so the collision check below
// sees the schema set this client will actually carry rather than the whole
// document's.
//
// A prefix that matches nothing is not an error -- generating a client for a
// service whose routes happen to carry no prefix is a legitimate no-op -- but a
// prefix whose removal would make two names collide is, because the alternative
// is one schema silently overwriting another.
// reserved names are left prefixed rather than rejected. They are the generated
// package's own exports -- error classes, the transport, the manifest -- so
// unlike a schema-versus-schema collision there is no choice to offer: the
// generated name cannot move, and a schema that keeps its prefix is unambiguous
// where a duplicated `export *` is not.
func StripPrefix(spec *APISpec, prefixes []string, reserved map[string]bool) error {
	prefixes = normalizePrefixes(prefixes)
	if spec == nil || len(prefixes) == 0 {
		return nil
	}

	rename, err := planRenames(spec, prefixes, reserved)
	if err != nil {
		return err
	}

	if len(rename) == 0 {
		return nil
	}

	applyRenames(spec, rename, prefixes)

	return nil
}

// normalizePrefixes drops empties and duplicates and orders what is left
// longest first.
//
// Longest first is the matching order, not a cosmetic sort. Given `Twin_` and
// `TwinOS_`, `TwinOS_Grant` matches both, and the shorter one would leave
// `OS_Grant` -- a name that is neither the original nor the intended strip, and
// that no collision check can catch because it does not clash with anything.
// Ties break lexicographically so a spec renames identically on every run.
//
// An empty entry is dropped rather than rejected: it means "this client strips
// nothing", which is the single-prefix no-op the caller is entitled to pass,
// and it would otherwise match every name and strip nothing from all of them.
func normalizePrefixes(prefixes []string) []string {
	out := make([]string, 0, len(prefixes))
	seen := make(map[string]bool, len(prefixes))

	for _, prefix := range prefixes {
		if prefix == "" || seen[prefix] {
			continue
		}

		seen[prefix] = true

		out = append(out, prefix)
	}

	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}

		return out[i] < out[j]
	})

	return out
}

// matchPrefix returns the prefix of name from an already-normalized set, so the
// longest match wins.
func matchPrefix(name string, prefixes []string) (string, bool) {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return prefix, true
		}
	}

	return "", false
}

// trimAny removes whichever prefix in the set leads name, and returns name
// untouched when none does.
func trimAny(name string, prefixes []string) string {
	if prefix, ok := matchPrefix(name, prefixes); ok {
		return strings.TrimPrefix(name, prefix)
	}

	return name
}

// planRenames maps each prefixed schema name to its stripped form, refusing any
// rename that would land on a name already taken.
//
// Two collisions are possible, and they are not the same mistake.
//
// A prefixed name can strip onto an unprefixed one the document already
// declares: `Studio_User` where a bare `User` exists. That one existed under a
// single prefix too.
//
// Two PREFIXED names can now also strip onto each other: `Portal_User` and
// `TwinOS_User` both land on `User`. This was unreachable while the prefix was
// one fixed string, because trimming it is injective and two distinct keys
// always strip to two distinct results. Widening to a set makes it reachable
// and, on a gateway that prefixes per service, likely -- which is why the
// `taken` bookkeeping that was previously belt-and-braces is now load-bearing.
//
// Both are refused rather than resolved. Merging them would be the aliasing bug
// this function exists to fix, run backwards: two names for one record is a
// cache that misses, and one name for two records is a cache that returns the
// wrong shape.
func planRenames(spec *APISpec, prefixes []string, reserved map[string]bool) (map[string]string, error) {
	rename := make(map[string]string)
	// Every name in the final document, so a rename can be checked against the
	// ones that are not moving as well as the ones that are. A name maps to
	// itself when it is staying put, and to its ORIGINAL name when it moved --
	// which is how the error below tells the two collisions apart.
	taken := make(map[string]string, len(spec.Schemas))

	for name := range spec.Schemas {
		if _, prefixed := matchPrefix(name, prefixes); !prefixed {
			taken[name] = name
		}
	}

	// Sorted, so a spec with two colliding names reports the same pair on every
	// run rather than whichever the map handed over first.
	names := make([]string, 0, len(spec.Schemas))
	for name := range spec.Schemas {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		prefix, prefixed := matchPrefix(name, prefixes)
		if !prefixed {
			continue
		}

		stripped := strings.TrimPrefix(name, prefix)

		// A schema named exactly the prefix strips to nothing. Left alone
		// rather than rejected: it is a degenerate name, not a collision, and
		// refusing to generate over it would be a surprising way to find out.
		if stripped == "" {
			continue
		}

		// Left prefixed, not rejected: the generated package owns this name and
		// cannot yield it. `TwinOS_ValidationError` staying as it is costs one
		// awkward typename; stripping it makes index.ts export two
		// `ValidationError`s and the package stops compiling.
		if reserved[stripped] {
			continue
		}

		if owner, clash := taken[stripped]; clash {
			// owner == stripped means the name it collides with is not moving:
			// the document declares a bare `User` and `Studio_User` wants to
			// become one. Otherwise both are prefixed, they belong to two
			// different services, and no edit to either service helps -- the
			// prefix set is what has to give.
			if owner == stripped {
				return nil, fmt.Errorf(
					"stripping %q from %q collides with %q; "+
						"remove the prefix from one of them, or generate this client without strip_prefix",
					prefix, name, owner,
				)
			}

			return nil, fmt.Errorf(
				"%q and %q both strip to %q; they are different types from different services, "+
					"so stripping both would key one name to two shapes. "+
					"Drop one prefix from strip_prefixes for this client, or rename one of the types upstream",
				owner, name, stripped,
			)
		}

		taken[stripped] = name
		rename[name] = stripped
	}

	if err := checkFilteredCollisions(spec, taken, prefixes); err != nil {
		return nil, err
	}

	return rename, nil
}

// checkFilteredCollisions refuses a strip that would give one typename two
// different shapes ACROSS clients.
//
// The check above cannot see this, and the reason is an ordering the caller
// chose deliberately. applySpecTransforms filters before it strips, so that a
// pair of names where only one survives the filter does not fail a client that
// has no collision in it. That reasoning holds for a client used on its own. It
// does not hold for the pattern these clients are generated for: a consumer
// fronting one gateway unions the per-service entity tables so that a record
// fetched through two clients is one cache entry, and in that union the two
// stripped names are one key. Whichever spread order puts last, wins; the other
// service's lists are then walked for a field its payloads do not have, and its
// records are never normalized. Nothing throws. The list still renders, from
// the raw response, and the cache is simply empty for them.
//
// SHAPE IS WHAT SEPARATES THE BUG FROM THE FEATURE. Two services describing one
// record under their own prefixes -- `Portal_WorkspaceResponse` and
// `Studio_WorkspaceResponse`, same fields -- collapsing to one name is the
// entire point of taking a SET of prefixes rather than one: two names for one
// record is a cache that misses. Merging those is intended and stays silent.
// Two DIFFERENT shapes under one name is the inverse failure, and that is what
// this refuses.
//
// Comparison runs over prefix-normalized copies. sameSchemaShape compares $ref
// strings, and the refs inside these schemas still carry their own service's
// prefix at this point in the pass, so a raw comparison would find every pair
// different -- turning the intended merge into a hard error and making the
// whole check useless.
func checkFilteredCollisions(spec *APISpec, taken map[string]string, prefixes []string) error {
	if len(spec.PrunedSchemas) == 0 {
		return nil
	}

	// Sorted, so a document with several collisions reports the same one on
	// every run rather than whichever the map handed over first.
	pruned := make([]string, 0, len(spec.PrunedSchemas))
	for name := range spec.PrunedSchemas {
		pruned = append(pruned, name)
	}

	sort.Strings(pruned)

	for _, name := range pruned {
		stripped := trimAny(name, prefixes)

		// A name that strips onto itself is not a collision with anything;
		// it is simply absent from this client.
		owner, clash := taken[stripped]
		if !clash || owner == name {
			continue
		}

		kept, present := spec.Schemas[owner]
		if !present {
			continue
		}

		if sameSchemaShape(
			strippedCopy(kept, prefixes, 0),
			strippedCopy(spec.PrunedSchemas[name], prefixes, 0),
		) {
			continue // one record described twice: the merge this feature is for
		}

		return fmt.Errorf(
			"%q and %q both strip to %q and describe different shapes; %q is filtered out of this "+
				"client, so nothing here reports the clash, but a consumer that unions the generated "+
				"entity tables gets one typename carrying two field maps and silently stops "+
				"normalizing whichever loses. Rename one of the types upstream so the two services "+
				"do not describe different records under one name",
			owner, name, stripped, name,
		)
	}

	return nil
}

// strippedCopy returns a copy of schema with every component pointer rewritten
// to its stripped form, so two services' descriptions of one record compare
// equal.
//
// A copy rather than an in-place rewrite: this runs while planRenames is still
// deciding whether the strip is legal at all, and a refusal must leave the
// specification exactly as it found it.
//
// Only pointers move. Property names, types, formats and required lists are a
// service's description of the record and are what the comparison is for.
//
// The depth bound is a backstop. Both convertSchema implementations build a
// finite tree out of decoded JSON -- a recursive type is spelled as a $ref,
// which this copies without following -- so the bound only ever engages on a
// hand-built Schema whose members point at each other.
func strippedCopy(schema *Schema, prefixes []string, depth int) *Schema {
	if schema == nil || depth > maxCompositionDepth {
		return schema
	}

	out := *schema

	if name, ok := strings.CutPrefix(out.Ref, componentRefPrefix); ok {
		out.Ref = componentRefPrefix + trimAny(name, prefixes)
	}

	if len(schema.Properties) > 0 {
		out.Properties = make(map[string]*Schema, len(schema.Properties))
		for prop, ps := range schema.Properties {
			out.Properties[prop] = strippedCopy(ps, prefixes, depth+1)
		}
	}

	out.Items = strippedCopy(schema.Items, prefixes, depth+1)
	out.OneOf = strippedCopySlice(schema.OneOf, prefixes, depth)
	out.AnyOf = strippedCopySlice(schema.AnyOf, prefixes, depth)
	out.AllOf = strippedCopySlice(schema.AllOf, prefixes, depth)

	if nested, ok := schema.AdditionalProperties.(*Schema); ok {
		out.AdditionalProperties = strippedCopy(nested, prefixes, depth+1)
	}

	if schema.Discriminator != nil && len(schema.Discriminator.Mapping) > 0 {
		mapping := make(map[string]string, len(schema.Discriminator.Mapping))

		for key, ref := range schema.Discriminator.Mapping {
			if name, ok := strings.CutPrefix(ref, componentRefPrefix); ok {
				mapping[key] = componentRefPrefix + trimAny(name, prefixes)

				continue
			}

			mapping[key] = ref
		}

		discriminator := *schema.Discriminator
		discriminator.Mapping = mapping
		out.Discriminator = &discriminator
	}

	return &out
}

func strippedCopySlice(members []*Schema, prefixes []string, depth int) []*Schema {
	if members == nil {
		return nil
	}

	out := make([]*Schema, len(members))
	for i, member := range members {
		out[i] = strippedCopy(member, prefixes, depth+1)
	}

	return out
}

// applyRenames rewrites every field that carries one of the renamed names.
//
// prefixes is passed alongside the map because two of these fields are not
// schema names and so have no entry in it: an operation id is a route
// identifier the gateway prefixed by the same rule, and a cache tag is a
// typename with `:{id}` or `[]` welded onto the end.
func applyRenames(spec *APISpec, rename map[string]string, prefixes []string) {
	schemas := make(map[string]*Schema, len(spec.Schemas))

	for name, schema := range spec.Schemas {
		if renamed, ok := rename[name]; ok {
			schemas[renamed] = schema

			continue
		}

		schemas[name] = schema
	}

	spec.Schemas = schemas

	// One visited set across the whole walk, not one per root. A schema reached
	// from two endpoints is the same pointer, and rewriting its $ref twice is
	// harmless only because the rewrite is idempotent -- but a self-referential
	// type (`Node{children: []Node}`) is not harmless: without this the walk
	// does not terminate.
	seen := make(map[*Schema]bool)

	for _, schema := range spec.Schemas {
		rewriteSchemaRefs(schema, rename, seen)
	}

	spec.Entities = renameEntityTable(spec.Entities, rename)
	spec.RoutingTypes = renameEntityTable(spec.RoutingTypes, rename)

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]

		ep.ID = trimAny(ep.ID, prefixes)
		ep.OperationID = trimAny(ep.OperationID, prefixes)
		ep.RootType = renamed(ep.RootType, rename)
		ep.Entity = renameEntityRef(ep.Entity, rename)
		ep.CacheTags = renameTagSet(ep.CacheTags, prefixes)

		for _, param := range ep.PathParams {
			rewriteSchemaRefs(param.Schema, rename, seen)
		}

		for _, param := range ep.QueryParams {
			rewriteSchemaRefs(param.Schema, rename, seen)
		}

		for _, param := range ep.HeaderParams {
			rewriteSchemaRefs(param.Schema, rename, seen)
		}

		rewriteBodyRefs(ep.RequestBody, rename, seen)

		for _, response := range ep.Responses {
			rewriteResponseRefs(response, rename, seen)
		}

		rewriteResponseRefs(ep.DefaultError, rename, seen)
	}

	for i := range spec.WebSockets {
		ws := &spec.WebSockets[i]

		ws.ID = trimAny(ws.ID, prefixes)
		ws.StreamBindings = renameBindings(ws.StreamBindings, rename, prefixes)
	}

	for i := range spec.SSEs {
		sse := &spec.SSEs[i]

		sse.ID = trimAny(sse.ID, prefixes)
		sse.StreamBindings = renameBindings(sse.StreamBindings, rename, prefixes)
	}

	for i := range spec.WebTransports {
		wt := &spec.WebTransports[i]

		wt.ID = trimAny(wt.ID, prefixes)
	}
}

// rewriteSchemaRefs walks one schema and rewrites every $ref that names a
// renamed component, following properties, items, composition and
// additionalProperties.
func rewriteSchemaRefs(schema *Schema, rename map[string]string, seen map[*Schema]bool) {
	if schema == nil || seen[schema] {
		return
	}

	seen[schema] = true

	if schema.Ref != "" {
		if name, ok := strings.CutPrefix(schema.Ref, componentRefPrefix); ok {
			if replacement, hit := rename[name]; hit {
				schema.Ref = componentRefPrefix + replacement
			}
		}
	}

	for _, property := range schema.Properties {
		rewriteSchemaRefs(property, rename, seen)
	}

	rewriteSchemaRefs(schema.Items, rename, seen)

	for _, member := range schema.OneOf {
		rewriteSchemaRefs(member, rename, seen)
	}

	for _, member := range schema.AnyOf {
		rewriteSchemaRefs(member, rename, seen)
	}

	for _, member := range schema.AllOf {
		rewriteSchemaRefs(member, rename, seen)
	}

	// additionalProperties is `any` because OpenAPI allows a bool there as well
	// as a schema; only the schema form can carry a $ref.
	if nested, ok := schema.AdditionalProperties.(*Schema); ok {
		rewriteSchemaRefs(nested, rename, seen)
	}

	// A discriminator's mapping values are $refs by specification, and a
	// polymorphic response decoded through a stale mapping picks the wrong
	// member rather than failing outright.
	if schema.Discriminator != nil {
		for key, ref := range schema.Discriminator.Mapping {
			if name, ok := strings.CutPrefix(ref, componentRefPrefix); ok {
				if replacement, hit := rename[name]; hit {
					schema.Discriminator.Mapping[key] = componentRefPrefix + replacement
				}
			}
		}
	}
}

func rewriteBodyRefs(body *RequestBody, rename map[string]string, seen map[*Schema]bool) {
	if body == nil {
		return
	}

	for _, media := range body.Content {
		if media != nil {
			rewriteSchemaRefs(media.Schema, rename, seen)
		}
	}
}

func rewriteResponseRefs(response *Response, rename map[string]string, seen map[*Schema]bool) {
	if response == nil {
		return
	}

	for _, media := range response.Content {
		if media != nil {
			rewriteSchemaRefs(media.Schema, rename, seen)
		}
	}

	for _, header := range response.Headers {
		if header != nil {
			rewriteSchemaRefs(header.Schema, rename, seen)
		}
	}
}

func renameEntityTable(table map[string]*EntityRef, rename map[string]string) map[string]*EntityRef {
	if table == nil {
		return nil
	}

	out := make(map[string]*EntityRef, len(table))

	for name, ref := range table {
		out[renamed(name, rename)] = renameEntityRef(ref, rename)
	}

	return out
}

// renameEntityRef rewrites an entity's own typename and the typenames of its
// field edges.
//
// The Fields values matter as much as Type does: they are how the runtime
// recognises a nested entity, so an edge left pointing at `Studio_Customer`
// after the table renamed that row to `Customer` stops resolving, and the
// nested record silently stops being normalized.
func renameEntityRef(ref *EntityRef, rename map[string]string) *EntityRef {
	if ref == nil {
		return nil
	}

	ref.Type = renamed(ref.Type, rename)

	for property, typename := range ref.Fields {
		ref.Fields[property] = renamed(typename, rename)
	}

	return ref
}

// renameTagSet strips the prefixes from cache tags.
//
// A tag is a typename with a suffix welded on -- `Order:{id}`, `Order[]` -- so
// the prefix is still leading and a plain trim is enough. Going through the
// rename map instead would mean parsing the suffix back off every tag to find
// the name to look up, for the same answer.
//
// Trimming a tag whose typename was NOT renamed -- one held back as reserved,
// or one that collided -- would desynchronise the tag from the entity it names,
// but that cannot happen here: a name is held back only when planRenames
// refuses it, and planRenames refusing a collision fails the whole generation
// rather than returning a partial map.
func renameTagSet(tags TagSet, prefixes []string) TagSet {
	return TagSet{
		Provides:    trimEach(tags.Provides, prefixes),
		Invalidates: trimEach(tags.Invalidates, prefixes),
	}
}

func trimEach(values []string, prefixes []string) []string {
	if values == nil {
		return nil
	}

	out := make([]string, len(values))
	for i, value := range values {
		out[i] = trimAny(value, prefixes)
	}

	return out
}

func renameBindings(bindings []StreamBinding, rename map[string]string, prefixes []string) []StreamBinding {
	for i := range bindings {
		bindings[i].EntityType = renamed(bindings[i].EntityType, rename)
		bindings[i].Invalidates = trimEach(bindings[i].Invalidates, prefixes)
	}

	return bindings
}

// renamed resolves one typename through the rename map, leaving anything the
// map does not mention untouched.
func renamed(name string, rename map[string]string) string {
	if replacement, ok := rename[name]; ok {
		return replacement
	}

	return name
}
