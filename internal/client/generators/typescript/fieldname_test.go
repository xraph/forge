package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// TestTsFieldName covers tsFieldName's full resolution order: strategy
// selection, override precedence (schema-scoped over global), overrides
// being used verbatim (never case-converted), NamingPreserve, the
// zero-value default (both for typescript and for other/unset languages),
// and an unrecognised strategy value.
func TestTsFieldName(t *testing.T) {
	cases := []struct {
		name       string
		schemaName string
		wireName   string
		config     client.GeneratorConfig
		want       string
	}{
		{
			name:       "camel strategy",
			schemaName: "User",
			wireName:   "user_id",
			config:     client.GeneratorConfig{FieldNaming: client.NamingCamel},
			want:       "userId",
		},
		{
			name:       "pascal strategy",
			schemaName: "User",
			wireName:   "user_id",
			config:     client.GeneratorConfig{FieldNaming: client.NamingPascal},
			want:       "UserId",
		},
		{
			name:       "snake strategy",
			schemaName: "User",
			wireName:   "userId",
			config:     client.GeneratorConfig{FieldNaming: client.NamingSnake},
			want:       "user_id",
		},
		{
			name:       "snake strategy is consistent with the acronym fix",
			schemaName: "User",
			wireName:   "HTTPStatus",
			config:     client.GeneratorConfig{FieldNaming: client.NamingSnake},
			want:       "http_status",
		},
		{
			name:       "preserve strategy returns the wire name unchanged",
			schemaName: "User",
			wireName:   "user_id",
			config:     client.GeneratorConfig{FieldNaming: client.NamingPreserve},
			want:       "user_id",
		},
		{
			name:       "preserve strategy does not case-convert an already-camel wire name",
			schemaName: "User",
			wireName:   "userId",
			config:     client.GeneratorConfig{FieldNaming: client.NamingPreserve},
			want:       "userId",
		},
		{
			name:       "schema-scoped override beats global override",
			schemaName: "User",
			wireName:   "user_id",
			config: client.GeneratorConfig{
				FieldNaming: client.NamingCamel,
				FieldOverrides: map[string]string{
					"User.user_id": "uid",
					"user_id":      "userIdentifier",
				},
			},
			want: "uid",
		},
		{
			name:       "global override applies when no schema-scoped entry matches",
			schemaName: "Other",
			wireName:   "user_id",
			config: client.GeneratorConfig{
				FieldNaming: client.NamingCamel,
				FieldOverrides: map[string]string{
					"User.user_id": "uid",
					"user_id":      "userIdentifier",
				},
			},
			want: "userIdentifier",
		},
		{
			name:       "override is used verbatim, bypassing the strategy entirely",
			schemaName: "User",
			wireName:   "user_id",
			config: client.GeneratorConfig{
				FieldNaming: client.NamingCamel,
				FieldOverrides: map[string]string{
					"User.user_id": "USER_ID_RAW",
				},
			},
			want: "USER_ID_RAW",
		},
		{
			name:       "zero-value config defaults to camel for typescript",
			schemaName: "User",
			wireName:   "user_id",
			config:     client.GeneratorConfig{Language: "typescript"},
			want:       "userId",
		},
		{
			name:       "zero-value config defaults to preserve for a non-typescript language",
			schemaName: "User",
			wireName:   "user_id",
			config:     client.GeneratorConfig{Language: "go"},
			want:       "user_id",
		},
		{
			name:       "fully zero-value config (hand-built, Language unset) defaults to preserve",
			schemaName: "User",
			wireName:   "user_id",
			config:     client.GeneratorConfig{},
			want:       "user_id",
		},
		{
			name:       "unrecognised strategy value falls back to preserve rather than panicking",
			schemaName: "User",
			wireName:   "user_id",
			config:     client.GeneratorConfig{FieldNaming: client.NamingStrategy("kebab")},
			want:       "user_id",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tsFieldName(c.schemaName, c.wireName, c.config); got != c.want {
				t.Errorf("tsFieldName(%q, %q, %+v) = %q, want %q", c.schemaName, c.wireName, c.config, got, c.want)
			}
		})
	}
}

