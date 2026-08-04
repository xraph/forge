// v2/cmd/forge/plugins/client_hooks_config_test.go
package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveHooks covers the --hooks / --react-query alias fold. The layer is
// one switch reachable by four names (two flags, two config keys), so the
// interesting cases are not "does true mean true" but the two asymmetries
// deliberately built into resolveHooks:
//
//   - --react-query=false still counts as *using* the deprecated name and must
//     report deprecated, even though it enables nothing. A user who typed the
//     retired flag is looking right at it, which is the one moment the notice
//     is worth printing.
//   - react_query: false in a config file must NOT report deprecated. yaml
//     gives an absent key and an explicit false the same zero value, so a
//     false there is indistinguishable from a config that never mentioned it —
//     warning on it would fire on every run of every project that never used
//     the flag at all.
func TestResolveHooks(t *testing.T) {
	cases := []struct {
		name              string
		hooksFlag         bool
		reactQueryFlag    bool
		reactQueryFlagSet bool
		cfgHooks          bool
		cfgReactQuery     bool
		wantEnabled       bool
		wantDeprecated    bool
	}{
		{
			name: "nothing set at all",
		},
		{
			name:        "--hooks alone",
			hooksFlag:   true,
			wantEnabled: true,
		},
		{
			name:        "hooks: true in config alone",
			cfgHooks:    true,
			wantEnabled: true,
		},
		{
			name:              "deprecated --react-query alone still enables the layer",
			reactQueryFlag:    true,
			reactQueryFlagSet: true,
			wantEnabled:       true,
			wantDeprecated:    true,
		},
		{
			name:           "deprecated react_query: true in config still enables the layer",
			cfgReactQuery:  true,
			wantEnabled:    true,
			wantDeprecated: true,
		},
		{
			name:              "--react-query=false enables nothing but is still a deprecated name",
			reactQueryFlag:    false,
			reactQueryFlagSet: true,
			wantEnabled:       false,
			wantDeprecated:    true,
		},
		{
			name:          "react_query: false in config is indistinguishable from absent, so no notice",
			cfgReactQuery: false,
		},
		{
			name:              "both names together are not a conflict",
			hooksFlag:         true,
			reactQueryFlag:    true,
			reactQueryFlagSet: true,
			wantEnabled:       true,
			wantDeprecated:    true,
		},
		{
			name:              "new flag on, old flag explicitly off: still on, still warned",
			hooksFlag:         true,
			reactQueryFlagSet: true,
			wantEnabled:       true,
			wantDeprecated:    true,
		},
		{
			name:        "config supplies hooks, flags absent",
			cfgHooks:    true,
			wantEnabled: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			enabled, deprecated := resolveHooks(
				c.hooksFlag,
				c.reactQueryFlag,
				c.reactQueryFlagSet,
				c.cfgHooks,
				c.cfgReactQuery,
			)

			assert.Equal(t, c.wantEnabled, enabled, "enabled")
			assert.Equal(t, c.wantDeprecated, deprecated, "deprecated")
		})
	}
}

// TestLoadClientConfigReadsBothHookKeys is the other half of the compatibility
// promise: resolveHooks can only honour react_query if the yaml layer still
// parses it. An existing .forge-client.yml written before the rename must
// select exactly what a migrated one selects.
func TestLoadClientConfigReadsBothHookKeys(t *testing.T) {
	cases := []struct {
		name           string
		yaml           string
		wantHooks      bool
		wantReactQuery bool
	}{
		{
			name:      "new hooks key",
			yaml:      "defaults:\n  language: typescript\n  hooks: true\n",
			wantHooks: true,
		},
		{
			name:           "deprecated react_query key",
			yaml:           "defaults:\n  language: typescript\n  react_query: true\n",
			wantReactQuery: true,
		},
		{
			name: "neither key",
			yaml: "defaults:\n  language: typescript\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".forge-client.yml"), []byte(c.yaml), 0o600))

			cfg, err := LoadClientConfig(dir)
			require.NoError(t, err)

			assert.Equal(t, c.wantHooks, cfg.Defaults.Hooks, "Defaults.Hooks")
			assert.Equal(t, c.wantReactQuery, cfg.Defaults.ReactQuery, "Defaults.ReactQuery")

			// Whichever key was used, the resolved gate must agree — this is
			// the property a user upgrading across the rename actually cares
			// about, and it is what generateClient passes to the generator.
			enabled, _ := resolveHooks(false, false, false, cfg.Defaults.Hooks, cfg.Defaults.ReactQuery)
			assert.Equal(t, c.wantHooks || c.wantReactQuery, enabled, "resolved hooks gate")
		})
	}
}
