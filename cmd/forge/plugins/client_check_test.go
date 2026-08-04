// v2/cmd/forge/plugins/client_check_test.go
package plugins

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
)

// Every test in this file drives the real `forge client check` command over a
// real spec FILE on disk and a real generated output directory. Nothing here
// hand-builds an intermediate representation: the whole value of check is that
// it agrees with generate about how a spec on disk turns into files on disk,
// and a test that assembles the IR itself cannot observe a disagreement about
// the steps on either side of it.

// runClientCLI runs one `forge client ...` invocation through the real CLI --
// the same command tree, flag parser and handler a user reaches -- and returns
// everything it printed plus the error it exited with.
func runClientCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	app := cli.New(cli.Config{
		Name:    "forge",
		Version: "test",
		Output:  &out,
	})

	if err := app.RegisterPlugin(NewClientPlugin(nil)); err != nil {
		t.Fatalf("register client plugin: %v", err)
	}

	err := app.Run(append([]string{"forge"}, args...))

	return out.String(), err
}

// ordersSpec is a small but not trivial specification: two operations, an
// entity with an inferred identity, and a component schema, so the generated
// output is more than one file.
func ordersSpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Orders", "version": "1.0.0"},
		"components": map[string]any{
			"schemas": map[string]any{
				"Order": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":    map[string]any{"type": "string"},
						"total": map[string]any{"type": "integer"},
					},
				},
			},
		},
		"paths": map[string]any{
			"/orders": map[string]any{
				"get": map[string]any{
					"operationId": "orderList",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "ok",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type":  "array",
										"items": map[string]any{"$ref": "#/components/schemas/Order"},
									},
								},
							},
						},
					},
				},
				"post": map[string]any{
					"operationId": "orderCreate",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/Order"},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{
							"description": "created",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Order"},
								},
							},
						},
					},
				},
			},
		},
	}
}

// writeSpecFile marshals a spec document to a path on disk.
func writeSpecFile(t *testing.T, path string, doc map[string]any) {
	t.Helper()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create spec directory: %v", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
}

// generatedProject sets up a temp working directory holding a spec and a
// freshly generated client, and makes it the process working directory so
// configuration resolution starts somewhere clean rather than inside this
// repository.
func generatedProject(t *testing.T) (dir, outputDir string) {
	t.Helper()

	dir = t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	outputDir = filepath.Join(dir, "client")

	out, err := runClientCLI(t, "client", "generate", "--from-spec", "openapi.json", "--output", outputDir)
	if err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read generated output: %v", err)
	}

	if len(entries) == 0 {
		t.Fatalf("generate produced no files in %s", outputDir)
	}

	return dir, outputDir
}

// The baseline: a tree nobody has touched since generation passes, and exits 0.
//
// This is also the determinism check. The generator sorts its maps by
// construction; if this test ever fails, the fault is in the generator, and
// normalising the comparison to hide it would turn every subsequent check run
// into noise.
func TestClientCheckPassesOnFreshlyGeneratedOutput(t *testing.T) {
	_, outputDir := generatedProject(t)

	out, err := runClientCLI(t, "client", "check", "--from-spec", "openapi.json", "--output", outputDir)
	if err != nil {
		t.Fatalf("check on an unmodified tree should exit 0, got %v (exit %d)\n%s",
			err, cli.GetExitCode(err), out)
	}

	if !strings.Contains(out, "up to date") {
		t.Fatalf("check should say the client is up to date, printed:\n%s", out)
	}
}

// check must resolve its spec source the same way generate does. Here neither
// command is given --from-spec: both auto-discover ./openapi.json. A check that
// resolved configuration even slightly differently would report drift on a tree
// generate had just written.
func TestClientCheckResolvesConfigurationLikeGenerate(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	if out, err := runClientCLI(t, "client", "generate"); err != nil {
		t.Fatalf("generate with defaults: %v\n%s", err, out)
	}

	out, err := runClientCLI(t, "client", "check")
	if err != nil {
		t.Fatalf("check with the same defaults should exit 0, got %v (exit %d)\n%s",
			err, cli.GetExitCode(err), out)
	}
}

// A committed file whose contents no longer match must fail, and must name the
// file. "Output differs" alone forces whoever hit this in CI to reproduce the
// run locally before they can start thinking.
func TestClientCheckFailsOnMutatedFile(t *testing.T) {
	_, outputDir := generatedProject(t)

	target := firstGeneratedFile(t, outputDir)

	original, err := os.ReadFile(filepath.Join(outputDir, target))
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}

	mutated := append([]byte("// a hand edit nobody remembers making\n"), original...)
	if err := os.WriteFile(filepath.Join(outputDir, target), mutated, 0o600); err != nil {
		t.Fatalf("mutate %s: %v", target, err)
	}

	out, err := runClientCLI(t, "client", "check", "--from-spec", "openapi.json", "--output", outputDir)
	if err == nil {
		t.Fatalf("check should have failed on a mutated file, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (drift)", code, cli.ExitError)
	}

	if !strings.Contains(out, target) {
		t.Fatalf("check must name the file that differs (%s), printed:\n%s", target, out)
	}

	if !strings.Contains(out, "-// a hand edit nobody remembers making") {
		t.Fatalf("check must show a readable diff of the change, printed:\n%s", out)
	}
}

