package typescript

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// specFileFixture is a complete OpenAPI document, written as text.
//
// Written as text ON PURPOSE. Every other test covering ops.ts and hooks.ts --
// including the one named "end to end" -- hand-builds client.APISpec with
// Endpoint.ID already populated, and neither intermediate-representation
// builder ever populates Endpoint.ID for a REST endpoint. So those fixtures
// agreed with each other and disagreed with production, and the emitters
// keying off ID produced `”: { ... }` twice in ops.ts and `export const use =
// query(ops.”);` twice in hooks.ts -- a syntax error, a duplicate object key
// and a duplicate `const`, surviving fourteen reviews because no test ever
// entered through SpecParser.
//
// The operations here deliberately cover all three key sources:
//   - `orders.list` / `orders.create`: a plain dotted operationId.
//   - `list-orders-legacy`: an operationId that is not a bare identifier, so
//     the manifest must quote the key and the hook must reach it with bracket
//     access rather than a dot.
//   - `/health`: no operationId at all, so the key is derived from the path.
const specFileFixture = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/orders": {
      "get": {
        "operationId": "orders.list",
        "responses": {
          "200": {
            "description": "ok",
            "content": { "application/json": { "schema": {
              "type": "array",
              "items": { "$ref": "#/components/schemas/Order" }
            } } }
          }
        }
      },
      "post": {
        "operationId": "orders.create",
        "responses": {
          "201": {
            "description": "created",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Order" } } }
          }
        }
      }
    },
    "/orders/legacy": {
      "get": {
        "operationId": "list-orders-legacy",
        "responses": {
          "200": {
            "description": "ok",
            "content": { "application/json": { "schema": {
              "type": "array",
              "items": { "$ref": "#/components/schemas/Order" }
            } } }
          }
        }
      }
    },
    "/health": {
      "get": {
        "responses": { "200": { "description": "ok" } }
      }
    }
  },
  "components": {
    "schemas": {
      "Order": {
        "type": "object",
        "properties": {
          "id": { "type": "string" },
          "total": { "type": "integer" }
        }
      }
    }
  }
}`

// writeSpecFile writes content to a temp file with the given name and returns
// its path.
func writeSpecFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	return path
}

// generateFromSpecFile runs the REAL entry point: a spec file on disk, through
// SpecParser.ParseFile, into Generator.Generate. Nothing in the intermediate
// representation is set by hand -- that is the entire point of this file, so
// resist any convenience helper that builds an APISpec literal.
func generateFromSpecFile(t *testing.T, path string) map[string]string {
	t.Helper()

	spec, err := client.NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	return generateFromMergedSpec(t, spec)
}

// generateFromMergedSpec is everything generateFromSpecFile does after it has
// an *client.APISpec in hand -- split out so a merged-sources caller (parse
// each document unresolved, MergeSpecs, resolve once) can drive the exact
// same generation path a single spec file does. Nothing below this point may
// diverge from generateFromSpecFile's old body, or the merged-sources E2E
// test stops testing the real generation path.
func generateFromMergedSpec(t *testing.T, spec *client.APISpec) map[string]string {
	t.Helper()

	cfg := baseConfig()
	cfg.Hooks = true

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	return out.Files
}

func TestGenerateFromSpecFileEmitsRealOperationKeys(t *testing.T) {
	files := generateFromSpecFile(t, writeSpecFile(t, "openapi.json", specFileFixture))

	ops, ok := files["src/ops.ts"]
	if !ok {
		t.Fatal("src/ops.ts was not generated")
	}

	hooks, ok := files["src/hooks.ts"]
	if !ok {
		t.Fatal("src/hooks.ts was not generated")
	}

	for _, want := range []string{
		"'orders.list': {",
		"'orders.create': {",
		"'list-orders-legacy': {",
		// No operationId in the spec: derived from method + path, by the same
		// rule rest.go applies.
		"'get.health': {",
		"entity: 'Order'",
		"Order: { idField: 'id' }",
	} {
		if !strings.Contains(ops, want) {
			t.Fatalf("ops.ts is missing %q\n\n%s", want, ops)
		}
	}

	for _, want := range []string{
		"import type { Order } from './types';",
		"export const useOrdersList = query(ops['orders.list']);",
		"export const useOrdersCreate = mutation<Order, Order>(ops['orders.create']);",
		"export const useListOrdersLegacy = query(ops['list-orders-legacy']);",
		"export const useGetHealth = query(ops['get.health']);",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("hooks.ts is missing %q\n\n%s", want, hooks)
		}
	}
}

// TestGenerateFromSpecFileIsSyntacticallyPlausible checks the three specific
// ways the ID bug manifested, plus the general duplicate-binding failure.
//
// These are cheap textual checks, not a TypeScript parse. They are here
// because the failure they cover is not a subtle semantic one: the generated
// module did not parse at all, and no assertion in the suite noticed.
func TestGenerateFromSpecFileIsSyntacticallyPlausible(t *testing.T) {
	files := generateFromSpecFile(t, writeSpecFile(t, "openapi.json", specFileFixture))

	for _, name := range []string{"src/ops.ts", "src/hooks.ts"} {
		content := files[name]

		if strings.Contains(content, "''") {
			t.Errorf("%s contains an empty key or literal ''\n\n%s", name, content)
		}

		// `ops.'x'` -- tsKey's quoted form used in a member-access position,
		// which does not parse.
		if strings.Contains(content, "ops.'") {
			t.Errorf("%s dots into a quoted key\n\n%s", name, content)
		}
	}

	assertNoDuplicateExportConst(t, "src/hooks.ts", files["src/hooks.ts"])
	assertNoDuplicateObjectKeys(t, "src/ops.ts", files["src/ops.ts"])
}

var exportConstRE = regexp.MustCompile(`(?m)^export const ([A-Za-z0-9_$]+)`)

func assertNoDuplicateExportConst(t *testing.T, name, content string) {
	t.Helper()

	seen := make(map[string]bool)

	for _, m := range exportConstRE.FindAllStringSubmatch(content, -1) {
		if seen[m[1]] {
			t.Fatalf("%s declares `export const %s` more than once\n\n%s", name, m[1], content)
		}

		seen[m[1]] = true
	}

	if len(seen) == 0 {
		t.Fatalf("%s exported nothing; this assertion would pass vacuously\n\n%s", name, content)
	}
}

var opsKeyRE = regexp.MustCompile(`(?m)^  ('[^']*'|[A-Za-z0-9_$]+): \{`)

