package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ref(name string) *Schema {
	return &Schema{Ref: componentRefPrefix + name}
}

// prefixedSpec is one service's slice of a merged gateway document: an
// envelope referencing a record, an entity table with a field edge between
// them, and an endpoint carrying tags, a root type and an operation id.
func prefixedSpec() *APISpec {
	return &APISpec{
		Schemas: map[string]*Schema{
			"Studio_WorkspaceResponse": {
				Type: "object",
				Properties: map[string]*Schema{
					"id":    {Type: "string"},
					"owner": ref("Studio_User"),
				},
			},
			"Studio_User": {Type: "object"},
			"Studio_workspaceListResponse": {
				Type: "object",
				Properties: map[string]*Schema{
					"data": {Type: "array", Items: ref("Studio_WorkspaceResponse")},
				},
			},
		},
		Entities: map[string]*EntityRef{
			"Studio_WorkspaceResponse": {
				Type:    "Studio_WorkspaceResponse",
				IDField: "id",
				Fields:  map[string]string{"owner": "Studio_User"},
			},
		},
		RoutingTypes: map[string]*EntityRef{
			"Studio_workspaceListResponse": {
				Type:   "Studio_workspaceListResponse",
				Fields: map[string]string{"data": "Studio_WorkspaceResponse"},
			},
		},
		Endpoints: []Endpoint{
			{
				ID:          "Studio_studio.workspace.list",
				OperationID: "Studio_studio.workspace.list",
				Method:      "GET",
				Path:        "/studio/api/studio/workspaces",
				RootType:    "Studio_workspaceListResponse",
				Entity: &EntityRef{
					Type:    "Studio_WorkspaceResponse",
					IDField: "id",
				},
				CacheTags: TagSet{
					Provides:    []string{"Studio_WorkspaceResponse:{id}", "Studio_WorkspaceResponse[]"},
					Invalidates: []string{"Studio_WorkspaceResponse[]"},
				},
				Responses: map[int]*Response{
					200: {Content: map[string]*MediaType{
						"application/json": {Schema: ref("Studio_workspaceListResponse")},
					}},
				},
				RequestBody: &RequestBody{Content: map[string]*MediaType{
					"application/json": {Schema: ref("Studio_WorkspaceResponse")},
				}},
			},
		},
	}
}

func TestStripPrefixRenamesEverySurface(t *testing.T) {
	spec := prefixedSpec()

	require.NoError(t, StripPrefix(spec, "Studio_", nil))

	t.Run("schema keys", func(t *testing.T) {
		assert.Contains(t, spec.Schemas, "WorkspaceResponse")
		assert.Contains(t, spec.Schemas, "workspaceListResponse")
		assert.NotContains(t, spec.Schemas, "Studio_WorkspaceResponse")
	})

	// A ref left pointing at the old key is the failure that produces a client
	// naming a type its own types.ts no longer exports.
	t.Run("refs, nested and through arrays", func(t *testing.T) {
		assert.Equal(t,
			componentRefPrefix+"User",
			spec.Schemas["WorkspaceResponse"].Properties["owner"].Ref)
		assert.Equal(t,
			componentRefPrefix+"WorkspaceResponse",
			spec.Schemas["workspaceListResponse"].Properties["data"].Items.Ref)
	})

	t.Run("entity table keys, types and field edges", func(t *testing.T) {
		entity := spec.Entities["WorkspaceResponse"]
		require.NotNil(t, entity)
		assert.Equal(t, "WorkspaceResponse", entity.Type)
		// The edge is how the runtime recognises a nested entity; left stale it
		// stops resolving and the nested record silently stops normalizing.
		assert.Equal(t, "User", entity.Fields["owner"])

		routing := spec.RoutingTypes["workspaceListResponse"]
		require.NotNil(t, routing)
		assert.Equal(t, "WorkspaceResponse", routing.Fields["data"])
	})

	t.Run("endpoint identifiers and cache tags", func(t *testing.T) {
		ep := spec.Endpoints[0]
		assert.Equal(t, "studio.workspace.list", ep.ID)
		assert.Equal(t, "studio.workspace.list", ep.OperationID)
		assert.Equal(t, "workspaceListResponse", ep.RootType)
		assert.Equal(t, "WorkspaceResponse", ep.Entity.Type)
		assert.Equal(t,
			[]string{"WorkspaceResponse:{id}", "WorkspaceResponse[]"},
			ep.CacheTags.Provides)
		assert.Equal(t, []string{"WorkspaceResponse[]"}, ep.CacheTags.Invalidates)
	})

	// Codec ids are derived from these refs at emit time rather than stored, so
	// a body or response ref left unrenamed silently drops a codec entry and
	// the client sends unrenamed fields.
	t.Run("request body and response refs", func(t *testing.T) {
		ep := spec.Endpoints[0]
		assert.Equal(t,
			componentRefPrefix+"WorkspaceResponse",
			ep.RequestBody.Content["application/json"].Schema.Ref)
		assert.Equal(t,
			componentRefPrefix+"workspaceListResponse",
			ep.Responses[200].Content["application/json"].Schema.Ref)
	})
}

// A self-referential type is the case that separates a walk with a visited set
// from one without: without it this does not return.
func TestStripPrefixTerminatesOnRecursiveSchema(t *testing.T) {
	node := &Schema{Type: "object", Properties: map[string]*Schema{}}
	node.Properties["children"] = &Schema{Type: "array", Items: ref("Studio_Node")}
	node.Properties["self"] = node

	spec := &APISpec{Schemas: map[string]*Schema{"Studio_Node": node}}

	require.NoError(t, StripPrefix(spec, "Studio_", nil))
	assert.Equal(t,
		componentRefPrefix+"Node",
		spec.Schemas["Node"].Properties["children"].Items.Ref)
}

