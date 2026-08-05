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

// resolveEntityFields fills in EntityRef.Fields for every entity in the spec:
// the property-to-typename edges the browser runtime walks to normalize a
// nested entity.
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
// EDGES ARE ONLY RECORDED TO TYPES THAT ARE THEMSELVES ENTITIES. A property
// whose type is a named non-entity -- an enum like `GoldenOrderStatus`, or a
// plain value struct -- is skipped, because the runtime's only use for the
// edge is to look the target up in this same table: `walkObject` reads
// `schema[type]`, finds nothing for a type with no entry, and descends with no
// typename at all. Emitting such an edge would put bytes in a file CI
// byte-diffs and buy nothing. The cost of that choice is real and named in the
// report: a chain `Order -> Shipment (not an entity) -> Carrier (an entity)`
// breaks at the first hop, and closing it needs table entries for non-entity
// types, which the runtime's EntityMeta cannot yet express.
//
// Calling this twice is safe: each entity's Fields is replaced, not merged.
func resolveEntityFields(spec *APISpec) {
	if spec == nil || len(spec.Entities) == 0 {
		return
	}

	for name, entity := range spec.Entities {
		if entity == nil {
			continue
		}

		schema := spec.Schemas[name]
		if schema == nil {
			// A declared entity (x-forge-entity) may name a type no component
			// describes, and a stream binding may name one the document never
			// defines. Neither is an error here -- the warning for it is
			// raised where the entity is registered -- but neither yields any
			// edges either.
			entity.Fields = nil

			continue
		}

		var fields map[string]string

		for prop, ps := range schema.Properties {
			target := namedSchemaTarget(ps, 0)
			if target == "" {
				continue
			}

			if _, ok := spec.Entities[target]; !ok {
				continue
			}

			if fields == nil {
				fields = make(map[string]string, len(schema.Properties))
			}

			fields[prop] = target
		}

		entity.Fields = fields
	}
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
	if s == nil || depth > maxNamedTargetDepth {
		return ""
	}

	// A `$ref` alongside `nullable: true` still names its target, so this test
	// comes first and covers the nullable-reference spelling of OpenAPI 3.0.
	if name := schemaName(s); name != "" {
		return name
	}

	if s.Items != nil {
		return namedSchemaTarget(s.Items, depth+1)
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
func compositionTarget(s *Schema, depth int) string {
	members := make([]*Schema, 0, len(s.OneOf)+len(s.AnyOf)+len(s.AllOf))
	members = append(members, s.OneOf...)
	members = append(members, s.AnyOf...)
	members = append(members, s.AllOf...)

	found := ""

	for _, member := range members {
		if member == nil || member.Type == "null" {
			continue
		}

		target := namedSchemaTarget(member, depth+1)
		if target == "" {
			return "" // an unnamed member: the wrapper is not one named type
		}

		if found != "" && found != target {
			return "" // two different named types
		}

		found = target
	}

	return found
}
