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

	// The timeout and the combined signal must stay live until the response
	// body has been fully read, not just until fetch() resolves (headers
	// only) — otherwise a server that sends headers then stalls the body
	// hangs forever, and (on the manual combineSignals fallback) a caller
	// abort during the body read never reaches the merged controller once
	// the forwarding listeners are gone (task 7b). A single `finally` block
	// wrapping the fetch call and the whole body-parsing section is what
	// covers every exit path — success, a thrown error, and
	// handleErrorResponse's throw — exactly once, so this asserts that
	// shape directly instead of counting call sites, which the old
	// (defective) two-call-site shape would also have satisfied.
	if !strings.Contains(code, "} finally {\n") {
		t.Error("expected the timeout/signal teardown to live in a finally block wrapping the fetch call and body read, so it runs on every exit path exactly once, after the body has been consumed")
	}

	finallyIdx := strings.Index(code, "} finally {\n")
	if finallyIdx == -1 {
		t.Fatal("no finally block found in executeRequest")
	}

	tail := code[finallyIdx:]
	if !strings.Contains(tail, "clearTimeout(timeoutId);") || !strings.Contains(tail, "combined.dispose();") {
		t.Error("expected the finally block to clear the timeout and dispose the combined signal")
	}

	// { once: true } is fine elsewhere (e.g. the backoff-sleep abort
	// listener, which removes itself explicitly when the timer wins) — this
	// only guards the combineSignals fallback specifically, which must keep
	// removing its listeners via explicit dispose() rather than relying on
	// { once: true } alone, which leaks on a non-abort exit.
	combineStart := strings.Index(code, "const combineSignals")
	combineEnd := strings.Index(code, "DEFAULT_RETRY_CONFIG")

	if combineStart == -1 || combineEnd == -1 || combineEnd <= combineStart {
		t.Fatal("could not locate the combineSignals block")
	}

	if strings.Contains(code[combineStart:combineEnd], "{ once: true }") {
		t.Error("fallback abort listeners inside combineSignals must be removed explicitly via dispose, not left to { once: true } which leaks on non-abort exits")
	}
}

// TestFetchClientSerializesBodyByRuntimeType is the string-level companion to
// the runtime proof in fetch_client_test.go's
// TestRequestBodySerializationByRuntimeType: it asserts the generated source
// actually contains the runtime-type checks the fix depends on, and that the
// old unconditional-JSON-stringify / unconditional-JSON-Content-Type code is
// gone. A string-only assertion can't prove runtime correctness by itself
// (hence the separate execution test), but it does pin the shape and catches
// an accidental revert immediately, without a Node/esbuild round trip.
func TestFetchClientSerializesBodyByRuntimeType(t *testing.T) {
	code := NewFetchClientGenerator().GenerateBaseClient(baseSpec(), baseConfig())

	if !strings.Contains(code, "instanceof FormData") {
		t.Error("expected executeRequest to check for a FormData body so it can pass it through untouched")
	}

	if !strings.Contains(code, "instanceof Blob") {
		t.Error("expected executeRequest to check for a Blob body so it can pass it through untouched")
	}

	if !strings.Contains(code, "instanceof ReadableStream") {
		t.Error("expected a ReadableStream body to be recognised (both for pass-through serialization and the no-retry guard)")
	}

	if strings.Contains(code, "requestConfig.body ? JSON.stringify(requestConfig.body) : undefined") {
		t.Error("the old unconditional JSON.stringify of every body must be gone")
	}

	if strings.Contains(code, "'Content-Type': 'application/json',\n    };\n  }") {
		t.Error("the constructor must no longer force a default 'Content-Type: application/json' on every request")
	}

	if !strings.Contains(code, "isJSONBody") {
		t.Error("expected a flag distinguishing a JSON-serialized body from a pass-through one, so the Content-Type default is applied conditionally")
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

// TestFormatDrivenTypes asserts that Schema.Format drives the emitted
// TypeScript type: a binary-format string becomes Blob (a DOM type upload
// callers can pass directly to FormData/fetch), a 64-bit integer format
// becomes string (values beyond Number.MAX_SAFE_INTEGER lose precision as a
// JS number), and date-time stays string (JSON.parse yields a string and
// nothing in the generated runtime revives it into a Date).
func TestFormatDrivenTypes(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Formats"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"blob":     {Type: "string", Format: "binary"},
			"when":     {Type: "string", Format: "date-time"},
			"big":      {Type: "integer", Format: "int64"},
			"ordinary": {Type: "string"},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "blob?: Blob;")
	assert.Contains(t, types, "when?: string;") // ISO-8601 string, not Date
	assert.Contains(t, types, "big?: string;")  // int64 exceeds Number.MAX_SAFE_INTEGER
	assert.Contains(t, types, "ordinary?: string;")

	// Blob is a DOM type, not a bare TS/ES type: confirm it actually resolves
	// under the generated tsconfig's "lib": ["ES2020", "DOM"] rather than just
	// asserting on the string. If DOM were ever dropped from lib, this would
	// fail with "Cannot find name 'Blob'" while the string assertion above
	// would still pass.
	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "generated types.ts using Blob for a binary-format property must type-check cleanly")
}

