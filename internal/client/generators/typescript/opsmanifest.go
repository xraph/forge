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
  readonly entity?: string;
  readonly provides: readonly string[];
  readonly invalidates: readonly string[];
}

export interface EntityMeta {
  readonly idField: string;
  readonly fields?: Readonly<Record<string, string>>;
}

`)

	g.writeOps(&buf, spec)
	g.writeEntities(&buf, spec)
	g.writeStreams(&buf, spec)

	return buf.String()
}

// writeOps emits the operation table in endpoint order.
//
// That order is only deterministic because both IR builders now walk paths in
// sorted order and methods in a fixed order (see sortedPathKeys/orderedPathOps
// in package client). They used to range straight over the spec's path map and
// over a `methods` map, so two parses of the same file produced two different
// endpoint orders and this file churned on every regeneration -- which is
// precisely what a CI drift check reports as a diff.
func (g *OpsManifestGenerator) writeOps(buf *strings.Builder, spec *client.APISpec) {
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
// nested entity. `fields` is omitted rather than emitted empty: an entity with
// no entity-typed property has no edges, and `fields: {}` would say the same
// thing in more bytes.
//
// Both the typenames and the property names inside `fields` are sorted. Go
// randomises map iteration, and this file is byte-diffed by CI, so an
// unsorted walk over EntityRef.Fields reports a spurious change on every run.
func (g *OpsManifestGenerator) writeEntities(buf *strings.Builder, spec *client.APISpec) {
	names := make([]string, 0, len(spec.Entities))
	for name := range spec.Entities {
		names = append(names, name)
	}

	sort.Strings(names)

	buf.WriteString("export const entities = {\n")

	for _, name := range names {
		entity := spec.Entities[name]
		if entity == nil {
			continue
		}

		buf.WriteString(fmt.Sprintf("  %s: { idField: %s", tsKey(name), tsString(entity.IDField)))

		if len(entity.Fields) > 0 {
			buf.WriteString(", fields: " + tsFieldMap(entity.Fields))
		}

		buf.WriteString(" },\n")
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
