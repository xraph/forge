package typescript

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/client"
)

// discriminatedPetSpec returns a spec with Cat/Dog/Pet shaped exactly like
// TestDiscriminatedUnion in gate_test.go, factored out so the narrowing
// proof below and any future test can build on the same fixture without
// duplicating it.
func discriminatedPetSpec() *client.APISpec {
	spec := baseSpec()
	spec.Schemas["Cat"] = &client.Schema{
		Type:     "object",
		Required: []string{"kind", "meows"},
		Properties: map[string]*client.Schema{
			"kind":  {Type: "string", Enum: []any{"cat"}},
			"meows": {Type: "boolean"},
		},
	}
	spec.Schemas["Dog"] = &client.Schema{
		Type:     "object",
		Required: []string{"kind", "barks"},
		Properties: map[string]*client.Schema{
			"kind":  {Type: "string", Enum: []any{"dog"}},
			"barks": {Type: "boolean"},
		},
	}
	spec.Schemas["Pet"] = &client.Schema{
		OneOf: []*client.Schema{
			{Ref: "#/components/schemas/Cat"},
			{Ref: "#/components/schemas/Dog"},
		},
		Discriminator: &client.Discriminator{
			PropertyName: "kind",
			Mapping:      map[string]string{"cat": "#/components/schemas/Cat", "dog": "#/components/schemas/Dog"},
		},
	}

	return spec
}

// TestDiscriminatedUnionNarrows is the narrowing proof required by Task 6:
// a string assertion that "export type Pet = Cat | Dog;" appears is not
// sufficient to prove TypeScript can actually narrow the union, because a
// union of two structurally-unrelated interfaces still type-checks even
// without narrowing working (e.g. via `any`-shaped access). This test
// compiles two consumer snippets against the real generated output:
//
//   - consumer.ts switches/if-narrows on the `kind` discriminant and then
//     accesses the member-specific field (Cat.meows / Dog.barks). This only
//     type-checks if TypeScript actually narrows Pet to Cat or Dog based on
//     the `kind: "cat"` / `kind: "dog"` literal types emitted by enumTSType
//     (Task 4) — which is exactly the mechanism Task 6 depends on.
//   - unnarrowed.ts accesses pet.meows with NO narrowing at all. This must
//     FAIL to type-check (TS2339, "Property 'meows' does not exist on type
//     'Dog'"), proving the union is a real discriminated union and not
//     something permissive enough that any member access always compiles
//     (e.g. if Cat/Dog were accidentally widened to Record<string, any>).
func TestDiscriminatedUnionNarrows(t *testing.T) {
	spec := discriminatedPetSpec()

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]
	assert.Contains(t, types, "export type Pet = Cat | Dog;")
	assert.Contains(t, types, `kind: "cat";`)
	assert.Contains(t, types, `kind: "dog";`)

	// Positive proof: narrowing via `if` and `switch` on the discriminant
	// must type-check cleanly.
	goodFiles := make(map[string]string, len(out.Files)+1)
	for k, v := range out.Files {
		goodFiles[k] = v
	}
	goodFiles["src/consumer.ts"] = `import { Pet } from './types';

export function describeIf(pet: Pet): string {
  if (pet.kind === 'cat') {
    return pet.meows ? 'purring' : 'sleeping';
  } else {
    return pet.barks ? 'barking' : 'quiet';
  }
}

export function describeSwitch(pet: Pet): string {
  switch (pet.kind) {
    case 'cat':
      return pet.meows ? 'purring' : 'sleeping';
    case 'dog':
      return pet.barks ? 'barking' : 'quiet';
  }
}
`

	goodDir := t.TempDir()
	writeTree(t, goodDir, goodFiles)

	errs := typeCheck(t, goodDir)
	assert.Empty(t, errs, "a consumer that narrows Pet on the `kind` discriminant must type-check cleanly:\n%s", strings.Join(errs, "\n"))

	// Negative proof: the SAME field access with NO narrowing must fail.
	// This rules out the union having accidentally widened to something
	// that makes every member field accessible unconditionally.
	badFiles := make(map[string]string, len(out.Files)+1)
	for k, v := range out.Files {
		badFiles[k] = v
	}
	badFiles["src/unnarrowed.ts"] = `import { Pet } from './types';

export function bad(pet: Pet): boolean {
  // No narrowing: 'meows' does not exist on the Dog branch of the union.
  return pet.meows;
}
`

	badDir := t.TempDir()
	writeTree(t, badDir, badFiles)

	badErrs := typeCheck(t, badDir)
	require.NotEmpty(t, badErrs, "accessing a member-specific field on Pet without narrowing must fail to type-check")

	if bad := errorsMentioning(badErrs, "TS2339"); len(bad) == 0 {
		t.Errorf("expected a TS2339 (property does not exist) error for the unnarrowed access, got:\n%s", strings.Join(badErrs, "\n"))
	}
}

