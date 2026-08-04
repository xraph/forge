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

// TestGeneratorEmitsHookFacadesWhenReactQueryEnabled is the integration test
// the retired query_internal_test.go's TestPeerDependencyOnlyWhenGenerated
// left uncovered: every other test in this package (facades_test.go,
// opsmanifest_test.go) calls a single generator's Generate directly, which
// never exercises the config.ReactQuery gate in generator.go's Generate,
// generatePackageJSON, or getDependencies at all. Without this test, that
// whole wiring path -- ops.ts/hooks.ts emission, the package.json dependency
// entry, and the getDependencies metadata -- could regress silently.
func TestGeneratorEmitsHookFacadesWhenReactQueryEnabled(t *testing.T) {
	cfg := baseConfig()
	cfg.ReactQuery = true

	out, err := NewGenerator().Generate(context.Background(), manifestSpec(), cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, ok := out.Files["src/ops.ts"]; !ok {
		t.Fatal("expected src/ops.ts to be generated when ReactQuery is enabled")
	}

	if _, ok := out.Files["src/hooks.ts"]; !ok {
		t.Fatal("expected src/hooks.ts to be generated when ReactQuery is enabled")
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

// TestGeneratorOmitsHookFacadesWhenReactQueryDisabled is the negative half of
// the test above: with the flag off, neither file should appear -- matching
// the pre-retirement behaviour where src/query.ts was likewise absent.
func TestGeneratorOmitsHookFacadesWhenReactQueryDisabled(t *testing.T) {
	cfg := baseConfig()
	cfg.ReactQuery = false

	out, err := NewGenerator().Generate(context.Background(), manifestSpec(), cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if _, ok := out.Files["src/ops.ts"]; ok {
		t.Fatal("src/ops.ts should not be generated when ReactQuery is disabled")
	}

	if _, ok := out.Files["src/hooks.ts"]; ok {
		t.Fatal("src/hooks.ts should not be generated when ReactQuery is disabled")
	}

	pkgJSON, ok := out.Files["package.json"]
	if !ok {
		t.Fatal("expected package.json to be generated")
	}

	if strings.Contains(pkgJSON, "@forge/client-core") {
		t.Fatalf("package.json should not depend on @forge/client-core when ReactQuery is disabled\n\n%s", pkgJSON)
	}
}
