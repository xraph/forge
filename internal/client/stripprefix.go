package client

import (
	"fmt"
	"sort"
	"strings"
)

// componentRefPrefix is the only $ref form this package emits or reads.
const componentRefPrefix = "#/components/schemas/"

// StripPrefix removes a leading service prefix from every generated identifier
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
func StripPrefix(spec *APISpec, prefix string, reserved map[string]bool) error {
	if spec == nil || prefix == "" {
		return nil
	}

	rename, err := planRenames(spec, prefix, reserved)
	if err != nil {
		return err
	}

	if len(rename) == 0 {
		return nil
	}

	applyRenames(spec, rename, prefix)

	return nil
}

// planRenames maps each prefixed schema name to its stripped form, refusing any
// rename that would land on a name already taken.
//
// The collision that actually happens is a prefixed name stripping onto an
// unprefixed one the document already declares: `Studio_User` where a bare
// `User` exists. Two PREFIXED names cannot collide with each other while the
// prefix is a single fixed string -- trimming it is injective, so two distinct
// keys always strip to two distinct results -- but renamed names are recorded
// in `taken` anyway, because that ceases to hold the moment this takes a list
// of prefixes rather than one, and a guard that costs a map write is cheaper
// than the silent overwrite it prevents.
func planRenames(spec *APISpec, prefix string, reserved map[string]bool) (map[string]string, error) {
	rename := make(map[string]string)
	// Every name in the final document, so a rename can be checked against the
	// ones that are not moving as well as the ones that are.
	taken := make(map[string]string, len(spec.Schemas))

	for name := range spec.Schemas {
		if !strings.HasPrefix(name, prefix) {
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
		if !strings.HasPrefix(name, prefix) {
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
			return nil, fmt.Errorf(
				"stripping %q from %q collides with %q; "+
					"remove the prefix from one of them, or generate this client without strip_prefix",
				prefix, name, owner,
			)
		}

		taken[stripped] = name
		rename[name] = stripped
	}

	return rename, nil
}

// applyRenames rewrites every field that carries one of the renamed names.
//
// prefix is passed alongside the map because two of these fields are not schema
// names and so have no entry in it: an operation id is a route identifier the
// gateway prefixed by the same rule, and a cache tag is a typename with `:{id}`
// or `[]` welded onto the end.
func applyRenames(spec *APISpec, rename map[string]string, prefix string) {
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

		ep.ID = strings.TrimPrefix(ep.ID, prefix)
		ep.OperationID = strings.TrimPrefix(ep.OperationID, prefix)
		ep.RootType = renamed(ep.RootType, rename)
		ep.Entity = renameEntityRef(ep.Entity, rename)
		ep.CacheTags = renameTagSet(ep.CacheTags, prefix)

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

		ws.ID = strings.TrimPrefix(ws.ID, prefix)
		ws.StreamBindings = renameBindings(ws.StreamBindings, rename, prefix)
	}

	for i := range spec.SSEs {
		sse := &spec.SSEs[i]

		sse.ID = strings.TrimPrefix(sse.ID, prefix)
		sse.StreamBindings = renameBindings(sse.StreamBindings, rename, prefix)
	}

	for i := range spec.WebTransports {
		wt := &spec.WebTransports[i]

		wt.ID = strings.TrimPrefix(wt.ID, prefix)
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

// renameTagSet strips the prefix from cache tags.
//
// A tag is a typename with a suffix welded on -- `Order:{id}`, `Order[]` -- so
// the prefix is still leading and a plain trim is enough. Going through the
// rename map instead would mean parsing the suffix back off every tag to find
// the name to look up, for the same answer.
func renameTagSet(tags TagSet, prefix string) TagSet {
	return TagSet{
		Provides:    trimEach(tags.Provides, prefix),
		Invalidates: trimEach(tags.Invalidates, prefix),
	}
}

func trimEach(values []string, prefix string) []string {
	if values == nil {
		return nil
	}

	out := make([]string, len(values))
	for i, value := range values {
		out[i] = strings.TrimPrefix(value, prefix)
	}

	return out
}

func renameBindings(bindings []StreamBinding, rename map[string]string, prefix string) []StreamBinding {
	for i := range bindings {
		bindings[i].EntityType = renamed(bindings[i].EntityType, rename)
		bindings[i].Invalidates = trimEach(bindings[i].Invalidates, prefix)
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
