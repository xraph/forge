package client

import (
	"fmt"
	"sort"

	"github.com/xraph/forge/internal/router"
)

// idempotentExtension marks an operation served behind Forge's idempotency
// middleware. An offline outbox reads it to decide whether a write whose
// outcome is uncertain may be sent again.
const idempotentExtension = "x-forge-idempotent"

// syncExtension marks a route as one of a sync-backed entity's endpoints:
// {protocol, entity, table, dataset, role}, or a list of those, where role is
// pull, push, stream or socket and table may be absent (one table per
// dataset, chosen at runtime).
const syncExtension = "x-forge-sync"

// syncRoles are the endpoint roles a sync declaration may name.
var syncRoles = map[string]bool{"pull": true, "push": true, "stream": true, "socket": true}

// resolveEndpointIdempotent reads x-forge-idempotent onto the endpoint.
//
// Called from every place an endpoint is built (the spec parser, the
// introspector's OpenAPI path and its raw-route path) for the reason
// resolveEndpointCacheMeta is one function: the live and file builders must
// not disagree about what a route declared.
func resolveEndpointIdempotent(spec *APISpec, ep *Endpoint, ext map[string]any) {
	if v, ok := boolExtension(spec, ext, idempotentExtension, endpointOrigin(ep)); ok {
		ep.Idempotent = v
	}
}

// collectSyncRoute reads one route's x-forge-sync declarations into
// spec.Sync, grouped by entity.
//
// The value is one object or a list of them, because one Grove route serves
// several tables. origin names the route in a warning; routePath is the path
// recorded for each declared role. kindRole is the role the route's kind
// implies: "stream" for an SSE channel, "socket" for a WebSocket channel, ""
// for an HTTP operation, where a pull or push route must say which it is.
//
// routePath is recorded in OpenAPI form (`{id}`, not `:id`). A document gives
// that already; the introspector's raw-route path hands over the path as it
// was registered, while the declaration's dataset placeholder is always
// written `{id}`. Normalizing here keeps the two in one style, so a client
// that substitutes the dataset into the path finds the placeholder.
func collectSyncRoute(spec *APISpec, origin, routePath, kindRole string, ext map[string]any) {
	raw, present := ext[syncExtension]
	if !present {
		return
	}

	routePath = router.ConvertPathToOpenAPIFormat(routePath)

	var entries []map[string]any

	switch v := raw.(type) {
	case map[string]any:
		entries = []map[string]any{v}
	case []map[string]any:
		entries = v
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				unusable(spec, origin, syncExtension, "an object or a list of objects throughout")

				continue
			}

			entries = append(entries, m)
		}
	default:
		unusable(spec, origin, syncExtension, "an object or a list of objects")

		return
	}

	for _, entry := range entries {
		protocol, _ := entry["protocol"].(string)
		entity, _ := entry["entity"].(string)
		table, _ := entry["table"].(string)
		dataset, _ := entry["dataset"].(string)
		// Specs from older servers wrote the parameter as they pleased.
		dataset = router.NormalizeSyncParam(dataset)

		role, _ := entry["role"].(string)
		switch {
		case role == "":
			role = kindRole
		case kindRole != "" && role != kindRole:
			// The route's kind decides: a channel is a stream or a socket
			// whatever the declaration says, and recording the declared
			// role would file the path under an endpoint it does not serve.
			spec.Warnings = append(spec.Warnings, fmt.Sprintf(
				"client: %s declares %s role %q, but the route is a %s channel; recording it as %s.",
				origin, syncExtension, role, kindRole, kindRole))

			role = kindRole
		}

		if protocol == "" || entity == "" || !syncRoles[role] {
			unusable(spec, origin, syncExtension,
				"an object with non-empty protocol and entity, and a role of pull, push, stream or socket"+
					" (a pull or push route must declare its role)")

			continue
		}

		decl := SyncDecl{Protocol: protocol, Entity: entity, Table: table, Dataset: dataset}
		setSyncRole(&decl, role, routePath)
		addSyncDecl(spec, decl, origin)
	}
}

