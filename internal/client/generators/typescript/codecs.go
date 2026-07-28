package typescript

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// CodecGenerator emits src/codecs.ts: a per-schema description of how a wire
// payload maps onto its TypeScript shape, plus the encode/decode runtime that
// walks it.
//
// The table is the deliverable Phase 3 depends on. Today every field's `ts`
// name equals its wire name, so encode/decode are effectively identity — the
// point of emitting it now is that the SHAPE is in place and gate-tested, so
// Phase 3 only has to change what the names are, not build the machinery
// that renames them.
type CodecGenerator struct{}

// NewCodecGenerator mirrors the other generators' constructor shape.
func NewCodecGenerator() *CodecGenerator {
	return &CodecGenerator{}
}

// codecEntry is the Go-side model of one emitted CODECS entry. It is
// marshalled to JSON rather than hand-written as TypeScript so that string
// escaping is correct for any schema or property name (see enumTSType, which
// takes the same approach for the same reason).
type codecEntry struct {
	Kind string `json:"kind"`

	// object (and allOf, which codecs as an object -- see allOfEntry)
	Fields map[string]codecField `json:"fields,omitempty"`

	// Required lists which of Fields' WIRE names must be present on the
	// value -- the data an undiscriminated union match tests against. Kept
	// sorted so the table (and the union match order it feeds) is
	// deterministic regardless of what order Required appeared in the
	// source schema, or what order multiple allOf members contributed it.
	Required []string `json:"required,omitempty"`

	// array
	Items string `json:"items,omitempty"`

	// record
	Values string `json:"values,omitempty"`

	// union. Discriminator is absent for an undiscriminated union: Members
	// is still populated, and the runtime falls back to trying each one
	// structurally in order (see codecRuntime's 'union' case) rather than
	// having nothing to decode against at all.
	Discriminator *codecDiscriminator `json:"discriminator,omitempty"`
	Members       []string            `json:"members,omitempty"`
}

type codecField struct {
	TS    string `json:"ts"`
	Codec string `json:"codec,omitempty"`
}

type codecDiscriminator struct {
	Wire string            `json:"wire"`
	Map  map[string]string `json:"map"`
}

// codecTable accumulates entries while walking the spec. Inline (non-$ref)
// nested schemas have no name of their own, so they get a synthetic id
// derived from the parent name and property path ("Nested.items"). Deriving
// it from the path rather than a counter is what keeps the table
// byte-identical across runs.
type codecTable struct {
	entries map[string]codecEntry

	// warnings accumulates generation-time messages that don't abort
	// generation but are worth surfacing -- currently just "this union has
	// no discriminator". Sorted before being handed back to the caller (see
	// CodecGenerator.Generate) so callers get a stable order regardless of
	// the recursion shape that produced them.
	warnings []string
}

// refName extracts the schema name from a "#/components/schemas/X" pointer.
// A ref that does not follow that shape yields "", which callers treat as
// "no codec" rather than emitting a dangling id.
func refName(ref string) string {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}

	return strings.TrimPrefix(ref, prefix)
}

// codecIDFor returns the codec id a property's value should be decoded with,
// registering a synthetic entry first when the property is an inline
// composite. Primitives get "" — there is nothing to rename inside a string
// or a number, and emitting a passthrough entry for every scalar would
// triple the table for no behavioural gain.
func (t *codecTable) codecIDFor(parentID, prop string, schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return ""
	}

	if name := refName(schema.Ref); name != "" {
		return name
	}

	synthetic := parentID + "." + prop

	switch {
	case schema.Type == "array" && schema.Items != nil:
		t.add(synthetic, spec, schema)
		return synthetic
	case len(schema.Properties) > 0:
		t.add(synthetic, spec, schema)
		return synthetic
	case len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 || len(schema.AllOf) > 0:
		// AllOf included alongside OneOf/AnyOf (Gap 2): a property whose
		// schema is a pure allOf composition -- no Properties of its own,
		// only AllOf -- would otherwise fall through every case above and
		// return "" (no codec), leaving it unwalked even though it renders
		// as a real object (an intersection type) on the TypeScript side.
		t.add(synthetic, spec, schema)
		return synthetic
	}

	if _, ok := additionalPropsSchema(schema.AdditionalProperties); ok {
		t.add(synthetic, spec, schema)
		return synthetic
	}

	return ""
}