// TestEnumsOfEveryScalarType asserts that an enum is rendered as a TS literal
// union regardless of the scalar type it decorates, not just for "string".
// Before this test, a numeric or boolean enum silently fell through to the
// bare base type (number/boolean), losing the literal union entirely, and a
// string enum value containing a quote broke the generated file because the
// old code hand-interpolated with fmt.Sprintf("'%v'", v).
//
// String literals are rendered double-quoted via json.Marshal (consistent
// with tsPropertyKey's escaping, and avoiding hand-rolled quote/backslash
// escaping) rather than single-quoted; both are valid TypeScript.
func TestEnumsOfEveryScalarType(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Enums"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"status": {Type: "string", Enum: []any{"active", "off"}},
			"code":   {Type: "integer", Enum: []any{1, 2, 3}},
			"flag":   {Type: "boolean", Enum: []any{true}},
			"quoted": {Type: "string", Enum: []any{"it's"}},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, `status?: "active" | "off";`)
	assert.Contains(t, types, `code?: 1 | 2 | 3;`)
	assert.Contains(t, types, `flag?: true;`)
	assert.Contains(t, types, `quoted?: "it's";`)
}

// TestEnumAdversarialValuesTypeCheck exercises the values that break a
// hand-rolled fmt.Sprintf("'%v'", v) enum renderer: an apostrophe, a double
// quote, a backslash, control characters (newline/tab), a non-ASCII value, a
// literal JSON null in the enum list, and a heterogeneous (mixed-type) enum.
// Assertions check both the exact rendered literal and that the resulting
// types.ts actually type-checks with tsc, since a string assertion alone
// would not catch a syntactically-broken-but-string-matching output.
func TestEnumAdversarialValuesTypeCheck(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["Adversarial"] = &client.Schema{
		Type: "object",
		Properties: map[string]*client.Schema{
			"apostrophe": {Type: "string", Enum: []any{"it's"}},
			"quote":      {Type: "string", Enum: []any{`say "hi"`}},
			"backslash":  {Type: "string", Enum: []any{`back\slash`}},
			"control":    {Type: "string", Enum: []any{"line1\nline2\ttab"}},
			"nonascii":   {Type: "string", Enum: []any{"café"}},
			"withNull":   {Type: "string", Enum: []any{"a", nil}},
			// Mixed-type enum: each value is rendered as a literal of its own
			// Go type, producing a heterogeneous TS literal union. This mirrors
			// how JSON Schema/OpenAPI enum arrays are permitted to be
			// heterogeneous, and TypeScript literal unions support mixing
			// string/number/boolean literals natively.
			"mixed": {Enum: []any{"a", 1, true}},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, `apostrophe?: "it's";`)
	assert.Contains(t, types, `quote?: "say \"hi\"";`)
	assert.Contains(t, types, `backslash?: "back\\slash";`)
	assert.Contains(t, types, "control?: \"line1\\nline2\\ttab\";")
	assert.Contains(t, types, `nonascii?: "café";`)
	assert.Contains(t, types, `withNull?: "a" | null;`)
	assert.Contains(t, types, `mixed?: "a" | 1 | true;`)

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "enum literals containing quotes/backslashes/control characters/non-ASCII/null/mixed types must type-check cleanly")
}

// TestEnumInQueryParamTypeChecks exercises rest.go's schemaToTSType (the
// second, independent implementation) with an enum on a query parameter,
// verifying it emits a literal union — not just generator.go's types.ts path.
func TestEnumInQueryParamTypeChecks(t *testing.T) {
	spec := baseSpec()
	spec.Endpoints = append(spec.Endpoints, client.Endpoint{
		Method: "GET", Path: "/widgets", OperationID: "widgets.list",
		QueryParams: []client.Parameter{
			{Name: "state", Schema: &client.Schema{Type: "string", Enum: []any{"it's", "off"}}},
		},
		Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
			"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}}},
	})

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	rest := out.Files["src/rest.ts"]
	assert.Contains(t, rest, `"it's" | "off"`)

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "an enum on a query parameter (rest.go's schemaToTSType) must type-check cleanly")
}