// A file the generator produces and the tree does not have is drift. This is
// the case a contents-only comparison misses entirely.
func TestClientCheckFailsOnMissingFile(t *testing.T) {
	_, outputDir := generatedProject(t)

	target := firstGeneratedFile(t, outputDir)

	if err := os.Remove(filepath.Join(outputDir, target)); err != nil {
		t.Fatalf("remove %s: %v", target, err)
	}

	out, err := runClientCLI(t, "client", "check", "--from-spec", "openapi.json", "--output", outputDir)
	if err == nil {
		t.Fatalf("check should have failed on a missing file, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (drift)", code, cli.ExitError)
	}

	if !strings.Contains(out, target) {
		t.Fatalf("check must name the missing file (%s), printed:\n%s", target, out)
	}
}

// A file in the committed tree that the generator no longer produces is drift
// too: it still compiles and still exports its old surface, and it is the file
// most likely to be imported by accident months later.
func TestClientCheckFailsOnExtraFile(t *testing.T) {
	_, outputDir := generatedProject(t)

	extra := filepath.Join(outputDir, "orphaned_endpoint.go")
	if err := os.WriteFile(extra, []byte("package client\n\n// left over from a spec that no longer has this route\n"), 0o600); err != nil {
		t.Fatalf("write extra file: %v", err)
	}

	out, err := runClientCLI(t, "client", "check", "--from-spec", "openapi.json", "--output", outputDir)
	if err == nil {
		t.Fatalf("check should have failed on an extra file, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (drift)", code, cli.ExitError)
	}

	if !strings.Contains(out, "orphaned_endpoint.go") {
		t.Fatalf("check must name the extra file, printed:\n%s", out)
	}

	if !strings.Contains(out, "not generated") {
		t.Fatalf("check must say the extra file is not generated, printed:\n%s", out)
	}
}

// A spec change that the committed client predates is the case this whole
// command exists for.
func TestClientCheckFailsWhenSpecMovedAhead(t *testing.T) {
	dir, outputDir := generatedProject(t)

	spec := ordersSpec()
	paths, _ := spec["paths"].(map[string]any)
	paths["/invoices"] = map[string]any{
		"get": map[string]any{
			"operationId": "invoiceList",
			"responses": map[string]any{
				"200": map[string]any{"description": "ok"},
			},
		},
	}

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), spec)

	out, err := runClientCLI(t, "client", "check", "--from-spec", "openapi.json", "--output", outputDir)
	if err == nil {
		t.Fatalf("check should have failed after the spec gained an endpoint, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (drift)", code, cli.ExitError)
	}
}

// Build artefacts a developer's tooling leaves in the output directory are not
// drift. Without this, check fails on every machine where anyone ever built the
// generated client -- which is every machine that uses it.
func TestClientCheckIgnoresBuildArtefacts(t *testing.T) {
	_, outputDir := generatedProject(t)

	nodeModules := filepath.Join(outputDir, "node_modules", "left-pad")
	if err := os.MkdirAll(nodeModules, 0o750); err != nil {
		t.Fatalf("create node_modules: %v", err)
	}

	if err := os.WriteFile(filepath.Join(nodeModules, "index.js"), []byte("module.exports = 1\n"), 0o600); err != nil {
		t.Fatalf("write node_modules file: %v", err)
	}

	out, err := runClientCLI(t, "client", "check", "--from-spec", "openapi.json", "--output", outputDir)
	if err != nil {
		t.Fatalf("check should ignore node_modules, got %v\n%s", err, out)
	}
}

// An output directory that was never generated reports as drift, not as a
// crash: "the client does not exist" is the most extreme staleness there is.
func TestClientCheckFailsWhenOutputDirectoryIsAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	out, err := runClientCLI(t, "client", "check", "--from-spec", "openapi.json", "--output", filepath.Join(dir, "never-generated"))
	if err == nil {
		t.Fatalf("check should have failed with no committed client at all, printed:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d (drift)", code, cli.ExitError)
	}
}

// firstGeneratedFile returns a deterministic top-level file from the generated
// output, so a test can mutate or remove "a generated file" without hard-coding
// a generator's file naming.
func firstGeneratedFile(t *testing.T, outputDir string) string {
	t.Helper()

	var names []string

	err := filepath.WalkDir(outputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(outputDir, path)
		if relErr != nil {
			return relErr
		}

		names = append(names, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		t.Fatalf("walk generated output: %v", err)
	}

	if len(names) == 0 {
		t.Fatalf("no generated files in %s", outputDir)
	}

	sortStrings(names)

	return names[0]
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
