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

	require.NoError(t, StripPrefix(spec, []string{"Studio_"}, nil))

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

	require.NoError(t, StripPrefix(spec, []string{"Studio_"}, nil))
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

		err := StripPrefix(spec, []string{"Studio_"}, nil)
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

		require.NoError(t, StripPrefix(spec, []string{"Studio_"}, nil))
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

	require.NoError(t, StripPrefix(spec, []string{"TwinOS_"}, reserved))

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
		require.NoError(t, StripPrefix(spec, []string{""}, nil))
		assert.Contains(t, spec.Schemas, "Studio_WorkspaceResponse")
	})

	// Generating for a service whose routes carry no prefix is legitimate, and
	// must not be an error.
	t.Run("a prefix that matches nothing changes nothing", func(t *testing.T) {
		spec := prefixedSpec()
		require.NoError(t, StripPrefix(spec, []string{"Portal_"}, nil))
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

		require.NoError(t, StripPrefix(spec, []string{"Studio_"}, nil))
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

	require.NoError(t, StripPrefix(spec, []string{"Studio_"}, nil))

	sse := spec.SSEs[0]
	assert.Equal(t, "orders.stream", sse.ID)
	assert.Equal(t, "Order", sse.StreamBindings[0].EntityType)
	assert.Equal(t, []string{"Order[]"}, sse.StreamBindings[0].Invalidates)
}

// crossPrefixedSpec is the auth service's slice of a merged gateway document.
//
// It is the shape that made a single prefix insufficient: identity owns
// `Identity_` and declares its own types under it, but it also re-describes
// what it fronts, so `Portal_WorkspaceResponse` and `TwinOS_Grant` sit in the
// same document -- the same records portal's and twinos' own clients call
// `WorkspaceResponse` and `Grant`.
func crossPrefixedSpec() *APISpec {
	return &APISpec{
		Schemas: map[string]*Schema{
			"Identity_SessionResponse": {
				Type: "object",
				Properties: map[string]*Schema{
					"workspace": ref("Portal_WorkspaceResponse"),
					"grant":     ref("TwinOS_Grant"),
				},
			},
			"Portal_WorkspaceResponse": {Type: "object"},
			"TwinOS_Grant":             {Type: "object"},
		},
		Entities: map[string]*EntityRef{
			"Identity_SessionResponse": {
				Type:    "Identity_SessionResponse",
				IDField: "id",
				Fields: map[string]string{
					"workspace": "Portal_WorkspaceResponse",
					"grant":     "TwinOS_Grant",
				},
			},
			"Portal_WorkspaceResponse": {Type: "Portal_WorkspaceResponse", IDField: "id"},
			"TwinOS_Grant":             {Type: "TwinOS_Grant", IDField: "id"},
		},
		Endpoints: []Endpoint{
			{
				ID:          "Identity_identity.session.get",
				OperationID: "Identity_identity.session.get",
				Method:      "GET",
				Path:        "/identity/session",
				RootType:    "Identity_SessionResponse",
				CacheTags: TagSet{
					Provides:    []string{"Identity_SessionResponse:{id}"},
					Invalidates: []string{"Portal_WorkspaceResponse[]"},
				},
			},
		},
	}
}

// TestStripPrefixStripsForeignServicePrefixes is the aliasing defect.
//
// Before the prefix set, identity's client kept `Portal_WorkspaceResponse`
// while portal's client called the same record `WorkspaceResponse`. A consumer
// unioning the two entity tables got two rows for one record, so a write
// through one client invalidated nothing the other had cached -- and no
// collision guard could see it, because a guard looks for one name carrying two
// shapes and this is two names carrying one.
func TestStripPrefixStripsForeignServicePrefixes(t *testing.T) {
	spec := crossPrefixedSpec()

	require.NoError(t, StripPrefix(spec, []string{"Identity_", "Portal_", "TwinOS_"}, nil))

	assert.Contains(t, spec.Schemas, "WorkspaceResponse", "a foreign prefix must strip like the client's own")
	assert.Contains(t, spec.Schemas, "Grant")
	assert.NotContains(t, spec.Schemas, "Portal_WorkspaceResponse")

	// The entity table is what a consumer unions, so its KEYS are the thing
	// that has to match portal's own client. Asserting the row's Type alone
	// would pass with the table still keyed by the prefixed name.
	assert.Contains(t, spec.Entities, "WorkspaceResponse")
	assert.NotContains(t, spec.Entities, "Portal_WorkspaceResponse")
	assert.Equal(t, "WorkspaceResponse", spec.Entities["WorkspaceResponse"].Type)

	// A field edge left pointing at the old name stops resolving, and the
	// nested record silently stops being normalized.
	assert.Equal(t, map[string]string{"workspace": "WorkspaceResponse", "grant": "Grant"},
		spec.Entities["SessionResponse"].Fields)

	assert.Equal(t, componentRefPrefix+"WorkspaceResponse",
		spec.Schemas["SessionResponse"].Properties["workspace"].Ref)

	// A cache tag naming a foreign type has to move with it, or the tag the
	// endpoint invalidates names a row the table no longer has.
	assert.Equal(t, []string{"WorkspaceResponse[]"}, spec.Endpoints[0].CacheTags.Invalidates)
}

// TestStripPrefixRefusesTwoServicesWithTheSameTypeName covers the collision
// that only a prefix SET can produce.
//
// Under one prefix, trimming is injective and two distinct names always strip
// to two distinct results. Two prefixes make `Portal_User` and `TwinOS_User`
// both land on `User`, which are different shapes from different services --
// merging them would be the aliasing bug run backwards.
func TestStripPrefixRefusesTwoServicesWithTheSameTypeName(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"Portal_User": {Type: "object"},
			"TwinOS_User": {Type: "object"},
		},
	}

	err := StripPrefix(spec, []string{"Portal_", "TwinOS_"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Portal_User")
	assert.Contains(t, err.Error(), "TwinOS_User")
	assert.Contains(t, err.Error(), "strip_prefixes",
		"the advice must name the knob that fixes it; neither service can rename its own type")

	// The single-prefix collision keeps its own advice, which is different:
	// there one of the two names really can move.
	bare := &APISpec{
		Schemas: map[string]*Schema{
			"Portal_User": {Type: "object"},
			"User":        {Type: "object"},
		},
	}

	err = StripPrefix(bare, []string{"Portal_"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove the prefix from one of them")
}

// TestStripPrefixMatchesTheLongestPrefix pins the ordering rule.
//
// `TwinOS_Grant` matches both `Twin_` and `TwinOS_`. The shorter one leaves
// `OS_Grant` -- neither the original nor the intended strip, and nothing
// downstream can catch it, because it collides with nothing.
func TestStripPrefixMatchesTheLongestPrefix(t *testing.T) {
	spec := &APISpec{
		Schemas: map[string]*Schema{
			"TwinOS_Grant": {Type: "object"},
			"Twin_Node":    {Type: "object"},
		},
	}

	// Declared shortest-first, so the test fails if normalizePrefixes is
	// dropped and matching falls back to declaration order.
	require.NoError(t, StripPrefix(spec, []string{"Twin_", "TwinOS_"}, nil))

	assert.Contains(t, spec.Schemas, "Grant")
	assert.Contains(t, spec.Schemas, "Node")
	assert.NotContains(t, spec.Schemas, "OS_Grant")
}

// TestStripPrefixIgnoresEmptyAndDuplicatePrefixes covers what the callers
// actually pass: a set assembled from a clients: block, where a client's own
// prefix is also one of the siblings' and an unconfigured client contributes "".
func TestStripPrefixIgnoresEmptyAndDuplicatePrefixes(t *testing.T) {
	spec := prefixedSpec()

	require.NoError(t, StripPrefix(spec, []string{"Studio_", "", "Studio_", ""}, nil))

	assert.Contains(t, spec.Schemas, "WorkspaceResponse")
	assert.Contains(t, spec.Schemas, "User")

	// An empty prefix leads every name. Were it not dropped it would match
	// first under longest-first ordering only by accident, and strip nothing
	// from everything -- a silent no-op across the whole document.
	untouched := prefixedSpec()
	require.NoError(t, StripPrefix(untouched, []string{"", ""}, nil))
	assert.Contains(t, untouched.Schemas, "Studio_WorkspaceResponse")
}
