package dart

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestBarrelExportsEveryPublicFile(t *testing.T) {
	barrel := file(t, generate(t, fixture(t, "default")), "lib/orders_forge_client.dart")

	assertContains(t, "barrel", barrel,
		"library;",
		"export 'src/bindings/orders_get.dart';",
		"export 'src/capabilities.dart';",
		"export 'src/errors.dart';",
		"export 'src/models/order.dart';",
		"export 'src/ops.dart';",
		"export 'src/pagination.dart';",
		"export 'src/rest.dart';",
		"export 'src/streaming/live_socket.dart';",
		"export 'src/streaming/rooms.dart';",
		"export 'src/support.dart' show Assign, Int64, Json, Unchanged, Value;",
	)

	for _, hidden := range []string{"src/codecs/", "export 'src/sync.dart'", "export 'src/support.dart';"} {
		if strings.Contains(barrel, hidden) {
			t.Errorf("barrel must not export %s:\n%s", hidden, barrel)
		}
	}
}

func TestPubspecPinsTheContractVersions(t *testing.T) {
	pubspec := file(t, generate(t, fixture(t, "default")), "pubspec.yaml")

	assertContains(t, "pubspec.yaml", pubspec,
		"name: orders_forge_client",
		"publish_to: none",
		"  sdk: ^3.13.0",
		"  forge_client: ^1.0.0-dev",
		"  http: ^1.6.0",
	)
}

func TestExclusiveDirsCoverEveryGeneratedDirectoryAndNothingElse(t *testing.T) {
	out := generate(t, fixture(t, "default"))

	want := []string{"lib/src/bindings", "lib/src/codecs", "lib/src/models", "lib/src/streaming"}
	if !slices.Equal(out.ExclusiveDirs, want) {
		t.Errorf("ExclusiveDirs = %v, want %v", out.ExclusiveDirs, want)
	}

	noHooks := generate(t, fixture(t, "no-hooks"))
	if !slices.Equal(noHooks.ExclusiveDirs, []string{"lib/src/codecs", "lib/src/models"}) {
		t.Errorf("ExclusiveDirs without hooks = %v", noHooks.ExclusiveDirs)
	}
}

// Every directory the generator owns holds only generated files, so a file the
// run did not write is stale by definition.
func TestEveryGeneratedDirectoryIsDeclaredExclusive(t *testing.T) {
	for _, f := range append(gateFixtures(), streamingFixture(), paginationFixture()) {
		t.Run(f.Name, func(t *testing.T) {
			out := generate(t, f)

			for name := range out.Files {
				dir := filepath.ToSlash(filepath.Dir(name))
				if dir == "lib/src" || dir == "lib" || dir == "." {
					continue
				}

				if !slices.Contains(out.ExclusiveDirs, dir) {
					t.Errorf("%s is written into %s, which is not in ExclusiveDirs %v", name, dir, out.ExclusiveDirs)
				}
			}
		})
	}
}

// A withdrawn operation must lose its binding file, and pruning must never
// reach the pub artefacts beside the package.
func TestWithdrawnOperationLosesItsBindingFile(t *testing.T) {
	dir := t.TempDir()
	writer := client.NewOutputManager()

	f := fixture(t, "default")
	if err := writer.WriteClient(generate(t, f), dir); err != nil {
		t.Fatal(err)
	}

	for _, artefact := range []string{".dart_tool/package_config.json", "pubspec.lock", "pubspec_overrides.yaml"} {
		path := filepath.Join(dir, artefact)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte("kept"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stale := filepath.Join(dir, "lib", "src", "bindings", "orders_delete.dart")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("first run did not write %s: %v", stale, err)
	}

	f.Spec = ordersSpec()
	f.Spec.Endpoints = slices.DeleteFunc(f.Spec.Endpoints, func(ep client.Endpoint) bool { return ep.OperationID == "orders.delete" })

	if err := writer.WriteClient(generate(t, f), dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("%s survived the operation's withdrawal", stale)
	}

	for _, artefact := range []string{".dart_tool/package_config.json", "pubspec.lock", "pubspec_overrides.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, artefact)); err != nil {
			t.Errorf("pruning removed %s: %v", artefact, err)
		}
	}
}

