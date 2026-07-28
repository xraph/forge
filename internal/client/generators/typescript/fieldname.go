package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// tsFieldName resolves the TypeScript-side identifier for a schema property.
//
// Resolution order:
//  1. A schema-scoped override ("SchemaName.wire_name") wins.
//  2. A bare global override ("wire_name") wins next.
//  3. Otherwise the effective naming strategy (see effectiveFieldNaming) is
//     applied to wireName.
//
// An override is used verbatim -- it bypasses the strategy entirely and is
// never case-converted, even if it happens to look like a wire name.
//
// A schema name or wire name that itself contains a "." makes the
// concatenated override key ambiguous: schemaName="User.Detail" wireName="id"
// and schemaName="User" wireName="Detail.id" both produce the key
// "User.Detail.id". This is a known, unresolved ambiguity -- OpenAPI schema
// names and JSON property names are conventionally dot-free, and the design
// doc specifies the key format literally with no escaping scheme. If this
// becomes a real collision, it needs an explicit escape/delimiter change to
// FieldOverrides' key format, not a fix here.
func tsFieldName(schemaName, wireName string, config client.GeneratorConfig) string {
	if override, ok := config.FieldOverrides[schemaName+"."+wireName]; ok {
		return override
	}

	if override, ok := config.FieldOverrides[wireName]; ok {
		return override
	}

	switch effectiveFieldNaming(config) {
	case client.NamingCamel:
		return toCamel(wireName)
	case client.NamingPascal:
		return toPascal(wireName)
	case client.NamingSnake:
		return toSnake(wireName)
	default:
		// client.NamingPreserve, and any unrecognised value (e.g. a typo'd
		// "kebab"), fall back to returning wireName unchanged. tsFieldName
		// has no error return -- the generator currently has no config
		// validation error path for this kind of field, only
		// GeneratorConfig.Validate()'s language/output-dir/package-name
		// checks -- so silently falling back is the only option that
		// doesn't mean adding a new error path this task isn't scoped to
		// wire in. Falling back to preserve, specifically, is the only
		// choice that cannot silently corrupt a name into something
		// plausible-but-wrong the way guessing camel/pascal/snake for an
		// unrecognised value could.
		return wireName
	}
}

// effectiveFieldNaming resolves config.FieldNaming's zero value (""), which
// is not one of the four NamingStrategy constants.
//
// This resolution deliberately happens here, at read time, rather than being
// baked into a config value once (e.g. inside DefaultConfig). Two things
// rule out defaulting at construction time alone:
//
//  1. Hand-built configs bypass DefaultConfig entirely. E.g.
//     cmd/forge/plugins/client.go builds `client.GeneratorConfig{Language: ...}`
//     directly, never calling DefaultConfig or NewConfig. A default only
//     written into DefaultConfig's return value would never reach that
//     caller, so its FieldNaming would stay "" -- exactly the case this task
//     requires to still behave correctly.
//  2. Even callers that do use the functional-options constructor can defeat
//     a construction-time default: NewConfig(WithLanguage("typescript")) runs
//     DefaultConfig() first (Language: "go") and applies WithLanguage after.
//     If DefaultConfig computed FieldNaming from Language at that point, it
//     would freeze in the "go" answer (preserve) before WithLanguage ever
//     runs, silently giving the wrong default for a config that ends up
//     targeting TypeScript.
//
// Resolving at read time avoids both failure modes uniformly, for every
// construction path, at the cost of config.FieldNaming (the stored zero
// value) and the effective strategy sometimes differing. Callers that need
// the effective strategy -- not just the raw field -- must call this
// function rather than reading config.FieldNaming directly.
func effectiveFieldNaming(config client.GeneratorConfig) client.NamingStrategy {
	if config.FieldNaming != "" {
		return config.FieldNaming
	}

	if config.Language == "typescript" {
		return client.NamingCamel
	}

	return client.NamingPreserve
}

