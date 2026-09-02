package client_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
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

func TestFilterResultSummary(t *testing.T) {
	cases := []struct {
		name   string
		result client.FilterResult
		want   string
	}{
		{
			name:   "a filter that dropped nothing says nothing",
			result: client.FilterResult{KeptEndpoints: 12, KeptSchemas: 30, KeptEntities: 8},
			want:   "",
		},
		{
			name: "a category the document never had is left out",
			result: client.FilterResult{
				KeptEndpoints: 23, DroppedEndpoints: 590,
				KeptSchemas: 41, DroppedSchemas: 380,
				KeptEntities: 4, DroppedEntities: 136,
			},
			want: "Path filter kept 23/613 endpoints, 41/421 schemas, 4/140 entity rows",
		},
		{
			// The line this rule exists for. Reporting only what shrank made
			// this read "kept 0/1 endpoints", as though the filter had matched
			// nothing, with the channel it was written for kept and unnamed.
			name: "a stream-only slice says what it kept",
			result: client.FilterResult{
				DroppedEndpoints: 1,
				KeptStreams:      1,
			},
			want: "Path filter kept 0/1 endpoints, 1/1 streams",
		},
		{
			name: "everything present is reported, shrunk or not",
			result: client.FilterResult{
				KeptEndpoints: 5,
				KeptStreams:   1, DroppedStreams: 3,
				KeptTags: 1, DroppedTags: 2,
			},
			want: "Path filter kept 5/5 endpoints, 1/4 streams, 1/3 tags",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.result.Summary(); got != tc.want {
				t.Errorf("Summary() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The counts have to describe the spec the client is generated from, so they
// are asserted against a real Apply rather than hand-set.
func TestFilterResultCountsWhatItDropped(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{
			{
				Path:     "/shop/orders",
				Method:   "GET",
				RootType: "Order",
				Tags:     []string{"Shop"},
				Responses: map[int]*client.Response{
					200: {Content: map[string]*client.MediaType{
						"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
					}},
				},
			},
			{Path: "/admin/tickets", Method: "GET", Tags: []string{"Admin"}},
		},
		WebSockets: []client.WebSocketEndpoint{{Path: "/admin/ws/audit"}},
		Schemas: map[string]*client.Schema{
			"Order":  {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
			"Ticket": {Type: "object", Properties: map[string]*client.Schema{"id": {Type: "string"}}},
		},
		Entities: map[string]*client.EntityRef{
			"Order":  {Type: "Order", IDField: "id"},
			"Ticket": {Type: "Ticket", IDField: "id"},
		},
		Tags: []client.Tag{{Name: "Shop"}, {Name: "Admin"}},
	}

	result := spec.Apply(client.PathFilter{Include: []string{"/shop/**"}})

	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"KeptEndpoints", result.KeptEndpoints, 1},
		{"DroppedEndpoints", result.DroppedEndpoints, 1},
		{"KeptStreams", result.KeptStreams, 0},
		{"DroppedStreams", result.DroppedStreams, 1},
		{"KeptSchemas", result.KeptSchemas, 1},
		{"DroppedSchemas", result.DroppedSchemas, 1},
		{"KeptEntities", result.KeptEntities, 1},
		{"DroppedEntities", result.DroppedEntities, 1},
		{"KeptTags", result.KeptTags, 1},
		{"DroppedTags", result.DroppedTags, 1},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// A $ref naming a component that does not exist -- a typo, or a rename applied
// to the component key and not to the pointer -- used to prune the whole
// document in silence.
//
// The walk marks the undeclared name, finds no schema behind it and stops. When
// that pointer is the endpoint's only response, it stops having reached
// nothing, so every schema and every entity row goes, Apply reports a filter
// that matched, and the generated client still compiles because an empty entity
// table is well-formed. Nothing anywhere said why.
func TestFilterWarnsOnDanglingComponentRef(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/models",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					// The component below is keyed "ModelList". This is the
					// rename that was applied in one place only.
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/ModelLst"}},
				}},
			},
		}},
		Schemas: map[string]*client.Schema{
			"ModelList": {Type: "object", Properties: map[string]*client.Schema{
				"items": {Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Model"}},
			}},
			"Model": {Type: "object"},
		},
		Entities: map[string]*client.EntityRef{
			"ModelList": {Type: "ModelList", IDField: "id"},
			"Model":     {Type: "Model", IDField: "id"},
		},
	}

	result := spec.Apply(client.PathFilter{Include: []string{"/api/v1"}})
	spec.ValidateRefs()

	if len(spec.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly one naming the unresolvable pointer", spec.Warnings)
	}

	warning := spec.Warnings[0]

	// The pointer as written, so it can be grepped for in the specification,
	// and the endpoint carrying it, so there is somewhere to start looking.
	for _, want := range []string{"#/components/schemas/ModelLst", "GET /api/v1/models"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning %q does not name %q", warning, want)
		}
	}

	// The pruning itself is unchanged: the document is invalid and the walk
	// genuinely reaches nothing. What changed is that it now says so.
	if result.KeptSchemas != 0 || result.KeptEntities != 0 {
		t.Errorf("kept %d schemas and %d entity rows, want 0 and 0: the walk reaches neither",
			result.KeptSchemas, result.KeptEntities)
	}
}

