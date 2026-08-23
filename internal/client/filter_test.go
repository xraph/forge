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

// Streams filter on their own path, the same way endpoints do, and their
// schemas and entity rows go with them.
//
// A channel is as much a per-service surface as a route is: a gateway that
// mounts three services publishes all their channels from one document, and a
// client for one of them has no business carrying the other two's.
func TestFilterNarrowsStreams(t *testing.T) {
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
		WebSockets: []client.WebSocketEndpoint{
			{
				Path:           "/shop/ws/presence",
				ReceiveSchema:  &client.Schema{Ref: "#/components/schemas/PresenceEvent"},
				StreamBindings: []client.StreamBinding{{Message: "seen", EntityType: "Presence"}},
			},
			{
				Path:           "/admin/ws/audit",
				ReceiveSchema:  &client.Schema{Ref: "#/components/schemas/AuditEvent"},
				StreamBindings: []client.StreamBinding{{Message: "logged", EntityType: "Audit"}},
			},
		},
		SSEs: []client.SSEEndpoint{
			{
				Path:           "/shop/sse/alerts",
				EventSchemas:   map[string]*client.Schema{"alert": {Ref: "#/components/schemas/Alert"}},
				StreamBindings: []client.StreamBinding{{Message: "alert", EntityType: "Alert"}},
			},
			{Path: "/admin/sse/metrics"},
		},
		WebTransports: []client.WebTransportEndpoint{
			{Path: "/shop/wt/sync"},
			{Path: "/admin/wt/replay"},
		},
		Schemas: map[string]*client.Schema{
			"Order":         {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"Presence":      {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"PresenceEvent": {Type: "object", Properties: map[string]*client.Schema{"who": {Ref: "#/components/schemas/Presence"}}},
			"Alert":         {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"Audit":         {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"AuditEvent":    {Type: "object", Properties: map[string]*client.Schema{"what": {Ref: "#/components/schemas/Audit"}}},
		},
		Entities: map[string]*client.EntityRef{
			"Order":    {Type: "Order", IDField: "id"},
			"Presence": {Type: "Presence", IDField: "id"},
			"Alert":    {Type: "Alert", IDField: "id"},
			"Audit":    {Type: "Audit", IDField: "id"},
		},
	}

	result := spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	if len(spec.WebSockets) != 1 || spec.WebSockets[0].Path != "/shop/ws/presence" {
		t.Errorf("websockets = %v, want only /shop/ws/presence", streamPaths(spec))
	}

	if len(spec.SSEs) != 1 || spec.SSEs[0].Path != "/shop/sse/alerts" {
		t.Errorf("sses = %v, want only /shop/sse/alerts", streamPaths(spec))
	}

	if len(spec.WebTransports) != 1 || spec.WebTransports[0].Path != "/shop/wt/sync" {
		t.Errorf("webtransports = %v, want only /shop/wt/sync", streamPaths(spec))
	}

	if result.KeptStreams != 3 || result.DroppedStreams != 3 {
		t.Errorf("kept %d dropped %d streams, want 3 and 3", result.KeptStreams, result.DroppedStreams)
	}

	// A surviving channel's entity survives with it, and so does the envelope
	// its messages arrive in.
	for _, name := range []string{"Presence", "Alert"} {
		if _, ok := spec.Entities[name]; !ok {
			t.Errorf("%s belongs to a kept channel and was pruned", name)
		}
	}

	if _, ok := spec.Schemas["PresenceEvent"]; !ok {
		t.Error("PresenceEvent is the kept channel's message schema and was pruned")
	}

	// A dropped channel takes its entity and message schema with it.
	if _, ok := spec.Entities["Audit"]; ok {
		t.Error("Audit belongs only to a dropped channel and should have been pruned")
	}

	if _, ok := spec.Schemas["AuditEvent"]; ok {
		t.Error("AuditEvent is a dropped channel's message schema and should have been pruned")
	}
}

// streamPaths renders a spec's surviving channels for a failure message.
func streamPaths(spec *client.APISpec) []string {
	var out []string

	for _, ws := range spec.WebSockets {
		out = append(out, ws.Path)
	}

	for _, sse := range spec.SSEs {
		out = append(out, sse.Path)
	}

	for _, wt := range spec.WebTransports {
		out = append(out, wt.Path)
	}

	return out
}

// A filter that selects streams and no REST route is a real client, not a
// mistake. The callers reject a filter that matched nothing at all, and before
// streams filtered there was nothing for them to count but endpoints.
func TestFilterStreamOnlySliceIsNotEmpty(t *testing.T) {
	spec := &client.APISpec{
		Endpoints:  []client.Endpoint{{Path: "/admin/tickets", Method: "GET"}},
		WebSockets: []client.WebSocketEndpoint{{Path: "/shop/ws/presence"}},
	}

	result := spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	if result.KeptEndpoints != 0 {
		t.Fatalf("kept %d endpoints, want 0", result.KeptEndpoints)
	}

	if result.KeptStreams != 1 {
		t.Errorf("kept %d streams, want 1 -- a stream-only client has to be countable", result.KeptStreams)
	}
}

// The tag list is joined into the README's overview as a description of what
// this client covers, so it has to describe this client. A gateway declares
// one tag per service it fronts, and all three landing in all three READMEs
// tells the reader the portal package speaks Studio.
func TestFilterKeepsOnlyTheTagsItsSurfaceUses(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{Path: "/shop/orders", Method: "GET", Tags: []string{"Shop"}},
			{Path: "/admin/tickets", Method: "GET", Tags: []string{"Admin"}},
		},
		WebSockets: []client.WebSocketEndpoint{
			{Path: "/shop/ws/presence", Tags: []string{"Realtime"}},
			{Path: "/admin/ws/audit", Tags: []string{"Audit"}},
		},
		Tags: []client.Tag{
			{Name: "Shop", Description: "orders and carts"},
			{Name: "Admin"},
			{Name: "Realtime"},
			{Name: "Audit"},
			{Name: "Unused"},
		},
	}

	spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	got := make([]string, 0, len(spec.Tags))
	for _, tag := range spec.Tags {
		got = append(got, tag.Name)
	}

	sort.Strings(got)

	want := []string{"Realtime", "Shop"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags = %v, want %v", got, want)
		}
	}

	// Order and description are the document's, not this filter's, so a
	// surviving tag arrives untouched.
	if spec.Tags[0].Name != "Shop" || spec.Tags[0].Description != "orders and carts" {
		t.Errorf("surviving tag lost its declaration order or description: %+v", spec.Tags[0])
	}
}

// The no-op filter stays a no-op for streams and tags too.
func TestFilterEmptyLeavesStreamsAndTagsAlone(t *testing.T) {
	spec := &client.APISpec{
		Endpoints:  []client.Endpoint{{Path: "/a", Method: "GET"}},
		WebSockets: []client.WebSocketEndpoint{{Path: "/ws/anything"}},
		Tags:       []client.Tag{{Name: "Unused"}},
	}

	spec.Apply(client.PathFilter{})

	if len(spec.WebSockets) != 1 {
		t.Error("an empty filter must not drop streams")
	}

	if len(spec.Tags) != 1 {
		t.Error("an empty filter must not drop tags")
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