// add builds the entry for one schema under the given id. It is called for
// both named schemas and synthetic inline ones; the only difference is where
// the id came from.
func (t *codecTable) add(id string, spec *client.APISpec, schema *client.Schema) {
	if schema == nil {
		return
	}

	if _, seen := t.entries[id]; seen {
		// Already built. Guards against a schema that references itself,
		// directly or through a cycle, which would otherwise recurse forever.
		return
	}

	// Reserve the id before recursing, so a self-reference hits the guard
	// above rather than re-entering.
	t.entries[id] = codecEntry{Kind: "passthrough"}

	switch {
	case len(schema.OneOf) > 0 || len(schema.AnyOf) > 0:
		t.entries[id] = t.unionEntry(id, schema, spec)
		return

	case len(schema.AllOf) > 0:
		t.entries[id] = t.allOfEntry(id, schema, spec)
		return

	case schema.Type == "array" && schema.Items != nil:
		t.entries[id] = codecEntry{
			Kind:  "array",
			Items: t.codecIDFor(id, "items", schema.Items, spec),
		}

		return

	case len(schema.Properties) > 0:
		fields := make(map[string]codecField, len(schema.Properties))
		for _, prop := range sortedKeys(schema.Properties) {
			fields[prop] = codecField{
				// ts == wire for now: renaming lands in Phase 3. The field
				// map has to exist today regardless, because it is what
				// Phase 3 rewrites.
				TS:    prop,
				Codec: t.codecIDFor(id, prop, schema.Properties[prop], spec),
			}
		}

		t.entries[id] = codecEntry{Kind: "object", Fields: fields, Required: requiredWireFields(fields, schema.Required)}

		return
	}

	if values, ok := additionalPropsSchema(schema.AdditionalProperties); ok {
		t.entries[id] = codecEntry{
			Kind: "record",
			// A nil values schema means `additionalProperties: true` — the
			// values are unconstrained, so there is nothing to descend into.
			Values: t.codecIDFor(id, "values", values, spec),
		}

		return
	}

	// Anything else (scalars, empty objects, unresolvable refs) stays the
	// passthrough reserved above.
}

// unionEntry builds a union entry. WITH a discriminator, decode can switch
// directly on its wire value. WITHOUT one, there is no tag to switch on, so
// the runtime instead tries each member in declared order and picks the
// first whose required wire fields are all present on the value (see
// codecRuntime's 'union' case) -- never a best-effort guess: no match falls
// back to passthrough. Because that ambiguity is real (a payload could
// structurally satisfy more than one member, or none), the caller records a
// warning naming this schema so it isn't silently invisible.
func (t *codecTable) unionEntry(id string, schema *client.Schema, spec *client.APISpec) codecEntry {
	members := schema.OneOf
	token := "oneOf"
	if len(members) == 0 {
		members = schema.AnyOf
		token = "anyOf"
	}

	memberIDs := make([]string, 0, len(members))

	for i, member := range members {
		if member == nil {
			continue
		}

		if name := refName(member.Ref); name != "" {
			// Force this member's entry to exist NOW rather than trusting the
			// top-level sortedKeys(spec.Schemas) loop to reach it eventually.
			// The evidence-free check just below inspects t.entries[name] --
			// if this union sorts alphabetically before its own member (e.g.
			// schema "Alpha" referencing "Zebra"), that entry would not have
			// been built yet, and would be misread as "no entry" (which the
			// check below also treats as evidence-free, but for the wrong
			// reason). t.add is idempotent -- a no-op if already built now
			// or later -- so calling it eagerly here is always safe.
			t.add(name, spec, spec.Schemas[name])
			memberIDs = append(memberIDs, name)
			continue
		}

		// Gap 1: an inline (non-$ref) member previously got no id at all here
		// and was silently skipped -- it could never be selected, structurally
		// or otherwise, no matter how well a payload matched it. The synthetic
		// id reuses the exact "<id>.oneOf<N>"/"<id>.anyOf<N>" scheme
		// checkSchemaFieldCollisions (fieldname.go) already defines for this
		// namespace, so the two agree on what an inline union member is called.
		synthetic := fmt.Sprintf("%s.%s%d", id, token, i)
		t.add(synthetic, spec, member)
		memberIDs = append(memberIDs, synthetic)
	}

	if schema.Discriminator == nil || schema.Discriminator.PropertyName == "" {
		t.warnings = append(t.warnings, fmt.Sprintf(
			"schema %q: union has no discriminator; members will be tried in declared order and matched by required wire fields (no match falls back to passthrough) -- add a discriminator to remove the ambiguity",
			id))

		// A member that is not an 'object' kind, or is an object with no
		// required fields, offers no evidence a structural match can test:
		// codecRuntime's union case would otherwise treat an empty required
		// list as vacuously satisfied by ANY payload, turning that member
		// into an unconditional catch-all rather than a real test -- exactly
		// the "best-effort guess" the whole feature exists to rule out. Such
		// a member is skipped entirely at runtime (see codecRuntime), so a
		// union whose first (or only) member is evidence-free degrades to
		// permanent passthrough -- degenerate, and worth calling out by name
		// rather than leaving the caller to notice via silent non-matching.
		for _, memberID := range memberIDs {
			entry, ok := t.entries[memberID]

			kind := "undefined"
			if ok {
				kind = entry.Kind
			}

			if !ok || entry.Kind != "object" || len(entry.Required) == 0 {
				t.warnings = append(t.warnings, fmt.Sprintf(
					"schema %q: union member %q offers no required wire fields to match on (kind %q) and can never be selected by structural matching -- give it required fields or add a discriminator",
					id, memberID, kind))
			}
		}

		return codecEntry{Kind: "union", Members: memberIDs}
	}

	mapping := make(map[string]string, len(schema.Discriminator.Mapping))
	for _, tag := range sortedKeys(schema.Discriminator.Mapping) {
		if name := refName(schema.Discriminator.Mapping[tag]); name != "" {
			mapping[tag] = name
		}
	}

	return codecEntry{
		Kind: "union",
		Discriminator: &codecDiscriminator{
			Wire: schema.Discriminator.PropertyName,
			Map:  mapping,
		},
		Members: memberIDs,
	}
}

