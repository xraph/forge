package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestFacadesEmitOneLinePerEndpoint(t *testing.T) {
	out := NewFacadeGenerator().Generate(manifestSpec(), client.GeneratorConfig{})

	for _, want := range []string{
		"import { query, mutation } from '@forge/client-core';",
		"import { ops } from './ops';",
		"export const useOrderList = query(ops.orderList);",
		"export const useOrderCreate = mutation(ops.orderCreate);",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("hooks.ts missing %q\n\n%s", want, out)
		}
	}
}

func TestFacadesUseQueryForReadsOnly(t *testing.T) {
	spec := &client.APISpec{Endpoints: []client.Endpoint{
		{ID: "a", Method: "GET"},
		{ID: "b", Method: "HEAD"},
		{ID: "c", Method: "POST"},
		{ID: "d", Method: "DELETE"},
	}}

	out := NewFacadeGenerator().Generate(spec, client.GeneratorConfig{})

	if strings.Count(out, "= query(") != 2 {
		t.Fatalf("want two queries (GET, HEAD)\n\n%s", out)
	}

	if strings.Count(out, "= mutation(") != 2 {
		t.Fatalf("want two mutations (POST, DELETE)\n\n%s", out)
	}
}

func TestFacadesAreDeterministic(t *testing.T) {
	gen := NewFacadeGenerator()

	first := gen.Generate(manifestSpec(), client.GeneratorConfig{})
	for i := 0; i < 50; i++ {
		if got := gen.Generate(manifestSpec(), client.GeneratorConfig{}); got != first {
			t.Fatal("hooks.ts differs between runs")
		}
	}
}

