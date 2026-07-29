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

// refAndOneOfYAML is a real OpenAPI YAML document (not a hand-built
// *client.Schema fixture) that a user would write on disk: a component
// schema "Widget", a "Container" schema whose "direct" property is a bare
// $ref to Widget, and whose "poly" property is a oneOf between a $ref to
// Widget and an inline string schema. This exercises the exact ingestion
// path (client.NewSpecParser().ParseFile against a YAML file) that phase 2's
// review found broken: shared.Schema.Ref and shared.Schema.OneOf both lacked
// yaml tags, so yaml.v3's no-tag fallback looked them up under "ref" and
// "oneof" — neither of which appears in this document — and silently left
// both fields unset.
const refAndOneOfYAML = `
openapi: 3.1.0
info:
  title: Ref OneOf E2E
  version: 1.0.0
paths:
  /noop:
    get:
      summary: noop
      responses:
        '200':
          description: ok
components:
  schemas:
    Widget:
      type: object
      properties:
        name:
          type: string
    Container:
      type: object
      properties:
        direct:
          $ref: '#/components/schemas/Widget'
        poly:
          oneOf:
            - $ref: '#/components/schemas/Widget'
            - type: string
`

// TestRefAndOneOfEndToEndFromParsedYAML is the end-to-end proof for the
// yaml-tag fix: it parses a real OpenAPI YAML file via
// client.NewSpecParser().ParseFile, generates a TypeScript client from the
// result, and asserts both that the emitted types actually reference the
// Widget schema (via $ref) and emit the oneOf union — and that the result
// type-checks with tsc. Before the fix, Schema.Ref and Schema.OneOf never
// populated from YAML, so "direct" and "poly" both silently degraded to
// `any`, losing all type information with no error anywhere in the pipeline.
func TestRefAndOneOfEndToEndFromParsedYAML(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "openapi.yaml")

	require.NoError(t, os.WriteFile(specFile, []byte(refAndOneOfYAML), 0o644))

	parsed, err := client.NewSpecParser().ParseFile(context.Background(), specFile)
	require.NoError(t, err)

	// Prove the parser itself populated the previously-broken fields before
	// even reaching the generator, so a generator-side fix could not mask a
	// still-broken parser.
	container, ok := parsed.Schemas["Container"]
	require.True(t, ok, "Container schema not found in parsed spec")

	direct, ok := container.Properties["direct"]
	require.True(t, ok, "Container.direct property not found")
	assert.Equal(t, "#/components/schemas/Widget", direct.Ref, "Container.direct.$ref must survive YAML parsing")

	poly, ok := container.Properties["poly"]
	require.True(t, ok, "Container.poly property not found")
	require.Len(t, poly.OneOf, 2, "Container.poly.oneOf must survive YAML parsing with both branches")
	assert.Equal(t, "#/components/schemas/Widget", poly.OneOf[0].Ref)
	assert.Equal(t, "string", poly.OneOf[1].Type)

	out, err := NewGenerator().Generate(context.Background(), parsed, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	// The referenced schema is emitted as its own named type.
	assert.Contains(t, types, "export interface Widget {")

	// The $ref property resolves to the named type, not `any`.
	assert.Contains(t, types, "direct?: Widget;")
	assert.NotContains(t, types, "direct?: any;")

	// The oneOf property emits a real union of the two branches, not `any`.
	assert.Contains(t, types, "poly?: Widget | string;")
	assert.NotContains(t, types, "poly?: any;")

	outDir := t.TempDir()
	writeTree(t, outDir, out.Files)

	errs := typeCheck(t, outDir)
	assert.Empty(t, errs, "a client generated from a real parsed OpenAPI YAML file using $ref and oneOf must type-check cleanly:\n%s", strings.Join(errs, "\n"))
}