// checkFieldNameCollisions reports every case where two distinct wire-name
// properties resolve, via tsFieldName, to the same client-side field name
// within the same object namespace. This must run -- and must abort
// generation -- before schema property renaming is wired into the actual
// output (a later task): two wire names landing on one identifier means
// whichever is rendered second silently overwrites the first in the
// generated interface, and nothing in the output signals that a field went
// missing.
//
// "Object namespace" is not only a top-level named schema. schemaToTSType's
// "object" case renders an inline (non-$ref) nested schema with declared
// Properties through the exact same objectPropsLiteral helper a top-level
// `export interface` uses, so an inline object nested inside a property, an
// array's items, an additionalProperties value schema, or a oneOf/anyOf/
// allOf member is just as real a collision surface as a named schema's own
// direct properties -- see checkSchemaFieldCollisions, which the walk below
// recurses into for exactly those shapes.
//
// tsFieldName is called for every property exactly as the renaming code
// path will, so FieldOverrides is consulted before any comparison happens:
//   - A schema-scoped or global override that gives one of the two wire
//     names in a would-be collision a different client name resolves it --
//     no error, because that is exactly how a caller is meant to fix this.
//   - An override's chosen *value* can just as easily collide with another
//     property's derived name (e.g. wire "a" overridden to "x", and wire
//     "x_" deriving to "x" under camel). Because tsFieldName is used
//     uniformly regardless of whether a name came from an override or from
//     the naming strategy, this case is caught by the same comparison --
//     there is no separate "override values" pass to add.
//
// Collisions across different namespaces are never reported: User.id and
// Post.id both deriving to "id" is normal, since each schema becomes its own
// TypeScript interface with its own, independent set of property names --
// and the same holds for two different inline namespaces, e.g.
// Order.shipping.street_name and Order.billing.street_name. A schema name
// colliding with a reserved streaming type name is a distinct namespace
// already covered by checkSchemaNameCollisions.
//
// Under NamingPreserve, tsFieldName returns wireName unchanged for every
// property that has no override, so two distinct wire names can never
// derive to the same name -- the walk is skipped entirely rather than
// running a pass that can only ever come back empty.
//
// All collisions found across the whole spec are reported at once, not just
// the first, so a caller does not have to fix them one regeneration at a
// time. Top-level schemas are walked in sortedKeys order and every nested
// namespace is walked in the same deterministic, sorted, depth-first order
// checkSchemaFieldCollisions defines, so the report is stable across runs.
//
// dedupeMessages runs over the final list before formatting the error. A
// schema that declares its own direct Properties AND an AllOf (legal,
// unusual) can have the SAME underlying collision reported twice, verbatim
// -- once by the top-level own-Properties check, once by
// checkFlattenedAllOfCollisions, which includes that schema's own
// Properties as the allOf's last layer (see checkFlattenedAllOfCollisions'
// doc comment). Both messages are correct and identical strings, so
// dropping the repeat is safe and unambiguous -- unlike a mismatch between
// two DIFFERENT messages describing the same collision (which would be a
// sign the two passes disagree about something, not merely overlap).
func checkFieldNameCollisions(spec *client.APISpec, config client.GeneratorConfig) error {
	if effectiveFieldNaming(config) == client.NamingPreserve {
		return nil
	}

	var messages []string

	visited := make(map[string]bool, len(spec.Schemas))

	for _, schemaName := range sortedKeys(spec.Schemas) {
		messages = append(messages, checkSchemaFieldCollisions(schemaName, spec.Schemas[schemaName], spec, config, visited)...)
	}

	messages = dedupeMessages(messages)

	if len(messages) == 0 {
		return nil
	}

	return fmt.Errorf(
		"field-name collision(s) detected, generation aborted (no files were produced):\n%s",
		strings.Join(messages, "\n"))
}

// dedupeMessages removes exact-duplicate strings from messages, preserving
// first-occurrence order (itself already deterministic, since messages is
// built by a deterministic, sorted walk) so the error text stays stable
// across runs.
func dedupeMessages(messages []string) []string {
	if len(messages) == 0 {
		return messages
	}

	seen := make(map[string]bool, len(messages))
	out := make([]string, 0, len(messages))

	for _, m := range messages {
		if seen[m] {
			continue
		}

		seen[m] = true
		out = append(out, m)
	}

	return out
}

