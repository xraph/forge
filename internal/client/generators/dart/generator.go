package dart

import (
	"context"
	"errors"
	"maps"
	"sort"

	"github.com/xraph/forge/internal/client"
	"github.com/xraph/forge/internal/client/generators"
)

// Generator generates Dart packages over package:forge_client.
type Generator struct{}

// NewGenerator creates the Dart generator.
func NewGenerator() generators.LanguageGenerator {
	return &Generator{}
}

// Name returns the generator name.
func (g *Generator) Name() string { return "dart" }

// SupportedFeatures returns the features the Dart generator emits.
func (g *Generator) SupportedFeatures() []string {
	return []string{
		generators.FeatureREST,
		generators.FeatureWebSocket,
		generators.FeatureSSE,
		generators.FeatureWebTransport,
		generators.FeatureAuth,
		generators.FeatureTypedErrors,
		generators.FeaturePolymorphicTypes,
		generators.FeatureRooms,
		generators.FeaturePresence,
		generators.FeatureTyping,
		generators.FeatureChannels,
	}
}

// Validate checks the spec can be generated.
func (g *Generator) Validate(specIface generators.APISpec) error {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return errors.New("spec is nil or invalid type")
	}

	if spec.Info.Title == "" {
		return errors.New("API title is required")
	}

	return nil
}

// emission is one run of the generator: its inputs, what earlier emitters
// resolved for later ones, and the client being built.
type emission struct {
	spec   *client.APISpec
	config client.GeneratorConfig
	hooks  bool
	table  *codecTable
	reg    *registry
	naming codecNaming
	ops    []*operation
	root   *restNode
	paths  map[*operation]string
	out    *generators.GeneratedClient
}

func (e *emission) warn(w ...string) { e.out.Warnings = append(e.out.Warnings, w...) }

func (e *emission) own(dir string) { e.out.ExclusiveDirs = append(e.out.ExclusiveDirs, dir) }

// emitters run in order; each writes its files into e.out.
var emitters = []func(*emission) error{
	emitSupport,
	emitModels,
	emitCodecs,
	emitPlan,
	emitErrors,
	emitRest,
}

// Generate produces the package.
func (g *Generator) Generate(_ context.Context, specIface generators.APISpec, configIface generators.GeneratorConfig) (*generators.GeneratedClient, error) {
	spec, ok := specIface.(*client.APISpec)
	if !ok || spec == nil {
		return nil, errors.New("spec is nil or invalid type")
	}

	config, ok := configIface.(client.GeneratorConfig)
	if !ok {
		return nil, errors.New("config is invalid type")
	}

	if err := checkFieldNameCollisions(spec, config); err != nil {
		return nil, err
	}

	e := &emission{
		spec:   spec,
		config: config,
		hooks:  config.HooksEnabled(),
		table:  buildCodecTable(spec, config),
		out: &generators.GeneratedClient{
			Files:    map[string]string{},
			Language: "dart",
			Version:  config.Version,
			Warnings: append([]string(nil), spec.Warnings...),
		},
	}

	e.reg = newRegistry(spec, config, e.table, ReservedIdentifiers())
	e.reg.build()
	e.warn(e.reg.warnings...)
	e.warn(e.table.warnings...)

	for _, emit := range emitters {
		if err := emit(e); err != nil {
			return nil, err
		}
	}

	finish(e)

	return e.out, nil
}

// finish writes the files that describe the whole package: the barrel and
// the pubspec.
func finish(e *emission) {
	files := e.out.Files
	files["lib/"+e.config.PackageName+".dart"] = renderBarrel(e.spec, files)

	// A Dart package without a pubspec cannot be resolved, imported or
	// analyzed, so client-only never drops it.
	files["pubspec.yaml"] = renderPubspec(e.spec, e.config)

	e.out.Dependencies = dependencies(e.hooks)
	sort.Strings(e.out.ExclusiveDirs)
	e.out.Warnings = dedupeMessages(e.out.Warnings)
}

// emitSupport writes lib/src/support.dart.
func emitSupport(e *emission) error {
	e.out.Files["lib/src/support.dart"] = renderSupport(e.hooks)

	return nil
}

// emitModels writes one model file per component schema.
func emitModels(e *emission) error {
	for _, name := range sortedKeys(e.reg.models) {
		m := e.reg.models[name]
		e.out.Files["lib/src/models/"+m.file+".dart"] = e.reg.renderModelFile(m)
	}

	if len(e.reg.models) > 0 {
		e.own("lib/src/models")
	}

	return nil
}

// emitCodecs writes the codec runtime and one codec file per component.
func emitCodecs(e *emission) error {
	e.naming = newCodecNaming(e.table, e.reg)

	maps.Copy(e.out.Files, renderCodecFiles(e.table, e.naming))

	e.own("lib/src/codecs")

	return nil
}

// dependencies lists the generated package's pub dependencies.
func dependencies(hooks bool) []generators.Dependency {
	deps := []generators.Dependency{{Name: "http", Version: httpConstraint, Type: "direct"}}

	if hooks {
		deps = append([]generators.Dependency{{Name: "forge_client", Version: forgeClientConstraint, Type: "direct"}}, deps...)
	}

	return deps
}

// emitPlan resolves every operation and the REST namespace tree.
func emitPlan(e *emission) error {
	ops, warnings := planOperations(e.spec, e.config, e.reg)
	e.ops = ops
	e.root, e.paths = restTree(ops, e.reg)
	e.warn(warnings...)

	return nil
}

// emitErrors writes lib/src/errors.dart.
func emitErrors(e *emission) error {
	e.out.Files["lib/src/errors.dart"] = renderErrors(e.hooks)

	return nil
}

// emitRest writes lib/src/rest.dart.
func emitRest(e *emission) error {
	e.out.Files["lib/src/rest.dart"] = renderRest(e.ops, e.root, e.paths, e.naming, e.reg, e.config.IncludeAuth)

	return nil
}
