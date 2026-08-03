package client_test

import (
	"sort"
	"testing"
	"time"

	"github.com/xraph/forge/internal/client"
)

func TestMatchPathForms(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Prefix, on a segment boundary.
		{"/identity", "/identity", true},
		{"/identity", "/identity/login", true},
		{"/identity", "/identity/v1/sessions/current", true},
		{"/identity", "/identity-provider", false},
		{"/identity", "/api/v1/identity", false},

		// A trailing slash on the pattern changes nothing.
		{"/identity/", "/identity/login", true},

		// Glob within one segment.
		{"/api/*/health", "/api/v1/health", true},
		{"/api/*/health", "/api/v1/deep/health", false},

		// Recursive glob is the prefix form written out.
		{"/api/**", "/api/v1/models", true},
		{"/api/**", "/api", true},
		{"/api/**", "/apiv1", false},

		// The root matches everything.
		{"/", "/anything/at/all", true},

		{"", "/anything", false},
	}

	for _, tc := range cases {
		spec := &client.APISpec{
			Endpoints: []client.Endpoint{{Path: tc.path, Method: "GET"}},
		}

		got := spec.Apply(client.PathFilter{Include: []string{tc.pattern}}).KeptEndpoints == 1
		if got != tc.want {
			t.Errorf("pattern %q against %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestFilterIncludeExcludePrecedence(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{Path: "/api/v1/models", Method: "GET"},
			{Path: "/api/v1/models/{id}", Method: "GET"},
			{Path: "/api/v1/internal/debug", Method: "GET"},
			{Path: "/identity/login", Method: "POST"},
			{Path: "/_health", Method: "GET"},
		},
	}

	// A narrow exclusion carving a hole in a broad include.
	result := spec.Apply(client.PathFilter{
		Include: []string{"/api/v1"},
		Exclude: []string{"/api/v1/internal"},
	})

	if result.KeptEndpoints != 2 {
		t.Fatalf("kept %d endpoints, want 2", result.KeptEndpoints)
	}

	if result.DroppedEndpoints != 3 {
		t.Errorf("dropped %d endpoints, want 3", result.DroppedEndpoints)
	}

	for _, ep := range spec.Endpoints {
		if ep.Path == "/api/v1/internal/debug" {
			t.Error("exclude must be applied after include")
		}
	}

	want := []string{"/_health", "/api/v1/internal/debug", "/identity/login"}
	if len(result.DroppedPaths) != len(want) {
		t.Fatalf("dropped paths = %v, want %v", result.DroppedPaths, want)
	}

	sort.Strings(result.DroppedPaths)

	for i := range want {
		if result.DroppedPaths[i] != want[i] {
			t.Errorf("dropped paths = %v, want %v", result.DroppedPaths, want)

			break
		}
	}
}

func TestFilterExcludeOnlyKeepsTheRest(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{Path: "/api/v1/models", Method: "GET"},
			{Path: "/identity/login", Method: "POST"},
		},
	}

	result := spec.Apply(client.PathFilter{Exclude: []string{"/identity"}})

	if result.KeptEndpoints != 1 || spec.Endpoints[0].Path != "/api/v1/models" {
		t.Fatalf("kept %v, want only /api/v1/models", spec.Endpoints)
	}
}

func TestFilterEmptyIsANoOp(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{Path: "/a", Method: "GET"}},
		Schemas:   map[string]*client.Schema{"Unused": {Type: "object"}},
	}

	result := spec.Apply(client.PathFilter{})

	if result.KeptEndpoints != 1 || result.KeptSchemas != 1 {
		t.Fatalf("empty filter changed the spec: %+v", result)
	}

	// Notably it must NOT prune. Generating over an unfiltered spec is the
	// existing behaviour and some callers depend on schemas the endpoints
	// never reference.
	if _, ok := spec.Schemas["Unused"]; !ok {
		t.Error("an empty filter must not prune schemas")
	}
}

// TestFilterPrunesUnreachableSchemas is the half that makes filtering worth
// having: endpoints look filtered while the types file plainly is not.
func TestFilterPrunesUnreachableSchemas(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				Path:   "/api/v1/models",
				Method: "GET",
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/ModelList"}},
					}},
				},
			},
			{
				Path:   "/identity/login",
				Method: "POST",
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Session"}},
					}},
				},
			},
		},
		Schemas: map[string]*client.Schema{
			"ModelList": {Type: "object", Properties: map[string]*client.Schema{
				"items": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Model"}},
			}},
			"Model": {Type: "object", Properties: map[string]*client.Schema{
				"bus": {Ref: "#/components/schemas/Bus"},
			}},
			"Bus":     {Type: "object"},
			"Session": {Type: "object"},
			"Orphan":  {Type: "object"},
		},
	}

	result := spec.Apply(client.PathFilter{Include: []string{"/api/v1"}})

	for _, name := range []string{"ModelList", "Model", "Bus"} {
		if _, ok := spec.Schemas[name]; !ok {
			t.Errorf("%s is reachable from a kept endpoint and was pruned", name)
		}
	}

	for _, name := range []string{"Session", "Orphan"} {
		if _, ok := spec.Schemas[name]; ok {
			t.Errorf("%s is unreachable and should have been pruned", name)
		}
	}

	if result.KeptSchemas != 3 || result.DroppedSchemas != 2 {
		t.Errorf("kept %d dropped %d schemas, want 3 and 2", result.KeptSchemas, result.DroppedSchemas)
	}
}

// A schema that references itself must not hang the walk.
func TestFilterHandlesRecursiveSchemas(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/tree",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Node"}},
				}},
			},
		}},
		Schemas: map[string]*client.Schema{
			"Node": {Type: "object", Properties: map[string]*client.Schema{
				"children": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Node"}},
			}},
		},
	}

	done := make(chan struct{})

	go func() {
		spec.Apply(client.PathFilter{Include: []string{"/api/v1"}})
		close(done)
	}()

	select {
	case <-done:
	case <-timeoutAfterSeconds(5):
		t.Fatal("recursive schema caused the reachability walk to hang")
	}

	if _, ok := spec.Schemas["Node"]; !ok {
		t.Error("Node was pruned despite being reachable")
	}
}

// A discriminator names variants nothing else references.
func TestFilterKeepsDiscriminatorVariants(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/events",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Event"}},
				}},
			},
		}},
		Schemas: map[string]*client.Schema{
			"Event": {
				Type: "object",
				Discriminator: &client.Discriminator{
					PropertyName: "kind",
					Mapping: map[string]string{
						"trip": "#/components/schemas/TripEvent",
					},
				},
			},
			"TripEvent": {Type: "object"},
			"Orphan":    {Type: "object"},
		},
	}

	spec.Apply(client.PathFilter{Include: []string{"/api/v1"}})

	if _, ok := spec.Schemas["TripEvent"]; !ok {
		t.Error("a discriminator variant was pruned, leaving a union that cannot resolve")
	}

	if _, ok := spec.Schemas["Orphan"]; ok {
		t.Error("Orphan should have been pruned")
	}
}

func timeoutAfterSeconds(n int) <-chan time.Time {
	return time.After(time.Duration(n) * time.Second)
}