// flattenAllOfLayers recursively resolves schema into an ordered list of the
// schemas that directly own the properties composing it: each AllOf member
// resolved through however many further $ref hops and nested AllOf
// compositions it takes to reach something with its own Properties, in the
// order those properties should be applied (earliest member first, the
// schema's own Properties -- which allOf permits alongside its members,
// unusual but legal -- last). This is what lets a three-level allOf
// inheritance chain (Outer.allOf[$ref Mid], where Mid.allOf[$ref Leaf], an
// ordinary OpenAPI pattern) resolve down to Leaf's actual fields, instead of
// stopping at Mid -- which has none of its own -- and silently producing an
// entry with no fields at all.
//
// Returning every contributing layer, rather than a single pre-merged map,
// is what lets allOfEntry notice when two layers declare the SAME wire
// field name with two DIFFERENT effective codecs, instead of silently
// letting the later layer win with no record that an earlier one's shape
// was discarded.
//
// A member that cannot be resolved at all -- a dangling $ref (the target
// name isn't in spec.Schemas), or a $ref in a shape refName doesn't
// recognise (e.g. a cross-file "./common.yaml#/Base") -- contributes no
// layers rather than panicking: the nil checks below turn "nothing found"
// into "no fields from this member", which allOfEntry's empty-result
// fallback (passthrough) then degrades safely instead of emitting a lying
// empty object.
//
// visited guards a schema-graph cycle -- through a $ref cycle (A allOf B,
// B allOf A) or a hand-built Go pointer cycle -- by tracking schema
// pointers already on the current resolution path; re-reaching one
// contributes no further layers rather than recursing forever. Because a
// named $ref always resolves to the SAME *client.Schema pointer from
// spec.Schemas, tracking pointers alone catches both cycle shapes with one
// mechanism.
func flattenAllOfLayers(schema *client.Schema, spec *client.APISpec, visited map[*client.Schema]bool) []*client.Schema {
	if schema == nil || visited[schema] {
		return nil
	}

	visited[schema] = true
	defer delete(visited, schema)

	if name := refName(schema.Ref); name != "" {
		// spec.Schemas[name] is nil for a dangling ref; the nil check above
		// turns that into "no layers" on the next call, not a crash.
		return flattenAllOfLayers(spec.Schemas[name], spec, visited)
	}

	var layers []*client.Schema

	for _, member := range schema.AllOf {
		layers = append(layers, flattenAllOfLayers(member, spec, visited)...)
	}

	if len(schema.Properties) > 0 {
		layers = append(layers, schema)
	}

	return layers
}

