package typescript

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/client"
)

// errorsMentioning returns the subset of errs containing needle.
func errorsMentioning(errs []string, needle string) []string {
	var out []string

	for _, e := range errs {
		if strings.Contains(e, needle) {
			out = append(out, e)
		}
	}

	return out
}

func TestNoDanglingAuthConfig(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			errs := typeCheck(t, generateTo(t, f))

			if bad := errorsMentioning(errs, "AuthConfig"); len(bad) > 0 {
				t.Errorf("AuthConfig is referenced but not exported:\n%s", strings.Join(bad, "\n"))
			}
		})
	}
}

func TestRESTExtendsConfiguredClientClass(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			errs := typeCheck(t, generateTo(t, f))

			for _, needle := range []string{"has no exported member 'Client'", "Property 'request' does not exist"} {
				if bad := errorsMentioning(errs, needle); len(bad) > 0 {
					t.Errorf("REST client does not extend the configured class:\n%s", strings.Join(bad, "\n"))
				}
			}
		})
	}
}

func TestNoUndeclaredRequire(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			errs := typeCheck(t, generateTo(t, f))

			if bad := errorsMentioning(errs, "Cannot find name 'require'"); len(bad) > 0 {
				t.Errorf("generated code uses an undeclared require:\n%s", strings.Join(bad, "\n"))
			}
		})
	}
}

func TestTypesQuoteNonIdentifierKeys(t *testing.T) {
	var fixture gateFixture

	for _, f := range gateFixtures() {
		if f.Name == "odd-keys" {
			fixture = f
		}
	}

	out, err := NewGenerator().Generate(context.Background(), fixture.Spec, fixture.Config)
	if err != nil {
		t.Fatal(err)
	}

	types := out.Files["src/types.ts"]

	if !strings.Contains(types, "\"content-type\"?: string;") {
		t.Errorf("expected quoted \"content-type\" key, got:\n%s", types)
	}

	if !strings.Contains(types, "\"3dtiles\"?: string;") {
		t.Errorf("expected quoted \"3dtiles\" key, got:\n%s", types)
	}

	if !strings.Contains(types, "\"it's\"?: string;") {
		t.Errorf("expected properly escaped \"it's\" key, got:\n%s", types)
	}

	if !strings.Contains(types, "\"back\\\\slash\"?: string;") {
		t.Errorf("expected properly escaped \"back\\\\slash\" key, got:\n%s", types)
	}

	errs := typeCheck(t, generateTo(t, fixture))

	// Verify the syntax errors we fixed are gone
	if bad := errorsMentioning(errs, "TS1131"); len(bad) > 0 {
		t.Errorf("should not have TS1131 errors:\n%s", strings.Join(bad, "\n"))
	}
	if bad := errorsMentioning(errs, "TS1351"); len(bad) > 0 {
		t.Errorf("should not have TS1351 errors:\n%s", strings.Join(bad, "\n"))
	}
	if bad := errorsMentioning(errs, "TS1109"); len(bad) > 0 {
		t.Errorf("should not have TS1109 errors:\n%s", strings.Join(bad, "\n"))
	}
	if bad := errorsMentioning(errs, "TS1128"); len(bad) > 0 {
		t.Errorf("should not have TS1128 errors:\n%s", strings.Join(bad, "\n"))
	}
}

func TestWSSSEFixtureEmitsStreamingFiles(t *testing.T) {
	var fixture gateFixture

	for _, f := range gateFixtures() {
		if f.Name == "ws-sse" {
			fixture = f
		}
	}

	if fixture.Name == "" {
		t.Fatal("ws-sse fixture not found in gateFixtures()")
	}

	out, err := NewGenerator().Generate(context.Background(), fixture.Spec, fixture.Config)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := out.Files["src/websocket.ts"]; !ok {
		t.Error("expected src/websocket.ts to be emitted by the ws-sse fixture")
	}

	if _, ok := out.Files["src/sse.ts"]; !ok {
		t.Error("expected src/sse.ts to be emitted by the ws-sse fixture")
	}
}

