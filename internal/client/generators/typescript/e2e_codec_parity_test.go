package typescript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// codecParityFixture is the defect written as a document.
//
// Every wire name is snake_case, and the identity of each entity is a
// snake_case property that is NOT called "id" -- so a table emitting the wire
// name and a payload decoded into camelCase disagree about the one field that
// decides whether a record is an entity at all. `x-forge-id` pins the identity
// explicitly rather than leaning on the "exactly one property named id"
// heuristic, precisely so the id is a name that visibly changes under
// renaming: `order_number` -> `orderNumber`, `customer_id` -> `customerId`.
//
// `primary_customer` is the field EDGE: its key is a property name (renamed)
// and its value is the typename `Customer` (never renamed). One row proves
// both halves of the rule at once.
const codecParityFixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/orders/{id}": {
      "get": {
        "operationId": "orders.get",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Order" } } } } }
      }
    },
    "/customers/{id}": {
      "get": {
        "operationId": "customers.get",
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }
        ],
        "responses": { "200": { "description": "ok", "content": { "application/json": {
          "schema": { "$ref": "#/components/schemas/Customer" } } } } }
      }
    }
  },
  "components": {
    "schemas": {
      "Order": {
        "type": "object",
        "required": ["order_number", "primary_customer"],
        "properties": {
          "order_number": { "type": "string", "x-forge-id": true },
          "primary_customer": { "$ref": "#/components/schemas/Customer" }
        }
      },
      "Customer": {
        "type": "object",
        "required": ["customer_id", "display_name"],
        "properties": {
          "customer_id": { "type": "string", "x-forge-id": true },
          "display_name": { "type": "string" }
        }
      }
    }
  }
}`

// generateFromSpecFileWith runs the real entry point -- a spec file on disk,
// through SpecParser.ParseFile, into Generator.Generate -- under a caller-
// supplied config, so one fixture can be generated twice (camel and preserve)
// without either run being built from a hand-assembled APISpec.
func generateFromSpecFileWith(t *testing.T, path string, cfg client.GeneratorConfig) map[string]string {
	t.Helper()

	spec, err := client.NewSpecParser().ParseFile(context.Background(), path)
	require.NoError(t, err, "ParseFile")

	cfg.Hooks = true

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	require.NoError(t, err, "Generate")

	return out.Files
}

// TestEntitiesTableIsRenamedToMatchTheDecodedPayload is the Go half: the
// manifest names the fields the DECODED payload has, not the wire ones.
//
// The runtime half -- that a snake_case response actually reaches the store
// under these names, through the real transport and the real decode path --
// is TestCodecParityEndToEndThroughTheRuntime below. Either half alone proves
// nothing: Go can emit a table the runtime never reads, and the runtime can
// read a table Go never emits.
func TestEntitiesTableIsRenamedToMatchTheDecodedPayload(t *testing.T) {
	cfg := baseConfig()
	cfg.FieldNaming = client.NamingCamel

	ops := generateFromSpecFileWith(t, writeSpecFile(t, "openapi.json", codecParityFixture), cfg)["src/ops.ts"]
	require.NotEmpty(t, ops, "src/ops.ts was not generated")

	// The id field, renamed. This is the assertion the whole change exists
	// for: `order_number` here would name a property the decoded payload does
	// not carry, the normalizer would conclude Order is not an entity, and
	// nothing would be stored and nothing would be reported.
	assert.Contains(t, ops,
		`Order: { idField: 'orderNumber', fields: { primaryCustomer: 'Customer' } },`,
		"the entities row must carry the CLIENT-side names -- id and property key renamed, typename untouched\n\n%s", ops)

	assert.Contains(t, ops, `Customer: { idField: 'customerId' },`,
		"the nested entity's own identity must be renamed too\n\n%s", ops)

	// The typename side of `fields`, stated as its own negative. A rule that
	// renamed values as well as keys would emit `primaryCustomer: 'customer'`
	// -- an edge naming a table row that does not exist, which breaks the
	// entities lookup for every nested entity in the document.
	assert.NotContains(t, ops, `'customer'`,
		"a typename is not a field name and must never be renamed\n\n%s", ops)

	// Wire names must be gone from the table entirely.
	table := entitiesTable(t, ops)
	for _, wire := range []string{"order_number", "customer_id", "primary_customer"} {
		assert.NotContains(t, table, wire,
			"the wire name %q leaked into the entities table, which is consulted against a decoded payload\n\n%s", wire, ops)
	}

	// And the codec ids the runtime needs to do the decoding in the first
	// place, on the operation itself.
	assert.Contains(t, ops, `responseCodec: 'Order',`,
		"ops.ts must carry the response codec id so the generic transport decodes what the typed method decodes\n\n%s", ops)

	// The third wire-named datum in this file, for the same reason: a cache
	// tag template is resolved against the DECODED response, so `{order_number}`
	// resolves to nothing, the query is registered under no item tag, and a
	// later write to that order invalidates nothing.
	assert.Contains(t, ops, `provides: ['Order:{orderNumber}'],`,
		"the derived item tag must name the decoded field\n\n%s", ops)
	assert.Contains(t, ops, `provides: ['Customer:{customerId}'],`,
		"the derived item tag must name the decoded field\n\n%s", ops)
}

// TestPreserveNamingLeavesTheManifestByteIdentical is the negative.
//
// Under NamingPreserve with no FieldOverrides nothing renames, no codec table
// is emitted, and every codec-shaped addition above must be absent -- not
// "harmlessly empty", absent, because CI byte-diffs generated output and a
// no-op config that churns this file is a false positive on every run.
//
// The expected value is the COMPLETE file, not a substring: a whole-file
// comparison is the only form in which "byte-identical" is actually being
// asserted. These are the exact bytes the generator produced for this fixture
// before any of this change existed.
func TestPreserveNamingLeavesTheManifestByteIdentical(t *testing.T) {
	ops := generateFromSpecFileWith(t, writeSpecFile(t, "openapi.json", codecParityFixture), preserveConfig())["src/ops.ts"]

	assert.Equal(t, preservedCodecParityOps, ops,
		"NamingPreserve with no FieldOverrides must emit exactly what it emitted before codecs were plumbed through")
}

// preservedCodecParityOps is the ops.ts for codecParityFixture under
// preserveConfig(), captured verbatim.
const preservedCodecParityOps = `/**
 * Operation manifest.
 *
 * Generated. Entity identity was resolved in Go against the response schema, so
 * the runtime never has to guess which field identifies a record -- the class of
 * guess that, made wrong on a type carrying both an id and a tenant id, keys two
 * tenants' records to one cache entry.
 */