// collisionSpec returns a minimal spec with one schema ("User") whose two
// properties, "user_id" and "userId", both derive to the client field
// "userId" under camel naming -- the canonical collision this task must
// catch.
func collisionSpec() *client.APISpec {
	return &client.APISpec{
		Info: client.APIInfo{Title: "Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"User": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"user_id": {Type: "string"},
					"userId":  {Type: "string"},
				},
			},
		},
	}
}

func collisionConfig() client.GeneratorConfig {
	config := client.DefaultConfig()
	config.Language = "typescript"
	config.PackageName = "probe"
	config.FieldNaming = client.NamingCamel

	return config
}

// TestGenerateFailsOnFieldNameCollision is the primary case this task adds:
// two wire names in one schema deriving to the same client field must make
// Generate error out, name both wire names and the schema, point at the
// FieldOverrides key that resolves it, and produce no files at all.
func TestGenerateFailsOnFieldNameCollision(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), collisionSpec(), collisionConfig())

	if err == nil {
		t.Fatal("expected an error for colliding field names, got nil")
	}

	if out != nil {
		t.Errorf("expected no generated client on collision, got %+v", out)
	}

	for _, want := range []string{"User", "user_id", "userId", `FieldOverrides["User.user_id"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestGenerateAllowsCollisionResolvedByOverride is the negative case: once a
// FieldOverrides entry disambiguates the pair, generation must succeed.
func TestGenerateAllowsCollisionResolvedByOverride(t *testing.T) {
	config := collisionConfig()
	config.FieldOverrides = map[string]string{"User.user_id": "userIdentifier"}

	out, err := NewGenerator().Generate(context.Background(), collisionSpec(), config)
	if err != nil {
		t.Fatalf("expected no error once the collision is resolved by an override, got: %v", err)
	}

	if out == nil {
		t.Fatal("expected a generated client once the collision is resolved")
	}
}

// TestGenerateAllowsSameCollisionAcrossDifferentSchemas asserts that the same
// pair of wire names landing on the same client name is fine when they
// belong to different schemas -- each schema gets its own interface, so
// there is no shared namespace to collide in.
func TestGenerateAllowsSameCollisionAcrossDifferentSchemas(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"User": {Type: "object", Properties: map[string]*client.Schema{"user_id": {Type: "string"}}},
			"Post": {Type: "object", Properties: map[string]*client.Schema{"userId": {Type: "string"}}},
		},
	}

	if _, err := NewGenerator().Generate(context.Background(), spec, collisionConfig()); err != nil {
		t.Fatalf("expected no error for a collision across different schemas, got: %v", err)
	}
}

// TestGenerateAllowsCollisionUnderPreserve asserts that NamingPreserve never
// reports a collision: with no case conversion, "user_id" and "userId"
// remain distinct client names.
func TestGenerateAllowsCollisionUnderPreserve(t *testing.T) {
	config := collisionConfig()
	config.FieldNaming = client.NamingPreserve

	if _, err := NewGenerator().Generate(context.Background(), collisionSpec(), config); err != nil {
		t.Fatalf("expected no error under NamingPreserve, got: %v", err)
	}
}

// TestCheckFieldNameCollisionsReportsAll asserts that every collision in the
// spec is reported in one pass -- not just the first -- so a caller does not
// have to fix them one regeneration at a time.
func TestCheckFieldNameCollisionsReportsAll(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"User": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"user_id": {Type: "string"},
					"userId":  {Type: "string"},
				},
			},
			"Post": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"author_id": {Type: "string"},
					"authorId":  {Type: "string"},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error reporting both schemas' collisions")
	}

	for _, want := range []string{"User", "Post", `FieldOverrides["User.user_id"]`, `FieldOverrides["Post.author_id"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestCheckFieldNameCollisionsDetectsOverrideValueCollision covers the
// corollary raised in review: a FieldOverrides *value* can coincide with
// another property's derived name just as easily as two derived names can
// coincide with each other, and tsFieldName is used uniformly for every
// property, so this must be caught too. Wire "a" is overridden to "x"; wire
// "x_" derives to "x" under camel (a lone trailing separator contributes no
// extra word), so both land on client name "x".
func TestCheckFieldNameCollisionsDetectsOverrideValueCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Thing": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"a":  {Type: "string"},
					"x_": {Type: "string"},
				},
			},
		},
	}

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Thing.a": "x"}

	err := checkFieldNameCollisions(spec, config)
	if err == nil {
		t.Fatal("expected a collision between an override value and a derived name")
	}

	if !strings.Contains(err.Error(), "Thing") {
		t.Errorf("error message missing schema name; got: %s", err.Error())
	}
}