// A pointer into another section of the document is legal, and refTargetName is
// permissive about it on purpose. Warning on every local pointer that resolves
// to no component schema would fire on every document that declares a shared
// response, which is how a real warning gets learned as noise.
func TestFilterDoesNotWarnOnPointerIntoAnotherSection(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/models",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Model"}},
				}},
			},
			DefaultError: &client.Response{Content: map[string]*client.MediaType{
				"application/json": {Schema: &client.Schema{Ref: "#/components/responses/Error"}},
			}},
		}},
		Schemas: map[string]*client.Schema{"Model": {Type: "object"}},
	}

	spec.Apply(client.PathFilter{Include: []string{"/api/v1"}})
	spec.ValidateRefs()

	if len(spec.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none: a pointer into another section declares no component", spec.Warnings)
	}

	if _, ok := spec.Schemas["Model"]; !ok {
		t.Error("Model is reachable from the kept endpoint and was pruned")
	}
}

// One broken pointer reached from several endpoints is one warning, against the
// first endpoint that reaches it. Endpoints are walked in slice order and a
// schema's properties are not, so the attribution has to come from the ordered
// half or it reshuffles between runs.
func TestFilterReportsEachDanglingRefOnceInOrder(t *testing.T) {
	response := func(ref string) map[int]*client.Response {
		return map[int]*client.Response{
			200: {Content: map[string]*client.MediaType{
				"application/json": {Schema: &client.Schema{Ref: ref}},
			}},
		}
	}

	build := func() *client.APISpec {
		return &client.APISpec{
			Endpoints: []client.Endpoint{
				{Path: "/api/v1/alpha", Method: "GET", Responses: response("#/components/schemas/Zeta")},
				{Path: "/api/v1/beta", Method: "POST", Responses: response("#/components/schemas/Zeta")},
				{Path: "/api/v1/gamma", Method: "GET", Responses: response("#/components/schemas/Alpha")},
			},
			Schemas: map[string]*client.Schema{"Kept": {Type: "object"}},
		}
	}

	first := build()
	first.Apply(client.PathFilter{Include: []string{"/api/v1"}})
	first.ValidateRefs()

	if len(first.Warnings) != 2 {
		t.Fatalf("Warnings = %v, want two: Zeta once and Alpha once", first.Warnings)
	}

	// Sorted by component name, so Alpha precedes Zeta regardless of which
	// endpoint reached which.
	if !strings.Contains(first.Warnings[0], "Alpha") || !strings.Contains(first.Warnings[1], "Zeta") {
		t.Errorf("Warnings = %v, want them sorted by component name", first.Warnings)
	}

	// Zeta is reached by /alpha and /beta; the earlier endpoint owns the line.
	if !strings.Contains(first.Warnings[1], "GET /api/v1/alpha") {
		t.Errorf("Zeta warning %q, want it attributed to the first endpoint reaching it", first.Warnings[1])
	}

	second := build()
	second.Apply(client.PathFilter{Include: []string{"/api/v1"}})
	second.ValidateRefs()

	if len(second.Warnings) != len(first.Warnings) {
		t.Fatalf("two runs of the same document gave %d and %d warnings",
			len(first.Warnings), len(second.Warnings))
	}

	for i := range first.Warnings {
		if first.Warnings[i] != second.Warnings[i] {
			t.Errorf("warning %d differs between runs:\n%s\n%s", i, first.Warnings[i], second.Warnings[i])
		}
	}
}

// The empty filter is the common case and used to be the blind spot: both
// production callers skip Apply entirely when no filter is set, so a check
// hung off the filter would have reported nothing for most runs.
func TestValidateRefsWarnsWithNoFilterApplied(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/models",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/ModelLst"}},
				}},
			},
		}},
		Schemas: map[string]*client.Schema{
			"ModelList": {Type: "object"},
		},
	}

	// No Apply at all, which is exactly what the callers do for an empty
	// filter. Nothing is pruned and the pointer is still wrong.
	spec.ValidateRefs()

	if len(spec.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one naming the unresolvable pointer", spec.Warnings)
	}

	for _, want := range []string{"#/components/schemas/ModelLst", "GET /api/v1/models"} {
		if !strings.Contains(spec.Warnings[0], want) {
			t.Errorf("warning %q does not name %q", spec.Warnings[0], want)
		}
	}

	if _, ok := spec.Schemas["ModelList"]; !ok {
		t.Error("ModelList was dropped; validation must not prune anything")
	}
}

