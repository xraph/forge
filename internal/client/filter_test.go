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

// TestFilterPrunesUnreachableEntities is the cache-metadata half of the same
// question TestFilterPrunesUnreachableSchemas asks about types.
//
// The entity table is normalization metadata, one row per typename, and a
// client that binds twenty endpoints out of six hundred has no use for the
// rows describing the other five hundred and eighty. Worse, those rows are
// shipped to a browser: a filtered client that carries the whole document's
// table is bytes the consumer downloads to never read.
func TestFilterPrunesUnreachableEntities(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				Path:     "/shop/orders",
				Method:   "GET",
				RootType: "Order",
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
					}},
				},
			},
			{
				Path:     "/admin/tickets",
				Method:   "GET",
				RootType: "Ticket",
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Ticket"}},
					}},
				},
			},
		},
		Schemas: map[string]*client.Schema{
			"Order": {Type: "object", Properties: map[string]*client.Schema{
				"id":       {Type: "string"},
				"customer": {Ref: "#/components/schemas/Customer"},
			}},
			"Customer": {Type: "object", Properties: map[string]*client.Schema{
				"id": {Type: "string"},
			}},
			"Ticket": {Type: "object", Properties: map[string]*client.Schema{
				"id":       {Type: "string"},
				"assignee": {Ref: "#/components/schemas/Agent"},
			}},
			"Agent": {Type: "object", Properties: map[string]*client.Schema{
				"id": {Type: "string"},
			}},
		},
		Entities: map[string]*client.EntityRef{
			"Order":    {Type: "Order", IDField: "id", Fields: map[string]string{"customer": "Customer"}},
			"Customer": {Type: "Customer", IDField: "id"},
			"Ticket":   {Type: "Ticket", IDField: "id", Fields: map[string]string{"assignee": "Agent"}},
			"Agent":    {Type: "Agent", IDField: "id"},
		},
	}

	spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	// Transitive reachability, not one level: the cache normalizes through
	// `order.customer`, so dropping Customer would break dependency tracking
	// silently rather than loudly.
	for _, name := range []string{"Order", "Customer"} {
		if _, ok := spec.Entities[name]; !ok {
			t.Errorf("%s is reachable from a kept endpoint and was pruned from the entity table", name)
		}
	}

	for _, name := range []string{"Ticket", "Agent"} {
		if _, ok := spec.Entities[name]; ok {
			t.Errorf("%s is unreachable and should have been pruned from the entity table", name)
		}
	}
}

// A routing type is a hop with no identity of its own -- a paginated envelope,
// an intermediate struct. It is emitted into the same table and prunes by the
// same rule.
func TestFilterPrunesUnreachableRoutingTypes(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				Path:     "/shop/orders",
				Method:   "GET",
				RootType: "PageOrder",
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/PageOrder"}},
					}},
				},
			},
			{
				Path:     "/admin/tickets",
				Method:   "GET",
				RootType: "PageTicket",
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/PageTicket"}},
					}},
				},
			},
		},
		Schemas: map[string]*client.Schema{
			"PageOrder": {Type: "object", Properties: map[string]*client.Schema{
				"items": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Order"}},
			}},
			"Order": {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"PageTicket": {Type: "object", Properties: map[string]*client.Schema{
				"items": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Ticket"}},
			}},
			"Ticket": {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
		},
		Entities: map[string]*client.EntityRef{
			"Order":  {Type: "Order", IDField: "id"},
			"Ticket": {Type: "Ticket", IDField: "id"},
		},
		RoutingTypes: map[string]*client.EntityRef{
			"PageOrder":  {Type: "PageOrder", Fields: map[string]string{"items": "Order"}},
			"PageTicket": {Type: "PageTicket", Fields: map[string]string{"items": "Ticket"}},
		},
	}

	spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	if _, ok := spec.RoutingTypes["PageOrder"]; !ok {
		t.Error("PageOrder routes a kept endpoint's response and was pruned")
	}

	if _, ok := spec.RoutingTypes["PageTicket"]; ok {
		t.Error("PageTicket is unreachable and should have been pruned")
	}
}

