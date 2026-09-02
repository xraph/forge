package typescript

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// The manifest is a tree of modules rather than one file: ops.ts assembles a
// table from src/ops/<operation>.ts and re-exports src/ops-meta.ts,
// src/security.ts, src/entities.ts and src/stream-bindings.ts.
//
// Most tests here ask whether the manifest DESCRIBES something -- an
// operation, an entity row, a security scheme -- and the answer does not
// depend on which module holds it. Those read the joined view below. A test
// that is specifically about placement (that one operation is reachable
// without the others) asserts on a single module instead, and should, because
// that is the property the split exists to provide.

// manifestText joins ops.ts and every module it re-exports or assembles from.
func manifestText(spec *client.APISpec, config client.GeneratorConfig) string {
	gen := NewOpsManifestGenerator()

	return gen.Generate(spec, config) + "\n" + joinFiles(gen.GenerateModules(spec, config))
}

// hooksText joins hooks.ts and every per-hook module it re-exports.
func hooksText(spec *client.APISpec, config client.GeneratorConfig) string {
	gen := NewFacadeGenerator()

	return gen.Generate(spec, config) + "\n" + joinFiles(gen.GenerateModules(spec, config))
}

// clientManifestText is manifestText for an already-generated client, for the
// end-to-end tests that go through Generator.Generate rather than calling one
// emitter directly.
// Exported so the external typescript_test package can use it too.
func ClientManifestText(files map[string]string) string {
	return joinFilesMatching(files, func(name string) bool {
		return name == "src/ops.ts" || name == "src/ops-meta.ts" ||
			name == "src/security.ts" || name == "src/entities.ts" ||
			name == "src/stream-bindings.ts" ||
			strings.HasPrefix(name, "src/ops/")
	})
}

// clientHooksText is hooksText for an already-generated client.
// Exported for the external typescript_test package.
func ClientHooksText(files map[string]string) string {
	return joinFilesMatching(files, func(name string) bool {
		return name == "src/hooks.ts" || strings.HasPrefix(name, "src/hooks/")
	})
}

// joinFiles concatenates a file map in sorted filename order, so a failure
// message reads the same way twice.
func joinFiles(files map[string]string) string {
	return joinFilesMatching(files, func(string) bool { return true })
}

