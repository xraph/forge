package dart

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// Ported from typescript/codecs.go (the table builder only, not the
// TypeScript renderer) with clientFieldName renamed clientFieldName and the
// codecField.TS field renamed Client. The Dart renderer is codecs.go.

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
	Client string `json:"client"`
	Codec  string `json:"codec,omitempty"`
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
	// clientFieldName, keyed by the SAME namespace id (see codecIDFor's doc
	// comment) that fieldname.go's collision guard and generator.go's
	// objectPropsLiteral/schemaToTSType also key their own clientFieldName calls
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
//
// Delegated to the client package so this generator and the reachability
// pruning that decides which schemas reach it cannot disagree about what a
// pointer names.
func refName(ref string) string {
	return client.ComponentRefName(ref)
}

// arrayRefCodecID returns the synthetic CODECS table id for an endpoint
// request/response body of the shape `{type: array, items: $ref X}` -- the
// single most common OpenAPI "list of X" wire shape, and (fix-round-1 review,
// IMPORTANT 1) one that previously got no codec on either side: rest.go's
// schemaCodecRef only ever accepted a DIRECT top-level $ref, so an array
// wrapping one fell through to "" on both the request-body and response
// paths, leaving every list endpoint's declared `types.X[]` camelCase type a
// lie over a wire-cased runtime payload.
//
// This is deliberately NOT derived via codecIDFor/codecTable.add's usual
// "<parentID>.<prop>" scheme: an endpoint has no schema id of its own to key
// that scheme under, and reusing codecIDFor here would also start
// registering (and silently codec'ing) other endpoint-boundary shapes --
// inline objects, oneOf/anyOf, allOf -- that rest.go's
// requestBodyCodecRef/responseCodecRef deliberately warn about and skip
// rather than guess at (see IMPORTANT 2 in the same review). Keeping this
// narrowly scoped to "array wrapping a direct $ref" is what lets
// registerEndpointArrayBodyCodecs register exactly this one additional shape
// without silently widening what gets codec'd.
//
// "[]" is not a valid OpenAPI component schema name (schema names are
// identifiers), so prefixing it here cannot collide with a real named
// schema's own top-level entry.
func arrayRefCodecID(itemName string) string {
	return "[]" + itemName
}

