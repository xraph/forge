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

	// object
	Fields map[string]codecField `json:"fields,omitempty"`

	// array
	Items string `json:"items,omitempty"`

	// record
	Values string `json:"values,omitempty"`

	// union
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
	case len(schema.OneOf) > 0 || len(schema.AnyOf) > 0:
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
		t.entries[id] = t.unionEntry(schema)
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

		t.entries[id] = codecEntry{Kind: "object", Fields: fields}

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

// unionEntry builds a union entry. A union WITHOUT a discriminator degrades
// to passthrough: with no tag to switch on there is no way to know which
// member's field map applies, and guessing by structural shape would rename
// fields based on a match that may be wrong.
func (t *codecTable) unionEntry(schema *client.Schema) codecEntry {
	members := schema.OneOf
	if len(members) == 0 {
		members = schema.AnyOf
	}

	memberIDs := make([]string, 0, len(members))

	for _, member := range members {
		if name := refName(member.Ref); name != "" {
			memberIDs = append(memberIDs, name)
		}
	}

	if schema.Discriminator == nil || schema.Discriminator.PropertyName == "" {
		return codecEntry{Kind: "passthrough"}
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

// Generate emits src/codecs.ts.
func (g *CodecGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	table := &codecTable{entries: map[string]codecEntry{}}

	for _, name := range sortedKeys(spec.Schemas) {
		table.add(name, spec, spec.Schemas[name])
	}

	var buf strings.Builder

	buf.WriteString("// Generated codec table\n")
	buf.WriteString("//\n")
	buf.WriteString("// Describes, per schema, how a wire payload maps onto its TypeScript\n")
	buf.WriteString("// shape. `ts` currently equals the wire name for every field — property\n")
	buf.WriteString("// renaming lands in a later phase — so encode/decode are effectively\n")
	buf.WriteString("// identity today. The table and the walk exist now so that turning\n")
	buf.WriteString("// renaming on is a change of names, not a change of machinery.\n\n")

	buf.WriteString("export type Codec =\n")
	buf.WriteString("  | { kind: 'object'; fields: Record<string, { ts: string; codec?: string }> }\n")
	buf.WriteString("  | { kind: 'array'; items?: string }\n")
	buf.WriteString("  | { kind: 'record'; values?: string }\n")
	buf.WriteString("  | { kind: 'union'; discriminator: { wire: string; map: Record<string, string> }; members: string[] }\n")
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

	return buf.String()
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

// codecRuntime is the emitted encode/decode implementation. The three rules
// it enforces are load-bearing:
//
//   - unknown keys pass through verbatim, so a server that adds a field does
//     not have it silently dropped by an older client;
//   - `record` renames its VALUES but never its KEYS, because a record's keys
//     are data (user-chosen ids), not schema-defined field names;
//   - a union with no discriminator falls back to passthrough rather than
//     guessing a member by structural shape.
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

      const tag = (value as Record<string, unknown>)[codec.discriminator.wire];
      if (typeof tag !== 'string') {
        return value;
      }

      const memberID = codec.discriminator.map[tag];
      if (!memberID) {
        return value;
      }

      return walk(value, memberID, toTS);
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