func assertNoDuplicateObjectKeys(t *testing.T, name, content string) {
	t.Helper()

	seen := make(map[string]bool)

	for _, m := range opsKeyRE.FindAllStringSubmatch(content, -1) {
		if seen[m[1]] {
			t.Fatalf("%s declares the object key %s more than once\n\n%s", name, m[1], content)
		}

		seen[m[1]] = true
	}

	if len(seen) == 0 {
		t.Fatalf("%s declared no operations; this assertion would pass vacuously\n\n%s", name, content)
	}
}

// TestGenerateFromSpecFileIsStableAcrossParses is the determinism guard the
// existing ones cannot be: both re-iterate one hand-built slice, so a
// randomized walk over the spec's path map is invisible to them. This one
// re-parses the file each round, which is what a CI drift check does.
func TestGenerateFromSpecFileIsStableAcrossParses(t *testing.T) {
	path := writeSpecFile(t, "openapi.json", specFileFixture)

	first := generateFromSpecFile(t, path)

	for i := 1; i < 12; i++ {
		next := generateFromSpecFile(t, path)

		if len(next) != len(first) {
			t.Fatalf("run %d: file count changed: %d != %d", i, len(next), len(first))
		}

		for name, content := range first {
			if next[name] != content {
				t.Fatalf("run %d: %s differs from run 0:\n\n%s\n--- vs ---\n\n%s",
					i, name, next[name], content)
			}
		}
	}
}

