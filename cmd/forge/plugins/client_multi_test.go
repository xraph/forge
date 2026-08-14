// v2/cmd/forge/plugins/client_multi_test.go
package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// basePlan is a resolved single-client plan, the state expandClients receives.
// The field values are deliberately non-zero so an inheritance assertion can
// tell "carried over from the base" apart from "left at the zero value".
func basePlan(clients []ClientGenConfig) *generationPlan {
	return &generationPlan{
		specPaths: []string{"/tmp/openapi.json"},
		outputDir: "./client",
		clients:   clients,
		config: client.GeneratorConfig{
			Language:    "typescript",
			OutputDir:   "./client",
			PackageName: "@acme/client",
			BaseURL:     "https://api.example.com",
			FieldNaming: client.NamingCamel,
			Hooks:       true,
			PathFilter: client.PathFilter{
				Include: []string{"/v1/**"},
			},
		},
	}
}

// TestExpandClientsWithoutBlock is the property that makes expandClients safe to
// call from generate, check and watch unconditionally: a config that never
// mentions clients: has to come out the far side completely unchanged.
func TestExpandClientsWithoutBlock(t *testing.T) {
	base := basePlan(nil)

	plans, err := expandClients(base)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Same(t, base, plans[0], "the base plan itself should be returned, not a copy")
}

func TestExpandClientsSplitsByService(t *testing.T) {
	base := basePlan([]ClientGenConfig{
		{
			Name:    "twinos",
			Output:  "./forge/twinos",
			Package: "@repo/twinos-client",
			Include: []string{"/twinos/**"},
		},
		{
			Name:    "studio",
			Output:  "./forge/studio",
			Package: "@repo/studio-client",
			Include: []string{"/studio/**"},
		},
	})

	plans, err := expandClients(base)
	require.NoError(t, err)
	require.Len(t, plans, 2)

	assert.Equal(t, "twinos", plans[0].name)
	assert.Equal(t, "./forge/twinos", plans[0].outputDir)
	assert.Equal(t, "./forge/twinos", plans[0].config.OutputDir)
	assert.Equal(t, "@repo/twinos-client", plans[0].config.PackageName)
	assert.Equal(t, []string{"/twinos/**"}, plans[0].config.PathFilter.Include)

	assert.Equal(t, "studio", plans[1].name)
	assert.Equal(t, []string{"/studio/**"}, plans[1].config.PathFilter.Include)

	// The filter is the whole point of the split. Two clients sharing one
	// GeneratorConfig value by reference would give both the last filter
	// written, and the bug would look like "the split silently did nothing".
	assert.NotEqual(t, plans[0].config.PathFilter.Include, plans[1].config.PathFilter.Include)
}

// TestExpandClientsInherits pins the shorthand that keeps a clients: block to a
// name, an output and a filter: everything else comes from the base.
func TestExpandClientsInherits(t *testing.T) {
	base := basePlan([]ClientGenConfig{
		{Name: "twinos", Output: "./forge/twinos", Include: []string{"/twinos/**"}},
	})

	plans, err := expandClients(base)
	require.NoError(t, err)
	require.Len(t, plans, 1)

	got := plans[0].config
	assert.Equal(t, "typescript", got.Language)
	assert.Equal(t, "@acme/client", got.PackageName)
	assert.Equal(t, "https://api.example.com", got.BaseURL)
	assert.Equal(t, client.NamingCamel, got.FieldNaming)
	assert.True(t, got.Hooks)
}

// TestExpandClientsInheritsFilterWhenUnset covers the other half of the replace
// rule: a client naming neither include nor exclude keeps the base filter,
// rather than silently widening to the whole specification.
func TestExpandClientsInheritsFilterWhenUnset(t *testing.T) {
	base := basePlan([]ClientGenConfig{
		{Name: "everything", Output: "./forge/all"},
	})

	plans, err := expandClients(base)
	require.NoError(t, err)
	assert.Equal(t, []string{"/v1/**"}, plans[0].config.PathFilter.Include)
}

// TestExpandClientsHooksOptOut is why Hooks is a pointer: false and unset differ
// only for a client opting out of a layer the defaults turned on.
func TestExpandClientsHooksOptOut(t *testing.T) {
	off := false
	base := basePlan([]ClientGenConfig{
		{Name: "plain", Output: "./forge/plain", Hooks: &off},
		{Name: "cached", Output: "./forge/cached"},
	})

	plans, err := expandClients(base)
	require.NoError(t, err)
	assert.False(t, plans[0].config.Hooks, "an explicit hooks: false must override the default")
	assert.True(t, plans[1].config.Hooks, "an absent hooks: must inherit the default")
}

// The three refusals below all guard the same failure: several clients writing
// into one directory, where the last to run wins and the result looks like a
// clean generation of a client nobody asked for.
func TestExpandClientsRefusals(t *testing.T) {
	cases := []struct {
		name    string
		plan    *generationPlan
		wantMsg string
	}{
		{
			name: "two clients sharing an output",
			plan: basePlan([]ClientGenConfig{
				{Name: "a", Output: "./forge/x"},
				{Name: "b", Output: "./forge/x"},
			}),
			wantMsg: "both generate into",
		},
		{
			name: "output differing only by a trailing separator",
			plan: basePlan([]ClientGenConfig{
				{Name: "a", Output: "./forge/x"},
				{Name: "b", Output: "./forge/x/"},
			}),
			wantMsg: "both generate into",
		},
		{
			name: "a client with no output",
			plan: basePlan([]ClientGenConfig{
				{Name: "a"},
			}),
			wantMsg: "declares no output",
		},
		{
			name: "a duplicated name",
			plan: basePlan([]ClientGenConfig{
				{Name: "a", Output: "./forge/x"},
				{Name: "a", Output: "./forge/y"},
			}),
			wantMsg: "twice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := expandClients(tc.plan)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// TestExpandClientsRefusesPinnedOutput: --output names one directory, a clients:
// block names several, and there is no reading of the pair that honours both.
func TestExpandClientsRefusesPinnedOutput(t *testing.T) {
	base := basePlan([]ClientGenConfig{
		{Name: "twinos", Output: "./forge/twinos"},
	})
	base.pinnedOutput = true

	_, err := expandClients(base)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--output cannot be combined")

	// Without a clients: block the flag is the ordinary way to say where the
	// client goes, and must keep working.
	plain := basePlan(nil)
	plain.pinnedOutput = true

	plans, err := expandClients(plain)
	require.NoError(t, err)
	assert.Len(t, plans, 1)
}

func TestSelectClients(t *testing.T) {
	base := basePlan([]ClientGenConfig{
		{Name: "twinos", Output: "./forge/twinos"},
		{Name: "studio", Output: "./forge/studio"},
		{Name: "portal", Output: "./forge/portal"},
	})

	plans, err := expandClients(base)
	require.NoError(t, err)

	t.Run("no selection means all", func(t *testing.T) {
		got, err := selectClients(plans, nil)
		require.NoError(t, err)
		assert.Len(t, got, 3)
	})

	t.Run("selection narrows, in the order given", func(t *testing.T) {
		got, err := selectClients(plans, []string{"portal", "twinos"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "portal", got[0].name)
		assert.Equal(t, "twinos", got[1].name)
	})

	t.Run("an unknown name names the ones that exist", func(t *testing.T) {
		_, err := selectClients(plans, []string{"twinos", "atlas"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no client named "atlas"`)
		assert.Contains(t, err.Error(), "twinos, studio, portal")
	})
}