// TestGeneratorEmitsHookFacadesWhenHooksEnabled is the integration test
// the retired query_internal_test.go's TestPeerDependencyOnlyWhenGenerated
// left uncovered: every other test in this package (facades_test.go,
// opsmanifest_test.go) calls a single generator's Generate directly, which
// never exercises the config.HooksEnabled() gate in generator.go's Generate,
// generatePackageJSON, or getDependencies at all. Without this test, that
// whole wiring path -- ops.ts/hooks.ts emission, the package.json dependency
// entry, and the getDependencies metadata -- could regress silently.
func TestGeneratorEmitsHookFacadesWhenHooksEnabled(t *testing.T) {
	cfg := baseConfig()
	cfg.Hooks = true

	out, err := NewGenerator().Generate(context.Background(), manifestSpec(), cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, ok := out.Files["src/ops.ts"]; !ok {
		t.Fatal("expected src/ops.ts to be generated when Hooks is enabled")
	}

	if _, ok := out.Files["src/hooks.ts"]; !ok {
		t.Fatal("expected src/hooks.ts to be generated when Hooks is enabled")
	}

	if _, ok := out.Files["src/query.ts"]; ok {
		t.Fatal("src/query.ts should no longer be generated -- the TanStack layer is retired")
	}

	pkgJSON, ok := out.Files["package.json"]
	if !ok {
		t.Fatal("expected package.json to be generated")
	}

	if !strings.Contains(pkgJSON, "@forge/client-core") {
		t.Fatalf("package.json missing the @forge/client-core dependency\n\n%s", pkgJSON)
	}

	if strings.Contains(pkgJSON, "@tanstack") {
		t.Fatalf("package.json still references @tanstack, the retired peer dependency\n\n%s", pkgJSON)
	}

	foundClientCoreDep := false

	for _, dep := range out.Dependencies {
		if dep.Name == "@forge/client-core" {
			foundClientCoreDep = true
		}
	}

	if !foundClientCoreDep {
		t.Fatalf("expected @forge/client-core in genClient.Dependencies, got %v", out.Dependencies)
	}
}

// TestGeneratorOmitsHookFacadesWhenHooksDisabled is the negative half of
// the test above: with the flag off, neither file should appear -- matching
// the pre-retirement behaviour where src/query.ts was likewise absent.
func TestGeneratorOmitsHookFacadesWhenHooksDisabled(t *testing.T) {
	cfg := baseConfig()
	cfg.Hooks = false

	out, err := NewGenerator().Generate(context.Background(), manifestSpec(), cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, ok := out.Files["src/ops.ts"]; ok {
		t.Fatal("src/ops.ts should not be generated when Hooks is disabled")
	}

	if _, ok := out.Files["src/hooks.ts"]; ok {
		t.Fatal("src/hooks.ts should not be generated when Hooks is disabled")
	}

	pkgJSON, ok := out.Files["package.json"]
	if !ok {
		t.Fatal("expected package.json to be generated")
	}

	if strings.Contains(pkgJSON, "@forge/client-core") {
		t.Fatalf("package.json should not depend on @forge/client-core when Hooks is disabled\n\n%s", pkgJSON)
	}
}

// TestDeprecatedReactQueryFieldMatchesHooks pins the compatibility promise
// made when Hooks took over from ReactQuery: a caller who set the old field
// and has not migrated must get byte-identical output, not merely "hooks.ts
// exists too".
//
// Asserting on the whole file set rather than a couple of filenames is
// deliberate. The alias is honoured in one place (GeneratorConfig.HooksEnabled)
// but read in four (generator.go :300, :463, :1435, :1511), and a later edit
// that reintroduces a direct config.Hooks read at any one of them would still
// pass a "is src/hooks.ts present?" check while quietly dropping the
// package.json dependency or the index.ts re-export.
func TestDeprecatedReactQueryFieldMatchesHooks(t *testing.T) {
	hooksCfg := baseConfig()
	hooksCfg.Hooks = true

	aliasCfg := baseConfig()
	aliasCfg.ReactQuery = true

	viaHooks, err := NewGenerator().Generate(context.Background(), manifestSpec(), hooksCfg)
	if err != nil {
		t.Fatalf("Generate with Hooks: %v", err)
	}

	viaAlias, err := NewGenerator().Generate(context.Background(), manifestSpec(), aliasCfg)
	if err != nil {
		t.Fatalf("Generate with deprecated ReactQuery: %v", err)
	}

	if len(viaAlias.Files) != len(viaHooks.Files) {
		t.Fatalf("deprecated ReactQuery generated %d files, Hooks generated %d", len(viaAlias.Files), len(viaHooks.Files))
	}

	for name, want := range viaHooks.Files {
		got, ok := viaAlias.Files[name]
		if !ok {
			t.Fatalf("deprecated ReactQuery did not generate %s", name)
		}

		if got != want {
			t.Fatalf("deprecated ReactQuery generated a different %s\n\nwant:\n%s\n\ngot:\n%s", name, want, got)
		}
	}

	if len(viaAlias.Dependencies) != len(viaHooks.Dependencies) {
		t.Fatalf("dependency metadata differs: alias %v, Hooks %v", viaAlias.Dependencies, viaHooks.Dependencies)
	}

	for i, want := range viaHooks.Dependencies {
		if viaAlias.Dependencies[i] != want {
			t.Fatalf("dependency %d differs: alias %v, Hooks %v", i, viaAlias.Dependencies[i], want)
		}
	}
}

// TestHooksEnabledHonoursBothFields is the unit-level truth table behind the
// integration test above. Setting both fields is not an error and not a
// conflict: they name the same switch, so any "on" wins.
func TestHooksEnabledHonoursBothFields(t *testing.T) {
	cases := []struct {
		name       string
		hooks      bool
		reactQuery bool
		want       bool
	}{
		{name: "neither set", want: false},
		{name: "Hooks only", hooks: true, want: true},
		{name: "deprecated ReactQuery only", reactQuery: true, want: true},
		{name: "both set", hooks: true, reactQuery: true, want: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := client.GeneratorConfig{Hooks: c.hooks, ReactQuery: c.reactQuery}

			if got := cfg.HooksEnabled(); got != c.want {
				t.Fatalf("HooksEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}
