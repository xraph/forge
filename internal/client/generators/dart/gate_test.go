package dart

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gateAnalysisOptions holds generated code to the contracts' analyzer
// baseline plus the core lints that matter for generated output. The rules are
// listed directly, so the package needs no dev dependency on package:lints.
const gateAnalysisOptions = `analyzer:
  language:
    strict-casts: true
    strict-inference: true
    strict-raw-types: true
linter:
  rules:
    - annotate_overrides
    - avoid_dynamic_calls
    - avoid_init_to_null
    - camel_case_types
    - constant_identifier_names
    - empty_constructor_bodies
    - library_private_types_in_public_api
    - non_constant_identifier_names
    - prefer_collection_literals
    - prefer_const_constructors
    - prefer_final_fields
    - prefer_final_locals
    - prefer_is_empty
    - public_member_api_docs
    - type_init_formals
    - unawaited_futures
    - unnecessary_const
    - unnecessary_new
    - unnecessary_this
    - use_super_parameters
`

// forgeClientDir is dart-packages/forge_client in this repository. Generated
// code is analyzed against it through a pubspec_overrides.yaml, the same
// mechanism a consuming repository uses to point a generated package at a
// local runtime.
func forgeClientDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "dart-packages", "forge_client"))
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

// requireDart skips the test only when fvm is not on PATH. The runtime ships
// in this repository, so a missing dart-packages/forge_client is a broken
// checkout and fails the test instead of skipping it: a gate that quietly
// stops compiling generated code against the real core is no gate. CI does
// not run this gate until plan 08 adds it, so until then it runs wherever a
// developer has fvm.
func requireDart(t *testing.T) string {
	t.Helper()

	fvm, err := exec.LookPath("fvm")
	if err != nil {
		t.Skip("fvm not found on PATH; skipping the Dart analyzer gate (install fvm to run it)")
	}

	if _, err := os.Stat(filepath.Join(forgeClientDir(t), "pubspec.yaml")); err != nil {
		t.Fatalf("dart-packages/forge_client/pubspec.yaml is missing, so generated code cannot be compiled against the real core: %v", err)
	}

	return fvm
}

// writePackage generates f into a temp directory set up to resolve and
// analyze: Flutter pinned through .fvmrc, forge_client overridden to the
// local package, and the gate's analysis options.
func writePackage(t *testing.T, f gateFixture) string {
	t.Helper()

	out, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
	if err != nil {
		t.Fatalf("%s: generate: %v", f.Name, err)
	}

	dir := t.TempDir()
	files := map[string]string{
		".fvmrc":                `{"flutter": "3.47.5"}` + "\n",
		"analysis_options.yaml": gateAnalysisOptions,
	}

	maps.Copy(files, out.Files)

	if f.Config.HooksEnabled() {
		files["pubspec_overrides.yaml"] = "dependency_overrides:\n  forge_client:\n    path: " + forgeClientDir(t) + "\n"
	}

	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// runFvm runs `fvm dart <args>` in dir and fails the test with its output
// when it exits non-zero.
func runFvm(t *testing.T, fvm, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), fvm, append([]string{"dart"}, args...)...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fvm dart %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}

	return string(out)
}

// TestGeneratedPackagesAnalyzeClean is the Dart counterpart of the
// TypeScript tsc gate: every fixture's output resolves and passes
// `dart analyze --fatal-infos`.
func TestGeneratedPackagesAnalyzeClean(t *testing.T) {
	fvm := requireDart(t)

	for _, f := range append(gateFixtures(), enumsFixture()) {
		t.Run(f.Name, func(t *testing.T) {
			dir := writePackage(t, f)
			runFvm(t, fvm, dir, "pub", "get")
			runFvm(t, fvm, dir, "analyze", "--fatal-infos")
		})
	}
}