// Streams are not path-filtered at all -- Apply narrows Endpoints and nothing
// else -- so every stream survives and so must the entity each of its bindings
// names. Pruning the entity table against endpoint reachability alone would
// leave a streams[] entry that can never normalize.
func TestFilterKeepsStreamEntities(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				Path:     "/shop/orders",
				Method:   "GET",
				RootType: "Order",
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
					}},
				},
			},
			{Path: "/admin/tickets", Method: "GET"},
		},
		WebSockets: []client.WebSocketEndpoint{{
			Path:           "/ws/presence",
			ReceiveSchema:  &client.Schema{Ref: "#/components/schemas/PresenceEvent"},
			StreamBindings: []client.StreamBinding{{Message: "seen", EntityType: "Presence"}},
		}},
		SSEs: []client.SSEEndpoint{{
			Path:           "/sse/alerts",
			EventSchemas:   map[string]*client.Schema{"alert": {Ref: "#/components/schemas/Alert"}},
			StreamBindings: []client.StreamBinding{{Message: "alert", EntityType: "Alert"}},
		}},
		Schemas: map[string]*client.Schema{
			"Order":         {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"Presence":      {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"PresenceEvent": {Type: "object", Properties: map[string]*client.Schema{"who": {Ref: "#/components/schemas/Presence"}}},
			"Alert":         {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"Orphan":        {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
		},
		Entities: map[string]*client.EntityRef{
			"Order":    {Type: "Order", IDField: "id"},
			"Presence": {Type: "Presence", IDField: "id"},
			"Alert":    {Type: "Alert", IDField: "id"},
			"Orphan":   {Type: "Orphan", IDField: "id"},
		},
	}

	spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	for _, name := range []string{"Order", "Presence", "Alert"} {
		if _, ok := spec.Entities[name]; !ok {
			t.Errorf("%s is reachable and was pruned from the entity table", name)
		}

		if _, ok := spec.Schemas[name]; !ok {
			t.Errorf("%s is reachable and its schema was pruned", name)
		}
	}

	if _, ok := spec.Entities["Orphan"]; ok {
		t.Error("Orphan is reachable from nothing and should have been pruned")
	}
}

// An entity a surviving endpoint declares but no component schema describes
// has no reachability to compute. It stays: a missing row is a silent
// correctness bug and an extra one is only weight.
func TestFilterKeepsUndescribedEntityOfKeptEndpoint(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				Path:   "/shop/orders",
				Method: "GET",
				Entity: &client.EntityRef{Type: "Order", IDField: "id"},
			},
			{
				Path:   "/admin/tickets",
				Method: "GET",
				Entity: &client.EntityRef{Type: "Ticket", IDField: "id"},
			},
		},
		Entities: map[string]*client.EntityRef{
			"Order":  {Type: "Order", IDField: "id"},
			"Ticket": {Type: "Ticket", IDField: "id"},
		},
	}

	spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	if _, ok := spec.Entities["Order"]; !ok {
		t.Error("Order is declared by a kept endpoint and must survive with no schema to prove it")
	}

	if _, ok := spec.Entities["Ticket"]; ok {
		t.Error("Ticket belongs only to a dropped endpoint and should have been pruned")
	}
}

// The no-op filter must stay a no-op here too, for the same reason it does not
// prune schemas: an unfiltered client is the existing behaviour and callers
// depend on rows their endpoints never reference.
func TestFilterEmptyLeavesTheEntityTableAlone(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{Path: "/a", Method: "GET"}},
		Schemas:   map[string]*client.Schema{"Unused": {Type: "object"}},
		Entities:  map[string]*client.EntityRef{"Unused": {Type: "Unused", IDField: "id"}},
	}

	spec.Apply(client.PathFilter{})

	if _, ok := spec.Entities["Unused"]; !ok {
		t.Error("an empty filter must not prune the entity table")
	}
}
