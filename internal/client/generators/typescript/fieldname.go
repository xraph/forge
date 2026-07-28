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
func checkFieldNameCollisions(spec *client.APISpec, config client.GeneratorConfig) error {
	if effectiveFieldNaming(config) == client.NamingPreserve {
		return nil
	}

	var messages []string

	visited := make(map[string]bool, len(spec.Schemas))

	for _, schemaName := range sortedKeys(spec.Schemas) {
		messages = append(messages, checkSchemaFieldCollisions(schemaName, spec.Schemas[schemaName], spec, config, visited)...)
	}

	if len(messages) == 0 {
		return nil
	}

	return fmt.Errorf(
		"field-name collision(s) detected, generation aborted (no files were produced):\n%s",
		strings.Join(messages, "\n"))
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
// visited guards against the one remaining cycle shape: a hand-built
// *client.Schema graph containing a literal Go pointer cycle with no $ref
// involved at all (e.g. schema.Properties["self"] pointing back to schema
// itself). This is not a shape spec_parser.go or introspector.go can
// produce from a real OpenAPI document (see
// TestOneOfSelfReferenceDoesNotInfiniteLoop's doc comment for the same
// observation about schemaToTSType), but the guard is cheap and
// codecTable.add already sets the same precedent: reserve the id before
// recursing, so re-entering the same id hits the guard instead of looping.
//
// allOf gets TWO passes, not one, because it is checked from two genuinely
// different namespaces:
//
//   - The per-member loop below (unchanged from before this comment) treats
//     each AllOf member as its OWN isolated namespace ("id.allOf<index>"),
//     which is correct for catching a collision WITHIN a single member's own
//     properties.
//   - checkFlattenedAllOfCollisions (below) additionally checks the
//     FLATTENED namespace -- reusing flattenAllOfLayers, codecs.go's own
//     allOf-resolution logic, rather than a second, independently
//     maintained notion of what an allOf's fields even are. allOfEntry
//     merges every member's properties into ONE table entry keyed by the
//     allOf schema's OWN id (not per-member ids), so two DIFFERENT wire
//     names on two DIFFERENT members that resolve to the same client name
//     are a real collision in that single merged namespace -- one the
//     per-member loop can never see, since it only ever compares a
//     member's properties against ITSELF. Missing this half is exactly how
//     a review found allOf-driven data loss survived a full task: the
//     guard and the table disagreed about what the allOf namespace was.
func checkSchemaFieldCollisions(id string, schema *client.Schema, spec *client.APISpec, config client.GeneratorConfig, visited map[string]bool) []string {
	if schema == nil || visited[id] {
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

	for i, member := range schema.AllOf {
		if member != nil && member.Ref == "" {
			messages = append(messages, checkSchemaFieldCollisions(fmt.Sprintf("%s.allOf%d", id, i), member, spec, config, visited)...)
		}
	}

	if len(schema.AllOf) > 0 {
		messages = append(messages, checkFlattenedAllOfCollisions(id, schema, spec, config)...)
	}

	return messages
}

// checkFlattenedAllOfCollisions checks the namespace allOfEntry
// (codecs.go) actually builds for an allOf composition: every contributing
// member flattened into ONE merged set of properties keyed by id, exactly
// as flattenAllOfLayers resolves it (the SAME function codecs.go's
// allOfEntry calls -- this deliberately does not reimplement allOf
// resolution a second time, since having the guard and the table disagree
// about what the namespace even is is exactly how a real collision
// survived a full review round undetected).
//
// tsFieldName is called with id as the schema name for every layer's
// property, matching how allOfEntry's own field derivation will work once
// renaming is wired into it (parentID stays the allOf schema's own id for
// every layer, never a per-layer id) -- so the FieldOverrides key this
// prints, "id.wireName", is the exact key that will actually take effect.
//
// This intentionally reports across ALL layers combined, not just pairs
// from DIFFERENT layers: a collision wholly within one layer's own
// properties would also be reported by the per-member loop in
// checkSchemaFieldCollisions (for that member's isolated "id.allOf<index>"
// namespace), so the same underlying issue can surface twice, once from
// each pass. Both messages are independently true and actionable; avoiding
// that overlap would require tracking which layer each already-claimed
// name came from and suppressing same-layer repeats, which is more
// bookkeeping than a rare double-reported message currently justifies.
func checkFlattenedAllOfCollisions(id string, schema *client.Schema, spec *client.APISpec, config client.GeneratorConfig) []string {
	layers, _ := flattenAllOfLayers(schema, "", spec, map[*client.Schema]bool{})

	var messages []string

	owner := make(map[string]string) // client name -> first wire name that claimed it

	for _, layer := range layers {
		for _, wireName := range sortedKeys(layer.Properties) {
			clientName := tsFieldName(id, wireName, config)

			if first, claimed := owner[clientName]; claimed {
				if first == wireName {
					// The identical wire name reachable through two
					// different paths to the same layer (e.g. a diamond
					// allOf graph) is not a real collision.
					continue
				}

				messages = append(messages, fmt.Sprintf(
					"schema %q: wire names %q and %q both resolve to client field %q; add FieldOverrides[%q] to disambiguate",
					id, first, wireName, clientName, id+"."+wireName))
			} else {
				owner[clientName] = wireName
			}
		}
	}

	return messages
}
