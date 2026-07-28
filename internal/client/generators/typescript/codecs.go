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
// Each field's `ts` name is derived from its wire name via tsFieldName --
// the same function generator.go's objectPropsLiteral/schemaToTSType use to
// render property keys, and fieldname.go's collision guard uses to detect a
// clash before generation ever reaches this table -- so all three agree on
// what a field is called and encode/decode are real renames, not identity.
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

	// record, AND an "object" entry for a schema that declares BOTH
	// Properties and additionalProperties (see codecTable.add's Properties
	// case): decode/encode's 'object' runtime case renames Fields as usual
	// and, for any key not in Fields, walks its VALUE through Values instead
	// of leaving it untouched -- matching the rendered intersection type
	// (objectPropsLiteral & Record<string, valueType>), which promises that
	// same value schema's fields are renamed too.
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

	// config is consulted for every field's client-side (`ts`) name, via
	// tsFieldName, keyed by the SAME namespace id (see codecIDFor's doc
	// comment) that fieldname.go's collision guard and generator.go's
	// objectPropsLiteral/schemaToTSType also key their own tsFieldName calls
	// by. All three must agree on that id -- otherwise a FieldOverrides
	// entry that silences a collision error at generation time would not
	// apply to this table, silently emitting an encode/decode pair that
	// still drops data instead of renaming it.
	config client.GeneratorConfig

	// warnings accumulates generation-time messages that don't abort
	// generation but are worth surfacing -- currently just "this union has
	// no discriminator". Sorted before being handed back to the caller (see
	// CodecGenerator.Generate) so callers get a stable order regardless of
	// the recursion shape that produced them.
	warnings []string

	// building tracks ids currently on the call stack inside add(), i.e.
	// reserved (see add's "reserve before recursing" comment) but not yet
	// assigned their real entry. This exists purely so unionEntry's
	// evidence-free-member warning can tell "this member is still being
	// built, one call frame up, because of a reference cycle" apart from
	// "this member really is passthrough" -- both look identical if you
	// only look at t.entries[id].Kind, since add() reserves a placeholder
	// {Kind: "passthrough"} before it knows what the schema actually is.
	// Without this, e.g. UA: oneOf[$ref UB], UB: oneOf[$ref UA] reports
	// UB's warning (checked from inside building UA) as if UB resolved to
	// kind "passthrough", when it is actually mid-construction as a union.
	building map[string]bool
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

	if t.building == nil {
		t.building = map[string]bool{}
	}

	t.building[id] = true
	defer delete(t.building, id)

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
				TS:    tsFieldName(id, prop, t.config),
				Codec: t.codecIDFor(id, prop, schema.Properties[prop], spec),
			}
		}

		entry := codecEntry{Kind: "object", Fields: fields, Required: requiredWireFields(fields, schema.Required)}

		// A schema can declare BOTH Properties and additionalProperties --
		// schemaToTSType/schemaToTypeScript render this as an intersection
		// (objectPropsLiteral & Record<string, valueType>), and generator.go
		// now renames properties INSIDE valueType too (via nsID+".values").
		// Falling through to the additionalProperties-only branch below
		// never runs for this shape (this `case` already returns), so
		// without this, such a schema's `.values` codec entry never got
		// registered at all: a declared-and-renamed value schema with no
		// codec id to walk it by, silently identity for every "additional"
		// key's value even though the emitted TYPE promises renamed fields.
		// Recording Values here, on the SAME "object" entry, is enough --
		// no new `kind` is needed, since decode/encode's 'object' case
		// (codecRuntime) already renames declared fields and can fall back
		// to `values` for anything left over.
		if values, ok := additionalPropsSchema(schema.AdditionalProperties); ok {
			entry.Values = t.codecIDFor(id, "values", values, spec)
		}

		t.entries[id] = entry

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

			// t.building[memberID] means memberID is reserved but not yet
			// assigned its real entry -- a call frame further up this same
			// stack is still building it (a reference cycle, e.g.
			// UA: oneOf[$ref UB], UB: oneOf[$ref UA]). t.entries[memberID]
			// would report the RESERVED placeholder {Kind: "passthrough"}
			// in that case, which looks identical to a genuinely
			// evidence-free passthrough member -- naming it accurately
			// here avoids a misleading "kind \"passthrough\"" for a member
			// that is actually mid-construction as something else entirely.
			kind := "undefined"

			switch {
			case t.building[memberID]:
				kind = "unknown (cyclic reference back to a schema still being built)"
			case ok:
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

// allOfLayer is one contributing layer flattenAllOfLayers resolves an allOf
// composition down to: the schema that directly owns the properties, plus
// the namespace id those properties must be keyed under for tsFieldName
// (and codecIDFor's synthetic-id derivation for any of the layer's own
// nested composites) to agree with what actually renders.
//
// nsID is "" for an INLINE layer -- one reached without crossing a $ref at
// all -- meaning its properties render as part of the allOf composition's
// own intersection member (objectPropsLiteral called with the
// composition's own id), so callers must substitute the composition's own
// id for an empty nsID. nsID is the resolved schema NAME for a layer
// reached via one or more $ref hops (the most immediate one before
// properties were found -- see the "label" parameter below): that layer's
// properties do NOT render as part of the composition at all --
// schemaToTSType's AllOf case returns a $ref member's bare type name
// without recursing into it (generator.go) -- they render under the ref
// target's OWN top-level `export interface`/`export type`, so that target
// name is the only namespace id whose FieldOverrides entries, or whose
// codec-table entry, the rendered output will ever actually consult.
// Using the composition's id for such a layer -- the behaviour before this
// fix -- let a printed FieldOverrides key silence the collision guard
// while having no effect on the rendered type at all (see allOfEntry and
// checkFlattenedAllOfCollisions for where nsID is consumed).
type allOfLayer struct {
	schema *client.Schema
	nsID   string
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
// A member that is ITSELF a union (oneOf/anyOf, with no Properties of its
// own) is a different failure shape from a dangling ref: it resolves to
// something real, just something with no single fixed set of properties --
// which alternative applies depends on the runtime value, not the schema
// alone, so there is genuinely nothing here to merge in without guessing.
// This is reported via the second return value (a label per such member --
// the $ref name if there is one, "an inline member" otherwise) rather than
// silently contributing zero layers the way a dangling ref does: unlike a
// dangling ref, this member's fields DO appear in the rendered TypeScript
// intersection type (schemaToTSType has no trouble rendering a union member
// of an allOf), so silently dropping it from the codec table would be the
// exact same lying-type failure Critical 1 was about, just non-empty and
// therefore invisible to the empty-fields safety net. allOfEntry turns
// these labels into a warning rather than guessing which alternative's
// shape to merge in.
//
// visited guards a schema-graph cycle -- through a $ref cycle (A allOf B,
// B allOf A) or a hand-built Go pointer cycle -- by tracking schema
// pointers already on the current resolution path; re-reaching one
// contributes no further layers rather than recursing forever. Because a
// named $ref always resolves to the SAME *client.Schema pointer from
// spec.Schemas, tracking pointers alone catches both cycle shapes with one
// mechanism.
//
// label carries "how did we get here" for two purposes: the union-member
// warning above (the $ref name that led to this schema, or "" for an
// inline schema reached directly from an AllOf slice, rendered as "an
// inline member" in the warning), and -- doubling as allOfLayer.nsID -- the
// namespace id a contributing layer's properties must be keyed under (see
// allOfLayer's doc comment). It is reset to "" for every AllOf member
// recursed into below, then set to the resolved name whenever a $ref hop
// is followed, so a schema found ANY number of hops deep is still
// attributed to the $ref (if any) that most immediately led to it -- the
// exact $ref name whose OWN top-level rendering will actually own these
// properties.
func flattenAllOfLayers(schema *client.Schema, label string, spec *client.APISpec, visited map[*client.Schema]bool) (layers []allOfLayer, polymorphicMembers []string) {
	if schema == nil || visited[schema] {
		return nil, nil
	}

	visited[schema] = true
	defer delete(visited, schema)

	if name := refName(schema.Ref); name != "" {
		// spec.Schemas[name] is nil for a dangling ref; the nil check above
		// turns that into "no layers" on the next call, not a crash.
		return flattenAllOfLayers(spec.Schemas[name], name, spec, visited)
	}

	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		desc := label
		if desc == "" {
			desc = "an inline member"
		}

		polymorphicMembers = append(polymorphicMembers, desc)
	}

	for _, member := range schema.AllOf {
		subLayers, subPolymorphic := flattenAllOfLayers(member, "", spec, visited)
		layers = append(layers, subLayers...)
		polymorphicMembers = append(polymorphicMembers, subPolymorphic...)
	}

	if len(schema.Properties) > 0 {
		layers = append(layers, allOfLayer{schema: schema, nsID: label})
	}

	return layers, polymorphicMembers
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
// A field declared by more than one layer resolves last-declared-wins WHEN
// the two layers' declarations resolve to DIFFERENT effective codecs -- the
// common case, and the only one conflict detection below can see. allOf is
// conventionally read as "base type, then extension", so a field the
// extension redeclares is meant to override the base's version of it, and
// a warning names the schema and field: the TypeScript type is the
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
// Known residual limitation, and where "last-declared-wins" above is
// actually WRONG: conflict detection compares the STRING codecIDFor
// returns for each layer's declaration of a field. For two $ref layers (or
// one $ref, one inline) declaring different shapes, that string genuinely
// differs per layer, so both the conflict warning AND the final winner are
// correct and last-declared-wins holds. For two INLINE sub-schemas at the
// SAME field name, codecIDFor synthesizes the id purely from "<id>.<prop>"
// (parentID and property name), with NO dependence on which layer or which
// schema shape produced it -- so both layers compute the identical id
// string, the conflict goes UNDETECTED (no warning), and because t.add
// no-ops once that id is already registered, the FIRST layer to register it
// wins, not the last. Fully unifying the two directions would mean giving
// codecIDFor a per-layer-aware synthetic id scheme for this one call site,
// which is a larger change than the case actually measured ($ref-vs-$ref
// conflicts, where the existing behavior is already correct) justifies.
//
// A member that is itself a union (oneOf/anyOf) contributes no layer at
// all -- flattenAllOfLayers has nothing to merge in from it, by design (see
// its own doc comment) -- and is reported via a SEPARATE warning naming the
// schema and the union member, since its fields still appear in the
// rendered TypeScript intersection type even though the codec table cannot
// represent them: silently dropping them would be Critical 1's lying-type
// failure again, just non-empty and therefore invisible to the
// empty-fields safety net below.
//
// That warning also states, explicitly, a scope limitation rather than
// attempting to close it: checkFlattenedAllOfCollisions (fieldname.go)
// cannot see through a union member's own alternatives to detect a
// collision between one alternative's wire name and a sibling allOf
// member's renamed field (e.g. allOf[{street_name}, $ref Poly] where
// Poly = oneOf[{streetName}] -- decoding could write the renamed
// "streetName" from street_name, then pass the wire key "streetName"
// (Poly's own alternative, present on the same value) through unrenamed
// into the SAME target key, one clobbering the other). Extending detection
// into a union's alternatives was considered and rejected for now: doing
// it soundly requires evaluating each alternative under ITS OWN eventual
// codec id (e.g. "Poly.oneOf0", not this allOf's id) to print a
// FieldOverrides key that would actually resolve anything, and avoiding
// false positives between two alternatives of the SAME union that can
// never be present simultaneously (they are mutually exclusive, not
// additive, unlike allOf's own members) -- getting that wrong would repeat
// the exact "prints a key that doesn't work" failure this whole area of
// the guard exists to eliminate. An explicit, named limitation is safer
// than a partially-correct extension.
//
// If NO layer contributes any fields at all -- every member is an
// unresolvable $ref, every member is itself a union, or the composition is
// genuinely empty -- the entry degrades to passthrough rather than an
// `object` with no `fields`. An empty `fields` map marshals to no `fields`
// key at all (it's `omitempty`), but the emitted `Codec` type declares
// `fields` required for `kind: 'object'`; tsc would reject the generated
// file, and at runtime `Object.entries(codec.fields)` would throw on
// `undefined`. Passthrough is the safe, honest degradation: an
// unresolvable composition can't be walked, so the table must not claim it
// can.
//
// Each layer's `ts` (and, for a nested composite property, the further
// synthetic id it recurses under) is derived using THAT layer's own nsID
// (see allOfLayer's doc comment), not unconditionally `id`: a field
// contributed by a $ref member renders under the ref target's own
// top-level export, so its FieldOverrides key and its codec-table
// namespace are the ref target's name, never this composition's. Getting
// this wrong (using `id` for every layer, the behaviour before this fix)
// let a FieldOverrides entry the collision guard printed apply to this
// table's entry while having zero effect on the actually-rendered type --
// exactly the "prints a key that doesn't work" failure this whole area of
// the guard exists to eliminate, reintroduced on the renderer side.
func (t *codecTable) allOfEntry(id string, schema *client.Schema, spec *client.APISpec) codecEntry {
	layers, polymorphicMembers := flattenAllOfLayers(schema, "", spec, map[*client.Schema]bool{})

	if len(polymorphicMembers) > 0 {
		names := make([]string, len(polymorphicMembers))
		copy(names, polymorphicMembers)
		sort.Strings(names)

		quoted := make([]string, len(names))
		for i, name := range names {
			quoted[i] = fmt.Sprintf("%q", name)
		}

		t.warnings = append(t.warnings, fmt.Sprintf(
			"schema %q: allOf member(s) %s are themselves unions (oneOf/anyOf) and cannot be statically flattened; their fields will not appear in this composition's codec and will not be renamed. "+
				"The field-name-collision guard cannot see through a union member's own alternatives either: a wire name declared by one of these alternatives that would collide with another member's renamed field once renaming lands is NOT detected by that guard -- review this composition manually before enabling renaming",
			id, strings.Join(quoted, ", ")))
	}

	fields := map[string]codecField{}
	fieldCodec := map[string]string{}
	conflicts := map[string]bool{}
	var required []string

	for _, layer := range layers {
		// An inline layer (nsID == "") renders as part of this composition's
		// own intersection member, so it is keyed under the composition's
		// own id. A layer reached via a $ref (nsID != "") renders under
		// that ref target's own top-level namespace instead -- see
		// allOfLayer's doc comment for why using `id` unconditionally here
		// (the pre-fix behaviour) produced a FieldOverrides key the
		// rendered type never actually consults.
		layerNSID := layer.nsID
		if layerNSID == "" {
			layerNSID = id
		}

		for _, prop := range sortedKeys(layer.schema.Properties) {
			codecID := t.codecIDFor(layerNSID, prop, layer.schema.Properties[prop], spec)

			if prev, ok := fieldCodec[prop]; ok && prev != codecID {
				conflicts[prop] = true
			}

			fieldCodec[prop] = codecID
			fields[prop] = codecField{TS: tsFieldName(layerNSID, prop, t.config), Codec: codecID}
		}

		required = append(required, layer.schema.Required...)
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
	table := &codecTable{entries: map[string]codecEntry{}, config: config}

	for _, name := range sortedKeys(spec.Schemas) {
		table.add(name, spec, spec.Schemas[name])
	}

	sort.Strings(table.warnings)

	var buf strings.Builder

	buf.WriteString("// Generated codec table\n")
	buf.WriteString("//\n")
	buf.WriteString("// Describes, per schema, how a wire payload maps onto its TypeScript\n")
	buf.WriteString("// shape. `ts` is the client-side field name derived from the wire name by\n")
	buf.WriteString("// the configured FieldNaming strategy (or a FieldOverrides entry); encode\n")
	buf.WriteString("// and decode below walk this table to rename between the two.\n\n")

	buf.WriteString("export type Codec =\n")
	buf.WriteString("  | { kind: 'object'; fields: Record<string, { ts: string; codec?: string }>; required?: string[]; values?: string }\n")
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

		// keyText guards a schema (or synthetic) id literally named
		// "__proto__" directly, since `id` is already a plain Go string here
		// -- no substring surgery needed for this side. protectDunderProto
		// below handles the other side: a `fields`/discriminator-`map` key
		// of the same name, buried inside the already-marshalled entry text.
		keyText := string(key)
		if id == "__proto__" {
			keyText = `["__proto__"]`
		}

		fmt.Fprintf(&buf, "  %s: %s,\n", keyText, protectDunderProto(spacedJSON(string(encoded))))
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

// protectDunderProto rewrites a plain `"__proto__": value` object-literal
// key -- wherever it appears in s, whether the top-level CODECS id, a
// `fields` key, or a discriminator `map` key -- into computed-key syntax,
// `["__proto__"]: value`.
//
// This is not cosmetic: `{ "__proto__": x }` written as a JS object LITERAL
// sets the object's PROTOTYPE rather than an own property (a special case
// in the language spec that applies only to a non-computed literal
// PropertyName, not to bracket/computed syntax). codecRuntime's rename map
// is built with `Object.entries(codec.fields)`, which only sees OWN
// enumerable properties -- so a wire field literally named "__proto__"
// would silently vanish from that map, and decode/encode would leave it
// completely untouched (passed through as an unrecognised key) even though
// the codec table claims it has a `ts` mapping for it.
//
// The replacement is safe as a blind substring search: json.Marshal has
// already escaped every key, so the exact 13-byte sequence `"__proto__":`
// (opening quote, "__proto__", closing quote, colon) can only occur when a
// JSON key is EXACTLY "__proto__" -- a longer key like "myproto__proto__x"
// marshals with its own quotes wrapping the WHOLE key
// (`"myproto__proto__x":`), never producing an embedded, separately-quoted
// `"__proto__"` substring. The pattern also cannot match inside a VALUE:
// object-position `"__proto__"` is always immediately followed by `:`;
// value-position `"__proto__"` (e.g. a `ts` that happens to equal the
// string "__proto__") is followed by `,` or `}`, never `:`.
func protectDunderProto(s string) string {
	return strings.ReplaceAll(s, `"__proto__":`, `["__proto__"]:`)
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
//     because guessing could rename fields based on a match that is wrong;
//   - every key written onto a walked result goes through setOwn
//     (Object.defineProperty), not bracket/dot assignment, so a field
//     literally named "__proto__" (wire or client name) becomes a real own
//     property instead of silently reassigning the result's prototype via
//     the legacy Object.prototype.__proto__ accessor.
const codecRuntime = `function codecFor(id?: string): Codec | undefined {
  return id ? CODECS[id] : undefined;
}

// setOwn assigns obj[key] = value via Object.defineProperty rather than
// bracket/dot assignment. This matters for exactly one key: "__proto__".
// obj[key] = value, when key is the literal string "__proto__", goes
// through the legacy Object.prototype.__proto__ ACCESSOR -- it sets obj's
// PROTOTYPE (silently ignoring the assignment entirely if value is not an
// object or null) instead of creating an own data property. A wire (or,
// under NamingPreserve/an override, client) field literally named
// "__proto__" would otherwise silently vanish from the walked result: not
// an error, just an object one property short of what the codec table
// claims it renamed. Object.defineProperty has no such special case for any
// key, "__proto__" included, so it is used unconditionally here rather than
// only when key happens to be "__proto__" -- one code path, not two.
function setOwn(obj: Record<string, unknown>, key: string, value: unknown): void {
  Object.defineProperty(obj, key, { value, writable: true, enumerable: true, configurable: true });
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
          setOwn(out, mapped.to, walk(val, mapped.codec, toTS));
        } else if (codec.values) {
          // Not a declared field, but this object also has a typed
          // additionalProperties value schema (a declared-Properties-AND-
          // additionalProperties schema, rendered as an intersection type):
          // the KEY is data and stays untouched, exactly like the 'record'
          // case below, but the VALUE still needs walking so its own
          // fields get renamed too.
          setOwn(out, key, walk(val, codec.values, toTS));
        } else {
          // Unknown key: pass through verbatim, name and value untouched.
          setOwn(out, key, val);
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
        setOwn(out, key, walk(val, codec.values, toTS));
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
