// v2/cmd/forge/plugins/client_field_config_test.go
package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// TestParseFieldNaming covers --field-naming's resolution: the four
// recognised strategies, the empty/unset case (which must pass through as
// client.NamingStrategy("") so the library's own effectiveFieldNaming
// still applies its per-language default), and -- the case this flag
// exists specifically to get right where the library layer does not --
// an unrecognised value must be REJECTED, not silently treated as
// "preserve" the way effectiveFieldNaming does for a hand-built
// GeneratorConfig (fieldname.go).
func TestParseFieldNaming(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    client.NamingStrategy
		wantErr bool
	}{
		{name: "empty is unset, not an error", value: "", want: ""},
		{name: "camel", value: "camel", want: client.NamingCamel},
		{name: "pascal", value: "pascal", want: client.NamingPascal},
		{name: "snake", value: "snake", want: client.NamingSnake},
		{name: "preserve", value: "preserve", want: client.NamingPreserve},
		{name: "unrecognised value is rejected, not silently preserved", value: "cammel", wantErr: true},
		{name: "case-sensitive: Camel is not camel", value: "Camel", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFieldNaming(c.value)

			if c.wantErr {
				require.Error(t, err, "parseFieldNaming(%q) should have been rejected", c.value)
				assert.Contains(t, err.Error(), c.value, "error should quote the offending value")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestParseFieldOverrides covers --field-overrides' comma-separated
// "key=clientName" format: the empty case (nil map, not an error -- this is
// the common "flag omitted" path), a single global override, a
// schema-scoped override (the "Schema.wire_name" key format
// client.GeneratorConfig.FieldOverrides itself uses), multiple
// comma-separated entries, and malformed entries (missing "=", empty key,
// empty value) that must be rejected with the exact offending entry named
// rather than silently skipped or partially applied.
func TestParseFieldOverrides(t *testing.T) {
	t.Run("empty value returns nil, not an error", func(t *testing.T) {
		got, err := parseFieldOverrides("")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("whitespace-only value returns nil, not an error", func(t *testing.T) {
		got, err := parseFieldOverrides("   ")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("single global override", func(t *testing.T) {
		got, err := parseFieldOverrides("api_key=apiKey")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"api_key": "apiKey"}, got)
	})

	t.Run("schema-scoped override", func(t *testing.T) {
		got, err := parseFieldOverrides("User.user_id=userIdentifier")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"User.user_id": "userIdentifier"}, got)
	})

	t.Run("multiple comma-separated entries, mixed scoped and global", func(t *testing.T) {
		got, err := parseFieldOverrides("User.user_id=userIdentifier,api_key=apiKey")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"User.user_id": "userIdentifier",
			"api_key":      "apiKey",
		}, got)
	})

	t.Run("surrounding whitespace around entries and around = is tolerated", func(t *testing.T) {
		got, err := parseFieldOverrides(" User.user_id = userIdentifier , api_key = apiKey ")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"User.user_id": "userIdentifier",
			"api_key":      "apiKey",
		}, got)
	})

	t.Run("missing = is rejected", func(t *testing.T) {
		_, err := parseFieldOverrides("api_key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "api_key")
	})

	t.Run("empty key is rejected", func(t *testing.T) {
		_, err := parseFieldOverrides("=apiKey")
		require.Error(t, err)
	})

	t.Run("empty value is rejected", func(t *testing.T) {
		_, err := parseFieldOverrides("api_key=")
		require.Error(t, err)
	})

	t.Run("one malformed entry among valid ones still fails the whole flag", func(t *testing.T) {
		// Partially applying overrides -- silently keeping the valid ones and
		// dropping the malformed one -- would be exactly the kind of silent
		// rename-that-never-happens this CLI validation exists to prevent.
		_, err := parseFieldOverrides("api_key=apiKey,broken")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "broken")
	})
}
