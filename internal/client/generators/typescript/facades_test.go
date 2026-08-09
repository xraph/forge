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
		"import { query, mutation } from '@forge-go/client-core';",
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

	if !strings.Contains(pkgJSON, "@forge-go/client-core") {
		t.Fatalf("package.json missing the @forge-go/client-core dependency\n\n%s", pkgJSON)
	}

	if strings.Contains(pkgJSON, "@tanstack") {
		t.Fatalf("package.json still references @tanstack, the retired peer dependency\n\n%s", pkgJSON)
	}

	foundClientCoreDep := false

	for _, dep := range out.Dependencies {
		if dep.Name == "@forge-go/client-core" {
			foundClientCoreDep = true
		}
	}

	if !foundClientCoreDep {
		t.Fatalf("expected @forge-go/client-core in genClient.Dependencies, got %v", out.Dependencies)
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

	if strings.Contains(pkgJSON, "@forge-go/client-core") {
		t.Fatalf("package.json should not depend on @forge-go/client-core when Hooks is disabled\n\n%s", pkgJSON)
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

func TestFacadeTypesMutationBindings(t *testing.T) {
	// Schemas is populated because an import is only emitted for a name
	// types.ts actually exports, and types.ts is generated from this map alone.
	// See mutationTypeArgs.
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				ID:       "orderUpdate",
				Method:   "PATCH",
				Path:     "/orders/{id}",
				RootType: "Order",
				Entity:   &client.EntityRef{Type: "Order", IDField: "id"},
			},
			{
				ID:       "orderList",
				Method:   "GET",
				Path:     "/orders",
				RootType: "PageOrder",
				Entity:   &client.EntityRef{Type: "Order", IDField: "id"},
			},
			{
				ID:     "ping",
				Method: "POST",
				Path:   "/ping",
			},
		},
		Schemas: map[string]*client.Schema{
			"Order":     objectSchema(),
			"PageOrder": objectSchema(),
		},
	}

	out := NewFacadeGenerator().Generate(spec, client.GeneratorConfig{Language: "typescript"})

	for _, want := range []string{
		"import type { Order } from './types';",
		"export const useOrderUpdate = mutation<Order, Order>(ops.orderUpdate);",
		"export const useOrderList = query(ops.orderList);",
		"export const usePing = mutation(ops.ping);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hooks.ts missing %q\ngot:\n%s", want, out)
		}
	}

	// A query's response type is deliberately not emitted in this change, so
	// PageOrder must not be imported for it.
	if strings.Contains(out, "PageOrder") {
		t.Errorf("hooks.ts should not reference query response types yet:\n%s", out)
	}
}

// TestMutationImportMatchesGeneratedTypeName guards against reintroducing a
// PascalCase renderer (toPascal, or anything else) on RootType/Entity.Type.
//
// types.ts exports every schema under its literal spec.Schemas key --
// generateTypes iterates sortedKeys(spec.Schemas) and writes
// `export interface %s` with that raw key, no renaming step in between. Both
// RootType and Entity.Type are themselves derived from that same raw key
// (schemaName in introspector.go reads it straight off a $ref), so they
// already agree with types.ts byte-for-byte. Running either of them through
// toPascal before emitting the import would silently import a name types.ts
// never exports whenever a schema's key is not already canonical PascalCase --
// exactly the case a snake_case schema name like "order_summary" exercises,
// and exactly the case every other fixture in this package is too
// well-behaved to catch.
func TestMutationImportMatchesGeneratedTypeName(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				ID:       "orderSummaryUpdate",
				Method:   "PATCH",
				Path:     "/order-summaries/{id}",
				RootType: "order_summary",
				Entity:   &client.EntityRef{Type: "order_summary", IDField: "id"},
			},
		},
		Schemas: map[string]*client.Schema{
			"order_summary": {
				Type:       "object",
				Properties: map[string]*client.Schema{"id": {Type: "string"}},
			},
		},
	}

	hooks := NewFacadeGenerator().Generate(spec, client.GeneratorConfig{Language: "typescript"})
	types := (&Generator{}).generateTypes(spec, client.GeneratorConfig{})

	// Confirms the premise this test relies on: types.ts really does export
	// the schema under its literal, non-canonical key.
	if !strings.Contains(types, "export interface order_summary {") {
		t.Fatalf("test setup invalid: types.ts does not export order_summary verbatim\n\n%s", types)
	}

	want := "import type { order_summary } from './types';"
	if !strings.Contains(hooks, want) {
		t.Errorf("hooks.ts import does not match what types.ts actually exports\nwant %q\ngot:\n%s", want, hooks)
	}

	// The regression this test exists to catch: a PascalCase import or type
	// argument for a name types.ts never declares. Checked narrowly rather
	// than a blanket `strings.Contains(hooks, "OrderSummary")` -- the hook
	// NAME is legitimately PascalCase (`useOrderSummaryUpdate`, via toPascal
	// in hookName, which is correct and untouched by this fix), so a broad
	// substring check would fail on correct output.
	if strings.Contains(hooks, "{ OrderSummary") || strings.Contains(hooks, "<OrderSummary") {
		t.Errorf("hooks.ts references OrderSummary, a name types.ts never exports:\n%s", hooks)
	}
}

