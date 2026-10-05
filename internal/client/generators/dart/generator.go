package dart

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

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

	// paged and streams are what the pagination and streaming emitters
	// planned, kept for the README to describe.
	paged   []paginated
	streams []streamClient

	out *generators.GeneratedClient
}

func (e *emission) warn(w ...string) { e.out.Warnings = append(e.out.Warnings, w...) }

// exclusiveDirs are the directories this generator owns outright: the output
// writer deletes any regular file in one of them that a run did not write.
//
// They are declared whether or not a run puts anything in them. The writer
// skips a directory that does not exist, so naming an empty one costs nothing,
// and not naming it is how a regenerate with fewer features (hooks off, a
// withdrawn schema or stream) would leave the last run's files behind. Those
// files import package:forge_client, which a package without hooks no longer
// depends on, so they would break analysis, not merely sit there.
//
// lib/src is fully generated: a hand-written file in it is deleted by the next
// run. Hand-written code belongs beside lib/, or in the consuming package.
// The writer prunes only the regular files directly in each directory and
// never a subdirectory, so lib/src does not reach lib/src/models and the
// others, which are governed by their own entries, and the pub artefacts at
// the package root (.dart_tool, pubspec.lock, pubspec_overrides.yaml) are in
// none of them.
var exclusiveDirs = []string{
	"lib/src",
	"lib/src/bindings",
	"lib/src/codecs",
	"lib/src/models",
	"lib/src/streaming",
}

// emitters run in order; each writes its files into e.out.
var emitters = []func(*emission) error{
	emitSupport,
	emitModels,
	emitCodecs,
	emitPlan,
	emitErrors,
	emitRest,
	emitOps,
	emitBindings,
	emitCapabilities,
	emitPagination,
	emitStreaming,
	emitTables,
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

// finish writes the files that describe the whole package: the barrel, the
// pubspec and the README.
func finish(e *emission) {
	files := e.out.Files
	files["lib/"+e.config.PackageName+".dart"] = renderBarrel(e.spec, files)

	// A Dart package without a pubspec cannot be resolved, imported or
	// analyzed, so client-only never drops it. It drops the README, the one
	// file a consuming repository plausibly writes for itself.
	files["pubspec.yaml"] = renderPubspec(e.spec, e.config)

	if !e.config.ClientOnly {
		e.out.Instructions = renderReadme(e)
	}

	e.out.Dependencies = dependencies(e.hooks)
	e.out.ExclusiveDirs = slices.Clone(exclusiveDirs)
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

	return nil
}

// emitCodecs writes the codec runtime and one codec file per component.
func emitCodecs(e *emission) error {
	e.naming = newCodecNaming(e.table, e.reg)

	maps.Copy(e.out.Files, renderCodecFiles(e.table, e.naming))

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

// emitOps writes ops.dart and sync.dart, with hooks only. The streams table
// is part of ops.dart and follows hooks, not the streaming flag, as the
// TypeScript manifest does.
func emitOps(e *emission) error {
	if !e.hooks {
		return nil
	}

	opsFile, warnings := renderOps(e.spec, e.config, e.ops, e.naming)
	e.out.Files["lib/src/ops.dart"] = opsFile
	e.warn(warnings...)

	syncFile, warnings := renderSync(e.spec)
	e.out.Files["lib/src/sync.dart"] = syncFile
	e.warn(warnings...)

	return nil
}

// emitBindings writes one binding file per operation, with hooks only.
func emitBindings(e *emission) error {
	if !e.hooks || len(e.ops) == 0 {
		return nil
	}

	for _, op := range e.ops {
		e.out.Files["lib/src/bindings/"+op.file+".dart"] = renderBinding(op, e.reg)
	}

	return nil
}

// emitCapabilities writes capabilities.dart when the document declares any
// scope, role or permission.
func emitCapabilities(e *emission) error {
	if capabilitiesNeeded(e.spec) {
		e.out.Files["lib/src/capabilities.dart"] = renderCapabilities(e.spec, operationKeys(e.spec.Endpoints))
	}

	return nil
}

// emitPagination writes pagination.dart when pagination is on.
func emitPagination(e *emission) error {
	if e.config.Pagination && len(e.ops) > 0 {
		e.paged = planPagination(e.ops, e.paths, e.reg)
		e.out.Files["lib/src/pagination.dart"] = renderPagination(e.paged, e.reg)
	}

	return nil
}

// emitStreaming writes the typed streaming clients and the feature clients.
// They are built on forge_client's connections, so they need hooks. The
// streams table in ops.dart is not part of this: it follows hooks alone, and
// the clients below follow the streaming flag as well.
func emitStreaming(e *emission) error {
	if !e.config.IncludeStreaming {
		return nil
	}

	endpoints := len(e.spec.WebSockets) + len(e.spec.SSEs) + len(e.spec.WebTransports)

	if !e.hooks {
		if endpoints > 0 || e.config.HasAnyStreamingFeature() {
			e.warn("streaming clients are generated only with --hooks: they are built on forge_client's connections, and a package without hooks depends on package:http alone")
		}

		return nil
	}

	clients, warnings := planStreams(e.spec, e.config, e.reg, e.naming)
	e.streams = clients
	e.warn(warnings...)

	for _, sc := range clients {
		e.out.Files["lib/src/streaming/"+sc.file+".dart"] = renderStream(sc, e.reg, e.naming)
	}

	features := renderFeatures(e.spec, e.config)
	maps.Copy(e.out.Files, features)

	if len(clients) > 0 || len(features) > 0 {
		e.out.Files["lib/src/streaming/live_socket.dart"] = renderLiveSocket(e.config)
	}

	return nil
}

// emitTables writes forge-tables.json when EmitTablesJSON is set.
func emitTables(e *emission) error {
	if !e.config.EmitTablesJSON {
		return nil
	}

	data, err := buildTables(e.spec, e.config, e.ops).MarshalCanonical()
	if err != nil {
		return fmt.Errorf("render %s: %w", client.TablesFile, err)
	}

	e.out.Files[client.TablesFile] = string(data)

	return nil
}

// buildTables collects the tables this package renders in the
// language-neutral form the parity test compares with TypeScript's. Every row
// comes from the function the Dart renderers read, so the file cannot say
// something the source does not.
func buildTables(spec *client.APISpec, config client.GeneratorConfig, ops []*operation) client.GeneratedTables {
	rows := entityRows(spec, config)
	entities := make(map[string]client.TableEntity, len(rows))

	for _, row := range rows {
		entities[row.name] = client.TableEntity{IDField: row.idField, Fields: row.fields}
	}

	opRows := make(map[string]client.TableOp, len(ops))
	keys := make([]string, len(ops))

	for i, op := range ops {
		opRows[op.key] = op.row
		keys[i] = op.key
	}

	security := make(map[string]client.TableSecurity, len(spec.Security))
	for _, s := range spec.Security {
		security[s.Key] = client.TableSecurity{Type: s.Type, In: s.In, Name: s.ParamName, Scheme: s.Scheme}
	}

	return client.GeneratedTables{
		Ops:             opRows,
		Entities:        entities,
		Streams:         streamRows(spec),
		SecuritySchemes: security,
		Capabilities:    capabilityTables(spec, keys),
	}
}
