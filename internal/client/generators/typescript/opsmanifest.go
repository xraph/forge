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
func (g *OpsManifestGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	// Whether this run renames anything at all. Everything codec-shaped below
	// -- the two OperationMeta fields, the values emitted into them, and the
	// renaming applied to the entities table -- is gated on it, so a
	// NamingPreserve run with no FieldOverrides emits byte-for-byte what it
	// emitted before any of this existed. That is not merely tidiness: CI
	// byte-diffs this file.
	needsCodecs := codecsNeeded(config)

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
  /**
   * Keys into securitySchemes below, so an AuthProvider can attach exactly
   * the credential this operation declared instead of blanketing every
   * request with every credential the document happens to carry.
   */
  readonly security?: readonly string[];
`)

	// The codec ids ride on OperationMeta rather than on a table of their own
	// because the runtime's REST transport drives the generated
	// `HTTPClient#request`, not the typed per-endpoint methods -- those take
	// positional, per-endpoint parameters no generic caller holding only an
	// OperationMeta can fill. OperationMeta is already the complete
	// generation-time description of one operation the runtime is handed
	// (method, path, entity, rootType, cache tags); a codec id is another
	// per-operation fact resolved in Go by exactly the same pass, so putting
	// it anywhere else would mean the transport needing a second lookup keyed
	// by the operation it already holds. rest.go writes the SAME ids into the
	// typed methods' RequestConfig from the same resolvers
	// (requestBodyCodecRef/responseCodecRef), which is what makes the two call
	// paths agree instead of contradicting the generated types.
	if needsCodecs {
		buf.WriteString(`  /**
   * Schema id (a key into src/codecs.ts's CODECS table) that renames this
   * operation's JSON request body from its TypeScript shape to the wire shape.
   *
   * Present only when the endpoint's request body is application/json AND
   * resolves to a named component schema -- the same condition under which the
   * generated typed method sets RequestConfig.bodyCodec, because both come
   * from the same resolver.
   */
  readonly bodyCodec?: string;
  /** The same, for decoding a JSON response back into its TypeScript shape. */
  readonly responseCodec?: string;
`)
	}

	buf.WriteString(`}

/**
 * A row with no idField is a signpost, not a record: an envelope, or a hop
 * between two entities. It is walked for its fields and never stored.
`)

	// Why the property names in this table are the CLIENT-side ones, not the
	// wire ones, whenever anything renames: the runtime normalizes a response
	// that `decode` has already renamed. A table still naming `order_number`
	// against a payload carrying `orderNumber` does not fail loudly -- a type
	// whose id field is absent simply is not an entity -- so the cache would
	// quietly stop caching. Wrong casing is visible; silent non-normalization
	// is not, which is why this half and the codec ids above are one change.
	if needsCodecs {
		buf.WriteString(` *
 * The property names below -- idField, and the KEYS of fields -- are the
 * client-side names this client's field naming produces, because that is what
 * the decoded payload actually carries. The VALUES of fields are typenames,
 * which are not field names and are never renamed.
`)
	}

	buf.WriteString(` */
export interface EntityMeta {
  readonly idField?: string;
  readonly fields?: Readonly<Record<string, string>>;
}

`)

	rows := entityRows(spec, config)

	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.name] = true
	}

	g.writeSecuritySchemes(&buf, spec)
	g.writeOps(&buf, spec, config, known, needsCodecs)
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
//
// EntityRef.IDField and the KEYS of EntityRef.Fields arrive here as verbatim
// wire names -- they trace back to schema.Properties keys, i.e. pre-rename --
// and are renamed here, through tsFieldName, into the names the DECODED
// payload carries. See renameEntityField for why that must happen in the same
// change that lets the runtime decode at all.
func entityRows(spec *client.APISpec, config client.GeneratorConfig) []entityRow {
	rows := make([]entityRow, 0, len(spec.Entities)+len(spec.RoutingTypes))

	for _, table := range []map[string]*client.EntityRef{spec.Entities, spec.RoutingTypes} {
		for name, ref := range table {
			if ref == nil {
				continue
			}

			rows = append(rows, entityRow{
				name:    name,
				idField: renameEntityField(name, ref.IDField, config),
				fields:  renameEntityFields(name, ref.Fields, config),
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	return rows
}

// renameEntityField resolves one wire property name of typeName into the
// client-side name the decoded payload carries.
//
// tsFieldName -- the SAME function generator.go renders every interface
// property through and codecs.go builds every codec entry from -- is what
// does the work, deliberately rather than a second copy of the camel/pascal/
// snake rule here. A second implementation of a naming rule is how the two
// drift, and a drift between the codec table and this one is invisible: the
// normalizer would look for a field the payload does not have, decide the type
// is not an entity, and store nothing, with no error anywhere.
//
// typeName is passed as tsFieldName's schema-name argument because an entity's
// typename IS its component-schema name -- the same id codecIDFor derives for
// that schema's own properties -- so a schema-scoped FieldOverrides entry
// ("Order.order_number") reaches the codec table and this table identically,
// which is the only way they can stay in step.
//
// An empty wireName means "this row has no identity" (an envelope, or a hop
// between entities) and is returned untouched rather than handed to
// tsFieldName, whose FieldOverrides lookup would otherwise consult the
// meaningless key "Order.".
func renameEntityField(typeName, wireName string, config client.GeneratorConfig) string {
	if wireName == "" {
		return ""
	}

	return tsFieldName(typeName, wireName, config)
}

// renameEntityFields renames the KEYS of a field-edge map and copies its
// VALUES verbatim.
//
// The asymmetry is the whole point. A key is a JSON property of typeName and
// gets renamed with everything else the payload carries; a value is a
// TYPENAME -- the name of another row in this very table, and of a generated
// TypeScript interface -- which is not a field name at all. Renaming a value
// would point the edge at a table key that does not exist ("Order.customer ->
// customer" instead of "Customer"), breaking the entities lookup outright for
// every nested entity.
//
// Returns nil for an empty input so writeEntities' `len(row.fields) > 0` check
// still omits the key entirely rather than emitting `fields: {}`.
func renameEntityFields(typeName string, fields map[string]string, config client.GeneratorConfig) map[string]string {
	if len(fields) == 0 {
		return nil
	}

	renamed := make(map[string]string, len(fields))
	for wireName, targetTypeName := range fields {
		renamed[renameEntityField(typeName, wireName, config)] = targetTypeName
	}

	return renamed
}

// renameDerivedIDTags rewrites the one cache tag whose placeholder is a
// schema property name -- the item tag DeriveTags builds as
// `Type:{IDField}` -- into the client-side name, for the same reason
// entityRows renames idField.
//
// The runtime resolves a `provides` template against the request arguments
// and then the RESPONSE (see resolveTags/QueryRegistry#settle), and the
// response it is handed is the one the codec already decoded. A template
// still saying `{order_number}` against a payload carrying `orderNumber`
// resolves to nothing, the query is registered under no item tag at all, and
// a later write to that order invalidates nothing -- the same silent
// stops-caching failure a wire-named idField causes, one table over.
//
// The rewrite is deliberately an EXACT match against the tag DeriveTags
// would have produced for this endpoint's entity, not a general pass over
// every placeholder. A route may declare arbitrary templates
// (`Customer:{req.customerId}`, `Shipment:{res.shipment.id}`), whose
// segments name properties of types this function has no way to resolve; a
// general rewrite would have to guess a namespace per segment, and guessing
// wrong here produces a tag that silently matches nothing. Matching only the
// derived form renames exactly what the generator itself wrote and leaves
// every hand-declared template alone. A declared template that happens to be
// byte-identical to the derived one means the same thing anyway.
//
// Under NamingPreserve with no FieldOverrides the replacement equals the
// original, so this is an identity pass and the emitted bytes do not change.
func renameDerivedIDTags(tags []string, entity *client.EntityRef, config client.GeneratorConfig) []string {
	if entity == nil || entity.IDField == "" || len(tags) == 0 {
		return tags
	}

	derived := entity.Type + ":{" + entity.IDField + "}"
	renamed := entity.Type + ":{" + renameEntityField(entity.Type, entity.IDField, config) + "}"

	if derived == renamed {
		return tags
	}

	out := make([]string, len(tags))

	for i, tag := range tags {
		if tag == derived {
			out[i] = renamed
		} else {
			out[i] = tag
		}
	}

	return out
}

// writeSecuritySchemes emits the securitySchemes table: every scheme the
// document declares, described once and referenced from operations by key.
//
// Normalized rather than inlined per operation, for the same reason the
// entities table is its own table: repeating four fields across two hundred
// operations is bundle weight for data that never varies. An operation
// carries only the scheme keys; an AuthProvider looks the rest up here.
//
// Omitted entirely -- not emitted as `{}` -- when the document declares no
// schemes, matching how writeEntities and writeStreams treat their own empty
// case: bytes for a table nothing will ever look up.
//
// spec.Security arrives already sorted by Key (the parser and introspector
// both guarantee it), so this walks it as-is rather than sorting again or
// ranging a map -- either of which would either be redundant or reintroduce
// the nondeterminism the upstream sort exists to remove.
func (g *OpsManifestGenerator) writeSecuritySchemes(buf *strings.Builder, spec *client.APISpec) {
	if len(spec.Security) == 0 {
		return
	}

	buf.WriteString("/**\n")
	buf.WriteString(" * Every security scheme the document declares, described once.\n")
	buf.WriteString(" *\n")
	buf.WriteString(" * Normalized rather than inlined per operation, for the same reason\n")
	buf.WriteString(" * `entities` is its own table: repeating four fields across two hundred\n")
	buf.WriteString(" * operations is bundle weight for data that never varies. An operation\n")
	buf.WriteString(" * carries the keys; an AuthProvider looks them up here.\n")
	buf.WriteString(" */\n")
	buf.WriteString("export const securitySchemes = {\n")

	for _, s := range spec.Security {
		buf.WriteString(fmt.Sprintf("  %s: { type: %s", tsKey(s.Key), tsString(s.Type)))

		// in/name are meaningless for http and oauth2 schemes, and scheme is
		// meaningless for apiKey -- each is emitted only when the document
		// actually populated it, so a bearer scheme reads as `{ type: 'http',
		// scheme: 'bearer' }` rather than trailing empty-string fields.
		if s.In != "" {
			buf.WriteString(fmt.Sprintf(", in: %s", tsString(s.In)))
		}

		if s.ParamName != "" {
			buf.WriteString(fmt.Sprintf(", name: %s", tsString(s.ParamName)))
		}

		if s.Scheme != "" {
			buf.WriteString(fmt.Sprintf(", scheme: %s", tsString(s.Scheme)))
		}

		buf.WriteString(" },\n")
	}

	buf.WriteString("} as const;\n\n")
}

// operationSecurityKeys flattens an endpoint's security requirements into the
// scheme keys `security` carries, sorted and deduplicated.
//
// A requirement's own Scopes are dropped here: this table exists to let an
// AuthProvider attach a credential, not to answer capability questions, and
// the scope-aware view of the same data already lives in capabilities.ts.
// Two SecurityRequirement entries naming the same scheme with different
// scopes -- a legal OpenAPI OR-of-scope-sets -- would otherwise emit the key
// twice, so the map dedupes before sorting.
func operationSecurityKeys(reqs []client.SecurityRequirement) []string {
	if len(reqs) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(reqs))

	keys := make([]string, 0, len(reqs))
	for _, req := range reqs {
		if req.SchemeName == "" || seen[req.SchemeName] {
			continue
		}

		seen[req.SchemeName] = true

		keys = append(keys, req.SchemeName)
	}

	sort.Strings(keys)

	return keys
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
	buf *strings.Builder, spec *client.APISpec, config client.GeneratorConfig,
	known map[string]bool, needsCodecs bool,
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

		// Renamed for the same reason the entities table is: these templates
		// are resolved against a response the codec has already decoded. See
		// renameDerivedIDTags.
		buf.WriteString(fmt.Sprintf("    provides: %s,\n",
			tsStringArray(renameDerivedIDTags(ep.CacheTags.Provides, ep.Entity, config))))
		buf.WriteString(fmt.Sprintf("    invalidates: %s,\n",
			tsStringArray(renameDerivedIDTags(ep.CacheTags.Invalidates, ep.Entity, config))))

		// Unlike provides/invalidates above, which always emit `[]` when
		// empty, an unsecured operation drops the field entirely: bundle
		// weight for a lookup an AuthProvider would run and find empty on
		// every one of the (usually many) unauthenticated operations.
		if keys := operationSecurityKeys(ep.Security); len(keys) > 0 {
			buf.WriteString(fmt.Sprintf("    security: %s,\n", tsStringArray(keys)))
		}

		// The codec ids the runtime's generic caller needs, resolved by the
		// SAME functions rest.go resolves the typed methods' RequestConfig
		// with -- so the two call paths cannot disagree about which codec
		// encodes a body or decodes a response.
		//
		// The warning half of each resolver's return is deliberately dropped
		// here: rest.go already appends it to RESTGenerator.warnings for the
		// identical endpoint, and reporting it twice would say a spec has two
		// problems where it has one. An unresolvable ref yields "" on both
		// sides, so the manifest stays silent exactly where the typed method
		// does.
		if needsCodecs {
			if codecID, _ := requestBodyCodecRef(ep); codecID != "" {
				buf.WriteString(fmt.Sprintf("    bodyCodec: %s,\n", tsString(codecID)))
			}

			if codecID, _ := responseCodecRef(ep); codecID != "" {
				buf.WriteString(fmt.Sprintf("    responseCodec: %s,\n", tsString(codecID)))
			}
		}

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