// allOfEntry builds an object entry for an allOf composition. The
// TypeScript side renders allOf as an intersection (schemaToTSType joins
// members with " & "), and a JSON value satisfying an intersection type is
// one flat object carrying every member's fields at once -- there is no
// wrapper or tag distinguishing which member contributed which field. That
// is exactly what the 'object' kind already models (a flat field map plus
// which of them are required), so this reuses it rather than adding a new
// `kind` that would need its own, functionally identical, runtime case.
//
// A field declared by more than one layer resolves last-declared-wins:
// allOf is conventionally read as "base type, then extension", so a field
// the extension redeclares is meant to override the base's version of it.
// When two layers declare the SAME wire name with DIFFERENT effective
// codecs -- a real shape conflict, not just harmless duplication -- a
// warning names the schema and field: the TypeScript type is the
// intersection of both members, so a conforming value can carry both
// members' nested field sets, and silently keeping only the last member's
// codec would leave the discarded member's nested fields unrenamed under a
// type that claims otherwise. This is deliberately a warning, not an
// attempt to merge the two nested codecs: two conflicting shapes for one
// field name have no single well-defined merged codec in general (they may
// not even be structurally compatible), so surfacing the ambiguity is the
// honest choice, the same one an undiscriminated union's ambiguity gets.
//
// Required is the UNION of every layer's required list, not an
// intersection: satisfying allOf means satisfying every member
// simultaneously, so a field required by any one of them is required on the
// composed value.
//
// Known residual limitation: conflict detection compares the STRING
// codecIDFor returns for each layer's declaration of a field, which
// correctly distinguishes two different $ref codecs (the common case, and
// the one this warns about) but cannot distinguish two different INLINE
// sub-schemas for the same field name -- codecIDFor synthesizes an inline
// property's id purely from "<id>.<prop>" (parentID and property name),
// with no dependence on which layer or which schema shape produced it, so
// two conflicting inline schemas at the same field name produce the SAME
// id string and are silently indistinguishable here. Closing this fully
// would mean giving codecIDFor a per-layer-aware synthetic id scheme for
// this one call site, which is not needed by any case this task's review
// actually measured ($ref-vs-$ref conflicts).
//
// If NO layer contributes any fields at all -- every member is an
// unresolvable $ref, or the composition is genuinely empty -- the entry
// degrades to passthrough rather than an `object` with no `fields`. An
// empty `fields` map marshals to no `fields` key at all (it's
// `omitempty`), but the emitted `Codec` type declares `fields` required for
// `kind: 'object'`; tsc would reject the generated file, and at runtime
// `Object.entries(codec.fields)` would throw on `undefined`. Passthrough is
// the safe, honest degradation: an unresolvable composition can't be
// walked, so the table must not claim it can.
func (t *codecTable) allOfEntry(id string, schema *client.Schema, spec *client.APISpec) codecEntry {
	layers := flattenAllOfLayers(schema, spec, map[*client.Schema]bool{})

	fields := map[string]codecField{}
	fieldCodec := map[string]string{}
	conflicts := map[string]bool{}
	var required []string

	for _, layer := range layers {
		for _, prop := range sortedKeys(layer.Properties) {
			codecID := t.codecIDFor(id, prop, layer.Properties[prop], spec)

			if prev, ok := fieldCodec[prop]; ok && prev != codecID {
				conflicts[prop] = true
			}

			fieldCodec[prop] = codecID
			fields[prop] = codecField{TS: prop, Codec: codecID}
		}

		required = append(required, layer.Required...)
	}

	if len(conflicts) > 0 {
		names := make([]string, 0, len(conflicts))
		for name := range conflicts {
			names = append(names, name)
		}

		sort.Strings(names)

		t.warnings = append(t.warnings, fmt.Sprintf(
			"schema %q: allOf members declare field(s) %s with different shapes; the last declared member's shape wins and earlier members' nested fields for that name will not be renamed",
			id, strings.Join(names, ", ")))
	}

	if len(fields) == 0 {
		return codecEntry{Kind: "passthrough"}
	}

	return codecEntry{Kind: "object", Fields: fields, Required: requiredWireFields(fields, required)}
}