func joinFilesMatching(files map[string]string, keep func(string) bool) string {
	names := make([]string, 0, len(files))

	for name := range files {
		if keep(name) {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	var buf strings.Builder

	for _, name := range names {
		buf.WriteString("// ===== " + name + "\n")
		buf.WriteString(files[name])
		buf.WriteString("\n")
	}

	return buf.String()
}

// TestManifestModulesAreReachableIndependently is the property the whole split
// exists for, stated once: the module for one operation names that operation
// and no other.
//
// Asserted on the emitted modules rather than on a bundle because that is
// where the generator's responsibility ends -- what a bundler then does with
// two files that do not reference each other is not this package's to prove.
func TestManifestModulesAreReachableIndependently(t *testing.T) {
	files := NewOpsManifestGenerator().GenerateModules(manifestSpec(), client.GeneratorConfig{})

	mod, ok := files["src/ops/orderList.ts"]
	if !ok {
		t.Fatalf("no module for orderList; got %v", sortedNames(files))
	}

	if !strings.Contains(mod, "op_orderList") {
		t.Errorf("orderList module does not declare its own operation:\n%s", mod)
	}

	if strings.Contains(mod, "orderCreate") {
		t.Errorf("orderList module names another operation, so importing one costs both:\n%s", mod)
	}
}

// A hook module must reach exactly one operation module. If it imported the
// table instead, splitting the operations would have bought nothing.
func TestHookModuleImportsOneOperationModule(t *testing.T) {
	files := NewFacadeGenerator().GenerateModules(manifestSpec(), client.GeneratorConfig{})

	mod, ok := files["src/hooks/useOrderList.ts"]
	if !ok {
		t.Fatalf("no module for useOrderList; got %v", sortedNames(files))
	}

	if !strings.Contains(mod, "from '../ops/orderList'") {
		t.Errorf("hook module does not import its operation module:\n%s", mod)
	}

	if strings.Contains(mod, "from './ops'") || strings.Contains(mod, "from '../ops'") {
		t.Errorf("hook module reaches the whole table, which retains every operation:\n%s", mod)
	}

	if strings.Contains(mod, "orderCreate") {
		t.Errorf("hook module names an unrelated operation:\n%s", mod)
	}
}

// Without the annotation a bundler must assume a module-scope call does
// something, and keeps every binding in whatever module it lands in.
func TestHookBindingsAreAnnotatedPure(t *testing.T) {
	files := NewFacadeGenerator().GenerateModules(manifestSpec(), client.GeneratorConfig{})

	for name, content := range files {
		if !strings.Contains(content, "/*#__PURE__*/") {
			t.Errorf("%s binds without a PURE annotation:\n%s", name, content)
		}
	}
}

// A hook module imports the one binder it uses. Importing both would pull the
// mutation path into a bundle that only ever reads.
func TestHookModuleImportsOnlyTheBinderItUses(t *testing.T) {
	files := NewFacadeGenerator().GenerateModules(manifestSpec(), client.GeneratorConfig{})

	if got := files["src/hooks/useOrderList.ts"]; strings.Contains(got, "mutation") {
		t.Errorf("read hook imports the mutation binder:\n%s", got)
	}

	if got := files["src/hooks/useOrderCreate.ts"]; strings.Contains(got, "{ query }") {
		t.Errorf("write hook imports the query binder:\n%s", got)
	}
}

func sortedNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// A hook module must import exactly the types its own binding names.
//
// The two type resolvers record what they used by writing into a shared map,
// so calling the mutation one to decide a read operation's arguments -- and
// then discarding the answer -- still leaves the entity type in the import
// line. It is erased by every bundler and costs a consumer nothing, which is
// why it would have gone unnoticed; it is also an unused import in generated
// code that a repository lints.
func TestHookModuleImportsOnlyTheTypesItNames(t *testing.T) {
	spec := &client.APISpec{
		Endpoints: []client.Endpoint{{
			ID: "orderList", Method: "GET", Path: "/orders",
			RootType: "PageOrder",
			Entity:   &client.EntityRef{Type: "Order", IDField: "id"},
		}},
		Schemas: map[string]*client.Schema{
			"PageOrder": {Type: "object"},
			"Order":     {Type: "object"},
		},
	}

	mod := NewFacadeGenerator().GenerateModules(spec, client.GeneratorConfig{})["src/hooks/useOrderList.ts"]

	if !strings.Contains(mod, "import type { PageOrder } from '../types';") {
		t.Errorf("the response type the binding names must be imported:\n%s", mod)
	}

	// Order is this operation's ENTITY, which only a mutation binding names.
	if strings.Contains(mod, "Order,") || strings.Contains(mod, "{ Order }") {
		t.Errorf("a query binding imported the entity type it never names:\n%s", mod)
	}
}

// Every emitted module has to name only types it brought into scope.
//
// The manifest's `satisfies` clauses reference OperationMeta and EntityMeta,
// which are declared in ops.ts. Inside ops.ts that needs no import and is easy
// to carry over into a module by accident -- src/entities.ts did exactly that,
// and shipped a file that did not compile. Cheap to assert here; otherwise it
// surfaces in the consuming repository's build.
func TestEmittedModulesImportTheTypesTheySatisfy(t *testing.T) {
	files := NewOpsManifestGenerator().GenerateModules(manifestSpec(), client.GeneratorConfig{})

	for name, content := range files {
		for _, typeName := range []string{"OperationMeta", "EntityMeta"} {
			if !strings.Contains(content, typeName) {
				continue
			}

			if !strings.Contains(content, "import type { "+typeName+" } from '") {
				t.Errorf("%s names %s without importing it:\n%s", name, typeName, content)
			}
		}
	}
}

// Client-only mode flattens every emitted path, and the directories declared
// for pruning have to flatten with them.
//
// Left pointing at src/ops the writer finds no such directory and prunes
// nothing, so a withdrawn operation keeps a module that still compiles and
// still binds a route the server dropped. Had the directory existed it would
// have been worse: every file in it measured against a key that now reads
// ops/x.ts, matching none of them, deleted as stale on every run. This is the
// mode the consumer that asked for the split actually generates in.
func TestClientOnlyFlattensExclusiveDirsWithTheFiles(t *testing.T) {
	spec := manifestSpec()
	spec.Info.Title = "Orders"

	out, err := NewGenerator().Generate(context.Background(), spec, client.GeneratorConfig{
		Language:   "typescript",
		ClientOnly: true,
		Hooks:      true,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	for _, dir := range out.ExclusiveDirs {
		if strings.HasPrefix(dir, "src/") {
			t.Errorf("ExclusiveDirs kept the src/ prefix the files lost: %q", dir)
		}

		found := false

		for name := range out.Files {
			if strings.HasPrefix(name, dir+"/") {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("no emitted file lives under declared exclusive dir %q; pruning is a no-op", dir)
		}
	}

	if len(out.ExclusiveDirs) == 0 {
		t.Fatal("a hooks client declares no exclusive dirs; this assertion would pass vacuously")
	}
}

// clientCodecText joins the codec layer of an already-generated client: the
// table, the table-free runtime, and every per-codec module.
func clientCodecText(files map[string]string) string {
	return joinFilesMatching(files, func(name string) bool {
		return name == "src/codecs.ts" || name == "src/codec-runtime.ts" ||
			strings.HasPrefix(name, "src/codecs/")
	})
}