// TestYAMLSpecCarriesForgeExtensionsIntoGeneratedClient is the YAML half of the
// entry-point coverage above: the same journey (file on disk -> ParseFile ->
// Generate), from a hand-written .yaml document rather than JSON.
//
// It replaces a test that asserted the opposite -- that a YAML spec produced a
// warning saying its x-forge-* extensions had been dropped. That was true while
// the extension-carrying types in internal/shared implemented only
// MarshalJSON/UnmarshalJSON, which yaml.v3 never consults. They now implement
// MarshalYAML/UnmarshalYAML too, so the extensions survive and the warning would
// be a lie.
func TestYAMLSpecCarriesForgeExtensionsIntoGeneratedClient(t *testing.T) {
	const yamlSpec = `openapi: 3.0.3
info:
  title: Orders
  version: 1.0.0
paths:
  /orders:
    get:
      operationId: orders.list
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/Order'
components:
  schemas:
    Order:
      type: object
      properties:
        order_number:
          type: string
          x-forge-id: true
        total:
          type: integer
`

	path := writeSpecFile(t, "openapi.yaml", yamlSpec)

	spec, err := client.NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(spec.Warnings) != 0 {
		t.Fatalf("parsing a YAML spec produced warnings: %v", spec.Warnings)
	}

	cfg := baseConfig()
	cfg.Hooks = true

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ops, ok := out.Files["src/ops.ts"]
	if !ok {
		t.Fatal("src/ops.ts was not generated")
	}

	// x-forge-id named order_number as the identity; without the YAML
	// extension path this would be absent entirely, or fall back to a
	// different property.
	//
	// It is emitted as `orderNumber`, not `order_number`: baseConfig targets
	// TypeScript, so field naming is camel, so the response this table is
	// consulted against has already been decoded into camelCase names. The
	// wire name here would name a property the decoded payload does not have
	// -- and a type whose id field is absent is simply not treated as an
	// entity, so nothing would be cached and nothing would complain.
	for _, want := range []string{"entity: 'Order'", "Order: { idField: 'orderNumber' }"} {
		if !strings.Contains(ops, want) {
			t.Fatalf("ops.ts is missing %q — x-forge-* did not survive the YAML path\n\n%s", want, ops)
		}
	}
}

// TestDeclaredIDFieldMissingFromSchemaWarns covers the other half of the
// identity contract: IDField is the JSON property name, and one that names no
// property is a cache key that never matches.
func TestDeclaredIDFieldMissingFromSchemaWarns(t *testing.T) {
	const wrongIDField = `{
  "openapi": "3.0.3",
  "info": { "title": "Orders", "version": "1.0.0" },
  "paths": {
    "/orders/{id}": {
      "get": {
        "operationId": "orders.get",
        "x-forge-entity": { "type": "Order", "idField": "ID" },
        "responses": {
          "200": {
            "description": "ok",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Order" } } }
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "Order": {
        "type": "object",
        "properties": { "id": { "type": "string" }, "total": { "type": "integer" } }
      }
    }
  }
}`

	path := writeSpecFile(t, "openapi.json", wrongIDField)

	spec, err := client.NewSpecParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	joined := strings.Join(spec.Warnings, "\n")
	if !strings.Contains(joined, `idField "ID"`) {
		t.Fatalf("declaring an id field the schema does not have produced no warning: %v", spec.Warnings)
	}

	// A warning that never leaves the intermediate representation is a warning
	// nobody sees, so check it reaches the generator's own output too. This
	// assertion used to live on the YAML-drops-extensions test that this change
	// removed; it is about warning propagation generally, not about YAML.
	cfg := baseConfig()
	cfg.Hooks = true

	out, err := (&Generator{}).Generate(context.Background(), spec, cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.Contains(strings.Join(out.Warnings, "\n"), `idField "ID"`) {
		t.Fatalf("the spec warning did not reach the generator output: %v", out.Warnings)
	}
}