func TestFetchClientCombinesSignalsAndThrowsErrors(t *testing.T) {
	code := NewFetchClientGenerator().GenerateBaseClient(baseSpec(), baseConfig())

	if strings.Contains(code, "requestConfig.signal || controller.signal") {
		t.Error("a caller-supplied signal must not replace the timeout signal")
	}

	if !strings.Contains(code, "class HTTPError extends Error") {
		t.Error("error responses must throw a real Error subclass")
	}

	if !strings.Contains(code, "throw new HTTPError(") {
		t.Error("handleErrorResponse must throw HTTPError")
	}

	if strings.Contains(code, ": requestConfig.signal\n") {
		t.Error("the AbortSignal.any-unavailable fallback must not yield the caller's signal alone; that silently disables the timeout")
	}

	if !strings.Contains(code, "forwardAbort") {
		t.Error("expected a manual signal-forwarding fallback (forwardAbort) for runtimes without AbortSignal.any")
	}

	if strings.Count(code, "combineSignals") == 0 {
		t.Error("expected a combineSignals helper")
	}

	if !strings.Contains(code, "dispose: () => void") {
		t.Error("combineSignals must return a disposable pair, not a bare signal, so fallback listeners can be removed")
	}

	if !strings.Contains(code, "combined.dispose()") {
		t.Error("expected the caller to dispose of the combined signal wherever the timeout is cleared")
	}

	if got := strings.Count(code, "combined.dispose()"); got < 2 {
		t.Errorf("expected combined.dispose() on both the success and error exit paths, found %d occurrence(s)", got)
	}

	if strings.Contains(code, "{ once: true }") {
		t.Error("fallback abort listeners must be removed explicitly via dispose, not left to { once: true } which leaks on non-abort exits")
	}
}

func TestGenerateExampleTestGatesAuthWhenIncludeAuthFalse(t *testing.T) {
	tg := NewTestingGenerator()

	// Test with IncludeAuth: false
	cfg := baseConfig()
	cfg.IncludeAuth = false

	code := tg.GenerateExampleTest(baseSpec(), cfg)

	// When auth is off, should not contain auth: property
	if strings.Contains(code, "auth:") {
		t.Error("GenerateExampleTest should not emit auth: when IncludeAuth is false")
	}

	// Should still contain at least one it(...) block to be a valid test suite
	if !strings.Contains(code, "it('should create a client instance'") {
		t.Error("GenerateExampleTest should still contain first it(...) block when auth is off")
	}

	// Test with IncludeAuth: true
	cfg.IncludeAuth = true

	code = tg.GenerateExampleTest(baseSpec(), cfg)

	// When auth is on, should contain auth: property
	if !strings.Contains(code, "auth:") {
		t.Error("GenerateExampleTest should emit auth: when IncludeAuth is true")
	}

	if !strings.Contains(code, "it('should set auth headers'") {
		t.Error("GenerateExampleTest should contain 'should set auth headers' it block when IncludeAuth is true")
	}
}

// TestGeneratedClientsTypeCheck is the full gate: every fixture in the corpus
// must generate a TypeScript client that type-checks with zero tsc errors.
// Tasks 1-12 each asserted the absence of one specific error class; this test
// asserts the absence of all of them, across every fixture, and is what CI
// runs to keep the corpus clean going forward.
func TestGeneratedClientsTypeCheck(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			if errs := typeCheck(t, generateTo(t, f)); len(errs) > 0 {
				t.Errorf("%d type error(s):\n%s", len(errs), strings.Join(errs, "\n"))
			}
		})
	}
}

// TestSchemaNameCollidesWithStreamingType asserts that a user schema named
// after a hardcoded streaming interface fails generation, rather than
// silently emitting two `export interface Message` declarations (a
// TypeScript duplicate-identifier error).
func TestSchemaNameCollidesWithStreamingType(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Message"] = &client.Schema{
		Type:       "object",
		Properties: map[string]*client.Schema{"body": {Type: "string"}},
	}

	cfg := baseConfig() // streaming on by default

	_, err := NewGenerator().Generate(context.Background(), spec, cfg)

	require.Error(t, err, "a user schema named Message collides with the generated streaming type")
	assert.Contains(t, err.Error(), "Message")
	assert.Contains(t, err.Error(), "collides")
}