// TestMutationImportsAreSortedWhenNamesDiverge is the case
// TestFacadeTypesMutationBindings cannot cover: every fixture elsewhere in
// this package happens to have RootType == Entity.Type, so imports never
// holds more than one distinct name and sort.Strings on a 0-or-1-element
// slice is a no-op that would still pass with the sort deleted entirely.
//
// An enveloped mutation -- the same shape e2e_envelope_test.go exercises for
// queries (a wrapper type distinct from the entity inside it) -- gives
// mutationTypeArgs two distinct names to import. RootType is inserted into
// the imports map before Entity.Type (see mutationTypeArgs), so choosing a
// RootType that sorts AFTER the entity name (PageOrder, inserted first) than
// Order (inserted second) means insertion order and sorted order disagree:
// asserting the exact rendered import line only proves the sort ran if the
// two orders differ, which they do here.
func TestMutationImportsAreSortedWhenNamesDiverge(t *testing.T) {
	// Both names are declared, because an undeclared one is not imported at
	// all and the sort would then have nothing to order. See mutationTypeArgs.
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				ID:       "orderReplace",
				Method:   "PUT",
				Path:     "/orders/{id}",
				RootType: "PageOrder",
				Entity:   &client.EntityRef{Type: "Order", IDField: "id"},
			},
		},
		Schemas: map[string]*client.Schema{
			"Order":     objectSchema(),
			"PageOrder": objectSchema(),
		},
	}

	out := NewFacadeGenerator().Generate(spec, client.GeneratorConfig{Language: "typescript"})

	want := "import type { Order, PageOrder } from './types';"
	if !strings.Contains(out, want) {
		t.Errorf("hooks.ts import is not sorted\nwant %q\ngot:\n%s", want, out)
	}
}

func objectSchema() *client.Schema {
	return &client.Schema{
		Type:       "object",
		Properties: map[string]*client.Schema{"id": {Type: "string"}},
	}
}

// TestMutationTypesAreOmittedForAnUndeclaredName is the case the non-empty
// check alone let through, and it produces a generated client that does not
// compile rather than one that is merely loosely typed.
//
// types.ts is generated from spec.Schemas and nothing else. A declared entity
// (x-forge-entity) may name a type no component describes -- introspector.go
// takes x-forge-entity.type verbatim from the spec author -- so a route
// annotated with a Go typename that differs from its OpenAPI component key
// names something types.ts never exports. Importing it is a hard tsc failure
// in the emitted client, which is why the endpoint falls back to the bare
// `mutation(ops.x)` instead.
//
// All or nothing, per the rule mutationTypeArgs already states: the endpoint
// whose ROOT type is missing must not emit `mutation<Order>` with the entity
// silently left as `unknown`, and neither may the endpoint missing the entity.
func TestMutationTypesAreOmittedForAnUndeclaredName(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				ID:       "orderUpdate",
				Method:   "PATCH",
				Path:     "/orders/{id}",
				RootType: "Order",
				// The Go typename an annotation supplied. No component
				// describes it, so types.ts never declares it.
				Entity: &client.EntityRef{Type: "domain.Order", IDField: "id"},
			},
			{
				ID:       "orderArchive",
				Method:   "POST",
				Path:     "/orders/{id}/archive",
				RootType: "ArchiveResult",
				Entity:   &client.EntityRef{Type: "Order", IDField: "id"},
			},
		},
		Schemas: map[string]*client.Schema{"Order": objectSchema()},
	}

	out := NewFacadeGenerator().Generate(spec, client.GeneratorConfig{Language: "typescript"})

	for _, want := range []string{
		"export const useOrderUpdate = mutation(ops.orderUpdate);",
		"export const useOrderArchive = mutation(ops.orderArchive);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hooks.ts missing %q\ngot:\n%s", want, out)
		}
	}

	// Nothing was imported at all: `Order` is declared, but it only ever
	// appears alongside a name that is not, and a partial argument list is
	// exactly what this function refuses to emit.
	if strings.Contains(out, "from './types'") {
		t.Errorf("hooks.ts imports a type for an endpoint that emitted no type arguments:\n%s", out)
	}

	for _, unwanted := range []string{"domain.Order", "ArchiveResult"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("hooks.ts references %q, a name types.ts never exports:\n%s", unwanted, out)
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
