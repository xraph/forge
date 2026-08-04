package typescript

import (
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
