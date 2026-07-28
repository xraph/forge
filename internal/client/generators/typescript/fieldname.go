package typescript

import "github.com/xraph/forge/internal/client"

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