// registerEndpointArrayBodyCodecs registers a synthetic array-of-$ref codec
// entry (arrayRefCodecID) for every endpoint request/response body of the
// shape `{type: array, items: $ref X}`. Endpoint bodies are not schemas --
// CodecGenerator.Generate's main loop only ever walks spec.Schemas -- so
// without this, an endpoint whose JSON body or response is a bare array
// wrapping a named schema has no codec-table entry reachable by rest.go's
// schemaCodecRef, even though the table already has everything an "array"
// kind entry needs (see codecTable.add's own `array` case, which this
// mirrors for the one shape endpoints can carry that a schema property
// walk never reaches).
//
// Idempotent via t.entries' existing "already seen" guard: two different
// endpoints referencing the same item schema (e.g. one endpoint returning
// `User[]`, another accepting `User[]` as a body) register the identical
// entry, harmlessly, regardless of which is walked first -- there is nothing
// endpoint-specific in the registered entry itself, only in the id used to
// look it up.
func registerEndpointArrayBodyCodecs(table *codecTable, spec *client.APISpec) {
	register := func(schema *client.Schema) {
		if schema == nil || schema.Type != "array" || schema.Items == nil {
			return
		}

		itemName := refName(schema.Items.Ref)
		if itemName == "" {
			return
		}

		id := arrayRefCodecID(itemName)
		if _, seen := table.entries[id]; seen {
			return
		}

		table.entries[id] = codecEntry{Kind: "array", Items: itemName}
	}

	for i := range spec.Endpoints {
		endpoint := &spec.Endpoints[i]

		if endpoint.RequestBody != nil {
			if media, ok := endpoint.RequestBody.Content["application/json"]; ok && media != nil {
				register(media.Schema)
			}
		}

		for _, resp := range endpoint.Responses {
			if resp == nil {
				continue
			}

			if media, ok := resp.Content["application/json"]; ok && media != nil {
				register(media.Schema)
			}
		}
	}

	// WebSocket/SSE/WebTransport message, event, and datagram schemas are
	// endpoint-boundary shapes exactly like the REST bodies/responses above --
	// not walked by CodecGenerator.Generate's spec.Schemas loop either -- so
	// without this, an array-of-$ref WS/SSE/WebTransport schema (e.g.
	// `{type: array, items: $ref User}`) leaves messageCodecRef
	// (websocket.go), sseEventCodecRef (sse.go), and wtCodecRef
	// (webtransport.go) resolving "[]User" via schemaCodecRef -- which
	// legitimately recognises the shape -- against a codec table that never
	// registered it. That produced no warning at all (schemaCodecRef isn't
	// wrong, only silent about what's actually in the table), and
	// decode()/encode() found nothing under "[]User" and passed the payload
	// through completely unrenamed. Registering here, the same narrow shape
	// as the REST loop above, is what makes rest.go's schemaCodecRef and this
	// table agree for streaming endpoints too -- the alternative (rejecting
	// the array shape in messageCodecRef/sseEventCodecRef/wtCodecRef and
	// warning instead) would leave every such WS/SSE/WebTransport endpoint
	// silently un-renamed at runtime, which is a strictly worse outcome for a
	// shape this common ("list of X" over a stream) than simply registering
	// it like every other array-of-$ref boundary already is.
	for i := range spec.WebSockets {
		ws := &spec.WebSockets[i]
		register(ws.SendSchema)
		register(ws.ReceiveSchema)
	}

	for i := range spec.SSEs {
		sse := &spec.SSEs[i]
		for _, name := range sortedKeys(sse.EventSchemas) {
			register(sse.EventSchemas[name])
		}
	}

	for i := range spec.WebTransports {
		wt := &spec.WebTransports[i]

		if wt.BiStreamSchema != nil {
			register(wt.BiStreamSchema.SendSchema)
			register(wt.BiStreamSchema.ReceiveSchema)
		}

		if wt.UniStreamSchema != nil {
			register(wt.UniStreamSchema.SendSchema)
			register(wt.UniStreamSchema.ReceiveSchema)
		}

		register(wt.DatagramSchema)
	}
}

// additionalPropertiesSegment is the synthetic path segment used for an
// additionalProperties VALUE schema's own codec/collision namespace ("<id>."
// + additionalPropertiesSegment), by codecTable.add, checkSchemaFieldCollisions
// (fieldname.go), and generator.go's objectPropsLiteral/schemaToTSType alike
// -- all three must agree on this token, same as every other namespace id
// scheme in this package.
//
// This is deliberately NOT "values": a schema declaring BOTH Properties and
// additionalProperties can also have a DECLARED property literally named
// "values" (an ordinary, plausible property name -- e.g. a paginated list
// wrapper `{ "values": [...] }`), which derives its OWN synthetic id via the
// exact same "<id>.<prop>" scheme codecIDFor uses for every property. Before
// this constant existed, both used the literal "values" unconditionally,
// so the two would collide on the SAME synthetic id for the SAME parent --
// t.add is idempotent (a no-op once an id is already registered), so
// whichever call reached it first silently won, and additionalProperties'
// actual value schema was never registered at all: an "unknown" key's value
// would decode through the wrong codec (the declared property's schema)
// instead of its own, silently corrupting data for any wire payload with
// both a "values" key and other, genuinely-unknown keys.
//
// "additionalProperties" -- the actual OpenAPI/JSON-Schema keyword this
// represents -- is not itself immune to an equally adversarial schema
// declaring a property literally named "additionalProperties" (no fixed
// string concatenated naively into a shared namespace can be made immune to
// an arbitrary wire name reproducing it exactly, without an escaping
// scheme -- see clientFieldName's doc comment, which already accepts this same
// class of ambiguity for a dotted schema/wire name). This constant closes
// the REALISTIC collision the review measured (a property plausibly named
// "values") rather than claiming adversarial-proof uniqueness that no
// single fixed token can actually provide.
const additionalPropertiesSegment = "additionalProperties"