// TestSchemaNameReservedOnlyWithStreamingDisabledSucceeds is the
// counterpart to TestSchemaNameCollidesWithStreamingType: the same schema
// name is only a problem when the colliding streaming type is actually
// emitted. With streaming off, "Message" is just a schema name and
// generation must succeed.
func TestSchemaNameReservedOnlyWithStreamingDisabledSucceeds(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Message"] = &client.Schema{
		Type:       "object",
		Properties: map[string]*client.Schema{"body": {Type: "string"}},
	}

	cfg := baseConfig()
	cfg.IncludeStreaming = false

	out, err := NewGenerator().Generate(context.Background(), spec, cfg)

	require.NoError(t, err, "a schema named Message must generate fine when streaming is disabled")
	assert.Contains(t, out.Files["src/types.ts"], "export interface Message {")
}

// TestPropertyJSDocIsEmitted asserts that per-property Description and
// Deprecated are rendered as a JSDoc block above the property, and that a
// property with neither yields no comment at all (never an empty /** */).
func TestPropertyJSDocIsEmitted(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Doc"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"kept":  {Type: "string", Description: "The name of the thing."},
			"old":   {Type: "string", Description: "Legacy field.", Deprecated: true},
			"plain": {Type: "string"},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "/** The name of the thing. */\n  kept?: string;")
	assert.Contains(t, types, "@deprecated")
	// A property with no description gets no comment at all — no empty /** */.
	assert.NotContains(t, types, "/**  */")
	// And no dangling comment-only line for "plain" either.
	assert.NotContains(t, types, "/**\n */")
}

// TestPropertyJSDocEscapesCommentTerminator guards against the same class of
// defect Phase 1 fixed in tsPropertyKey: a description containing the literal
// "*/" would close the JSDoc block early and corrupt everything after it in
// types.ts. This is verified by actually running tsc against the generated
// output, not just by asserting on the string — a string-only assertion would
// not catch a case where escaping produces syntactically different but still
// broken output.
func TestPropertyJSDocEscapesCommentTerminator(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Doc"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"tricky": {Type: "string", Description: "Closes early with */ right in the middle."},
		},
	}
	cfg := baseConfig()

	out, err := NewGenerator().Generate(context.Background(), spec, cfg)
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	// The raw terminator must not appear inside the comment body unescaped.
	assert.NotContains(t, types, "with */ right")
	assert.Contains(t, types, `with *\/ right`)

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "generated types.ts with an escaped */ in a description must still type-check cleanly")
}

// TestPropertyJSDocPreservesBlankLinesInMultilineDescriptions documents a
// deliberate deviation from the brief's reference implementation, which skips
// empty lines within a multi-line description. A blank line in prose is a
// paragraph break, not noise; dropping it silently joins two paragraphs into
// one run-on paragraph. This generator instead keeps blank lines as bare " *"
// continuation lines, matching how JSDoc/TSDoc tooling (and hand-written
// JSDoc) represents paragraph breaks.
func TestPropertyJSDocPreservesBlankLinesInMultilineDescriptions(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Doc"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"para": {Type: "string", Description: "First paragraph.\n\nSecond paragraph."},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "   * First paragraph.\n   *\n   * Second paragraph.\n")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "multi-line JSDoc with a blank continuation line must still type-check cleanly")
}

func TestGenerateTestUtilsGatesAuthWhenIncludeAuthFalse(t *testing.T) {
	tg := NewTestingGenerator()

	// Test with IncludeAuth: false
	cfg := baseConfig()
	cfg.IncludeAuth = false

	code := tg.GenerateTestUtils(baseSpec(), cfg)

	// When auth is off, should not contain auth: property in createMockClient
	if strings.Contains(code, "auth:") {
		t.Error("GenerateTestUtils should not emit auth: when IncludeAuth is false")
	}

	// Test with IncludeAuth: true
	cfg.IncludeAuth = true

	code = tg.GenerateTestUtils(baseSpec(), cfg)

	// When auth is on, should contain auth: property
	if !strings.Contains(code, "auth:") {
		t.Error("GenerateTestUtils should emit auth: when IncludeAuth is true")
	}
}