// checkSchemaFieldCollisions checks one object namespace -- identified by
// id -- for property-name collisions, then recurses into every inline
// composite reachable from it: a property's own inline object, an array's
// inline-object items, an additionalProperties inline-object value schema,
// and inline (non-$ref) oneOf/anyOf/allOf members.
//
// id doubles as the "schema name" half of tsFieldName's lookup and as the
// prefix of the FieldOverrides key printed in a collision message. For a
// top-level schema, id is simply its name (e.g. "User"), matching existing
// behavior exactly. For a nested namespace, id is a synthetic dotted path
// built the same way codecTable's codecIDFor already builds codec-table ids
// for the same shapes ("Order.shipping" for a nested property,
// "Order.line_items.items" for array items, "Order.extras.values" for an
// additionalProperties value) -- reusing that scheme, rather than inventing
// a second one, is what makes the FieldOverrides key this function prints
// actually work: tsFieldName builds its schema-scoped lookup key as
// id + "." + wireName, so calling it with id="Order.shipping" and
// wireName="street_name" checks exactly the key
// FieldOverrides["Order.shipping.street_name"] -- the same literal string
// printed in the error and the same one a caller pastes back in. This does
// inherit the pre-existing dot-concatenation ambiguity documented on
// tsFieldName (a component of the path itself containing a literal "."
// could make two different nestings print the same key); that risk already
// existed for top-level schema/wire names and is not resolved here, per the
// same reasoning tsFieldName's doc comment gives.
//
// codecIDFor has no synthetic-id scheme for an inline oneOf/anyOf/allOf
// member -- codecTable.unionEntry only ever extracts $ref members for its
// discriminator mapping and silently skips inline ones, so there is no
// existing id to reuse there. This function invents "<id>.oneOf<index>" /
// "<id>.anyOf<index>" / "<id>.allOf<index>" for that one shape, following
// the same "<parent>.<token>" shape as every id codecIDFor does define, so
// it is at least self-consistent with the rest of the scheme even though it
// is not literally shared with codecs.go today.
//
// A property whose own schema is a $ref is never recursed into: that named
// schema is already checked independently by the top-level walk in
// checkFieldNameCollisions, under its own name. Recursing into it here too
// would check the same properties twice, under two different ids, and (via
// a $ref cycle, e.g. a self-referential linked-list-shaped schema) could
// recurse forever -- codecIDFor makes exactly the same "$ref means reuse
// the target's name and stop" choice for the same reason.
//
// visited guards against re-entering a namespace id that has already been
// walked. In practice this catches the finite, real overlap
// checkFlattenedAllOfCollisions' doc comment describes (an allOf schema that
// also declares its own direct Properties: the own-Properties block above
// and the allOf pass both recurse into the SAME id for that last layer, and
// visited turns the second visit into a no-op instead of a duplicate walk)
// and, more generally, any two different paths through the schema graph that
// happen to derive the identical synthetic id.
//
// It does NOT guard against a hand-built *client.Schema graph containing a
// literal Go pointer cycle with no $ref involved at all (e.g.
// schema.Properties["self"] pointing back to schema itself, reproduced
// directly against this function) -- every recursive step here appends a new
// path segment (id+".self", id+".self.self", ...), so the id keeps growing
// and never repeats, and visited[id] never comes back true. That shape is
// not reachable from a parsed OpenAPI document ($ref properties are never
// recursed into here -- see the paragraph above -- and that is the only way
// spec_parser.go or introspector.go could produce a cycle; see
// TestOneOfSelfReferenceDoesNotInfiniteLoop's doc comment for the same
// observation about schemaToTSType), so it is not worth a real fix. The
// maxFieldCollisionDepth check below is a cheap, unconditional bound against
// it (and against any other pathologically deep hand-built schema graph)
// rather than leaving an unbounded id string and an unbounded stack as the
// only limits.
//
// allOf is handled by ONE pass -- checkFlattenedAllOfCollisions, below --
// not a per-member loop, and this is a deliberate correction of an earlier
// design. A per-member loop that checked each AllOf member as its OWN
// isolated namespace ("id.allOf<index>") existed here previously, on the
// reasoning that it would catch a collision WITHIN a single member's own
// properties. That reasoning was wrong: allOfEntry (codecs.go) never
// builds any table entry keyed "id.allOf<index>" for ANY allOf shape --
// every member's properties, at every depth, get merged straight into
// entries keyed by the allOf schema's own id ("id" for top-level
// properties, "id.<prop>" for a nested inline composite one level down,
// exactly like codecIDFor computes for a property of a plain object). A
// per-member-namespaced check therefore printed a FieldOverrides key that
// named a namespace the table never uses -- for a single inline member
// with an internal collision, let alone a NESTED inline object inside a
// member (e.g. allOf[{payload: {full_name, fullName}}], where the table
// keys the collision "id.payload", not "id.allOf0.payload"). Applying that
// phantom key made checkFieldNameCollisions report success while the
// collision it was invented to describe was still live -- converting a
// caught error into a silently wrong one, which is worse than not catching
// it at all. checkFlattenedAllOfCollisions is the single source of truth
// for the allOf namespace now, because it reuses flattenAllOfLayers --
// codecs.go's own allOf-resolution logic -- rather than a second,
// independently maintained notion of what an allOf's fields (and their
// nested structure) even are; the guard and the table cannot disagree
// about the namespace if there is only one function that computes it.
// maxFieldCollisionDepth bounds checkSchemaFieldCollisions' recursion depth
// (approximated by the number of "." path segments in id) against a
// hand-built schema graph containing a genuine Go pointer cycle with no
// $ref involved -- see checkSchemaFieldCollisions' doc comment for why
// visited cannot catch that shape. No real OpenAPI-derived spec nests this
// deep, so the cap is generous rather than tight.
const maxFieldCollisionDepth = 200

