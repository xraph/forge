package typescript

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func bitemporalSpec() *client.APISpec {
	return &client.APISpec{
		Info: client.APIInfo{Title: "Grid", Version: "1.0.0"},
		Endpoints: []client.Endpoint{
			{
				OperationID: "networkmodel.list",
				Path:        "/api/v1/models",
				Method:      "GET",
				QueryParams: []client.Parameter{
					{Name: "limit", In: "query", Schema: &client.Schema{Type: "integer"}},
					{Name: "validAt", In: "query", Schema: &client.Schema{Type: "string"}},
					{Name: "knownAt", In: "query", Schema: &client.Schema{Type: "string"}},
				},
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/ModelList"}},
					}},
				},
			},
			{
				OperationID: "networkmodel.create",
				Path:        "/api/v1/models",
				Method:      "POST",
				RequestBody: &client.RequestBody{
					Required: true,
					Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/NewModel"}},
					},
				},
				Responses: map[int]*client.Response{
					201: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/NetworkModel"}},
					}},
				},
			},
		},
		Schemas: map[string]*client.Schema{
			"ModelList":    {Type: "object"},
			"NewModel":     {Type: "object"},
			"NetworkModel": {Type: "object"},
		},
	}
}

// TestQueryKeysCarryEveryParameter is the property the whole layer exists for.
//
// A key missing a parameter serves one request's cached answer to a different
// request. With two independent time axes that failure is silent and
// plausible: a key on validAt alone returns what is true now to a caller that
// asked what was known then.
func TestQueryKeysCarryEveryParameter(t *testing.T) {
	out, _ := NewReactQueryGenerator().Generate(bitemporalSpec(), client.GeneratorConfig{
		APIName:    "Client",
		ReactQuery: true,
	})

	if !strings.Contains(out, "networkmodelList: (limit?:") {
		t.Fatalf("no key builder for networkmodel.list:\n%s", out)
	}

	for _, param := range []string{"limit", "validAt", "knownAt"} {
		if !strings.Contains(out, param+",") && !strings.Contains(out, param+" }") {
			t.Errorf("query key omits %q, so a cached answer can serve a different request", param)
		}
	}

	if !strings.Contains(out, "['networkmodel', 'list', { limit, validAt, knownAt }] as const") {
		t.Errorf("key payload is not the full parameter set:\n%s", out)
	}
}

// TestHooksCallTheGeneratedClient pins the layering: hooks must delegate, not
// issue their own requests.
func TestHooksCallTheGeneratedClient(t *testing.T) {
	out, _ := NewReactQueryGenerator().Generate(bitemporalSpec(), client.GeneratorConfig{
		APIName:    "Client",
		ReactQuery: true,
	})

	if !strings.Contains(out, "client.networkmodel.list(limit, validAt, knownAt, { signal })") {
		t.Errorf("hook does not delegate to the generated method:\n%s", out)
	}

	// Re-deriving the request would mean a fetch or a URL in this file.
	for _, forbidden := range []string{"fetch(", "new Request(", "`/api/v1"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("hooks re-derive the API surface (%q); they must call the client", forbidden)
		}
	}
}

// TestReadsAreQueriesWritesAreMutations covers the split.
func TestReadsAreQueriesWritesAreMutations(t *testing.T) {
	out, _ := NewReactQueryGenerator().Generate(bitemporalSpec(), client.GeneratorConfig{
		APIName:    "Client",
		ReactQuery: true,
	})

	if !strings.Contains(out, "export function useNetworkmodelList(") ||
		!strings.Contains(out, "return useQuery({") {
		t.Error("a GET should become a useQuery hook")
	}

	if !strings.Contains(out, "export function useNetworkmodelCreate(") ||
		!strings.Contains(out, "return useMutation({") {
		t.Error("a POST should become a useMutation hook")
	}

	// A mutation must not be keyed: caching a write serves a stale answer to
	// a request whose entire purpose was to change something.
	//
	// Bounded to the function body. Hooks are emitted in sorted order, so
	// "create" precedes "list" and slicing to the end of the file would sweep
	// in a legitimate query key from the next hook.
	start := strings.Index(out, "export function useNetworkmodelCreate(")
	if start < 0 {
		t.Fatal("no mutation hook to inspect")
	}

	end := strings.Index(out[start:], "\n}\n")
	if end < 0 {
		t.Fatal("mutation hook body is unterminated")
	}

	if body := out[start : start+end]; strings.Contains(body, "queryKey:") {
		t.Errorf("a mutation must not carry a query key:\n%s", body)
	}
}

// TestGeneratedOutputIsDeterministic guards the map iteration in collect().
func TestGeneratedOutputIsDeterministic(t *testing.T) {
	config := client.GeneratorConfig{APIName: "Client", ReactQuery: true}

	first, _ := NewReactQueryGenerator().Generate(bitemporalSpec(), config)

	for i := 0; i < 12; i++ {
		next, _ := NewReactQueryGenerator().Generate(bitemporalSpec(), config)
		if next != first {
			t.Fatal("output varies between runs; a generator whose bytes move cannot be reviewed in a diff")
		}
	}
}

// TestNoEndpointsProducesNoFile keeps an empty module out of the package.
func TestNoEndpointsProducesNoFile(t *testing.T) {
	out, _ := NewReactQueryGenerator().Generate(&client.APISpec{}, client.GeneratorConfig{
		ReactQuery: true,
	})

	if out != "" {
		t.Errorf("a spec with no endpoints should produce no query module, got:\n%s", out)
	}
}

// TestPeerDependencyOnlyWhenGenerated: a non-React consumer must not inherit
// a dependency on a UI library.
func TestPeerDependencyOnlyWhenGenerated(t *testing.T) {
	g := &Generator{}
	spec := &client.APISpec{Info: client.APIInfo{Title: "Grid", Version: "1.0.0"}}

	with := g.generatePackageJSON(spec, client.GeneratorConfig{
		PackageName: "@scope/c", Version: "1.0.0", ReactQuery: true,
	})
	if !strings.Contains(with, "@tanstack/react-query") {
		t.Error("hooks generated but no peer dependency declared")
	}

	without := g.generatePackageJSON(spec, client.GeneratorConfig{
		PackageName: "@scope/c", Version: "1.0.0",
	})
	if strings.Contains(without, "@tanstack/react-query") {
		t.Error("a client without hooks must not depend on react-query")
	}
}