func TestStripPrefixRefusesCollisions(t *testing.T) {
	t.Run("with a name that is not moving", func(t *testing.T) {
		spec := &APISpec{Schemas: map[string]*Schema{
			"Studio_User": {Type: "object"},
			"User":        {Type: "object"},
		}}

		err := StripPrefix(spec, "Studio_", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "collides")
		// Refused outright: a partial rename is worse than none, because the
		// generated client would compile against half-renamed names.
		assert.Contains(t, spec.Schemas, "Studio_User")
	})

	// Two prefixed names cannot collide with each other under a single fixed
	// prefix: trimming it is injective. A doubled prefix is the closest thing
	// to that case and is NOT a collision -- it strips one level, landing on a
	// name whose own occupant is itself moving out of the way.
	t.Run("a doubled prefix strips one level and is not a collision", func(t *testing.T) {
		spec := &APISpec{Schemas: map[string]*Schema{
			"Studio_Studio_User": {Type: "object"},
			"Studio_User":        {Type: "object"},
		}}

		require.NoError(t, StripPrefix(spec, "Studio_", nil))
		assert.Contains(t, spec.Schemas, "Studio_User")
		assert.Contains(t, spec.Schemas, "User")
		assert.Len(t, spec.Schemas, 2)
	})
}

// TestStripPrefixKeepsReservedNamesPrefixed covers the case the first real
// generation hit: the twinos spec declares `TwinOS_ValidationError`, which
// strips onto the error class errors.ts already exports, and index.ts re-exports
// both with `export *`.
//
// Skipped rather than refused, which is the deliberate asymmetry against a
// schema-versus-schema collision: there, either name could be the one to keep
// and the tool must not guess; here the generated name cannot move at all, so
// leaving the schema prefixed is the only answer available.
func TestStripPrefixKeepsReservedNamesPrefixed(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"TwinOS_ValidationError": {Type: "object"},
			"TwinOS_AppResponse":     {Type: "object", Properties: map[string]*Schema{"e": ref("TwinOS_ValidationError")}},
		},
		Endpoints: []Endpoint{{
			ID:       "TwinOS_apps.get",
			RootType: "TwinOS_ValidationError",
		}},
	}

	reserved := map[string]bool{"ValidationError": true}

	require.NoError(t, StripPrefix(spec, "TwinOS_", reserved))

	assert.Contains(t, spec.Schemas, "TwinOS_ValidationError")
	assert.NotContains(t, spec.Schemas, "ValidationError")

	// Everything else still strips: one reserved name must not disable the pass.
	assert.Contains(t, spec.Schemas, "AppResponse")
	assert.Equal(t, "apps.get", spec.Endpoints[0].ID)

	// And references to the name that stayed put must stay pointing at it --
	// this is where a skip is easy to get wrong, by recording the rename and
	// then not performing it.
	assert.Equal(t,
		componentRefPrefix+"TwinOS_ValidationError",
		spec.Schemas["AppResponse"].Properties["e"].Ref)
	assert.Equal(t, "TwinOS_ValidationError", spec.Endpoints[0].RootType)
}

func TestStripPrefixNoOps(t *testing.T) {
	t.Run("an empty prefix changes nothing", func(t *testing.T) {
		spec := prefixedSpec()
		require.NoError(t, StripPrefix(spec, "", nil))
		assert.Contains(t, spec.Schemas, "Studio_WorkspaceResponse")
	})

	// Generating for a service whose routes carry no prefix is legitimate, and
	// must not be an error.
	t.Run("a prefix that matches nothing changes nothing", func(t *testing.T) {
		spec := prefixedSpec()
		require.NoError(t, StripPrefix(spec, "Portal_", nil))
		assert.Contains(t, spec.Schemas, "Studio_WorkspaceResponse")
		assert.Equal(t, "Studio_studio.workspace.list", spec.Endpoints[0].ID)
	})

	// A schema named exactly the prefix would strip to the empty string, which
	// is not a name any emitter can write.
	t.Run("a schema named exactly the prefix is left alone", func(t *testing.T) {
		spec := &APISpec{Schemas: map[string]*Schema{
			"Studio_":     {Type: "object"},
			"Studio_User": {Type: "object"},
		}}

		require.NoError(t, StripPrefix(spec, "Studio_", nil))
		assert.Contains(t, spec.Schemas, "Studio_")
		assert.Contains(t, spec.Schemas, "User")
	})
}

func TestStripPrefixRenamesStreamBindings(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{"Studio_Order": {Type: "object"}},
		SSEs: []SSEEndpoint{{
			ID: "Studio_orders.stream",
			StreamBindings: []StreamBinding{{
				Message:     "order.updated",
				EntityType:  "Studio_Order",
				Intent:      StreamUpsert,
				Invalidates: []string{"Studio_Order[]"},
			}},
		}},
	}

	require.NoError(t, StripPrefix(spec, "Studio_", nil))

	sse := spec.SSEs[0]
	assert.Equal(t, "orders.stream", sse.ID)
	assert.Equal(t, "Order", sse.StreamBindings[0].EntityType)
	assert.Equal(t, []string{"Order[]"}, sse.StreamBindings[0].Invalidates)
}
