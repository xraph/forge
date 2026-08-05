package typescript

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// OpsManifestGenerator emits src/ops.ts: the data the runtime needs to cache,
// invalidate and bind streams, with no logic in it.
//
// Keeping this a data file rather than generated code is what lets a runtime
// defect be fixed by publishing the runtime instead of regenerating every
// consuming repository.
type OpsManifestGenerator struct{}

func NewOpsManifestGenerator() *OpsManifestGenerator { return &OpsManifestGenerator{} }

// Generate produces ops.ts.
func (g *OpsManifestGenerator) Generate(spec *client.APISpec, _ client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(`/**
 * Operation manifest.
 *
 * Generated. Entity identity was resolved in Go against the response schema, so
 * the runtime never has to guess which field identifies a record -- the class of
 * guess that, made wrong on a type carrying both an id and a tenant id, keys two
 * tenants' records to one cache entry.
 */

export interface OperationMeta {
  readonly method: string;
  readonly path: string;
  /** The entity this operation's cache contract is about. */
  readonly entity?: string;
  /**
   * The typename of the response document itself -- of its ELEMENTS when the
   * response is a bare array -- which is what indexes the entities table below.
   *
   * Not interchangeable with the entity field above. On a paginated read the
   * two differ: entity is 'Order' while rootType is 'PageOrder'. Normalizing
   * such a response against 'Order' would read Order's field edges against an
   * envelope's properties and descend into nothing.
   */
  readonly rootType?: string;
  readonly provides: readonly string[];
  readonly invalidates: readonly string[];
}

/**
 * A row with no idField is a signpost, not a record: an envelope, or a hop
 * between two entities. It is walked for its fields and never stored.
 */
export interface EntityMeta {
  readonly idField?: string;
  readonly fields?: Readonly<Record<string, string>>;
}

`)

	rows := entityRows(spec)

	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.name] = true
	}

	g.writeOps(&buf, spec, known)
	g.writeEntities(&buf, rows)
	g.writeStreams(&buf, spec)

	return buf.String()
}

// entityRow is one line of the `entities` table: an entity, or a type that only
// routes typenames onward.
type entityRow struct {
	name    string
	idField string
	fields  map[string]string
}

