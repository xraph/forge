package client

// maxNamedTargetDepth bounds how many array / composition wrappers are
// unwrapped while looking for the named type a property contains.
//
// The wrappers a real document produces nest one or two deep (`[]Order`,
// `oneOf: [$ref, null]`). The bound exists for a hand-built APISpec whose
// Schema values point back at each other -- something no parser can produce,
// because both convertSchema implementations build a finite tree out of
// decoded JSON, but which a test or a future in-memory builder can. Without
// it that input is a stack overflow rather than a refusal.
const maxNamedTargetDepth = 16

// resolveEntityFields fills in EntityRef.Fields for every entity in the spec,
// and builds spec.RoutingTypes: together, the property-to-typename edges the
// browser runtime walks to normalize a nested entity.
//
// This runs as a pass over the finished spec rather than inside InferEntity,
// and it has to. InferEntity is handed one schema and decides whether that
// schema is an entity; a property's `$ref` names a component it cannot see,
// and the set of entities is not complete until every endpoint and every
// stream binding has been resolved. So the edge from `Order.customer` to
// `Customer` is only knowable once both are in spec.Entities and spec.Schemas
// is whole.
//
// Both intermediate-representation builders call it at the end of their
// construction -- Introspector.Introspect for a live router, SpecParser.ParseFile
// for a file -- and they call this same function rather than each carrying its
// own copy. A live-versus-file divergence in this package's cache metadata has
// been a recurring defect, and two implementations of one rule is how that
// divergence gets written.
//
// EDGES ARE RECORDED TO EVERY TYPE FROM WHICH AN ENTITY IS REACHABLE, not only
// to entities. A property whose type is a named non-entity is kept when an
// entity sits somewhere beneath it, and that non-entity type gets its own row
// in spec.RoutingTypes carrying Fields and no identity. This is what closes the
// chain `Order -> Shipment (not an entity) -> Carrier (an entity)`, which used
// to break at the first hop, and it is what lets a paginated envelope
// (`PageOrder{items: []Order}`) normalize at all: the runtime reads
// `schema[type].fields` to decide where to descend, so a wrapper with no row
// there ends the walk.
//
// A named type with NO entity anywhere beneath it -- an enum like
// `GoldenOrderStatus`, a plain value struct -- is still skipped, and so is any
// type no root reaches. Both would put bytes in a file CI byte-diffs and buy
// nothing: the runtime's only use for a row is to descend through it, and there
// is nothing under these worth reaching.
//
// TERMINATION IS THIS FUNCTION'S RESPONSIBILITY. It did not used to be:
// schemaName stops at a `$ref` rather than following it, so the old
// property-local pass could not revisit anything and terminated structurally.
// Reachability across refs removes that property, and a component graph really
// does cycle -- `Order -> Customer -> Order` is an ordinary bidirectional
// association, and a self-edge (`Order.parent -> Order`) is ordinary too. Both
// traversals below are therefore worklists over a visited set, where a name is
// enqueued only on the transition that first marks it. The node set is
// spec.Schemas, which is finite, so each terminates after at most that many
// pops.
//
// ResolveEntityFields resolves entity field edges over a specification. Call it
// once, after merging every source: see MergeSpecs, which deliberately leaves
// RoutingTypes nil for this function to rebuild.
func ResolveEntityFields(spec *APISpec) { resolveEntityFields(spec) }

// Calling this twice is safe: each entity's Fields is replaced rather than
// merged, and spec.RoutingTypes is rebuilt from scratch.
func resolveEntityFields(spec *APISpec) {
	if spec == nil {
		return
	}

	edges := namedEdges(spec)
	useful := usefulTypes(spec, edges)
	kept := keptTypes(spec, edges, useful)

	spec.RoutingTypes = nil

	for name, entity := range spec.Entities {
		if entity == nil {
			continue
		}

		// A declared entity (x-forge-entity) may name a type no component
		// describes, and a stream binding may name one the document never
		// defines. Neither is an error here -- the warning for it is raised
		// where the entity is registered -- but neither yields any edges
		// either, and namedEdges simply has no row for it.
		entity.Fields = usefulFields(edges[name], useful)
	}

	for name := range kept {
		if _, isEntity := spec.Entities[name]; isEntity {
			continue
		}

		fields := usefulFields(edges[name], useful)
		if len(fields) == 0 {
			continue
		}

		if spec.RoutingTypes == nil {
			spec.RoutingTypes = make(map[string]*EntityRef)
		}

		spec.RoutingTypes[name] = &EntityRef{Type: name, Fields: fields}
	}
}

