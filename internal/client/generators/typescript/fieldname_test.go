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
// such a value schema under "<parentID>.values", reused here.
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

	for _, want := range []string{"display_name", "displayName", `FieldOverrides["Order.extras.values.display_name"]`} {
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
// inline allOf member is an identical collision surface and the walk above
// treats it the same way ("<id>.allOf<index>").
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

	for _, want := range []string{"full_name", "fullName", `FieldOverrides["Order.combined.allOf0.full_name"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q; got: %s", want, err.Error())
		}
	}
}
