// v2/cmd/forge/plugins/client_generate_merge_test.go
package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xraph/forge/cli"
)

// Every test in this file drives the real `forge client generate` command,
// the same way client_check_test.go drives `check`: nothing here hand-builds
// an intermediate representation or calls resolveGenerationPlan directly, so
// what passes here is proof the feature reaches an actual user of the CLI,
// not just that the merge primitives in internal/client work in isolation.

// streamSpec is an AsyncAPI 3.0 document binding the same Order entity
// ordersSpec() (client_check_test.go) describes over REST, this time as a
// stream channel. Both the `operations:` block and the `x-forge-stream`
// binding are required for the channel to reach the generated `streams`
// table at all -- see internal/client/generators/typescript/e2e_merged_sources_test.go
// for why.
func streamSpec() map[string]any {
	return map[string]any{
		"asyncapi": "3.0.0",
		"info":     map[string]any{"title": "Orders Streams", "version": "1.0.0"},
		"channels": map[string]any{
			"orders": map[string]any{
				"address": "/ws/orders",
				"messages": map[string]any{
					"orderUpdated": map[string]any{
						"payload": map[string]any{"$ref": "#/components/schemas/Order"},
					},
				},
				"x-forge-stream": []any{
					map[string]any{"message": "orderUpdated", "entityType": "Order", "intent": "upsert"},
				},
			},
		},
		"operations": map[string]any{
			"orderUpdated": map[string]any{
				"action":  "receive",
				"channel": map[string]any{"$ref": "#/channels/orders"},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Order": map[string]any{
					"type":           "object",
					"x-forge-entity": map[string]any{"idField": "id"},
					"properties":     map[string]any{"id": map[string]any{"type": "string"}},
				},
			},
		},
	}
}

// writeRawFile writes content verbatim -- used where writeSpecFile's
// map[string]any-to-JSON marshaling would refuse to produce the broken
// document a test needs (an unparseable source, for instance).
func writeRawFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The feature this whole task exists for: two --from-spec sources, one REST
// and one stream, merge into one package whose ops.ts streams table actually
// names the channel from the second document. This is the same assertion
// internal/client/generators/typescript/e2e_merged_sources_test.go makes
// through the generator directly; this test makes it through the real `forge
// client generate` command instead, which is the path a user actually runs.
func TestClientGenerateMergesMultipleFromSpecSources(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())
	writeSpecFile(t, filepath.Join(dir, "asyncapi.json"), streamSpec())

	outputDir := filepath.Join(dir, "client")

	out, err := runClientCLI(t, "client", "generate",
		"--from-spec", "openapi.json",
		"--from-spec", "asyncapi.json",
		"--language", "typescript",
		"--hooks",
		"--output", outputDir,
	)
	if err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}

	ops, err := os.ReadFile(filepath.Join(outputDir, "src", "ops.ts"))
	if err != nil {
		t.Fatalf("read generated ops.ts: %v\n---generate output---\n%s", err, out)
	}

	if !strings.Contains(string(ops), "/ws/orders") {
		t.Fatalf("ops.ts does not mention the channel from the merged AsyncAPI source\n\n%s", ops)
	}
}

// A source that fails to parse must abort the whole run, not degrade to a
// package generated from only the sources that did parse. Silently dropping
// the broken source would be exactly the partial-generation failure this
// feature exists to remove, just at the CLI layer instead of inside
// resolveMergedSpec.
func TestClientGenerateAbortsOnAnyUnparseableSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())
	writeRawFile(t, filepath.Join(dir, "broken.json"), "{ this is not valid json")

	outputDir := filepath.Join(dir, "client")

	out, err := runClientCLI(t, "client", "generate",
		"--from-spec", "openapi.json",
		"--from-spec", "broken.json",
		"--output", outputDir,
	)
	if err == nil {
		t.Fatalf("generate with one unparseable source must fail, got success:\n%s", out)
	}

	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("generate must not write any output when a source fails to parse; %s exists", outputDir)
	}
}

// A merged specification with nothing in it (every source parses but
// contributes no endpoints and no streams) is rejected the same way an
// unparseable source is: resolveMergedSpec's own emptiness check, exercised
// through the real command rather than the function directly.
func TestClientGenerateRejectsAnEmptyMergedSpec(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	empty := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Empty", "version": "1.0.0"},
	}

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), empty)

	outputDir := filepath.Join(dir, "client")

	out, err := runClientCLI(t, "client", "generate", "--from-spec", "openapi.json", "--output", outputDir)
	if err == nil {
		t.Fatalf("generate from a spec with no endpoints and no streams must fail, got success:\n%s", out)
	}

	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("generate must not write any output for an empty merged spec; %s exists", outputDir)
	}
}

