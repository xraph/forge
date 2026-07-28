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