func checkSchemaFieldCollisions(id string, schema *client.Schema, spec *client.APISpec, config client.GeneratorConfig, visited map[string]bool) []string {
	if schema == nil || visited[id] || strings.Count(id, ".") > maxFieldCollisionDepth {
		return nil
	}

	visited[id] = true

	var messages []string

	if len(schema.Properties) > 0 {
		owner := make(map[string]string, len(schema.Properties)) // client name -> first wire name that claimed it

		for _, wireName := range sortedKeys(schema.Properties) {
			prop := schema.Properties[wireName]
			clientName := tsFieldName(id, wireName, config)

			if first, claimed := owner[clientName]; claimed {
				messages = append(messages, fmt.Sprintf(
					"schema %q: wire names %q and %q both resolve to client field %q; add FieldOverrides[%q] to disambiguate",
					id, first, wireName, clientName, id+"."+wireName))
			} else {
				owner[clientName] = wireName
			}

			if prop != nil && prop.Ref == "" {
				messages = append(messages, checkSchemaFieldCollisions(id+"."+wireName, prop, spec, config, visited)...)
			}
		}
	}

	if schema.Type == "array" && schema.Items != nil && schema.Items.Ref == "" {
		messages = append(messages, checkSchemaFieldCollisions(id+".items", schema.Items, spec, config, visited)...)
	}

	if values, ok := additionalPropsSchema(schema.AdditionalProperties); ok && values != nil && values.Ref == "" {
		messages = append(messages, checkSchemaFieldCollisions(id+".values", values, spec, config, visited)...)
	}

	for i, member := range schema.OneOf {
		if member != nil && member.Ref == "" {
			messages = append(messages, checkSchemaFieldCollisions(fmt.Sprintf("%s.oneOf%d", id, i), member, spec, config, visited)...)
		}
	}

	for i, member := range schema.AnyOf {
		if member != nil && member.Ref == "" {
			messages = append(messages, checkSchemaFieldCollisions(fmt.Sprintf("%s.anyOf%d", id, i), member, spec, config, visited)...)
		}
	}

	// AllOf inline members are NOT recursed into with their own
	// "id.allOf<index>" namespace here, unlike OneOf/AnyOf just above --
	// see this function's doc comment for why: codecs.go's allOfEntry
	// never builds a table entry under that namespace for any allOf shape,
	// so doing so here would reintroduce the exact phantom-key defect this
	// comment describes. checkFlattenedAllOfCollisions (below) is the only
	// allOf check, and it is unconditional on schema.AllOf being non-empty.
	if len(schema.AllOf) > 0 {
		messages = append(messages, checkFlattenedAllOfCollisions(id, schema, spec, config, visited)...)
	}

	return messages
}

