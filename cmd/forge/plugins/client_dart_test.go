package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

func TestNewClientGeneratorRegistersDart(t *testing.T) {
	gen, err := newClientGenerator()
	require.NoError(t, err)

	assert.Contains(t, gen.ListGenerators(), "dart")
}

func TestReservedIdentifiersForDart(t *testing.T) {
	reserved := reservedIdentifiers("dart")

	for _, name := range []string{"ApiError", "RestClient", "Value", "QueryState"} {
		assert.True(t, reserved[name], "dart reserved identifiers lack %q", name)
	}
}

func TestParseInt64Mode(t *testing.T) {
	cases := []struct {
		value   string
		want    client.Int64Mode
		wantErr bool
	}{
		{value: "", want: ""},
		{value: "string", want: client.Int64String},
		{value: "int", want: client.Int64Int},
		{value: "bigint", wantErr: true},
	}

	for _, c := range cases {
		got, err := parseInt64Mode(c.value)

		if c.wantErr {
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.value)

			continue
		}

		require.NoError(t, err)
		assert.Equal(t, c.want, got)
	}
}

func TestExpandClientsClientOnlyAndInt64Overrides(t *testing.T) {
	on, off := true, false

	base := basePlan([]ClientGenConfig{
		{Name: "dart", Language: "dart", Output: "./flutter/dart", ClientOnly: &off, Int64: "int"},
		{Name: "ts", Output: "./forge/ts"},
		{Name: "only", Output: "./forge/only", ClientOnly: &on},
	})
	base.config.ClientOnly = true

	plans, err := expandClients(base)
	require.NoError(t, err)

	assert.False(t, plans[0].config.ClientOnly, "an explicit client_only: false must override the default")
	assert.Equal(t, client.Int64Int, plans[0].config.Int64)
	assert.Equal(t, "dart", plans[0].config.Language)
	assert.True(t, plans[1].config.ClientOnly, "an absent client_only must inherit the default")
	assert.Equal(t, client.Int64Mode(""), plans[1].config.Int64)
	assert.True(t, plans[2].config.ClientOnly)
}

func TestLoadClientConfigReadsInt64AndClientOnly(t *testing.T) {
	yaml := `defaults:
  language: dart
  int64: int
  client_only: true
clients:
  - name: dart
    language: dart
    output: ./flutter/dart
    int64: string
    client_only: false
  - name: plain
    language: dart
    output: ./flutter/plain
`

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".forge-client.yml"), []byte(yaml), 0o600))

	cfg, err := LoadClientConfig(dir)
	require.NoError(t, err)

	assert.Equal(t, "int", cfg.Defaults.Int64)
	assert.True(t, cfg.Defaults.ClientOnly)
	require.Len(t, cfg.Clients, 2)
	assert.Equal(t, "string", cfg.Clients[0].Int64)
	require.NotNil(t, cfg.Clients[0].ClientOnly)
	assert.False(t, *cfg.Clients[0].ClientOnly)
	assert.Nil(t, cfg.Clients[1].ClientOnly, "an absent client_only must stay nil so it inherits")
}