// namedEdges reduces every component schema to its property-to-typename edges.
//
// This is the only pass that reads schemas, and it is deliberately local: it
// resolves each property to at most one name and never follows the resulting
// ref. The graph it produces is what the two reachability passes below walk,
// which keeps "how a property names a type" (namedSchemaTarget, with its own
// depth bound for hand-built cyclic Schema values) separate from "which types
// are worth a row" -- the question that actually needs a visited set.
func namedEdges(spec *APISpec) map[string]map[string]string {
	out := make(map[string]map[string]string, len(spec.Schemas))

	for name, schema := range spec.Schemas {
		// EntityProperties rather than schema.Properties, so that an allOf
		// composition contributes the edges of everything it composes. The two
		// halves have to agree: InferEntity reading through composition while
		// this pass did not would make a composed type an entity with no way
		// to reach the entities nested under it, which is the same silent
		// half-normalization as having no row at all.
		effective := EntityProperties(spec, schema)

		var props map[string]string

		for prop, ps := range effective {
			target := namedSchemaTarget(ps, 0)
			if target == "" {
				continue
			}

			if props == nil {
				props = make(map[string]string, len(effective))
			}

			props[prop] = target
		}

		if props != nil {
			out[name] = props
		}
	}

	return out
}

// usefulTypes returns every typename from which an entity is reachable,
// entities included.
//
// Computed by walking the edge graph BACKWARDS from the entities rather than
// forwards from each candidate. Both directions answer the same question, but
// the reverse walk answers it for every type in one pass and, more to the
// point, is exact on cycles: a forward memoized descent has to decide what a
// cycle back to an in-progress node means before it knows the answer, and gets
// `A -> B -> A` with no entity in it wrong in whichever order it happens to
// start. Marking outward from a known-true seed has no such state.
func usefulTypes(spec *APISpec, edges map[string]map[string]string) map[string]bool {
	reverse := make(map[string][]string, len(edges))

	for src, props := range edges {
		for _, target := range props {
			reverse[target] = append(reverse[target], src)
		}
	}

	useful := make(map[string]bool, len(spec.Entities))
	queue := make([]string, 0, len(spec.Entities))

	for name, entity := range spec.Entities {
		if entity == nil || useful[name] {
			continue
		}

		useful[name] = true

		queue = append(queue, name)
	}

	for len(queue) > 0 {
		name := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		for _, src := range reverse[name] {
			if useful[src] {
				continue
			}

			useful[src] = true

			queue = append(queue, src)
		}
	}

	return useful
}

// keptTypes narrows the useful types to those some root actually reaches.
//
// The roots are the entities, the endpoints' response root types, and the
// types a channel's messages arrive as. Without this pass every useful type in
// the document gets a row, including ones only a request body mentions -- a
// `CreateOrderRequest{customer: Customer}` is useful by the definition above,
// and no response will ever be walked through it.
//
// THE CHANNELS ARE ROOTS FOR THE SAME REASON THE ENDPOINTS ARE. A message
// usually arrives as an envelope -- `PresenceEvent{who: Presence}` rather than
// a bare Presence -- and the runtime reads `schema[type].fields` to decide
// where to descend, so an envelope with no row ends the walk at the top of the
// payload. While only the endpoints seeded this, an envelope that no HTTP
// response happened to return was useful and unreached: it got no row, and the
// stream binding naming the entity under it normalized nothing, silently.
func keptTypes(spec *APISpec, edges map[string]map[string]string, useful map[string]bool) map[string]bool {
	kept := make(map[string]bool, len(useful))
	queue := make([]string, 0, len(useful))

	push := func(name string) {
		if name == "" || !useful[name] || kept[name] {
			return
		}

		kept[name] = true

		queue = append(queue, name)
	}

	for name, entity := range spec.Entities {
		if entity != nil {
			push(name)
		}
	}

	for i := range spec.Endpoints {
		push(spec.Endpoints[i].RootType)
	}

	for _, name := range streamRootTypes(spec) {
		push(name)
	}

	for len(queue) > 0 {
		name := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		for _, target := range edges[name] {
			push(target)
		}
	}

	return kept
}

// streamRootTypes names the types a channel's messages arrive as.
//
// This is the stream half of Endpoint.RootType, and it is derived here instead
// of being stored on the endpoint because nothing else needs it: a channel
// carries its message schemas directly, and namedSchemaTarget answers the same
// question about them that RootType answers about a response.
//
// Duplicates are not filtered. keptTypes pushes through a visited set, so the
// second sighting of a name costs one map lookup and saves this function a
// second one.
func streamRootTypes(spec *APISpec) []string {
	var out []string

	add := func(s *Schema) {
		if name := namedSchemaTarget(s, 0); name != "" {
			out = append(out, name)
		}
	}

	addStream := func(stream *StreamSchema) {
		if stream == nil {
			return
		}

		add(stream.SendSchema)
		add(stream.ReceiveSchema)
	}

	for i := range spec.WebSockets {
		ws := &spec.WebSockets[i]

		add(ws.SendSchema)
		add(ws.ReceiveSchema)

		for _, schema := range ws.MessageTypes {
			add(schema)
		}
	}

	for i := range spec.SSEs {
		for _, schema := range spec.SSEs[i].EventSchemas {
			add(schema)
		}
	}

	for i := range spec.WebTransports {
		wt := &spec.WebTransports[i]

		addStream(wt.UniStreamSchema)
		addStream(wt.BiStreamSchema)
		add(wt.DatagramSchema)
	}

	addStreamingFeatureRoots(spec.Streaming, add)

	return out
}