// A source explicitly configured in .forge-client.yml but malformed must
// fail loudly, exactly as it did before sources were merged -- it must NOT
// silently fall through to auto-discovery, even though a perfectly good
// openapi.json sits right there waiting to be auto-discovered. If this test
// ever passes because generate quietly picked up openapi.json instead of
// reporting the empty source.url, that is the regression finding 1 exists to
// catch.
func TestClientGenerateFailsLoudlyOnMalformedConfiguredURLSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Present so a silent fall-through to auto-discovery would otherwise
	// succeed -- proving the failure below is deliberate validation, not an
	// absence of anything to discover.
	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	writeRawFile(t, filepath.Join(dir, ".forge-client.yml"), "source:\n  type: url\n")

	out, err := runClientCLI(t, "client", "generate")
	if err == nil {
		t.Fatalf("generate with an empty configured source.url must fail, got success:\n%s", out)
	}

	if code := cli.GetExitCode(err); code != cli.ExitUsageError {
		t.Fatalf("exit code = %d, want %d (usage/configuration)", code, cli.ExitUsageError)
	}

	if !strings.Contains(err.Error(), "source.url is empty in .forge-client.yml") {
		t.Fatalf("error = %v, want it to name the empty source.url", err)
	}
}

// The same failure mode, for a configured file source with an empty path.
func TestClientGenerateFailsLoudlyOnMalformedConfiguredFileSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())
	writeRawFile(t, filepath.Join(dir, ".forge-client.yml"), "source:\n  type: file\n")

	_, err := runClientCLI(t, "client", "generate")
	if err == nil {
		t.Fatal("generate with an empty configured source.path must fail, got success")
	}

	if !strings.Contains(err.Error(), "source.path is empty in .forge-client.yml") {
		t.Fatalf("error = %v, want it to name the empty source.path", err)
	}
}

// And for a source.type this CLI does not recognise at all.
func TestClientGenerateFailsLoudlyOnUnknownConfiguredSourceType(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())
	writeRawFile(t, filepath.Join(dir, ".forge-client.yml"), "source:\n  type: ftp\n")

	_, err := runClientCLI(t, "client", "generate")
	if err == nil {
		t.Fatal("generate with an unrecognised configured source.type must fail, got success")
	}

	if !strings.Contains(err.Error(), "unknown source type in config: ftp") {
		t.Fatalf("error = %v, want it to name the unrecognised type", err)
	}
}

// Pins the semantics finding 3 asked to be pinned rather than changed:
// --from-spec now APPENDS to a configured source instead of replacing it. A
// .forge-client.yml naming the REST document plus one --from-spec flag
// naming the stream document must merge, exactly as two --from-spec flags
// do above -- not have the flag silently win and drop the configured REST
// source.
func TestClientGenerateAppendsFlagSourcesToConfiguredSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "rest.json"), ordersSpec())
	writeSpecFile(t, filepath.Join(dir, "asyncapi.json"), streamSpec())
	writeRawFile(t, filepath.Join(dir, ".forge-client.yml"), "source:\n  type: file\n  path: rest.json\n")

	outputDir := filepath.Join(dir, "client")

	out, err := runClientCLI(t, "client", "generate",
		"--from-spec", "asyncapi.json",
		"--language", "typescript",
		"--package", "probe",
		"--hooks",
		"--output", outputDir,
	)
	if err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}

	ops, err := os.ReadFile(filepath.Join(outputDir, "src", "ops.ts"))
	if err != nil {
		t.Fatalf("read generated ops.ts: %v\n---generate output---\n%s", err, out)
	}

	content := string(ops)

	// The REST operation from the CONFIGURED source (rest.json, never named
	// on the command line) must be present -- proving the flag-provided
	// stream source was merged in, not substituted for it.
	if !strings.Contains(content, "orderList") {
		t.Fatalf("ops.ts is missing the configured source's REST operation; --from-spec overrode "+
			"source.path instead of appending to it\n\n%s", content)
	}

	if !strings.Contains(content, "/ws/orders") {
		t.Fatalf("ops.ts is missing the --from-spec source's stream channel\n\n%s", content)
	}
}