// TestCheckFieldNameCollisionsCatchesOverrideCollisionUnderPreserve is P3-T7's
// carried gap (progress.md): checkFieldNameCollisions short-circuited to nil
// whenever effectiveFieldNaming(config) == NamingPreserve, without ever
// considering FieldOverrides. But an override renames a field EVEN UNDER
// preserve (tsFieldName checks FieldOverrides before consulting the naming
// strategy at all -- see its doc comment), and codecsNeeded already treats
// preserve+overrides as "codecs are live" for exactly this reason. So two
// distinct wire names ("a_field", "b_field") given the SAME override value
// ("sameName") under preserve silently overwrite one another in the rendered
// interface and in the codec table -- and, before this fix, generation
// reported no error at all.
func TestCheckFieldNameCollisionsCatchesOverrideCollisionUnderPreserve(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Thing": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"a_field": {Type: "string"},
					"b_field": {Type: "string"},
				},
			},
		},
	}

	config := collisionConfig()
	config.FieldNaming = client.NamingPreserve
	config.FieldOverrides = map[string]string{
		"Thing.a_field": "sameName",
		"Thing.b_field": "sameName",
	}

	err := checkFieldNameCollisions(spec, config)
	if err == nil {
		t.Fatal("expected a collision error: two overrides map different wire names (a_field, b_field) to the same client name (sameName) under preserve, but checkFieldNameCollisions reported none")
	}

	for _, want := range []string{"Thing", "a_field", "b_field", "sameName"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// --- Round 2: inline composite namespaces -----------------------------
//
// Review round 1 found that checkFieldNameCollisions only ever looked at a
// schema's own direct Properties, never descending into an inline (non-$ref)
// nested object -- even though schemaToTSType's "object" case renders such a
// nested schema via the exact same objectPropsLiteral helper used for
// top-level named schemas (generator.go's schemaToTypeScript "object" case),
// making it just as real a collision surface. The tests below cover each
// inline-composite shape the reviewer asked about, after first confirming
// (by reading schemaToTSType and codecIDFor) that the shape actually reaches
// rendered output.

// nestedCollisionConfig is collisionConfig but with a distinct schema/field
// vocabulary per test isn't needed -- collisionConfig itself is naming-scheme
// only (camel, no overrides by default), so it is reused as-is by every test
// below; each test sets its own FieldOverrides when it needs one.

// TestGenerateFailsOnNestedInlineObjectFieldCollision: an inline object
// nested one level inside a named schema (Order.shipping) with two wire
// names that collide under camel. schemaToTSType's "object" case renders
// Order.shipping via objectPropsLiteral exactly like a top-level interface,
// and codecIDFor already gives inline objects with declared Properties a
// synthetic id of "<parentID>.<prop>" -- here "Order.shipping" -- so the
// FieldOverrides key this test expects reuses that exact scheme.
func TestGenerateFailsOnNestedInlineObjectFieldCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"shipping": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"street_name": {Type: "string"},
							"streetName":  {Type: "string"},
						},
					},
				},
			},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, collisionConfig())

	if err == nil {
		t.Fatal("expected an error for a field-name collision inside a nested inline object")
	}

	if out != nil {
		t.Errorf("expected no generated client on collision, got %+v", out)
	}

	for _, want := range []string{"street_name", "streetName", `FieldOverrides["Order.shipping.street_name"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestNestedFieldOverrideKeyActuallyResolvesCollision proves the key printed
// above is not just cosmetically plausible: pasting it into FieldOverrides
// verbatim must make the collision go away, because tsFieldName builds its
// schema-scoped lookup key by concatenating whatever "schema name" it is
// called with and the wire name with a ".", and the nested walk calls it
// with the synthetic id ("Order.shipping") as that schema name -- so
// "Order.shipping" + "." + "street_name" reconstructs the exact same string
// a caller would paste in.
func TestNestedFieldOverrideKeyActuallyResolvesCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"shipping": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"street_name": {Type: "string"},
							"streetName":  {Type: "string"},
						},
					},
				},
			},
		},
	}

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Order.shipping.street_name": "streetNameAlt"}

	if _, err := NewGenerator().Generate(context.Background(), spec, config); err != nil {
		t.Fatalf("expected the printed FieldOverrides key to resolve the nested collision, got: %v", err)
	}
}

// TestGenerateFailsOnTwoLevelsDeepNestedCollision: the same inline-object
// surface, one level deeper (Order.shipping.geo), proving the walk recurses
// rather than stopping after one level of nesting.
func TestGenerateFailsOnTwoLevelsDeepNestedCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"shipping": {
						Type: "object",
						Properties: map[string]*client.Schema{
							"geo": {
								Type: "object",
								Properties: map[string]*client.Schema{
									"lat_long": {Type: "string"},
									"latLong":  {Type: "string"},
								},
							},
						},
					},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for a field-name collision two levels of inline nesting deep")
	}

	for _, want := range []string{"lat_long", "latLong", `FieldOverrides["Order.shipping.geo.lat_long"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestGenerateFailsOnArrayItemsInlineObjectCollision: an inline object used
// as an array's `items` schema (Order.line_items, array of inline objects).
// schemaToTSType's "array" case renders `itemType[]` where itemType comes
// from schemaToTSType on schema.Items -- an inline object with Properties
// hits the same objectPropsLiteral path. codecIDFor already registers such
// an items schema under "<parentID>.items", reused here.
func TestGenerateFailsOnArrayItemsInlineObjectCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"line_items": {
						Type: "array",
						Items: &client.Schema{
							Type: "object",
							Properties: map[string]*client.Schema{
								"unit_price": {Type: "string"},
								"unitPrice":  {Type: "string"},
							},
						},
					},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for a field-name collision inside array items")
	}

	for _, want := range []string{"unit_price", "unitPrice", `FieldOverrides["Order.line_items.items.unit_price"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestGenerateFailsOnAdditionalPropertiesValueInlineObjectCollision: an
// inline object used as an `additionalProperties` VALUE schema
// (Order.extras, a map whose values are inline objects). schemaToTSType's
// "object" case's `allowed` branches call schemaToTSType on the value
// schema, which hits objectPropsLiteral the same way. codecIDFor registers
// such a value schema under "<parentID>." + additionalPropertiesSegment
// (codecs.go) -- not the more obvious "<parentID>.values", which a
// declared property literally named "values" could otherwise collide with
// -- reused here.
func TestGenerateFailsOnAdditionalPropertiesValueInlineObjectCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"extras": {
						Type: "object",
						AdditionalProperties: &client.Schema{
							Type: "object",
							Properties: map[string]*client.Schema{
								"display_name": {Type: "string"},
								"displayName":  {Type: "string"},
							},
						},
					},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for a field-name collision inside an additionalProperties value schema")
	}

	for _, want := range []string{"display_name", "displayName", `FieldOverrides["Order.extras.additionalProperties.display_name"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestGenerateFailsOnOneOfInlineMemberFieldCollision: an inline (non-$ref)
// object as a member of a oneOf (Order.payment). schemaToTSType's OneOf
// handling calls schemaToTSType on every member regardless of whether it has
// a $ref, so an inline member with Properties renders via objectPropsLiteral
// exactly like the other shapes above. Unlike the three shapes above,
// codecIDFor/unionEntry does NOT register non-$ref union members under any
// id at all today (it only extracts $ref members for the discriminator
// mapping) -- there is no existing scheme to reuse here, so this test pins
// the "oneOf<index>" token the walk invents for this one shape.
func TestGenerateFailsOnOneOfInlineMemberFieldCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"payment": {
						OneOf: []*client.Schema{
							{
								Type: "object",
								Properties: map[string]*client.Schema{
									"card_number": {Type: "string"},
									"cardNumber":  {Type: "string"},
								},
							},
							{Type: "string"},
						},
					},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for a field-name collision inside a oneOf inline member")
	}

	for _, want := range []string{"card_number", "cardNumber", `FieldOverrides["Order.payment.oneOf0.card_number"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestGenerateAllowsNestedCollisionAcrossDifferentParents: the same wire
// names colliding under two different inline namespaces (Order.shipping and
// Order.billing) must not error against each other -- each inline object is
// its own namespace, exactly like two different top-level schemas.
func TestGenerateAllowsNestedCollisionAcrossDifferentParents(t *testing.T) {
	addr := func() *client.Schema {
		return &client.Schema{Type: "string"}
	}

	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"shipping": {Type: "object", Properties: map[string]*client.Schema{"street_name": addr()}},
					"billing":  {Type: "object", Properties: map[string]*client.Schema{"streetName": addr()}},
				},
			},
		},
	}

	if err := checkFieldNameCollisions(spec, collisionConfig()); err != nil {
		t.Fatalf("expected no error for a collision across two different inline namespaces, got: %v", err)
	}
}

// TestGenerateFailsOnAllOfInlineMemberFieldCollision: an inline (non-$ref)
// object as an allOf member (Order.combined). Not explicitly requested by
// the review, but added for the same reason as oneOf/anyOf: schemaToTSType's
// AllOf handling (generator.go's schemaToTSType, the "&"-joined branch)
// calls schemaToTSType on every member exactly like OneOf/AnyOf do, so an
// inline allOf member is an identical collision surface.
//
// The expected key here was originally "Order.combined.allOf0.full_name" --
// a fix round 3 review found that this was itself a phantom key: the codec
// table never builds an entry under an "id.allOf<index>" namespace for ANY
// allOf shape (allOfEntry flattens every member's top-level properties
// straight into "id" -- "Order.combined" here, since these are the
// member's own DIRECT properties, not nested one level deeper). The
// correct, now-fixed key is "Order.combined.full_name", matching exactly
// what checkFlattenedAllOfCollisions (fieldname.go) and allOfEntry
// (codecs.go) both compute, since they now share flattenAllOfLayers as one
// source of truth for the namespace.
func TestGenerateFailsOnAllOfInlineMemberFieldCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Order": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"combined": {
						AllOf: []*client.Schema{
							{
								Type: "object",
								Properties: map[string]*client.Schema{
									"full_name": {Type: "string"},
									"fullName":  {Type: "string"},
								},
							},
						},
					},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for a field-name collision inside an allOf inline member")
	}

	for _, want := range []string{"full_name", "fullName", `FieldOverrides["Order.combined.full_name"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}

	// Prove the key actually resolves it -- exactly the property Finding 1
	// found was violated when the printed key named a phantom namespace.
	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Order.combined.full_name": "fullNameAlt"}

	if _, err := NewGenerator().Generate(context.Background(), spec, config); err != nil {
		t.Fatalf("expected the printed FieldOverrides key to resolve the collision, got: %v", err)
	}
}

// --- Fix round 2, CRITICAL 2: collisions ACROSS allOf members -----------
//
// TestGenerateFailsOnAllOfInlineMemberFieldCollision (above) checks each
// AllOf member as its OWN isolated namespace ("id.allOf<index>") -- correct
// for a collision WITHIN one member's own properties, but codecs.go's
// allOfEntry does not keep members isolated: it FLATTENS every member's
// properties into ONE merged table entry keyed by the allOf schema's own
// id. Two DIFFERENT wire names on two DIFFERENT members that resolve to the
// same client name is therefore a real collision in that single merged
// namespace, invisible to the per-member check because it never compares
// one member's properties against another's. A prior fix round found and
// fixed the codec-table SYMPTOM of the resulting data loss (silently
// dropping one member's shape once renaming lands) but never taught THIS
// guard to check the same flattened namespace -- so the guard and the
// table disagreed about what the allOf namespace even was, which is how it
// went undetected through a full review round.

// allOfFlattenedCollisionSpec returns Base (declaring street_name) and Addr
// = allOf[$ref Base, inline{streetName}] -- a $ref member colliding with an
// inline one, which the review measured directly. Callers that need the
// pure-inline-vs-inline variant build Addr without Base themselves (see
// TestGenerateFailsOnAllOfFlattenedInlineInlineCollision).
func allOfFlattenedCollisionSpec() *client.APISpec {
	return &client.APISpec{
		Info: client.APIInfo{Title: "Flattened Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Base": {Type: "object", Properties: map[string]*client.Schema{"street_name": {Type: "string"}}},
			"Addr": {
				AllOf: []*client.Schema{
					{Ref: "#/components/schemas/Base"},
					{Type: "object", Properties: map[string]*client.Schema{"streetName": {Type: "string"}}},
				},
			},
		},
	}
}

// TestGenerateFailsOnAllOfFlattenedInlineInlineCollision: BOTH members
// inline (no $ref at all) -- the review's first measured variant. Before
// this fix: checkFieldNameCollisions returned nil, Generate returned nil,
// zero warnings, for an allOf whose emitted codec entry is
// `{"fields": {"streetName": ..., "street_name": ...}}` -- both wire names
// present, unrenamed, in the SAME merged entry.
func TestGenerateFailsOnAllOfFlattenedInlineInlineCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Flattened Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Addr": {
				AllOf: []*client.Schema{
					{Type: "object", Properties: map[string]*client.Schema{"street_name": {Type: "string"}}},
					{Type: "object", Properties: map[string]*client.Schema{"streetName": {Type: "string"}}},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for wire names colliding across two INLINE allOf members")
	}

	for _, want := range []string{"street_name", "streetName", `FieldOverrides["Addr.streetName"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestGenerateFailsOnAllOfFlattenedRefInlineCollision: one $ref member
// (Base, declaring street_name) and one inline member (declaring
// streetName) -- the review's second measured variant.
func TestGenerateFailsOnAllOfFlattenedRefInlineCollision(t *testing.T) {
	spec := allOfFlattenedCollisionSpec()

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for wire names colliding between a $ref allOf member and an inline one")
	}

	for _, want := range []string{"street_name", "streetName", `FieldOverrides["Addr.streetName"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}

// TestGenerateAllowsAllOfFlattenedNonCollidingFields is the false-positive
// guard the review explicitly asked for: a $ref member and an inline member
// declaring genuinely DIFFERENT fields (street_name vs zip_code) must not
// be reported as a collision just because they are both allOf members.
func TestGenerateAllowsAllOfFlattenedNonCollidingFields(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Flattened Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Base": {Type: "object", Properties: map[string]*client.Schema{"street_name": {Type: "string"}}},
			"Addr": {
				AllOf: []*client.Schema{
					{Ref: "#/components/schemas/Base"},
					{Type: "object", Properties: map[string]*client.Schema{"zip_code": {Type: "string"}}},
				},
			},
		},
	}

	if err := checkFieldNameCollisions(spec, collisionConfig()); err != nil {
		t.Fatalf("expected no collision for genuinely non-colliding allOf members, got: %v", err)
	}
}

// TestAllOfFlattenedFieldOverrideKeyActuallyResolvesCollision proves the
// key printed above is not just cosmetically plausible: pasting it into
// FieldOverrides verbatim must make the collision go away. checkFlattenedAllOfCollisions
// calls tsFieldName with the allOf schema's OWN id (never a per-layer id),
// matching exactly how allOfEntry keys the merged table entry -- so
// "Addr" + "." + "streetName" is the same key a caller would paste in.
func TestAllOfFlattenedFieldOverrideKeyActuallyResolvesCollision(t *testing.T) {
	spec := allOfFlattenedCollisionSpec()

	config := collisionConfig()
	config.FieldOverrides = map[string]string{"Addr.streetName": "streetNameAlt"}

	if _, err := NewGenerator().Generate(context.Background(), spec, config); err != nil {
		t.Fatalf("expected the printed FieldOverrides key to resolve the flattened allOf collision, got: %v", err)
	}
}

// TestGenerateFailsOnBothFieldNamesInOneObjectControl is the control case
// the review asked to confirm still works: two colliding wire names
// declared directly on ONE object (no allOf at all) must still error --
// proving the flattened-allOf addition didn't accidentally change the
// ordinary, non-allOf collision path.
func TestGenerateFailsOnBothFieldNamesInOneObjectControl(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Flattened Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Addr": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"street_name": {Type: "string"},
					"streetName":  {Type: "string"},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for two colliding wire names declared directly on one object")
	}

	// sortedKeys visits "streetName" before "street_name" ('N' < '_'), so it
	// is the first claimer; "street_name" is the one flagged.
	if !strings.Contains(err.Error(), `FieldOverrides["Addr.street_name"]`) {
		t.Errorf("error message missing FieldOverrides key; got: %s", err.Error())
	}
}

