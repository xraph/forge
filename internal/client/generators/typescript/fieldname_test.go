package typescript

import (
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