// The same holds for the other generated directories: a model, a codec and a
// streaming client the specification no longer declares must go.
func TestWithdrawnSchemasAndStreamsLoseTheirFiles(t *testing.T) {
	dir := t.TempDir()
	writer := client.NewOutputManager()

	f := fixture(t, "default")
	if err := writer.WriteClient(generate(t, f), dir); err != nil {
		t.Fatal(err)
	}

	stale := []string{
		"lib/src/models/customer.dart",
		"lib/src/codecs/customer_codec.dart",
		"lib/src/streaming/telemetry_transport.dart",
	}

	for _, name := range stale {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Fatalf("first run did not write %s: %v", name, err)
		}
	}

	// A file this generator never wrote, in a directory it owns, is stale too.
	stray := filepath.Join(dir, "lib", "src", "streaming", "hand_written.dart")
	if err := os.WriteFile(stray, []byte("// stray\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f.Spec = ordersSpec()
	delete(f.Spec.Schemas, "Customer")
	delete(f.Spec.Entities, "Customer")
	f.Spec.WebTransports = nil

	// Drop everything that still names the withdrawn schema.
	delete(f.Spec.Schemas["Order"].Properties, "customer")

	f.Spec.Endpoints = slices.DeleteFunc(f.Spec.Endpoints, func(ep client.Endpoint) bool { return ep.OperationID == "uploads.create" })
	f.Spec.SSEs = nil

	if err := writer.WriteClient(generate(t, f), dir); err != nil {
		t.Fatal(err)
	}

	for _, name := range append(stale, "lib/src/streaming/hand_written.dart") {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Errorf("%s survived regeneration", name)
		}
	}
}

// Client-only keeps pubspec.yaml, without which a Dart package cannot be
// resolved, and drops only the README.
func TestClientOnlyKeepsThePubspec(t *testing.T) {
	out := generate(t, fixture(t, "client-only"))

	file(t, out, "pubspec.yaml")

	if out.Instructions != "" {
		t.Errorf("client-only must not write a README, got:\n%s", out.Instructions)
	}
}

func TestReadmeDescribesTheGeneratedPackage(t *testing.T) {
	readme := generate(t, fixture(t, "default")).Instructions

	assertContains(t, "README", readme,
		"# orders_forge_client",
		"import 'package:orders_forge_client/orders_forge_client.dart';",
		"RestClient(baseUrl: Uri.parse('https://api.example.com'))",
		"api.orders.get(",
		"## Use the bindings",
		"ordersGet(",
		"`Int64`",
	)
}

// The README names only what the run wrote: each optional section follows the
// file it describes.
func TestReadmeSectionsFollowTheGeneratedFiles(t *testing.T) {
	def := generate(t, fixture(t, "default"))

	assertContains(t, "README", def.Instructions,
		"## Paginate",
		"ordersListPaginated",
		"## Capabilities",
		"lib/src/capabilities.dart",
		"## Streaming",
		"StreamingClient",
	)

	// Without hooks the package depends on package:http alone, so the README
	// cannot mention the runtime or anything built on it.
	noHooks := generate(t, fixture(t, "no-hooks")).Instructions

	for _, absent := range []string{"## Use the bindings", "## Streaming", "package:forge_client"} {
		if strings.Contains(noHooks, absent) {
			t.Errorf("no-hooks README mentions %q though the package has none of it:\n%s", absent, noHooks)
		}
	}

	minimal := generate(t, fixture(t, "minimal")).Instructions

	for _, absent := range []string{"## Paginate", "## Capabilities"} {
		if strings.Contains(minimal, absent) {
			t.Errorf("minimal README documents %q though the spec has none of it:\n%s", absent, minimal)
		}
	}

	intOut := generate(t, fixture(t, "int64-int")).Instructions
	assertContains(t, "int64-int README", intOut, "--int64=int")
}

func TestReadmeNeverUsesAnEmDash(t *testing.T) {
	for _, f := range gateFixtures() {
		if strings.ContainsRune(generate(t, f).Instructions, '\u2014') {
			t.Errorf("%s README contains an em dash", f.Name)
		}
	}
}

func TestTablesFileOnlyOnRequest(t *testing.T) {
	f := fixture(t, "default")

	if _, ok := generate(t, f).Files[client.TablesFile]; ok {
		t.Errorf("%s emitted without EmitTablesJSON", client.TablesFile)
	}

	f.Config.EmitTablesJSON = true
	assertContains(t, client.TablesFile, file(t, generate(t, f), client.TablesFile), `"orders.get": {`, `"idempotent": true`)
}

func TestGenerationIsDeterministic(t *testing.T) {
	fixtures := append(gateFixtures(), streamingFixture(), paginationFixture(), enumsFixture(), restFixture())
	fixtures = append(fixtures, capabilitiesFixtures()...)

	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			f.Config.EmitTablesJSON = true
			first := generate(t, f)

			for i := 1; i < 12; i++ {
				next, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
				if err != nil {
					t.Fatal(err)
				}

				if len(next.Files) != len(first.Files) {
					t.Fatalf("run %d: %d files, want %d", i, len(next.Files), len(first.Files))
				}

				for name, content := range first.Files {
					if next.Files[name] != content {
						t.Fatalf("run %d: %s differs from run 0", i, name)
					}
				}

				if next.Instructions != first.Instructions {
					t.Fatalf("run %d: README differs from run 0", i)
				}

				if !slices.Equal(next.Warnings, first.Warnings) || !slices.Equal(next.ExclusiveDirs, first.ExclusiveDirs) {
					t.Fatalf("run %d: warnings or exclusive dirs differ from run 0", i)
				}
			}
		})
	}
}