// entityRows merges the spec's entities and routing types into the single table
// the runtime reads, sorted by typename.
//
// One table because the runtime has one question -- "given this typename, where
// do I descend and what identifies it" -- and the answer for a routing type is
// just that second half being empty. The two are kept apart in Go, where
// `spec.Entities[name]` is read as "is this an entity"; they are disjoint by
// construction there, so no name can arrive here twice.
//
// Go randomises map iteration and this file is byte-diffed by CI, so the sort
// is load-bearing rather than cosmetic.
func entityRows(spec *client.APISpec) []entityRow {
	rows := make([]entityRow, 0, len(spec.Entities)+len(spec.RoutingTypes))

	for _, table := range []map[string]*client.EntityRef{spec.Entities, spec.RoutingTypes} {
		for name, ref := range table {
			if ref == nil {
				continue
			}

			rows = append(rows, entityRow{name: name, idField: ref.IDField, fields: ref.Fields})
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	return rows
}

// writeOps emits the operation table in endpoint order.
//
// That order is only deterministic because both IR builders now walk paths in
// sorted order and methods in a fixed order (see sortedPathKeys/orderedPathOps
// in package client). They used to range straight over the spec's path map and
// over a `methods` map, so two parses of the same file produced two different
// endpoint orders and this file churned on every regeneration -- which is
// precisely what a CI drift check reports as a diff.
func (g *OpsManifestGenerator) writeOps(
	buf *strings.Builder, spec *client.APISpec, known map[string]bool,
) {
	buf.WriteString("export const ops = {\n")

	keys := operationKeys(spec.Endpoints)

	for i := range spec.Endpoints {
		ep := &spec.Endpoints[i]

		buf.WriteString(fmt.Sprintf("  %s: {\n", tsKey(keys[i])))
		buf.WriteString(fmt.Sprintf("    method: %s,\n", tsString(ep.Method)))
		buf.WriteString(fmt.Sprintf("    path: %s,\n", tsString(ep.Path)))

		if ep.Entity != nil {
			buf.WriteString(fmt.Sprintf("    entity: %s,\n", tsString(ep.Entity.Type)))
		}

		// Emitted whenever the table can answer it, INCLUDING when it repeats
		// the entity name. Omitting the repetition and letting the runtime fall
		// back to `entity` would be correct only while the two happen to agree,
		// and the case this whole field exists for is the one where they do
		// not; a reader of the manifest should not have to know which.
		//
		// A root type with no row is dropped instead: the runtime's only use
		// for it is to index this table, and a lookup that misses descends with
		// no typename, exactly as an absent field does.
		if known[ep.RootType] {
			buf.WriteString(fmt.Sprintf("    rootType: %s,\n", tsString(ep.RootType)))
		}

		buf.WriteString(fmt.Sprintf("    provides: %s,\n", tsStringArray(ep.CacheTags.Provides)))
		buf.WriteString(fmt.Sprintf("    invalidates: %s,\n", tsStringArray(ep.CacheTags.Invalidates)))
		buf.WriteString("  },\n")
	}

	buf.WriteString("} as const satisfies Record<string, OperationMeta>;\n\n")
}

// writeEntities emits the typename-to-metadata table, sorted by typename.
//
// Each entry carries the id field, and -- when the type has any -- the
// property-to-typename edges the runtime descends through to normalize a
// nested entity. Both are omitted rather than emitted empty. `fields: {}` would
// say in more bytes what an absent key already says; an absent `idField` says
// something stronger, that this row is an envelope or an intermediate hop and
// has no identity to be stored under, which is why it is a legal row rather
// than a malformed one.
//
// The property names inside `fields` are sorted for the same reason the rows
// are: Go randomises map iteration, and this file is byte-diffed by CI, so an
// unsorted walk reports a spurious change on every run.
func (g *OpsManifestGenerator) writeEntities(buf *strings.Builder, rows []entityRow) {
	buf.WriteString("export const entities = {\n")

	for _, row := range rows {
		buf.WriteString(fmt.Sprintf("  %s: {", tsKey(row.name)))

		parts := make([]string, 0, 2)
		if row.idField != "" {
			parts = append(parts, "idField: "+tsString(row.idField))
		}

		if len(row.fields) > 0 {
			parts = append(parts, "fields: "+tsFieldMap(row.fields))
		}

		if len(parts) > 0 {
			buf.WriteString(" " + strings.Join(parts, ", ") + " ")
		}

		buf.WriteString("},\n")
	}

	buf.WriteString("} as const satisfies Record<string, EntityMeta>;\n\n")
}

// tsFieldMap renders a property-to-typename map as an object literal with its
// keys in sorted order.
func tsFieldMap(fields map[string]string) string {
	props := make([]string, 0, len(fields))
	for prop := range fields {
		props = append(props, prop)
	}

	sort.Strings(props)

	parts := make([]string, 0, len(props))
	for _, prop := range props {
		parts = append(parts, fmt.Sprintf("%s: %s", tsKey(prop), tsString(fields[prop])))
	}

	return "{ " + strings.Join(parts, ", ") + " }"
}

// writeStreams emits channel bindings from both WebSocket and SSE endpoints.
func (g *OpsManifestGenerator) writeStreams(buf *strings.Builder, spec *client.APISpec) {
	type channel struct {
		path     string
		bindings []client.StreamBinding
	}

	channels := make([]channel, 0, len(spec.WebSockets)+len(spec.SSEs))

	for i := range spec.WebSockets {
		if b := spec.WebSockets[i].StreamBindings; len(b) > 0 {
			channels = append(channels, channel{spec.WebSockets[i].Path, b})
		}
	}

	for i := range spec.SSEs {
		if b := spec.SSEs[i].StreamBindings; len(b) > 0 {
			channels = append(channels, channel{spec.SSEs[i].Path, b})
		}
	}

	sort.Slice(channels, func(i, j int) bool { return channels[i].path < channels[j].path })

	buf.WriteString("export const streams = [\n")

	for _, ch := range channels {
		for _, b := range ch.bindings {
			buf.WriteString("  {\n")
			buf.WriteString(fmt.Sprintf("    channel: %s,\n", tsString(ch.path)))
			buf.WriteString(fmt.Sprintf("    message: %s,\n", tsString(b.Message)))
			buf.WriteString(fmt.Sprintf("    entity: %s,\n", tsString(b.EntityType)))
			buf.WriteString(fmt.Sprintf("    intent: %s,\n", tsString(string(b.Intent))))
			buf.WriteString(fmt.Sprintf("    invalidates: %s,\n", tsStringArray(b.Invalidates)))
			buf.WriteString("  },\n")
		}
	}

	buf.WriteString("] as const;\n")
}

// tsString renders a single-quoted TypeScript string literal.
//
// Escaping is not paranoia: a path or tag reaching the file unescaped closes the
// literal and the generated module stops parsing, which surfaces as a build
// error in a file nobody wrote by hand.
func tsString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`)

	return "'" + r.Replace(s) + "'"
}

// tsStringArray renders a string array literal.
func tsStringArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}

	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, tsString(item))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// tsMember renders a member access on object.
//
// tsKey is correct for an object-literal key but NOT after a dot: it returns a
// quoted string for anything that is not a bare identifier, and `ops.'x.y'` is
// a syntax error. Anything tsKey would quote is emitted as bracket access
// instead -- `ops['get.orders.id']` -- which is the same property, spelled the
// way TypeScript accepts in an expression.
func tsMember(object, key string) string {
	if tsKey(key) == key {
		return object + "." + key
	}

	return object + "[" + tsString(key) + "]"
}

// tsKey renders an object key, quoting it when it is not a bare identifier.
func tsKey(s string) string {
	for i, r := range s {
		valid := r == '_' || r == '$' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !valid {
			return tsString(s)
		}
	}

	if s == "" {
		return tsString(s)
	}

	return s
}
