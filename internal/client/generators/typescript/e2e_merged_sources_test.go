package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// mergedRestDoc and mergedStreamDoc are two independent documents describing
// the same entity: mergedRestDoc is the REST surface, mergedStreamDoc is the
// stream surface. Nothing in one references the other by file -- MergeSpecs
// is what has to bring them together.
const mergedRestDoc = `
openapi: 3.1.0
info:
  title: Orders
  version: 1.0.0
paths:
  /orders:
    get:
      operationId: listOrders
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Order'
components:
  schemas:
    Order:
      type: object
      x-forge-entity:
        idField: id
      properties:
        id:
          type: string
`

// mergedStreamDoc carries both an `operations:` block and an `x-forge-stream:`
// binding on the channel. Both are load-bearing, not decoration:
//
//   - parseAsyncAPI (spec_parser.go) builds spec.WebSockets only by ranging
//     asyncAPISpec.Operations and following each operation's Channel.Ref --
//     a document with `channels:` alone yields zero stream endpoints, no
//     matter how the channel itself is described.
//   - writeStreams (opsmanifest.go) only emits a channel into the generated
//     `streams` table when that channel's StreamBindings is non-empty, and
//     StreamBindings is populated exclusively from the channel's
//     `x-forge-stream` extension -- not merely from the referenced schema
//     carrying `x-forge-entity`. Without this block the channel would still
//     become a WebSocket endpoint, but `streams` would stay empty and the
//     one assertion this file exists for would never have been able to fail.
const mergedStreamDoc = `
asyncapi: 3.0.0
info:
  title: Orders Streams
  version: 1.0.0
channels:
  orders:
    address: /ws/orders
    messages:
      orderUpdated:
        payload:
          $ref: '#/components/schemas/Order'
    x-forge-stream:
      - message: orderUpdated
        entityType: Order
        intent: upsert
operations:
  orderUpdated:
    action: receive
    channel:
      $ref: '#/channels/orders'
components:
  schemas:
    Order:
      type: object
      x-forge-entity:
        idField: id
      properties:
        id:
          type: string
`

// generateFromSpecFiles is the plural form of generateFromSpecFile: it parses
// each document without resolving, merges, resolves once, then drives the
// generator exactly as the singular helper does (see generateFromMergedSpec
// in e2e_specfile_test.go).
func generateFromSpecFiles(t *testing.T, paths ...string) map[string]string {
	t.Helper()

	parser := client.NewSpecParser()
	specs := make([]*client.APISpec, 0, len(paths))

	for _, p := range paths {
		spec, err := parser.ParseFileUnresolved(context.Background(), p)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}

		specs = append(specs, spec)
	}

	merged := client.MergeSpecs(specs...)
	if merged == nil {
		t.Fatal("MergeSpecs returned nil")
	}

	client.ResolveEntityFields(merged)

	return generateFromMergedSpec(t, merged)
}

// mustHaveBothKinds parses and merges rest and stream exactly as
// generateFromSpecFiles does, and fails the test unless the merged spec
// genuinely carries both a REST endpoint and a stream endpoint -- so a
// fixture regression (say, an AsyncAPI document missing its `operations:`
// block) is reported directly rather than showing up as a mysterious
// missing file or an always-empty streams table three layers down.
func mustHaveBothKinds(t *testing.T, rest, stream string) {
	t.Helper()

	parser := client.NewSpecParser()

	restSpec, err := parser.ParseFileUnresolved(context.Background(), rest)
	if err != nil {
		t.Fatalf("parse %s: %v", rest, err)
	}

	streamSpec, err := parser.ParseFileUnresolved(context.Background(), stream)
	if err != nil {
		t.Fatalf("parse %s: %v", stream, err)
	}

	merged := client.MergeSpecs(restSpec, streamSpec)
	if merged == nil {
		t.Fatal("MergeSpecs returned nil")
	}

	if len(merged.Endpoints) == 0 {
		t.Fatal("merged spec has no REST endpoints; the fixture or the merge is broken")
	}

	if len(merged.WebSockets) == 0 {
		t.Fatal("merged spec has no stream endpoints; the AsyncAPI fixture's operations: block did not produce one")
	}
}

func TestMergedSourcesProduceOnePackageWithBoth(t *testing.T) {
	rest := writeSpecFile(t, "openapi.yaml", mergedRestDoc)
	stream := writeSpecFile(t, "asyncapi.yaml", mergedStreamDoc)

	mustHaveBothKinds(t, rest, stream)

	files := generateFromSpecFiles(t, rest, stream)

	for _, want := range []string{"ops.ts", "hooks.ts", "rest.ts", "websocket.ts"} {
		found := false

		for name := range files {
			if strings.HasSuffix(name, "/"+want) || name == want {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("%s was not generated from a merged pair of documents (files: %v)", want, fileNames(files))
		}
	}
}

// TestMergedSourcesPopulateStreamsManifest is the assertion that makes this
// feature real. A package containing both file sets while `streams` stays
// empty is exactly the bug being fixed: every file is present and
// `{ live: true }` still does nothing.
func TestMergedSourcesPopulateStreamsManifest(t *testing.T) {
	rest := writeSpecFile(t, "openapi.yaml", mergedRestDoc)
	stream := writeSpecFile(t, "asyncapi.yaml", mergedStreamDoc)

	mustHaveBothKinds(t, rest, stream)

	files := generateFromSpecFiles(t, rest, stream)

	ops, ok := files["src/ops.ts"]
	if !ok {
		t.Fatal("src/ops.ts was not generated from a merged spec")
	}

	// writeStreams (opsmanifest.go) always emits `export const streams = [
	// ... ] as const;`; an empty table is the array with nothing between the
	// brackets, not the object-literal shape `streams: {}` a naive check
	// might expect.
	if strings.Contains(ops, "export const streams = [\n] as const;") {
		t.Errorf("ops.ts carries an empty streams table; { live: true } would do nothing\n\n%s", ops)
	}

	if !strings.Contains(ops, "export const streams = [") {
		t.Errorf("ops.ts does not export a streams table at all\n\n%s", ops)
	}

	if !strings.Contains(ops, "/ws/orders") {
		t.Errorf("ops.ts streams table does not mention the channel from the AsyncAPI document\n\n%s", ops)
	}
}

func TestSingleSpecFileStillGeneratesIdentically(t *testing.T) {
	path := writeSpecFile(t, "openapi.yaml", mergedRestDoc)

	singular := generateFromSpecFile(t, path)
	plural := generateFromSpecFiles(t, path)

	for name, want := range singular {
		if plural[name] != want {
			t.Errorf("%s differs between the single-source and merge paths", name)
		}
	}

	for name := range plural {
		if _, ok := singular[name]; !ok {
			t.Errorf("%s was produced by the merge path but not the single-source path", name)
		}
	}
}

func fileNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	return names
}
