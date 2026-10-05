package dart

import (
	"fmt"

	"github.com/xraph/forge/internal/client"
)

// modelKind is the shape a component schema is generated as.
type modelKind int

const (
	kindClass modelKind = iota
	kindEnum
	kindUnion
	kindAlias
)

// componentModel is one component schema and the model file it becomes.
type componentModel struct {
	schemaName string
	schema     *client.Schema
	dartName   string
	file       string
	kind       modelKind

	// decls are the declarations the file holds: the component's own first,
	// then every inline type its properties needed a name for.
	decls []decl

	// imports are the other components whose model files this one names.
	imports map[string]bool

	// target caches an alias component's resolved type.
	target *dartType
}

// registry assigns every component a Dart name and file, then builds each
// one's declarations. Names are claimed in sorted order and components before
// inline types, so the same document always yields the same names.
type registry struct {
	spec      *client.APISpec
	config    client.GeneratorConfig
	codecs    *codecTable
	models    map[string]*componentModel
	taken     map[string]bool
	resolving map[string]bool
	warnings  []string
}

func newRegistry(spec *client.APISpec, config client.GeneratorConfig, codecs *codecTable, reserved map[string]bool) *registry {
	r := &registry{
		spec:      spec,
		config:    config,
		codecs:    codecs,
		models:    make(map[string]*componentModel, len(spec.Schemas)),
		taken:     make(map[string]bool, len(reserved)+len(spec.Schemas)),
		resolving: map[string]bool{},
	}

	for name := range reserved {
		r.taken[name] = true
	}

	files := map[string]bool{}

	for _, name := range sortedKeys(spec.Schemas) {
		schema := spec.Schemas[name]
		if schema == nil {
			continue
		}

		base := typeIdent(name)

		dartName := base
		if r.taken[dartName] {
			dartName = base + "Model"
			r.warnings = append(r.warnings, fmt.Sprintf(
				"schema %q is generated as %s because %s is already a name the generated package or forge_client exports",
				name, dartName, base))
		}

		dartName = uniqueNames([]string{dartName}, func(s string) string { return s }, r.taken, false)[0]

		r.models[name] = &componentModel{
			schemaName: name,
			schema:     schema,
			dartName:   dartName,
			file:       uniqueNames([]string{dartName}, fileStem, files, true)[0],
			kind:       kindOf(schema),
			imports:    map[string]bool{},
		}
	}

	return r
}

// kindOf classifies a component schema.
func kindOf(s *client.Schema) modelKind {
	switch {
	case len(s.OneOf) > 0 || len(s.AnyOf) > 0:
		return kindUnion
	case len(s.AllOf) > 0, len(s.Properties) > 0:
		return kindClass
	case hasEnumValues(s):
		return kindEnum
	}

	return kindAlias
}

// build constructs every component's declarations.
func (r *registry) build() {
	for _, name := range sortedKeys(r.models) {
		m := r.models[name]
		c := rctx{owner: m, nsID: name, hint: m.dartName, imports: m.imports}

		var own decl

		switch m.kind {
		case kindClass:
			own = r.buildClass(m.dartName, m.schema, c)
		case kindUnion:
			own = r.buildUnion(m.dartName, m.schema, c)
		case kindEnum:
			own = r.buildEnum(m.dartName, m.schema)
		case kindAlias:
			own = &aliasDecl{name: m.dartName, schemaName: name, doc: m.schema.Description, target: r.aliasTarget(m)}
		}

		m.decls = append([]decl{own}, m.decls...)
	}

	sortStrings(r.warnings)
}