// A broken pointer inside a component that no endpoint reaches is invisible to
// a reachability walk, which is why this is a pass over what ships rather than
// a hook in that walk. With no filter set the component is emitted regardless,
// and the hole surfaces as generated code naming a type nothing generated.
func TestValidateRefsWarnsInsideAnUnreachedComponent(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/health",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Type: "object"}},
				}},
			},
		}},
		Schemas: map[string]*client.Schema{
			// Reached by nothing. Shipped anyway, with a pointer at a name
			// the document never declares.
			"AuditRecord": {Type: "object", Properties: map[string]*client.Schema{
				"actor": {Ref: "#/components/schemas/Principl"},
			}},
		},
	}

	spec.ValidateRefs()

	if len(spec.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one: an orphan component still ships its pointers", spec.Warnings)
	}

	for _, want := range []string{"#/components/schemas/Principl", `component schema "AuditRecord"`} {
		if !strings.Contains(spec.Warnings[0], want) {
			t.Errorf("warning %q does not name %q", spec.Warnings[0], want)
		}
	}
}

// A pointer written three schemas deep is reported against the schema that
// writes it, not against whichever endpoint happens to reach that schema. The
// endpoint does not reference the missing name and saying it does sends the
// reader to the wrong file.
func TestValidateRefsAttributesToTheSchemaThatWritesThePointer(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/orders",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/Order"}},
				}},
			},
		}},
		Schemas: map[string]*client.Schema{
			"Order": {Type: "object", Properties: map[string]*client.Schema{
				"customer": {Ref: "#/components/schemas/Custmer"},
			}},
		},
	}

	spec.ValidateRefs()

	if len(spec.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want one", spec.Warnings)
	}

	if !strings.Contains(spec.Warnings[0], `component schema "Order"`) {
		t.Errorf("warning %q, want it attributed to Order, which writes the pointer", spec.Warnings[0])
	}

	if strings.Contains(spec.Warnings[0], "/api/v1/orders") {
		t.Errorf("warning %q blames the endpoint, which does not write the pointer", spec.Warnings[0])
	}
}

// stubGenerator is the smallest thing Generator.Generate will hand a spec to.
// It records the warnings the spec carried at that moment, which is the only
// thing this test is asking about.
type stubGenerator struct{ warnings []string }

func (g *stubGenerator) Name() string                { return "go" }
func (g *stubGenerator) SupportedFeatures() []string { return nil }
func (g *stubGenerator) Validate(generators.APISpec) error {
	return nil
}

func (g *stubGenerator) Generate(
	_ context.Context, spec generators.APISpec, _ generators.GeneratorConfig,
) (*generators.GeneratedClient, error) {
	g.warnings = append(g.warnings, spec.(*client.APISpec).Warnings...)

	return &generators.GeneratedClient{
		Files:    map[string]string{"client.go": ""},
		Warnings: append([]string(nil), spec.(*client.APISpec).Warnings...),
	}, nil
}

// The warning is worth nothing if the generation path does not run it. Nothing
// else proves Generate is where it is wired, and Generate is the one place all
// three entry points -- file, router and the CLI plugin -- funnel through.
func TestValidateRefsRunsDuringGeneration(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			Path:   "/api/v1/models",
			Method: "GET",
			Responses: map[int]*client.Response{
				200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/ModelLst"}},
				}},
			},
		}},
		Schemas: map[string]*client.Schema{"ModelList": {Type: "object"}},
	}

	stub := &stubGenerator{}

	gen := client.NewGenerator()
	if err := gen.Register(stub); err != nil {
		t.Fatalf("Register: %v", err)
	}

	generated, err := gen.Generate(context.Background(), spec, client.GeneratorConfig{
		Language:    "go",
		OutputDir:   t.TempDir(),
		PackageName: "models",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// On the spec by the time the language generator sees it, so the generator
	// copies it onto the client the way it copies every other spec warning.
	if len(stub.warnings) != 1 || !strings.Contains(stub.warnings[0], "ModelLst") {
		t.Errorf("generator saw warnings %v, want one naming ModelLst", stub.warnings)
	}

	if len(generated.Warnings) != 1 {
		t.Errorf("generated client carries warnings %v, want the one", generated.Warnings)
	}
}