// TestAdditionalProperties asserts that Schema.AdditionalProperties (typed
// `any` in the IR because JSON Schema allows either a bool or a schema)
// drives an open-ended map type instead of being silently ignored:
//
//   - properties absent, additionalProperties a schema -> Record<string, V>
//   - properties absent, additionalProperties true      -> Record<string, any>
//   - properties present AND additionalProperties a schema -> an intersection
//     of the declared properties with Record<string, V>, NOT an interface with
//     an index signature: `{ id: string } & Record<string, number>` type-checks,
//     but `interface { id: string; [key: string]: number }` does not — an index
//     signature must be compatible with every declared property, and `id` is a
//     string while the index signature promises number (TS2411).
//   - additionalProperties false or absent -> unchanged, ordinary interface
//
// A property whose own (nested, unnamed) schema is an open-ended map is
// exercised too: schemaToTSType has its own, separate "object" branch from
// schemaToTypeScript's, and both must apply the same fix or a nested map
// property silently degrades to Record<string, any>, losing the value type.
func TestAdditionalProperties(t *testing.T) {
	spec := baseSpec()
	spec.Schemas["OpenTyped"] = &client.Schema{
		Type:                 "object",
		AdditionalProperties: &client.Schema{Type: "string"},
	}
	spec.Schemas["OpenAny"] = &client.Schema{
		Type:                 "object",
		AdditionalProperties: true,
	}
	spec.Schemas["Mixed"] = &client.Schema{
		Type:                 "object",
		Required:             []string{"id"},
		Properties:           map[string]*client.Schema{"id": {Type: "string"}},
		AdditionalProperties: &client.Schema{Type: "number"},
	}
	spec.Schemas["Closed"] = &client.Schema{
		Type:                 "object",
		Properties:           map[string]*client.Schema{"id": {Type: "string"}},
		AdditionalProperties: false,
	}
	spec.Schemas["HasMap"] = &client.Schema{
		Type:     "object",
		Required: []string{"tags"},
		Properties: map[string]*client.Schema{
			"tags": {
				Type:                 "object",
				AdditionalProperties: &client.Schema{Type: "string"},
			},
		},
	}

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "export type OpenTyped = Record<string, string>;")
	assert.Contains(t, types, "export type OpenAny = Record<string, any>;")
	// Mixed keeps id AND folds in an index signature via intersection, not a
	// (TS2411-invalid) interface with a conflicting index signature.
	assert.Contains(t, types, "export type Mixed = {\n  id: string;\n} & Record<string, number>;")
	assert.Contains(t, types, "id: string;")
	assert.NotContains(t, types, "export type Closed = Record")
	assert.Contains(t, types, "export interface Closed {")
	// Nested case: schemaToTSType, not just schemaToTypeScript, must apply the
	// value-type fix for a property whose own schema is an open-ended map.
	assert.Contains(t, types, "tags: Record<string, string>;")

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	errs := typeCheck(t, dir)
	assert.Empty(t, errs, "additionalProperties output (Record<string,V>, Record<string,any>, the Mixed intersection, and the nested HasMap.tags case) must type-check cleanly")
}

// TestAdditionalPropertiesEndToEndFromParsedSpec is the end-to-end proof for
// the additionalProperties fix: it exercises the real ingestion path a user
// hits — an OpenAPI YAML *file on disk*, parsed by client.NewSpecParser(),
// not a hand-built *client.Schema fixture. TestAdditionalProperties above
// (and the generator-level fix it drove) only proves the generator does the
// right thing once Schema.AdditionalProperties already holds a *client.Schema
// or bool; it says nothing about whether a real spec file ever produces that
// shape in the first place. Before spec_parser.go's convertSchema normalised
// the raw decoder output (and before shared.Schema.AdditionalProperties
// carried an explicit yaml tag), this exact YAML document parsed to
// map[string]any (or, for YAML specifically, didn't populate the field at
// all — see TestSpecParserAdditionalProperties in the client package for
// that half of the fix), and the generator emitted an empty `export
// interface OpenTyped {}` instead of Record<string, string> — silently, with
// no error anywhere in the pipeline.
func TestAdditionalPropertiesEndToEndFromParsedSpec(t *testing.T) {
	const spec = `
openapi: 3.1.0
info:
  title: AP End To End
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
    OpenTyped:
      type: object
      additionalProperties:
        type: string
`

	dir := t.TempDir()
	specFile := filepath.Join(dir, "openapi.yaml")

	require.NoError(t, os.WriteFile(specFile, []byte(spec), 0o644))

	parsed, err := client.NewSpecParser().ParseFile(context.Background(), specFile)
	require.NoError(t, err)

	out, err := NewGenerator().Generate(context.Background(), parsed, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "export type OpenTyped = Record<string, string>;")
	assert.NotContains(t, types, "export interface OpenTyped")

	outDir := t.TempDir()
	writeTree(t, outDir, out.Files)

	errs := typeCheck(t, outDir)
	assert.Empty(t, errs, "a client generated from a real parsed OpenAPI YAML file with a schema-valued additionalProperties must type-check cleanly")
}

// TestDiscriminatedUnion asserts that a polymorphic schema (oneOf, here with
// a discriminator) is emitted as a real TypeScript union instead of falling
// to schemaToTypeScript's default branch, which — before this fix — produced
// `export type Pet = any;` because a polymorphic schema has no Schema.Type to
// switch on.
func TestDiscriminatedUnion(t *testing.T) {
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

	out, err := NewGenerator().Generate(context.Background(), spec, baseConfig())
	require.NoError(t, err)

	types := out.Files["src/types.ts"]

	assert.Contains(t, types, "export type Pet = Cat | Dog;")
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
