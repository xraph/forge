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
// properties of the same schema resolve, via tsFieldName, to the same
// client-side field name. This must run -- and must abort generation --
// before schema property renaming is wired into the actual output (a later
// task): two wire names landing on one identifier means whichever is
// rendered second silently overwrites the first in the generated interface,
// and nothing in the output signals that a field went missing.
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
// Collisions across different schemas are never reported: User.id and
// Post.id both deriving to "id" is normal, since each schema becomes its own
// TypeScript interface with its own, independent set of property names.
// Property names are also the only collision surface this check needs to
// consider -- schemaToTypeScript's object case emits exactly the properties
// in schema.Properties and nothing else keyed by name (the
// additionalProperties case adds an unnamed `Record<string, V>` intersection
// member, which cannot collide with a specific property key); a schema name
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
// time. Schemas are walked in sortedKeys order and, within each schema,
// properties are walked in sortedKeys order, so the report is deterministic
// across runs.
func checkFieldNameCollisions(spec *client.APISpec, config client.GeneratorConfig) error {
	if effectiveFieldNaming(config) == client.NamingPreserve {
		return nil
	}

	var messages []string

	for _, schemaName := range sortedKeys(spec.Schemas) {
		schema := spec.Schemas[schemaName]
		if schema == nil {
			continue
		}

		owner := make(map[string]string, len(schema.Properties)) // client name -> first wire name that claimed it

		for _, wireName := range sortedKeys(schema.Properties) {
			clientName := tsFieldName(schemaName, wireName, config)

			first, claimed := owner[clientName]
			if !claimed {
				owner[clientName] = wireName
				continue
			}

			messages = append(messages, fmt.Sprintf(
				"schema %q: wire names %q and %q both resolve to client field %q; add FieldOverrides[%q] to disambiguate",
				schemaName, first, wireName, clientName, schemaName+"."+wireName))
		}
	}

	if len(messages) == 0 {
		return nil
	}

	return fmt.Errorf(
		"field-name collision(s) detected, generation aborted (no files were produced):\n%s",
		strings.Join(messages, "\n"))
}