// --- Fix round 3, FINDING 1: nested inline composite inside an allOf member ---

// extractFieldOverrideKey pulls the literal string out of the first
// `FieldOverrides["..."]` in msg, so a test can both assert on it AND use
// it as an actual map key -- proving the key is not just plausible-looking
// but genuinely the one tsFieldName will look up.
func extractFieldOverrideKey(t *testing.T, msg string) string {
	t.Helper()

	const marker = `FieldOverrides["`

	idx := strings.Index(msg, marker)
	if idx == -1 {
		t.Fatalf("no FieldOverrides key found in message: %s", msg)
	}

	rest := msg[idx+len(marker):]

	end := strings.Index(rest, `"]`)
	if end == -1 {
		t.Fatalf("malformed FieldOverrides key in message: %s", msg)
	}

	return rest[:end]
}

// TestGenerateFailsOnAllOfNestedInlineObjectCollision is FINDING 1's
// regression guard: Addr = allOf[{payload: {full_name, fullName}}] -- a
// collision nested ONE LEVEL INSIDE an inline allOf member's own property,
// not at the member's top level. Before this fix, the only message printed
// named "Addr.allOf0.payload.full_name" -- a namespace the codec table
// never builds (the table keys this object "Addr.payload", since
// allOfEntry flattens the member's top-level "payload" property using the
// SAME "<id>.<prop>" derivation codecIDFor uses everywhere else). Applying
// that phantom key made Generate succeed while the collision -- and the
// rename-time data loss it causes -- was still fully live.
//
// The rule this test holds the fix to, verbatim from the review: "every
// key this guard prints must name a namespace that actually exists in the
// emitted table." It extracts the printed key, looks up its namespace
// (everything before the last ".") in the emitted CODECS table, and fails
// if that namespace is absent.
func TestGenerateFailsOnAllOfNestedInlineObjectCollision(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Flattened Nested Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Addr": {
				AllOf: []*client.Schema{
					{
						Type: "object",
						Properties: map[string]*client.Schema{
							"payload": {
								Type: "object",
								Properties: map[string]*client.Schema{
									"full_name": {Type: "string"},
									"fullName":  {Type: "string"},
								},
							},
						},
					},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for a field-name collision nested inside an allOf member's own inline object")
	}

	for _, want := range []string{"full_name", "fullName", `FieldOverrides["Addr.payload.full_name"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}

	key := extractFieldOverrideKey(t, err.Error()) // "Addr.payload.full_name"

	lastDot := strings.LastIndex(key, ".")
	if lastDot == -1 {
		t.Fatalf("printed key %q has no namespace component", key)
	}

	namespace := key[:lastDot] // "Addr.payload"

	code := codecLayerText(spec, collisionConfig())
	if !strings.Contains(code, `"`+namespace+`":`) {
		t.Fatalf("printed key names namespace %q, which is absent from the emitted CODECS table:\n%s", namespace, code)
	}

	// The printed key must actually resolve the collision when pasted in.
	config := collisionConfig()
	config.FieldOverrides = map[string]string{key: "fullNameAlt"}

	if _, err := NewGenerator().Generate(context.Background(), spec, config); err != nil {
		t.Fatalf("expected the printed FieldOverrides key to resolve the nested collision, got: %v", err)
	}
}

// --- Fix round 3, FINDING 3: no more double-reporting with a bogus second key ---

// TestGenerateDedupesOwnPropertiesAllOfDuplicateMessage: a schema declaring
// BOTH its own direct Properties (with an internal collision) AND an AllOf
// (legal, unusual) triggers the SAME collision message from two
// independent passes -- the top-level own-Properties check, and
// checkFlattenedAllOfCollisions, which includes the schema's own
// Properties as the allOf's last layer. Both messages are byte-identical
// (same schema id, same wire names, same key), so checkFieldNameCollisions
// dedupes them rather than printing the same actionable message twice.
func TestGenerateDedupesOwnPropertiesAllOfDuplicateMessage(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Dedup API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Base": {Type: "object", Properties: map[string]*client.Schema{"other": {Type: "string"}}},
			"Addr": {
				Type: "object",
				Properties: map[string]*client.Schema{
					"full_name": {Type: "string"},
					"fullName":  {Type: "string"},
				},
				AllOf: []*client.Schema{
					{Ref: "#/components/schemas/Base"},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for the own-properties collision")
	}

	count := strings.Count(err.Error(), `wire names "fullName" and "full_name"`)
	if count != 1 {
		t.Errorf("expected the own-properties collision reported exactly once (not duplicated by the allOf pass), got %d occurrences; message:\n%s", count, err.Error())
	}
}

// TestGenerateFailsOnAllOfCollisionSpanningMembersOneAndThree pins that
// removing the old per-member "id.allOf<index>" loop did not lose
// cross-member detection for a collision that spans two members that are
// NOT adjacent in declaration order (member 0 and member 2 of three) --
// the flattened pass compares every layer against every other regardless
// of position, so this is not expected to behave differently from the
// adjacent-member case, but it is cheap to pin directly rather than assume.
func TestGenerateFailsOnAllOfCollisionSpanningMembersOneAndThree(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "Spanning Collision API", Version: "1.0.0"},
		Schemas: map[string]*client.Schema{
			"Addr": {
				AllOf: []*client.Schema{
					{Type: "object", Properties: map[string]*client.Schema{"street_name": {Type: "string"}}},
					{Type: "object", Properties: map[string]*client.Schema{"zip_code": {Type: "string"}}},
					{Type: "object", Properties: map[string]*client.Schema{"streetName": {Type: "string"}}},
				},
			},
		},
	}

	err := checkFieldNameCollisions(spec, collisionConfig())
	if err == nil {
		t.Fatal("expected an error for wire names colliding across the first and third allOf members")
	}

	for _, want := range []string{"street_name", "streetName", `FieldOverrides["Addr.streetName"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}