// TestAllOfMixesRefAndInlineObject answers brief question 1: when allOf
// mixes a $ref with an inline object schema, does schemaToTSType render the
// inline part usefully, or does it collapse to Record<string, any>?
//
// Before the schemaToTSType "object" case learned to fall back to
// objectPropsLiteral for a schema with declared Properties but no
// additionalProperties (the same fix that answers question 3), the inline
// branch had no Ref, no AdditionalProperties, and Type == "object", so it
// hit the "object" case's default branch and collapsed to
// Record<string, any> — losing the "extra" field entirely and producing
// `export type Combined = Base & Record<string, any>;`. This test pins the
// fixed behavior: the inline member renders as its own object type literal.
func TestAllOfMixesRefAndInlineObject(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Base"] = &client.Schema{
		Type:       "object",
		Required:   []string{"id"},
		Properties: map[string]*client.Schema{"id": {Type: "string"}},
	}
	spec.Schemas["Combined"] = &client.Schema{
		AllOf: []*client.Schema{
			{Ref: "#/components/schemas/Base"},
			{Type: "object", Properties: map[string]*client.Schema{"extra": {Type: "string"}}},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "export type Combined = Base & {\n  extra?: string;\n};")
	assert.NotContains(t, types, "Combined = Base & Record<string, any>")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "an allOf mixing a $ref with an inline object schema must type-check cleanly:\n%s", strings.Join(errs, "\n"))
}

// TestOneOfInlineObjectMembersRenderAsObjectLiterals answers brief question
// 3: a oneOf whose members are inline object schemas (no $ref) must render
// each member as an object type literal, not collapse to
// Record<string, any>. Also proves the resulting union still narrows on a
// discriminant-shaped property, same as the $ref-member case.
func TestOneOfInlineObjectMembersRenderAsObjectLiterals(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Shape"] = &client.Schema{
		OneOf: []*client.Schema{
			{Type: "object", Required: []string{"kind", "radius"}, Properties: map[string]*client.Schema{
				"kind": {Type: "string", Enum: []any{"circle"}}, "radius": {Type: "number"},
			}},
			{Type: "object", Required: []string{"kind", "side"}, Properties: map[string]*client.Schema{
				"kind": {Type: "string", Enum: []any{"square"}}, "side": {Type: "number"},
			}},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, `export type Shape = {
  kind: "circle";
  radius: number;
} | {
  kind: "square";
  side: number;
};`)
	assert.NotContains(t, types, "Shape = Record<string, any> | Record<string, any>")

	files := make(map[string]string, len(out.Files)+1)
	for k, v := range out.Files {
		files[k] = v
	}
	files["src/consumer.ts"] = `import { Shape } from './types';

export function area(shape: Shape): number {
  if (shape.kind === 'circle') {
    return Math.PI * shape.radius * shape.radius;
  }
  return shape.side * shape.side;
}
`

	dir := t.TempDir()
	writeTree(t, dir, files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "a oneOf of inline object schemas must render as narrowable object literals and type-check cleanly:\n%s", strings.Join(errs, "\n"))
}