export interface OperationMeta {
  readonly method: string;
  readonly path: string;
  /** The entity this operation's cache contract is about. */
  readonly entity?: string;
  /**
   * The typename of the response document itself -- of its ELEMENTS when the
   * response is a bare array -- which is what indexes the entities table below.
   *
   * Not interchangeable with the entity field above. On a paginated read the
   * two differ: entity is 'Order' while rootType is 'PageOrder'. Normalizing
   * such a response against 'Order' would read Order's field edges against an
   * envelope's properties and descend into nothing.
   */
  readonly rootType?: string;
  readonly provides: readonly string[];
  readonly invalidates: readonly string[];
}

/**
 * A row with no idField is a signpost, not a record: an envelope, or a hop
 * between two entities. It is walked for its fields and never stored.
 */
export interface EntityMeta {
  readonly idField?: string;
  readonly fields?: Readonly<Record<string, string>>;
}

export const ops = {
  'customers.get': {
    method: 'GET',
    path: '/customers/{id}',
    entity: 'Customer',
    rootType: 'Customer',
    provides: ['Customer:{customer_id}'],
    invalidates: [],
  },
  'orders.get': {
    method: 'GET',
    path: '/orders/{id}',
    entity: 'Order',
    rootType: 'Order',
    provides: ['Order:{order_number}'],
    invalidates: [],
  },
} as const satisfies Record<string, OperationMeta>;

export const entities = {
  Customer: { idField: 'customer_id' },
  Order: { idField: 'order_number', fields: { primary_customer: 'Customer' } },
} as const satisfies Record<string, EntityMeta>;