// checkFlattenedAllOfCollisions checks the namespace allOfEntry
// (codecs.go) actually builds for an allOf composition: every contributing
// member's properties, flattened into ONE merged set keyed by id, exactly
// as flattenAllOfLayers resolves it (the SAME function codecs.go's
// allOfEntry calls -- see checkSchemaFieldCollisions's doc comment for why
// reusing it, rather than reimplementing allOf resolution a second time,
// is the whole point of this function existing).
//
// tsFieldName is called with id as the schema name for every layer's
// TOP-level property, matching how allOfEntry's own field derivation works
// (parentID stays the allOf schema's own id for every layer, never a
// per-layer id) -- so the FieldOverrides key this prints for a top-level
// collision, "id.wireName", is the exact key that will actually take
// effect.
//
// A property that is itself an inline composite (a nested object, an
// array's inline-object items, an inline additionalProperties value
// schema, or a further oneOf/anyOf/allOf) is recursed into via
// checkSchemaFieldCollisions using id+"."+wireName as the child's own
// namespace -- codecIDFor derives that SAME "<parent>.<prop>" id for any
// property, allOf-contributed or not, so this reuses the identical
// resolution codecs.go performs rather than inventing a different scheme
// for allOf specifically. Without this recursion, a collision nested
// inside an allOf-contributed property (e.g.
// allOf[{payload: {full_name, fullName}}], which the table keys
// "id.payload") would be invisible to every pass: the flattened check only
// ever looked at each layer's OWN top-level properties, one level too
// shallow to see it.
//
// visited is the SAME set threaded through the whole
// checkSchemaFieldCollisions walk (not a fresh one per allOf composition):
// if schema also has its own direct Properties (allOf permits this
// alongside AllOf, unusual but legal), flattenAllOfLayers includes schema
// itself as the LAST layer, so this function's per-property recursion into
// e.g. id+"."+wireName for one of schema's own properties would otherwise
// re-enter a namespace the OWN-Properties block above already recursed
// into -- reusing visited makes that a no-op instead of a duplicate walk,
// which is also why the schema-own-Properties-collision case can still
// surface a duplicate MESSAGE (the top-level owner-map check above and
// this function's owner-map check both run independently over the same
// properties) -- checkFieldNameCollisions dedupes exact-duplicate message
// strings for that reason, deliberately, rather than this function trying
// to detect and suppress the overlap itself.
func checkFlattenedAllOfCollisions(id string, schema *client.Schema, spec *client.APISpec, config client.GeneratorConfig, visited map[string]bool) []string {
	layers, _ := flattenAllOfLayers(schema, "", spec, map[*client.Schema]bool{})

	var messages []string

	owner := make(map[string]string) // client name -> first wire name that claimed it

	for _, layer := range layers {
		for _, wireName := range sortedKeys(layer.Properties) {
			prop := layer.Properties[wireName]
			clientName := tsFieldName(id, wireName, config)

			if first, claimed := owner[clientName]; claimed {
				if first != wireName {
					messages = append(messages, fmt.Sprintf(
						"schema %q: wire names %q and %q both resolve to client field %q; add FieldOverrides[%q] to disambiguate",
						id, first, wireName, clientName, id+"."+wireName))
				}
				// first == wireName: the identical wire name reachable
				// through two different paths to the same layer (e.g. a
				// diamond allOf graph) -- not a real collision. The
				// recursion below still runs for this occurrence, but
				// visited makes a repeat walk of the same namespace a
				// no-op rather than a duplicate.
			} else {
				owner[clientName] = wireName
			}

			if prop != nil && prop.Ref == "" {
				messages = append(messages, checkSchemaFieldCollisions(id+"."+wireName, prop, spec, config, visited)...)
			}
		}
	}

	return messages
}