// generateWithTimeout runs Generate on a goroutine with a hard deadline so a
// test asserting "does not infinite-loop" fails fast instead of hanging CI
// if that invariant is ever broken.
func generateWithTimeout(t *testing.T, spec *client.APISpec, cfg client.GeneratorConfig, timeout time.Duration) map[string]string {
	t.Helper()

	type genResult struct {
		files map[string]string
		err   error
	}

	done := make(chan genResult, 1)

	go func() {
		res, genErr := NewGenerator().Generate(context.Background(), spec, cfg)

		var files map[string]string
		if res != nil {
			files = res.Files
		}

		done <- genResult{files: files, err: genErr}
	}()

	select {
	case result := <-done:
		require.NoError(t, result.err)
		return result.files
	case <-time.After(timeout):
		t.Fatalf("Generate did not return within %s; suspected infinite loop", timeout)
		return nil
	}
}

// TestOneOfSelfReferenceDoesNotInfiniteLoop answers brief question 4: a
// schema whose oneOf includes a $ref back to itself must not infinite-loop
// or stack-overflow during generation.
//
// It terminates by construction rather than by accident: schemaToTSType's
// Ref branch (`if schema.Ref != "" { ... return typeName }`) never
// dereferences the referenced schema's body — it only ever returns the
// schema's own name as a type reference — so a $ref-mediated cycle can never
// recurse into schemaToTSType a second time no matter how deep the cycle is.
// A real stack overflow would only be reachable via a literal Go pointer
// cycle inside a single *client.Schema (e.g. schema.OneOf[i] == schema
// itself, bypassing $ref entirely), which is not a shape either
// spec_parser.go or introspector.go can produce from a real OpenAPI
// document, so it is out of scope here.
//
// Generation succeeding and terminating is one thing; the emitted TypeScript
// being valid is another. `export type Node = string | Node;` — a directly
// self-referential union with no object/array indirection — is REJECTED by
// tsc with TS2456 ("Type alias 'Node' circularly references itself").
// That is TypeScript's own recursive-type-alias rule, not a defect in this
// generator: a bare `type X = A | X` has no indirection for the compiler to
// defer resolution through, and no other OpenAPI-to-TypeScript generator
// can make that construct valid either. TestSelfReferenceWithIndirection
// below shows the realistic shape (self-reference via an object property,
// not via the top-level union itself) type-checks cleanly.
func TestOneOfSelfReferenceDoesNotInfiniteLoop(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Node"] = &client.Schema{
		OneOf: []*client.Schema{
			{Type: "string"},
			{Ref: "#/components/schemas/Node"},
		},
	}

	files := generateWithTimeout(t, spec, baseConfig(), 10*time.Second)

	types := files["src/types.ts"]
	assert.Contains(t, types, "export type Node = string | Node;")

	dir := t.TempDir()
	writeTree(t, dir, files)

	errs := typeCheck(t, dir)
	if bad := errorsMentioning(errs, "TS2456"); len(bad) == 0 {
		t.Errorf("expected tsc to reject the directly self-referential union with TS2456, got:\n%s", strings.Join(errs, "\n"))
	}
}

// TestSelfReferenceWithIndirection is the realistic counterpart to
// TestOneOfSelfReferenceDoesNotInfiniteLoop: self-reference through an
// object property (the shape a real recursive schema — e.g. an expression
// tree or a linked structure — actually takes) rather than directly at the
// top level of the union. This does type-check, confirming the TS2456 seen
// above is specific to the no-indirection case and not a general limitation
// of the emitted unions.
func TestSelfReferenceWithIndirection(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Expr"] = &client.Schema{
		OneOf: []*client.Schema{
			{Type: "object", Required: []string{"value"}, Properties: map[string]*client.Schema{
				"value": {Type: "number"},
			}},
			{Type: "object", Required: []string{"left", "right", "op"}, Properties: map[string]*client.Schema{
				"left":  {Ref: "#/components/schemas/Expr"},
				"right": {Ref: "#/components/schemas/Expr"},
				"op":    {Type: "string"},
			}},
		},
	}

	files := generateWithTimeout(t, spec, baseConfig(), 10*time.Second)

	dir := t.TempDir()
	writeTree(t, dir, files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "a self-referencing oneOf via object-property indirection must type-check cleanly:\n%s", strings.Join(errs, "\n"))
}