// codecIDFor returns the codec id a property's value should be decoded with,
// registering a synthetic entry first when the property is an inline
// composite. Primitives get "" -- there is nothing to rename inside a string
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
				Client: clientFieldName(id, prop, t.config),
				Codec:  t.codecIDFor(id, prop, schema.Properties[prop], spec),
			}
		}

		entry := codecEntry{Kind: "object", Fields: fields, Required: requiredWireFields(fields, schema.Required)}

		// A schema can declare BOTH Properties and additionalProperties --
		// schemaToTSType/schemaToTypeScript render this as an intersection
		// (objectPropsLiteral & Record<string, valueType>), and generator.go
		// now renames properties INSIDE valueType too (via
		// nsID+"."+additionalPropertiesSegment). Falling through to the
		// additionalProperties-only branch below never runs for this shape
		// (this `case` already returns), so without this, such a schema's
		// codec entry never got registered at all: a declared-and-renamed
		// value schema with no codec id to walk it by, silently identity
		// for every "additional" key's value even though the emitted TYPE
		// promises renamed fields. Recording Values here, on the SAME
		// "object" entry, is enough -- no new `kind` is needed, since
		// decode/encode's 'object' case (codecRuntime) already renames
		// declared fields and can fall back to `values` for anything left
		// over. additionalPropertiesSegment (not the literal "values") is
		// what keeps this id distinct from a DECLARED property that
		// happens to be named "values" -- see that constant's doc comment.
		if values, ok := additionalPropsSchema(schema.AdditionalProperties); ok {
			entry.Values = t.codecIDFor(id, additionalPropertiesSegment, values, spec)
		}

		t.entries[id] = entry

		return
	}

	if values, ok := additionalPropsSchema(schema.AdditionalProperties); ok {
		t.entries[id] = codecEntry{
			Kind: "record",
			// A nil values schema means `additionalProperties: true` -- the
			// values are unconstrained, so there is nothing to descend into.
			// This branch only runs when schema.Properties is empty (the
			// switch above already returned otherwise), so there is no
			// DECLARED property to collide with here -- additionalPropertiesSegment
			// is used anyway, for consistency with the combined case above
			// and with fieldname.go's guard, which checks this shape
			// unconditionally regardless of whether Properties is empty.
			Values: t.codecIDFor(id, additionalPropertiesSegment, values, spec),
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

	// Fix-round-2 review: warn when members disagree on, or entirely omit,
	// the discriminator property's rendered TS name -- see codecRuntime's
	// encode-direction discriminator handling (the 'union' case), which
	// must try every distinct name any member declares (falling back to the
	// wire name itself) precisely because this can happen, and cannot
	// always resolve it (an ambiguous or absent candidate set still passes
	// through unrenamed on encode).
	t.checkDiscriminatorNameAgreement(id, schema.Discriminator.PropertyName, memberIDs)

	return codecEntry{
		Kind: "union",
		Discriminator: &codecDiscriminator{
			Wire: schema.Discriminator.PropertyName,
			Map:  mapping,
		},
		Members: memberIDs,
	}
}

// checkDiscriminatorNameAgreement warns when a discriminated union's
// members disagree on -- or entirely omit -- the TS name their own `fields`
// table renders the discriminator's WIRE property as. codecRuntime's
// encode-direction discriminator resolution (the JS 'union' case) tries
// every DISTINCT ts name any member declares for this property (plus the
// wire name itself as a last resort) rather than guessing a single one, so
// neither case below is a hard failure -- but both are real spec smells:
//
//   - if no member declares the property at all, encode() can only ever try
//     the bare wire name, which is virtually never present on a TS-shaped
//     `src` (the property isn't even part of any member's declared
//     TypeScript type) -- an effectively un-encodable union in practice,
//     even though decode() (which reads the wire name directly against a
//     wire-shaped `src`) works fine. Strict OpenAPI requires the
//     discriminator property to be declared and required on every member;
//     this is what catches a spec that doesn't conform;
//   - if members declare it under DIFFERENT ts names (e.g. a schema-scoped
//     FieldOverrides entry naming only one of them differently),
//     codecRuntime's encode-direction resolution tries every one of those
//     names as a candidate and accepts whichever single one both exists on
//     the payload and resolves via the discriminator mapping -- correct,
//     but genuinely dependent on runtime data rather than something
//     generation time can fully verify in advance, so it's surfaced here
//     too.
//
// Only members that actually built to an 'object' kind entry are
// consulted -- a member that is itself a union, or failed to resolve at
// all, contributes no evidence either way (the same "evidence-free member"
// treatment the undiscriminated match's own warning already applies).
func (t *codecTable) checkDiscriminatorNameAgreement(id, wire string, memberIDs []string) {
	names := map[string]bool{}

	for _, memberID := range memberIDs {
		entry, ok := t.entries[memberID]
		if !ok || entry.Kind != "object" {
			continue
		}

		if field, ok := entry.Fields[wire]; ok {
			names[field.Client] = true
		}
	}

	switch len(names) {
	case 0:
		t.warnings = append(t.warnings, fmt.Sprintf(
			"schema %q: discriminator property %q is declared by NO member -- decoding still works (it reads the wire name directly against a wire-shaped payload), but encoding a value into this union can only ever try that same wire name against a TypeScript-shaped payload, which will almost never be present since the property isn't part of any member's declared type -- add %q as a required property on every member to fix this",
			id, wire, wire))
	case 1:
		// Every member that declares it agrees -- nothing to warn about.
	default:
		sorted := make([]string, 0, len(names))
		for name := range names {
			sorted = append(sorted, name)
		}
		sort.Strings(sorted)

		t.warnings = append(t.warnings, fmt.Sprintf(
			"schema %q: discriminator property %q renders under different TypeScript names across members (%s) -- encoding tries every declared name and accepts whichever one resolves, but a payload that ambiguously matches more than one is passed through unrenamed rather than guessed; give every member the same rendered name (e.g. a matching FieldOverrides entry) to remove the ambiguity",
			id, wire, strings.Join(sorted, ", ")))
	}
}

// allOfLayer is one contributing layer flattenAllOfLayers resolves an allOf
// composition down to: the schema that directly owns the properties, plus
// the namespace id those properties must be keyed under for clientFieldName
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
// allOfLayer's doc comment).
//
// It is PROPAGATED UNCHANGED into every AllOf member recursed into below
// (NOT reset to "") -- an inline member declared directly in schema.AllOf
// renders as part of whatever schema itself is currently being rendered as
// (schemaToTSType's AllOf case only ever changes nsID for a FURTHER $ref
// hop; an inline member always inherits the parent's own nsID), so its
// layer must inherit that SAME namespace, whatever schema's own label
// currently is. label is then OVERWRITTEN with the resolved name whenever a
// $ref hop is actually followed (see the refName branch below), which is
// what still correctly attributes a schema found any number of $ref hops
// deep to the nearest one, regardless of what was passed in -- so
// propagating label through the AllOf loop only ever matters for a
// genuinely inline member; a member that is itself a $ref discards whatever
// was passed to it the moment its own ref is resolved.
//
// Getting this wrong (resetting to "" unconditionally, as an earlier fix
// round did) reproduces CRITICAL 1's exact failure one level deeper: for
// Mid = allOf[$ref Leaf, inline{street_name}] reached via Addr's own
// allOf[inline{streetName}, $ref Mid], the inline "street_name" member
// nested inside Mid would incorrectly fall back to Addr's id instead of
// Mid's -- the same "prints/uses a namespace nothing renders under" defect,
// just one $ref hop further from the top-level composition.
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
		// label, not "": an inline member declared directly in schema.AllOf
		// must inherit whatever namespace schema ITSELF is currently
		// rendering under. If schema was reached via a $ref (label != ""),
		// an inline member nested inside it renders as part of THAT ref
		// target's own top-level export (e.g. Mid = allOf[$ref Leaf,
		// inline{street_name}]: when Mid is reached via another
		// composition's $ref, schemaToTSType still renders Mid's inline
		// member under Mid's own name -- schemaToTSType's AllOf case only
		// ever resets nsID for a FURTHER $ref hop, never for an inline
		// member) -- so passing "" here, unconditionally, is exactly the
		// bug this comment fixes: it made such a member fall back to the
		// OUTERMOST composition's id instead of the nearest enclosing
		// $ref's, one level short of where CRITICAL 1 fixed the direct
		// $ref-member case. If schema was NOT reached via a $ref (label ==
		// ""), member correctly still gets "" (falls back to whatever the
		// caller's own composition id is). A member that is ITSELF a $ref
		// ignores whatever label is passed to it anyway -- the refName
		// branch below immediately overwrites it with the resolved name --
		// so passing label through here is a no-op for that case and only
		// matters for a genuinely inline member.
		subLayers, subPolymorphic := flattenAllOfLayers(member, label, spec, visited)
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
			fields[prop] = codecField{Client: clientFieldName(layerNSID, prop, t.config), Codec: codecID}
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
// buildCodecTable resolves every schema into codec entries.
//
// Extracted so the table and the per-codec modules are built by one pass over
// the specification rather than two: the modules describe the same codecs the
// table lists, and two builders would be two chances for them to disagree
// about what a schema decodes to.
func buildCodecTable(spec *client.APISpec, config client.GeneratorConfig) *codecTable {
	table := &codecTable{entries: map[string]codecEntry{}, config: config}

	for _, name := range sortedKeys(spec.Schemas) {
		table.add(name, spec, spec.Schemas[name])
	}

	registerEndpointArrayBodyCodecs(table, spec)

	sort.Strings(table.warnings)

	return table
}

// sortedCodecIDs orders table keys deterministically. Synthetic ids contain
// a dot and named ones do not, but they share one namespace and one sort --
// splitting them would only make the emitted order harder to predict.
func sortedCodecIDs(entries map[string]codecEntry) []string {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	return ids
}

// additionalPropsSchema interprets Schema.AdditionalProperties, which the IR
// types as `any` because JSON Schema allows either a bool or a schema.
// Returns (valueSchema, allowed). A nil valueSchema with allowed=true means
// "any value". A nil valueSchema with allowed=false means additional
// properties are absent or explicitly disallowed -- the ordinary closed
// interface case.
//
// The IR field is populated by copying shared.Schema.AdditionalProperties
// (also `any`) straight through in both spec_parser.go and introspector.go.
// shared.Schema has no custom UnmarshalJSON for that field, so when a spec is
// parsed from a JSON/YAML document (spec_parser.go), a schema-valued
// `additionalProperties` decodes via encoding/json's generic `any` handling
// to map[string]any, not *client.Schema -- only a bool or a genuine
// *client.Schema constructed in Go (e.g. by the introspector, or by tests
// building the IR directly) take the other two branches. That map[string]any
// case is real and reachable in this codebase, but is deliberately not
// normalised into a *client.Schema here: doing so is a separate piece of
// work (re-running schema conversion on a raw map) outside this fix's scope.
func additionalPropsSchema(v any) (*client.Schema, bool) {
	switch t := v.(type) {
	case nil:
		return nil, false
	case bool:
		return nil, t
	case *client.Schema:
		return t, true
	}

	return nil, false
}