// requiredWireFields returns required filtered to names present in fields,
// deduplicated and sorted for determinism. Filtering guards against a
// malformed schema listing a required name that isn't one of its own
// properties; deduplication matters once a name can be required by more
// than one source (an allOf's members can each separately require the same
// field); sorting means the emitted `required` array -- and therefore the
// order a structural union match tests fields in -- never depends on the
// order `required` happened to be built in.
func requiredWireFields(fields map[string]codecField, required []string) []string {
	if len(required) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(required))
	out := make([]string, 0, len(required))

	for _, r := range required {
		if _, ok := fields[r]; !ok || seen[r] {
			continue
		}

		seen[r] = true
		out = append(out, r)
	}

	sort.Strings(out)

	return out
}

// Generate emits src/codecs.ts. The second return value lists
// generation-time warnings -- an undiscriminated union was found and will
// be resolved structurally rather than by a discriminator; one of that
// union's members offers no evidence a structural match can ever use; or
// an allOf composition has two members declaring the same wire field with
// different shapes -- returning them on this existing return path, rather
// than adding a logger dependency or a package-level global, is what keeps
// CodecGenerator a pure function callers (and tests) can call directly with
// no setup. The top-level Generator.Generate (generator.go) forwards these
// onto GeneratedClient.Warnings, which is the one place a caller already
// looks for out-of-band information about a generation run. Warnings are
// sorted before being returned, so their order is deterministic regardless
// of the schema walk's recursion shape.
func (g *CodecGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) (string, []string) {
	table := &codecTable{entries: map[string]codecEntry{}}

	for _, name := range sortedKeys(spec.Schemas) {
		table.add(name, spec, spec.Schemas[name])
	}

	sort.Strings(table.warnings)

	var buf strings.Builder

	buf.WriteString("// Generated codec table\n")
	buf.WriteString("//\n")
	buf.WriteString("// Describes, per schema, how a wire payload maps onto its TypeScript\n")
	buf.WriteString("// shape. `ts` currently equals the wire name for every field — property\n")
	buf.WriteString("// renaming lands in a later phase — so encode/decode are effectively\n")
	buf.WriteString("// identity today. The table and the walk exist now so that turning\n")
	buf.WriteString("// renaming on is a change of names, not a change of machinery.\n\n")

	buf.WriteString("export type Codec =\n")
	buf.WriteString("  | { kind: 'object'; fields: Record<string, { ts: string; codec?: string }>; required?: string[] }\n")
	buf.WriteString("  | { kind: 'array'; items?: string }\n")
	buf.WriteString("  | { kind: 'record'; values?: string }\n")
	buf.WriteString("  | { kind: 'union'; discriminator?: { wire: string; map: Record<string, string> }; members: string[] }\n")
	buf.WriteString("  | { kind: 'passthrough' };\n\n")

	buf.WriteString("export const CODECS: Record<string, Codec> = {\n")

	for _, id := range sortedCodecIDs(table.entries) {
		encoded, err := json.Marshal(table.entries[id])
		if err != nil {
			// codecEntry contains only strings, maps and slices of strings,
			// none of which can fail to marshal. Emitting a passthrough is
			// the safe degradation if that ever changes.
			encoded = []byte(`{"kind":"passthrough"}`)
		}

		key, err := json.Marshal(id)
		if err != nil {
			continue
		}

		fmt.Fprintf(&buf, "  %s: %s,\n", key, spacedJSON(string(encoded)))
	}

	buf.WriteString("};\n\n")

	buf.WriteString(codecRuntime)

	return buf.String(), table.warnings
}

// sortedCodecIDs orders table keys deterministically. Synthetic ids contain
// a dot and named ones do not, but they share one namespace and one sort —
// splitting them would only make the emitted order harder to predict.
func sortedCodecIDs(entries map[string]codecEntry) []string {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	return ids
}

// spacedJSON widens encoding/json's compact output into the `{"kind": "object"}`
// spacing the rest of the generated TypeScript uses. It only touches the
// separator between a key and its value, so string CONTENTS are untouched —
// a property name containing `":` is not a real risk here because
// json.Marshal has already escaped it, but the substitution is deliberately
// narrow for that reason.
func spacedJSON(s string) string {
	// `,"` cannot occur inside a marshalled string: json.Marshal escapes an
	// embedded quote to `\"`, so the byte before the quote is a backslash,
	// not a comma. The key/value separator substitution is safe for the same
	// reason.
	s = strings.ReplaceAll(s, `":`, `": `)

	return strings.ReplaceAll(s, `,"`, `, "`)
}

