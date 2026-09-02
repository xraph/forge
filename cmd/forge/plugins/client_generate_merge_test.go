// v2/cmd/forge/plugins/client_generate_merge_test.go
package plugins

import (
	"os"
	"path/filepath"
	"sort"
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

	ops, err := readManifestTree(outputDir)
	if err != nil {
		t.Fatalf("read generated manifest: %v\n---generate output---\n%s", err, out)
	}

	if !strings.Contains(ops, "/ws/orders") {
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

	ops, err := readManifestTree(outputDir)
	if err != nil {
		t.Fatalf("read generated manifest: %v\n---generate output---\n%s", err, out)
	}

	content := ops

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

// `list` inspects one document, but its --from-spec flag is repeatable
// because generate and check need it to be. Passing two and describing only
// the first, with no signal at all, is the same silent degradation this
// feature exists to remove -- so it says which one it used.
func TestClientListWarnsWhenGivenMoreThanOneSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())
	writeSpecFile(t, filepath.Join(dir, "asyncapi.json"), streamSpec())

	out, err := runClientCLI(t, "client", "list",
		"--from-spec", "openapi.json",
		"--from-spec", "asyncapi.json",
	)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}

	if !strings.Contains(out, "openapi.json") || !strings.Contains(out, "ignoring the other 1") {
		t.Fatalf("list with two --from-spec values must say which one it used and how many it "+
			"ignored; it printed:\n%s", out)
	}
}

// One source, the ordinary case, must stay quiet.
func TestClientListIsSilentWithOneSource(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	out, err := runClientCLI(t, "client", "list", "--from-spec", "openapi.json")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}

	if strings.Contains(out, "ignoring the other") {
		t.Fatalf("list with one source must not warn about ignored sources:\n%s", out)
	}
}

// The end-to-end shape of the duplicate-route bug, through the real command
// and the DEFAULT language: two OpenAPI documents sharing a route (the same
// /orders operations, as a gateway document and the service behind it would)
// must produce one Go method for it, not two -- `method Client.OrderList
// already declared` is a package that cannot be built. The warning has to
// reach the terminal too, which it only does if the Go generator carries
// spec warnings through.
func TestClientGenerateDropsRoutesDeclaredByTwoSources(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeSpecFile(t, filepath.Join(dir, "openapi.json"), ordersSpec())

	// A second REST document repeating one of the same routes.
	duplicate := ordersSpec()
	duplicate["info"] = map[string]any{"title": "Orders Gateway", "version": "1.0.0"}
	writeSpecFile(t, filepath.Join(dir, "gateway.json"), duplicate)

	outputDir := filepath.Join(dir, "client")

	out, err := runClientCLI(t, "client", "generate",
		"--from-spec", "openapi.json",
		"--from-spec", "gateway.json",
		"--output", outputDir,
	)
	if err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}

	rest, err := os.ReadFile(filepath.Join(outputDir, "rest.go"))
	if err != nil {
		t.Fatalf("read generated rest.go: %v\n---generate output---\n%s", err, out)
	}

	if n := strings.Count(string(rest), "func (c *Client) OrderList("); n != 1 {
		t.Fatalf("rest.go declares OrderList %d times, want 1 -- a duplicate route reached the "+
			"generator\n\n%s", n, rest)
	}

	if !strings.Contains(out, "declared in more than one source") {
		t.Fatalf("the Go run must report the dropped duplicate route; it printed:\n%s", out)
	}
}

// readManifestTree returns ops.ts together with every module it re-exports or
// assembles from, joined.
//
// The manifest is a tree now -- ops.ts is a barrel over src/ops-meta.ts,
// src/security.ts, src/entities.ts, src/stream-bindings.ts and one module per
// operation under src/ops/ -- so a test asking whether the manifest describes
// an operation or a channel has to read the tree, not the barrel. Reading only
// ops.ts would still find the operation KEYS, because the assembled table
// names them, and would find nothing else: not a method, not a path, not a
// stream channel.
func readManifestTree(outputDir string) (string, error) {
	src := filepath.Join(outputDir, "src")

	paths := []string{
		filepath.Join(src, "ops.ts"),
		filepath.Join(src, "ops-meta.ts"),
		filepath.Join(src, "security.ts"),
		filepath.Join(src, "entities.ts"),
		filepath.Join(src, "stream-bindings.ts"),
	}

	modules, err := filepath.Glob(filepath.Join(src, "ops", "*.ts"))
	if err != nil {
		return "", err
	}

	sort.Strings(modules)

	var buf strings.Builder

	for _, path := range append(paths, modules...) {
		data, err := os.ReadFile(path)
		if err != nil {
			// Only ops.ts is unconditional: a document with no security
			// schemes emits no security.ts, and one with no operations emits
			// no src/ops at all.
			if os.IsNotExist(err) && !strings.HasSuffix(path, "ops.ts") {
				continue
			}

			return "", err
		}

		buf.Write(data)
		buf.WriteString("\n")
	}

	return buf.String(), nil
}