export const streams = [
] as const;
`

// TestCodecParityEndToEndThroughTheRuntime is the proof the feature works,
// and the half Go assertions alone cannot give.
//
// It generates a real client for codecParityFixture under camelCase naming,
// drops the client-core runtime sources next to it, and under Node:
//
//  1. builds a RestTransport over the GENERATED client -- the same generic
//     `HTTPClient#request` seam the query cache drives, never a typed
//     per-endpoint method;
//  2. executes ops['orders.get'], with global fetch answering a wholly
//     snake_case wire document;
//  3. checks what the transport resolved with is camelCase -- which can only
//     happen if the manifest carried the response codec id and the transport
//     forwarded it onto the request config;
//  4. writes that result into an EntityStore against the GENERATED entities
//     table, and checks the order and its nested customer landed under
//     `Order:ord-7` and `Customer:cus-3`.
//
// Step 4 is the one that fails silently without the second half of this
// change: a table naming `order_number` against a decoded payload carrying
// `orderNumber` does not error, it just decides Order is not an entity, and
// the store stays empty.
func TestCodecParityEndToEndThroughTheRuntime(t *testing.T) {
	cfg := baseConfig()
	cfg.FieldNaming = client.NamingCamel

	files := generateFromSpecFileWith(t, writeSpecFile(t, "openapi.json", codecParityFixture), cfg)

	dir := t.TempDir()
	writeTree(t, dir, files)
	copyClientCoreSources(t, filepath.Join(dir, "src", "__core"))

	driver := `
import { RESTClient } from './rest';
import { entities, ops } from './ops';
import { EntityStore } from './__core/store';
import { resolveTags } from './__core/tags';
import { RestTransport } from './__core/transport';

// The WIRE document: every name snake_case, exactly as the server sends it.
(globalThis as any).fetch = async () =>
  new Response(
    JSON.stringify({
      order_number: 'ord-7',
      primary_customer: { customer_id: 'cus-3', display_name: 'Ada' },
    }),
    { status: 200, headers: { 'content-type': 'application/json' } },
  );

async function main() {
  const client = new RESTClient({ baseURL: 'http://example.invalid' });
  const transport = new RestTransport({ client });

  const meta = ops['orders.get'];

  // The generic path. A caller holding only an OperationMeta cannot call
  // client.orders.get(id) -- that signature is positional and per-endpoint --
  // so this is the seam the whole runtime actually goes through.
  const decoded = await transport.execute({ meta, args: { path: { id: 'ord-7' } } });

  const store = new EntityStore();
  const { skeleton } = store.write(decoded, entities, meta.rootType ?? meta.entity);

  // Exactly what QueryRegistry#settle does with this operation's provides:
  // resolve them against the request arguments and the response it got back.
  const tags = resolveTags(meta.provides, { path: { id: 'ord-7' }, response: decoded });

  console.log(
    JSON.stringify({
      decoded,
      skeleton,
      keys: [...store.keys()].sort(),
      order: store.getRecord('Order:ord-7')?.data ?? null,
      customer: store.getRecord('Customer:cus-3')?.data ?? null,
      tags: tags.tags,
      unresolvedTags: tags.unresolved,
    }),
  );
}

main().catch((err) => {
  console.error(err);
  throw err;
});
`
	writeTree(t, dir, map[string]string{"src/__driver_codec_parity.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codec_parity.ts")

	var result struct {
		Decoded        map[string]any `json:"decoded"`
		Skeleton       map[string]any `json:"skeleton"`
		Keys           []string       `json:"keys"`
		Order          map[string]any `json:"order"`
		Customer       map[string]any `json:"customer"`
		Tags           []string       `json:"tags"`
		UnresolvedTags []string       `json:"unresolvedTags"`
	}

	decodeLastLine(t, stdout, &result)

	// --- 1. the generic transport decoded, exactly as a typed method would. ---
	require.NotNil(t, result.Decoded, "the transport resolved with nothing")
	assert.Equal(t, "ord-7", result.Decoded["orderNumber"],
		"the REST transport must apply the same response codec the typed method does; got %v", result.Decoded)
	assert.NotContains(t, result.Decoded, "order_number",
		"a wire-cased field survived, so the codec id never reached HTTPClient#request; got %v", result.Decoded)

	// --- 2. and the entity landed in the store, under the right key. ---
	assert.Equal(t, []string{"Customer:cus-3", "Order:ord-7"}, result.Keys,
		"the decoded response must normalize; an entities table still naming wire fields stores NOTHING and says nothing about it")

	assert.Equal(t, map[string]any{"__ref": "Order:ord-7"}, result.Skeleton)

	require.NotNil(t, result.Order, "Order:ord-7 is not in the store")
	assert.Equal(t, "ord-7", result.Order["orderNumber"])
	assert.Equal(t, map[string]any{"__ref": "Customer:cus-3"}, result.Order["primaryCustomer"],
		"the renamed field EDGE must still be followed -- the key is renamed, the typename it points at is not")

	require.NotNil(t, result.Customer, "Customer:cus-3 is not in the store")
	assert.Equal(t, map[string]any{"customerId": "cus-3", "displayName": "Ada"}, result.Customer)

	// --- 3. and the cache tag resolved, against that same decoded response. ---
	assert.Empty(t, result.UnresolvedTags,
		"a provides template that names a wire field resolves to nothing against a decoded response: the query is registered under no item tag and a later write to this order invalidates nothing")
	assert.Equal(t, []string{"Order:ord-7"}, result.Tags)
}

// copyClientCoreSources copies packages/client-core/src into dst so the driver
// above can import the real runtime by relative path.
//
// Copying rather than importing the published package: the runtime under test
// is the working tree's, not whatever npm last installed, and esbuild resolving
// a relative path needs no node_modules to exist at all.
func copyClientCoreSources(t *testing.T, dst string) {
	t.Helper()

	// This file lives at internal/client/generators/typescript.
	src := filepath.Join("..", "..", "..", "..", "packages", "client-core", "src")

	entries, err := os.ReadDir(src)
	require.NoError(t, err, "client-core sources must be readable from the test's working directory")

	require.NoError(t, os.MkdirAll(dst, 0o755))

	copied := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ts") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(src, entry.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dst, entry.Name()), content, 0o644))

		copied++
	}

	require.NotZero(t, copied, "no client-core sources were copied; the driver would bundle nothing")
}