// codecRuntime is the emitted encode/decode implementation. The rules it
// enforces are load-bearing:
//
//   - unknown keys pass through verbatim, so a server that adds a field does
//     not have it silently dropped by an older client;
//   - `record` renames its VALUES but never its KEYS, because a record's keys
//     are data (user-chosen ids), not schema-defined field names;
//   - a union WITH a discriminator resolves by its tag, exactly as before;
//   - a union WITHOUT one tries each declared member in order, structurally:
//     the first whose required wire fields are all present on the value
//     wins. No match falls back to passthrough -- never a best-effort guess,
//     because guessing could rename fields based on a match that is wrong.
const codecRuntime = `function codecFor(id?: string): Codec | undefined {
  return id ? CODECS[id] : undefined;
}

function walk(value: unknown, id: string | undefined, toTS: boolean): unknown {
  const codec = codecFor(id);
  if (!codec || value === null || value === undefined) {
    return value;
  }

  switch (codec.kind) {
    case 'object': {
      if (typeof value !== 'object' || Array.isArray(value)) {
        return value;
      }

      const src = value as Record<string, unknown>;
      const out: Record<string, unknown> = {};

      // Build the rename map in the requested direction. Decoding maps a
      // wire key to its ts name; encoding maps back.
      const rename = new Map<string, { to: string; codec?: string }>();
      for (const [wire, field] of Object.entries(codec.fields)) {
        if (toTS) {
          rename.set(wire, { to: field.ts, codec: field.codec });
        } else {
          rename.set(field.ts, { to: wire, codec: field.codec });
        }
      }

      for (const [key, val] of Object.entries(src)) {
        const mapped = rename.get(key);
        if (mapped) {
          out[mapped.to] = walk(val, mapped.codec, toTS);
        } else {
          // Unknown key: pass through verbatim, name and value untouched.
          out[key] = val;
        }
      }

      return out;
    }

    case 'array': {
      if (!Array.isArray(value)) {
        return value;
      }

      return value.map((item) => walk(item, codec.items, toTS));
    }

    case 'record': {
      if (typeof value !== 'object' || Array.isArray(value)) {
        return value;
      }

      const src = value as Record<string, unknown>;
      const out: Record<string, unknown> = {};

      // Keys are data here, not field names — they are never renamed.
      for (const [key, val] of Object.entries(src)) {
        out[key] = walk(val, codec.values, toTS);
      }

      return out;
    }

    case 'union': {
      if (typeof value !== 'object' || Array.isArray(value)) {
        return value;
      }

      const src = value as Record<string, unknown>;

      if (codec.discriminator) {
        const tag = src[codec.discriminator.wire];
        if (typeof tag !== 'string') {
          return value;
        }

        const memberID = codec.discriminator.map[tag];
        if (!memberID) {
          return value;
        }

        return walk(value, memberID, toTS);
      }

      // No discriminator: try each member in the order it was declared,
      // taking the first whose required wire fields are ALL present on the
      // value. A member that is evidence-free -- not an 'object' at all, or
      // an object with no required fields -- is SKIPPED, not treated as a
      // vacuous match: an empty required list is satisfied by every object
      // trivially, which would turn that member into an unconditional
      // catch-all rather than a real structural test, defeating "never a
      // best-effort guess" for the common case where a schema simply has no
      // required fields declared (the OpenAPI default). If every member is
      // skipped, fall through to the same passthrough a genuine mismatch
      // gets -- generation-time warns when a member has no evidence to
      // offer (see unionEntry), so this is never a silent surprise.
      for (const memberID of codec.members ?? []) {
        const member = codecFor(memberID);
        const required = member && member.kind === 'object' ? member.required : undefined;
        if (!required || required.length === 0) {
          continue;
        }

        if (required.every((field) => Object.prototype.hasOwnProperty.call(src, field))) {
          return walk(value, memberID, toTS);
        }
      }

      // No member matched: pass through verbatim rather than guess.
      return value;
    }

    default:
      return value;
  }
}

/** Convert a wire payload into its TypeScript shape. */
export function decode<T>(value: unknown, codecID?: string): T {
  return walk(value, codecID, true) as T;
}

/** Convert a TypeScript value into its wire shape. */
export function encode(value: unknown, codecID?: string): unknown {
  return walk(value, codecID, false);
}
`