// registerSyncEntities gives every sync-backed entity an entities-table row,
// so the store can key its records even when no REST operation returns it.
// Identity is inferred exactly as for any other entity; a type that does not
// resolve is reported, since its records would have no key.
//
// Called from resolveEntityFields, the one point where the complete schema
// set is known, so a sync entity described by another merged document still
// resolves.
func registerSyncEntities(spec *APISpec) {
	for _, decl := range spec.Sync {
		if spec.Entities[decl.Entity] != nil {
			continue
		}

		if ref := InferEntity(spec, decl.Entity, spec.Schemas[decl.Entity]); ref != nil {
			if spec.Entities == nil {
				spec.Entities = make(map[string]*EntityRef)
			}

			spec.Entities[decl.Entity] = ref

			continue
		}

		warning := fmt.Sprintf(
			"client: x-forge-sync names entity %q, but no component schema gives it an identity field"+
				" (an `id`, or one marked x-forge-id); its records cannot be keyed in the store.",
			decl.Entity)

		if !contains(spec.Warnings, warning) {
			spec.Warnings = append(spec.Warnings, warning)
		}
	}
}

func setSyncRole(decl *SyncDecl, role, routePath string) {
	switch role {
	case "pull":
		decl.Pull = routePath
	case "push":
		decl.Push = routePath
	case "stream":
		decl.Stream = routePath
	case "socket":
		decl.Socket = routePath
	}
}

// addSyncDecl folds decl into the row for its entity, keeping spec.Sync
// sorted by entity. The first declaration of a field wins; a later one that
// disagrees is reported, because two routes claiming different tables for one
// entity is a mistake in the document, not a choice to make silently.
func addSyncDecl(spec *APISpec, decl SyncDecl, origin string) {
	i := sort.Search(len(spec.Sync), func(i int) bool { return spec.Sync[i].Entity >= decl.Entity })
	if i == len(spec.Sync) || spec.Sync[i].Entity != decl.Entity {
		spec.Sync = append(spec.Sync, SyncDecl{})
		copy(spec.Sync[i+1:], spec.Sync[i:])
		spec.Sync[i] = decl

		return
	}

	row := &spec.Sync[i]

	keep := func(field string, have *string, next string) {
		switch {
		case next == "" || *have == next:
		case *have == "":
			*have = next
		default:
			spec.Warnings = append(spec.Warnings, fmt.Sprintf(
				"client: %s declares %s %s %q for entity %q, which an earlier route declared as %q; keeping %q.",
				origin, syncExtension, field, next, decl.Entity, *have, *have))
		}
	}

	keep("protocol", &row.Protocol, decl.Protocol)
	keep("table", &row.Table, decl.Table)
	keep("dataset", &row.Dataset, decl.Dataset)
	keep("pull", &row.Pull, decl.Pull)
	keep("push", &row.Push, decl.Push)
	keep("stream", &row.Stream, decl.Stream)
	keep("socket", &row.Socket, decl.Socket)
}

// filterSync drops the sync endpoints a path filter excludes, and the rows
// left with none.
func (s *APISpec) filterSync(f PathFilter) {
	if len(s.Sync) == 0 {
		return
	}

	kept := s.Sync[:0]

	for _, decl := range s.Sync {
		for _, p := range []*string{&decl.Pull, &decl.Push, &decl.Stream, &decl.Socket} {
			if *p != "" && !f.allows(*p) {
				*p = ""
			}
		}

		if decl.Pull == "" && decl.Push == "" && decl.Stream == "" && decl.Socket == "" {
			continue
		}

		kept = append(kept, decl)
	}

	if len(kept) == 0 {
		s.Sync = nil

		return
	}

	s.Sync = kept
}

// renameSyncEntities applies a typename rename to every sync row, re-sorting
// because a rename can move a row.
func renameSyncEntities(spec *APISpec, rename map[string]string) {
	if len(spec.Sync) == 0 {
		return
	}

	for i := range spec.Sync {
		spec.Sync[i].Entity = renamed(spec.Sync[i].Entity, rename)
	}

	sort.SliceStable(spec.Sync, func(i, j int) bool { return spec.Sync[i].Entity < spec.Sync[j].Entity })
}