// addStreamingFeatureRoots covers the schemas the AsyncAPI streaming
// extensions contribute. They hang off StreamingSpec rather than off any
// channel, so nothing above reaches them.
func addStreamingFeatureRoots(streaming *StreamingSpec, add func(*Schema)) {
	if streaming == nil {
		return
	}

	if rooms := streaming.Rooms; rooms != nil {
		for _, schema := range []*Schema{
			rooms.JoinSchema, rooms.LeaveSchema, rooms.SendSchema, rooms.ReceiveSchema,
			rooms.MemberJoinSchema, rooms.MemberLeaveSchema, rooms.HistorySchema,
		} {
			add(schema)
		}
	}

	if presence := streaming.Presence; presence != nil {
		add(presence.UpdateSchema)
		add(presence.EventSchema)
	}

	if typing := streaming.Typing; typing != nil {
		add(typing.StartSchema)
		add(typing.StopSchema)
	}

	if channels := streaming.Channels; channels != nil {
		for _, schema := range []*Schema{
			channels.SubscribeSchema, channels.UnsubscribeSchema,
			channels.PublishSchema, channels.MessageSchema,
		} {
			add(schema)
		}
	}
}

// usefulFields drops the edges whose target has no entity beneath it. Returns
// nil rather than an empty map so an entity with no followable property is
// indistinguishable from one the old pass produced.
func usefulFields(props map[string]string, useful map[string]bool) map[string]string {
	var fields map[string]string

	for prop, target := range props {
		if !useful[target] {
			continue
		}

		if fields == nil {
			fields = make(map[string]string, len(props))
		}

		fields[prop] = target
	}

	return fields
}

// namedSchemaTarget reports the component name of what a property contains, or
// "" when that cannot be resolved to exactly one named type.
//
// Three shapes are understood, which is every shape this repository's own
// OpenAPI generator produces plus the one idiom hand-written and third-party
// documents add:
//
//   - A direct `$ref`, which is how a nested struct field is emitted.
//   - An array whose items are a `$ref`. The ELEMENT name is returned, not any
//     marker for the array: `[]LineItem` is a list of LineItem, and the
//     runtime propagates the typename through an array unchanged.
//   - A oneOf/anyOf/allOf wrapper resolving to a single named type. Forge's
//     own generator never emits one (verified: nothing in internal/router
//     assigns OneOf, AnyOf or AllOf), but SpecParser accepts any OpenAPI
//     document, and `oneOf: [{$ref: X}, {type: null}]` is the standard way to
//     spell a nullable reference.
//
// Everything else resolves to "": an inline object has no name, and a cache
// key needs a stable typename.
func namedSchemaTarget(s *Schema, depth int) string {
	name, _ := namedTarget(s, depth)

	return name
}

// namedTarget is namedSchemaTarget plus whether an array wrapper was crossed
// reaching the name.
//
// The field map deliberately discards that second answer -- a typename
// propagates through an array unchanged, so `[]LineItem` and `LineItem` route
// identically -- but envelope resolution needs it, because it is the whole
// difference between an operation that provides `Order[]` and one that provides
// a single `Order:{id}`.
func namedTarget(s *Schema, depth int) (string, bool) {
	if s == nil || depth > maxNamedTargetDepth {
		return "", false
	}

	// A `$ref` alongside `nullable: true` still names its target, so this test
	// comes first and covers the nullable-reference spelling of OpenAPI 3.0.
	if name := schemaName(s); name != "" {
		return name, false
	}

	if s.Items != nil {
		name, _ := namedTarget(s.Items, depth+1)

		return name, name != ""
	}

	return compositionTarget(s, depth)
}

// compositionTarget resolves a oneOf/anyOf/allOf wrapper to the single named
// type it describes.
//
// `null`-typed members are ignored, which is what makes the nullable-reference
// idiom resolve to its referent. Every other member must resolve to the same
// name; a wrapper naming two different types, or holding one unnamed member,
// returns "".
//
// Refusing on ambiguity is the same rule InferEntity applies to a schema with
// two identity-shaped fields, and for the same reason. Picking one of two
// candidate typenames writes records of the wrong type into another type's
// keyspace, which is a cross-type collision in the store rather than a merely
// under-normalized response.
func compositionTarget(s *Schema, depth int) (string, bool) {
	members := make([]*Schema, 0, len(s.OneOf)+len(s.AnyOf)+len(s.AllOf))
	members = append(members, s.OneOf...)
	members = append(members, s.AnyOf...)
	members = append(members, s.AllOf...)

	found := ""
	list := false

	for _, member := range members {
		if member == nil || member.Type == "null" {
			continue
		}

		target, memberIsList := namedTarget(member, depth+1)
		if target == "" {
			return "", false // an unnamed member: the wrapper is not one named type
		}

		if found != "" && found != target {
			return "", false // two different named types
		}

		found = target
		list = list || memberIsList
	}

	return found, list
}
