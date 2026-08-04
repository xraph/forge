# TypeScript Client Generator — Phase 1: `tsc` Gate and Correctness Fixes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `tsc --noEmit` gate to the Go test suite, then fix every defect it reports so the generated TypeScript client compiles under `strict` and is byte-identical across runs.

**Architecture:** A Go test writes a generated client to `t.TempDir()` and shells out to `tsc --noEmit` against the client's own generated `tsconfig.json`. The generated tsconfig needs no `node_modules`, so the gate is hermetic and fast. Each subsequent task fixes one defect, driven by a test that reproduces it first.

**Tech Stack:** Go 1.26, TypeScript 5.8 (`tsc` from `PATH` or `npx`), `stretchr/testify` (already used in this package).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-ts-client-generator-design.md`.
- **Strict TDD.** No implementation code before a failing test exists for it. Every task runs its test and confirms it fails *for the expected reason* before implementing.
- The gate calls `t.Skip` when neither `tsc` nor `npx` resolves. It must **not** skip on `testing.Short()` — CI runs `go test -short` (`.github/workflows/go.yml:60`), and skipping on `-short` would make the gate dead in CI.
- All generated-file iteration must be deterministic. Go map iteration is randomized; every `range` over a map that affects output must be sorted first.
- Phase 1 changes no public Go API and no generated-client API. Behaviour changes are limited to making output valid and stable.
- Commit after every task. Conventional-commit prefixes (`test:`, `fix:`, `chore:`) — the repo enforces them (`.github/workflows/pr-conventional-commits.yml`).
- No `Co-Authored-By` trailers.

## Measured Baseline

Generated from a probe spec with `client.DefaultConfig()` plus `Language: "typescript"`, then type-checked. **Every fixture fails today, including the default configuration:**

| Fixture | `tsc` errors | Dominant cause |
| --- | --- | --- |
| `default` | 12 | missing `AuthConfig`, `require` undeclared |
| `apiname` (`APIName: "APIClient"`) | 15 | the above plus `Module './client' has no exported member 'Client'` |
| `odd-keys` (property `content-type`, `3dtiles`) | 5 | `TS1131 Property or signature expected` — syntax error |
| `with-auth` (spec has a security scheme) | 4 | `require` undeclared |
| `no-streaming` | 3 | missing `AuthConfig` |

This corrects the spec's claim that only non-determinism affects the default configuration; the default configuration emits 12 type errors.

## File Structure

| File | Responsibility | Change |
| --- | --- | --- |
| `internal/client/generators/typescript/tscheck_test.go` | Locate `tsc`, run it against a directory, return parsed errors. Test-only. | Create |
| `internal/client/generators/typescript/fixtures_test.go` | The shared fixture corpus (specs + configs) used by the gate and the determinism test. | Create |
| `internal/client/generators/typescript/gate_test.go` | The gate: every fixture compiles with zero errors. | Create |
| `internal/client/generators/typescript/naming.go` | Shared case conversion (`toCamel`, `toPascal`), replacing four divergent copies. | Create |
| `internal/client/generators/typescript/naming_test.go` | Table-driven tests for case conversion. | Create |
| `internal/client/generators/typescript/generator.go` | Auth emission, property-key quoting, sorted schema iteration. | Modify |
| `internal/client/generators/typescript/rest.go` | `Client` import, path encoding, tree insertion, delete dead code. | Modify |
| `internal/client/generators/typescript/sse.go`, `websocket.go`, `webtransport.go`, `rooms.go`, `presence.go`, `typing.go`, `channels.go` | `require` shim, sorted event iteration. | Modify |
| `internal/client/generators/typescript/fetch_client.go` | Combined abort signals, real `Error` throws. | Modify |
| `.github/workflows/go.yml` | Add Node setup so the gate is live in CI. | Modify |

---

### Task 1: `tsc` harness

**Files:**
- Create: `internal/client/generators/typescript/tscheck_test.go`

**Interfaces:**
- Produces: `func findTSC(t *testing.T) []string` — returns the argv prefix to invoke tsc (`["tsc"]` or `["npx","tsc"]`), calling `t.Skip` if neither resolves. `func typeCheck(t *testing.T, dir string) []string` — runs `tsc --noEmit -p tsconfig.json` in `dir`, returns one string per `error TSxxxx` line, empty slice when clean.

- [ ] **Step 1: Write the failing test**

```go
package typescript

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const probeTSConfig = `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "lib": ["ES2020", "DOM"],
    "strict": true,
    "moduleResolution": "bundler",
    "noEmit": true
  },
  "include": ["src/**/*"]
}
`

func TestTypeCheckAcceptsValidTypeScript(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"tsconfig.json": probeTSConfig,
		"src/a.ts":      "export const n: number = 1;\n",
	})

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("expected valid TypeScript to compile cleanly, got:\n%v", errs)
	}
}

func TestTypeCheckRejectsInvalidTypeScript(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"tsconfig.json": probeTSConfig,
		"src/a.ts":      "export const n: number = 'not a number';\n",
	})

	errs := typeCheck(t, dir)
	if len(errs) == 0 {
		t.Fatal("expected a type error, got none")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestTypeCheck -v`
Expected: FAIL to compile — `undefined: typeCheck`.

- [ ] **Step 3: Write minimal implementation**

Append to `tscheck_test.go`:

```go
import (
	"os/exec"
	"strings"
)

// findTSC returns the argv prefix used to invoke the TypeScript compiler, or
// skips the test when no compiler is available. CI installs Node so the gate is
// live there; local runs without Node degrade to a skip rather than a failure.
func findTSC(t *testing.T) []string {
	t.Helper()

	if path, err := exec.LookPath("tsc"); err == nil {
		return []string{path}
	}

	if path, err := exec.LookPath("npx"); err == nil {
		return []string{path, "--no-install", "tsc"}
	}

	t.Skip("neither tsc nor npx found on PATH; skipping TypeScript type check")

	return nil
}

// typeCheck runs tsc against dir and returns one entry per reported error.
func typeCheck(t *testing.T, dir string) []string {
	t.Helper()

	argv := findTSC(t)
	argv = append(argv, "--noEmit", "-p", "tsconfig.json")

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	var errs []string

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "error TS") {
			errs = append(errs, strings.TrimSpace(line))
		}
	}

	// tsc exited non-zero but emitted nothing parseable: surface it verbatim so
	// a broken toolchain is not mistaken for a clean run.
	if len(errs) == 0 {
		t.Fatalf("tsc failed with no parseable diagnostics: %v\n%s", err, out)
	}

	return errs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestTypeCheck -v`
Expected: PASS, both subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/tscheck_test.go
git commit -m "test(client/typescript): add tsc type-check harness"
```

---

### Task 2: Fixture corpus

**Files:**
- Create: `internal/client/generators/typescript/fixtures_test.go`

**Interfaces:**
- Consumes: `writeTree` from Task 1.
- Produces: `func gateFixtures() []gateFixture` where `type gateFixture struct { Name string; Spec *client.APISpec; Config client.GeneratorConfig }`, and `func generateTo(t *testing.T, f gateFixture) string` which generates the client into a temp dir and returns that dir.

- [ ] **Step 1: Write the failing test**

```go
package typescript

import "testing"

func TestGateFixturesCoverKnownDefects(t *testing.T) {
	want := []string{"default", "apiname", "odd-keys", "with-auth", "no-streaming"}

	got := make(map[string]bool)
	for _, f := range gateFixtures() {
		got[f.Name] = true
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("fixture %q missing from corpus", name)
		}
	}
}

func TestGenerateToProducesTSConfig(t *testing.T) {
	dir := generateTo(t, gateFixtures()[0])

	if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err != nil {
		t.Fatalf("expected generated tsconfig.json: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run 'TestGate|TestGenerateTo' -v`
Expected: FAIL to compile — `undefined: gateFixtures`, `undefined: generateTo`.

- [ ] **Step 3: Write minimal implementation**

```go
package typescript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/client"
)

type gateFixture struct {
	Name   string
	Spec   *client.APISpec
	Config client.GeneratorConfig
}

// baseSpec returns a spec exercising path params, query params, a request body,
// and a $ref response.
func baseSpec() *client.APISpec {
	user := &client.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]*client.Schema{
			"id":         {Type: "string"},
			"user_id":    {Type: "string"},
			"created_at": {Type: "string", Format: "date-time"},
		},
	}

	return &client.APISpec{
		Info: client.APIInfo{Title: "Probe API", Version: "1.0.0", Description: "probe"},
		Endpoints: []client.Endpoint{
			{
				Method: "GET", Path: "/users/{id}", OperationID: "users.get",
				PathParams:  []client.Parameter{{Name: "id", Schema: &client.Schema{Type: "string"}, Required: true}},
				QueryParams: []client.Parameter{{Name: "include_deleted", Schema: &client.Schema{Type: "boolean"}}},
				Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}}},
			},
			{
				Method: "POST", Path: "/users", OperationID: "users.create",
				RequestBody: &client.RequestBody{Required: true, Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}},
				Responses: map[int]*client.Response{201: {Content: map[string]*client.MediaType{
					"application/json": {Schema: &client.Schema{Ref: "#/components/schemas/User"}}}}},
			},
		},
		Schemas: map[string]*client.Schema{"User": user},
	}
}

func baseConfig() client.GeneratorConfig {
	cfg := client.DefaultConfig()
	cfg.Language = "typescript"
	cfg.PackageName = "probe"

	return cfg
}

func gateFixtures() []gateFixture {
	oddKeys := baseSpec()
	oddKeys.Schemas["Weird"] = &client.Schema{Type: "object", Properties: map[string]*client.Schema{
		"content-type": {Type: "string"},
		"3dtiles":      {Type: "string"},
	}}

	withAuth := baseSpec()
	withAuth.Security = []client.SecurityScheme{{Type: "http", Name: "bearerAuth", Scheme: "bearer"}}

	apiName := baseConfig()
	apiName.APIName = "APIClient"

	noStreaming := baseConfig()
	noStreaming.IncludeStreaming = false

	return []gateFixture{
		{Name: "default", Spec: baseSpec(), Config: baseConfig()},
		{Name: "apiname", Spec: baseSpec(), Config: apiName},
		{Name: "odd-keys", Spec: oddKeys, Config: baseConfig()},
		{Name: "with-auth", Spec: withAuth, Config: baseConfig()},
		{Name: "no-streaming", Spec: baseSpec(), Config: noStreaming},
	}
}

func generateTo(t *testing.T, f gateFixture) string {
	t.Helper()

	out, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
	if err != nil {
		t.Fatalf("%s: generate: %v", f.Name, err)
	}

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	return dir
}
```

Add `"os"` and `"path/filepath"` to the test file's imports for Step 1's assertions.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run 'TestGate|TestGenerateTo' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/fixtures_test.go
git commit -m "test(client/typescript): add generator fixture corpus"
```

---

### Task 3: Fix dangling `AuthConfig`

`generator.go:382` emits `export interface AuthConfig` only when `config.IncludeAuth && client.NeedsAuthConfig(spec)`, but `generator.go:395` emits `auth?: AuthConfig` and `generator.go:794` imports it whenever `config.IncludeAuth`. Three sites, two conditions. Unify on `config.IncludeAuth`; an unused exported interface is harmless, an unresolved one is not.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go:382`, `:794`
- Test: `internal/client/generators/typescript/gate_test.go` (create)

**Interfaces:**
- Consumes: `typeCheck` (Task 1), `gateFixtures`, `generateTo` (Task 2).

- [ ] **Step 1: Write the failing test**

```go
package typescript

import (
	"strings"
	"testing"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestNoDanglingAuthConfig -v`
Expected: FAIL on `default`, `apiname`, `no-streaming` with `Module '"./types"' has no exported member 'AuthConfig'` and `Cannot find name 'AuthConfig'`. `with-auth` passes.

- [ ] **Step 3: Write minimal implementation**

In `generator.go`, `generateTypes`, replace the guard at line 382:

```go
	// Auth config interface. Emitted whenever auth is enabled, because
	// ClientConfig.auth and the client.ts import are both gated on IncludeAuth
	// alone — a narrower condition here leaves those references unresolved.
	if config.IncludeAuth {
```

In `generateClient`, replace the unconditional import at line 794:

```go
	if config.IncludeAuth {
		buf.WriteString("import { ClientConfig, AuthConfig } from './types';\n")
	} else {
		buf.WriteString("import { ClientConfig } from './types';\n")
	}
```

The `private auth?: AuthConfig;` field and the auth-header block in `generateClient` must be wrapped in the same `if config.IncludeAuth` so no reference survives when auth is off.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestNoDanglingAuthConfig -v`
Expected: PASS, all five fixtures.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/generator.go internal/client/generators/typescript/gate_test.go
git commit -m "fix(client/typescript): emit AuthConfig whenever it is referenced"
```

---

### Task 4: Fix the `Client` / `APIName` import mismatch

`rest.go:111` hardcodes `import { Client } from './client'` and `rest.go:115` emits `extends Client`, but `generator.go:797` emits `export class <APIName>`. With `APIName: "APIClient"` this produces `TS2305` plus cascading `TS2339 Property 'request' does not exist`.

**Files:**
- Modify: `internal/client/generators/typescript/rest.go:107-126`
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestRESTExtendsConfiguredClientClass -v`
Expected: FAIL on `apiname` with all three errors; other fixtures pass because `DefaultConfig` sets `APIName: "Client"`.

- [ ] **Step 3: Write minimal implementation**

In `rest.go`, `Generate`, replace the hardcoded name with the configured one:

```go
	base := config.APIName
	if base == "" {
		base = "Client"
	}

	buf.WriteString("import { RequestConfig } from './fetch';\n")
	fmt.Fprintf(&buf, "import { %s } from './client';\n", base)
	buf.WriteString("import * as types from './types';\n\n")

	fmt.Fprintf(&buf, "export class RESTClient extends %s {\n", base)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestRESTExtendsConfiguredClientClass -v`
Expected: PASS, all five fixtures.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/rest.go internal/client/generators/typescript/gate_test.go
git commit -m "fix(client/typescript): extend the configured client class in rest.ts"
```

---

### Task 5: Quote invalid property keys in `types.ts`

`generator.go:643` emits property names raw. A schema property named `content-type` produces `content-type?: string;` — `TS1131`. `tsPropertyKey` already exists in `rest.go:150` but `generator.go` never calls it.

**Files:**
- Modify: `internal/client/generators/typescript/generator.go:634-644`
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
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

	if !strings.Contains(types, "'content-type'?: string;") {
		t.Errorf("expected quoted 'content-type' key, got:\n%s", types)
	}

	if !strings.Contains(types, "'3dtiles'?: string;") {
		t.Errorf("expected quoted '3dtiles' key, got:\n%s", types)
	}

	if errs := typeCheck(t, generateTo(t, fixture)); len(errs) > 0 {
		t.Errorf("odd-keys fixture must compile:\n%s", strings.Join(errs, "\n"))
	}
}
```

Add `"context"` to the test file imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestTypesQuoteNonIdentifierKeys -v`
Expected: FAIL — unquoted keys, plus `TS1131`, `TS1109`, `TS1351`.

- [ ] **Step 3: Write minimal implementation**

In `generator.go`, `schemaToTypeScript`, replace the property line:

```go
			buf.WriteString(fmt.Sprintf("  %s%s: %s;\n", tsPropertyKey(propName), optional, tsType))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestTypesQuoteNonIdentifierKeys -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/generator.go internal/client/generators/typescript/gate_test.go
git commit -m "fix(client/typescript): quote non-identifier property keys in types.ts"
```

---

### Task 6: Declare `require` in the Node fallback

The streaming generators emit `require('ws')` / `require('eventsource')` inside a package declaring `"type": "module"`, and the generated `tsconfig.json` sets no `types`, so `require` is unresolved (`TS2580`) in `channels.ts`, `presence.ts`, `rooms.ts`, `typing.ts`, `sse.ts`, `websocket.ts`. This affects even the otherwise-clean `with-auth` fixture.

Phase 1 emits a local declaration to make the code valid. Phase 4 replaces the whole fallback with dynamic `import()` per the spec; this is deliberately the minimal change that lets the gate go green.

**Files:**
- Modify: `sse.go` (`generatePolyfillSetup`), `websocket.go` (`generatePolyfillSetup`), and the equivalent preamble in `rooms.go`, `presence.go`, `typing.go`, `channels.go`, `webtransport.go`
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestNoUndeclaredRequire -v`
Expected: FAIL on `default`, `apiname`, `with-auth` with `TS2580`.

- [ ] **Step 3: Write minimal implementation**

In each generator's polyfill preamble, immediately before the `getWebSocket` / `getEventSource` function, emit:

```go
	buf.WriteString("// Node.js CommonJS fallback. Phase 4 replaces this with dynamic import().\n")
	buf.WriteString("declare const require: ((id: string) => any) | undefined;\n\n")
```

and change the call sites to guard on it, so the declaration is honest about the value possibly being absent:

```go
	buf.WriteString("      if (typeof require === 'undefined') {\n")
	buf.WriteString("        throw new Error('No WebSocket implementation available in this environment.');\n")
	buf.WriteString("      }\n")
```

Apply the same pattern in every file listed above. Search for `require(` across the package to confirm none are missed:

```bash
grep -rn "require(" internal/client/generators/typescript/*.go
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestNoUndeclaredRequire -v`
Expected: PASS, all five fixtures.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "fix(client/typescript): declare require in the Node fallback path"
```

---

### Task 7: Deterministic output

`spec.Schemas`, `schema.Properties`, `sse.EventSchemas`, and `ws.MessageTypes` are ranged over directly. Go randomizes map iteration, so `types.ts`, SSE listeners, and WS handlers reorder between runs — confirmed to differ between run 0 and run 1 of 12.

**Files:**
- Modify: `generator.go:375` (`generateTypes`), `generator.go:634` (`schemaToTypeScript`), `sse.go:310` and `:352`, `websocket.go` (`MessageTypes` iteration), `webtransport.go` (same)
- Test: `internal/client/generators/typescript/determinism_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
package typescript

import (
	"context"
	"testing"
)

func TestGenerationIsDeterministic(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			first, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
			if err != nil {
				t.Fatal(err)
			}

			for i := 1; i < 12; i++ {
				next, err := NewGenerator().Generate(context.Background(), f.Spec, f.Config)
				if err != nil {
					t.Fatal(err)
				}

				if len(next.Files) != len(first.Files) {
					t.Fatalf("run %d: file count changed: %d != %d", i, len(next.Files), len(first.Files))
				}

				for name, content := range first.Files {
					if next.Files[name] != content {
						t.Fatalf("run %d: %s differs from run 0", i, name)
					}
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestGenerationIsDeterministic -v -count=1`
Expected: FAIL — `src/types.ts differs from run 0` within the first few iterations.

- [ ] **Step 3: Write minimal implementation**

Add a shared helper to `generator.go`:

```go
// sortedKeys returns the keys of m in ascending order. Generated output must be
// byte-identical across runs, and Go randomizes map iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
```

Replace every output-affecting map range. In `generateTypes`:

```go
	for _, name := range sortedKeys(spec.Schemas) {
		typeCode := g.schemaToTypeScript(name, spec.Schemas[name], spec)
		buf.WriteString(typeCode)
		buf.WriteString("\n")
	}
```

In `schemaToTypeScript`:

```go
		for _, propName := range sortedKeys(schema.Properties) {
			prop := schema.Properties[propName]
			// ... unchanged body
		}
```

In `sse.go` both loops over `sse.EventSchemas`, and in `websocket.go` / `webtransport.go` for `MessageTypes`, apply the same pattern. Confirm none remain:

```bash
grep -rn "range spec.Schemas\|range schema.Properties\|range sse.EventSchemas\|range ws.MessageTypes" internal/client/generators/typescript/*.go
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestGenerationIsDeterministic -v -count=3`
Expected: PASS, all five fixtures, three runs.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "fix(client/typescript): sort map iteration for deterministic output"
```

---

### Task 8: Encode path parameters

`rest.go:543` interpolates `${id}` raw, so an id containing `/`, `?`, `#`, or a space corrupts the URL.

**Files:**
- Modify: `internal/client/generators/typescript/rest.go:536-547`
- Test: `internal/client/generators/typescript/rest_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPathParamsAreURLEncoded(t *testing.T) {
	spec := &client.APISpec{
		Info: client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{{
			Method: "GET", Path: "/files/{path}", OperationID: "files.get",
			PathParams: []client.Parameter{{Name: "path", Schema: &client.Schema{Type: "string"}, Required: true}},
			Responses:  map[int]*client.Response{204: {Description: "ok"}},
		}},
	}

	code := NewRESTGenerator().Generate(spec, client.DefaultConfig())

	assert.Contains(t, code, "${encodeURIComponent(String(path))}")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestPathParamsAreURLEncoded -v`
Expected: FAIL — output contains `${path}`, not the encoded form.

- [ ] **Step 3: Write minimal implementation**

In `generatePathExpression`:

```go
	for _, param := range endpoint.PathParams {
		paramName := r.toTSParamName(param.Name)
		placeholder := fmt.Sprintf("{%s}", param.Name)
		// Path segments must be escaped: an unencoded '/' or '?' in a value
		// silently changes which route the request reaches.
		path = strings.ReplaceAll(path, placeholder, "${encodeURIComponent(String("+paramName+"))}")
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestPathParamsAreURLEncoded -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/rest.go internal/client/generators/typescript/rest_test.go
git commit -m "fix(client/typescript): url-encode path parameters"
```

---

### Task 9: Fix endpoint-tree clobbering

`insertIntoTree` (`rest.go:48`) converts a leaf into a branch, but the reverse order silently discards work: inserting `users.list` when `users` is already a branch replaces the entire branch. Duplicate operation IDs also overwrite silently.

**Files:**
- Modify: `internal/client/generators/typescript/rest.go:48-91`
- Test: `internal/client/generators/typescript/rest_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestEndpointTreeKeepsBothOrders(t *testing.T) {
	mk := func(opID string) client.Endpoint {
		return client.Endpoint{
			Method: "GET", Path: "/x", OperationID: opID,
			Responses: map[int]*client.Response{204: {Description: "ok"}},
		}
	}

	// Branch created first, then a sibling leaf at the same name.
	code := NewRESTGenerator().Generate(&client.APISpec{
		Info:      client.APIInfo{Title: "T", Version: "1"},
		Endpoints: []client.Endpoint{mk("users.active.list"), mk("users.list")},
	}, client.DefaultConfig())

	assert.Contains(t, code, "active: {", "nested namespace must survive")
	assert.Contains(t, code, "list: async (", "sibling method must survive")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestEndpointTreeKeepsBothOrders -v`
Expected: FAIL — `active: {` absent; the leaf insertion replaced the `users` branch.

- [ ] **Step 3: Write minimal implementation**

Replace the leaf case in `insertIntoTree`:

```go
	if len(parts) == 1 {
		name := parts[0]

		if existing := node.Children[name]; existing != nil && !existing.IsLeaf {
			// A namespace already occupies this name. Keep the namespace and
			// hang the method inside it rather than discarding the subtree.
			existing.Children[name] = &EndpointNode{
				MethodName: name,
				Endpoint:   endpoint,
				IsLeaf:     true,
			}

			return
		}

		node.Children[name] = &EndpointNode{
			MethodName: name,
			Endpoint:   endpoint,
			IsLeaf:     true,
		}

		return
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestEndpointTree -v`
Expected: PASS, including the pre-existing tree tests in `rest_test.go`.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/rest.go internal/client/generators/typescript/rest_test.go
git commit -m "fix(client/typescript): stop leaf insertion from discarding a namespace"
```

---

### Task 10: Shared case conversion

`toCamelCase` (`rest.go:614`) lowercases the tail of every part, so `toCamelCase("userId")` returns `"userid"`. Four near-duplicate copies exist across `rest.go`, `sse.go`, `websocket.go`, and `webtransport.go`.

**Files:**
- Create: `internal/client/generators/typescript/naming.go`, `naming_test.go`
- Modify: `rest.go:608-636`, `sse.go:516`, `websocket.go`, `webtransport.go` to delegate

- [ ] **Step 1: Write the failing test**

```go
package typescript

import "testing"

func TestToCamel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user_id", "userId"},
		{"user-id", "userId"},
		{"userId", "userId"},   // already camel: must be preserved, not lowercased
		{"UserID", "userID"},   // leading cap dropped, interior caps kept
		{"id", "id"},
		{"", ""},
	}

	for _, c := range cases {
		if got := toCamel(c.in); got != c.want {
			t.Errorf("toCamel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToPascal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user_id", "UserId"},
		{"message.created", "MessageCreated"},
		{"userId", "UserId"},
	}

	for _, c := range cases {
		if got := toPascal(c.in); got != c.want {
			t.Errorf("toPascal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run 'TestToCamel|TestToPascal' -v`
Expected: FAIL to compile — `undefined: toCamel`.

- [ ] **Step 3: Write minimal implementation**

```go
package typescript

import "strings"

// splitWords breaks name on separators and on lower-to-upper boundaries, so an
// already-camelCase name round-trips instead of being flattened.
func splitWords(name string) []string {
	var (
		words []string
		cur   strings.Builder
	)

	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}

	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.':
			flush()
		case i > 0 && r >= 'A' && r <= 'Z' && runes[i-1] >= 'a' && runes[i-1] <= 'z':
			flush()
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}

	flush()

	return words
}

// toCamel converts name to camelCase, preserving interior capitalisation.
func toCamel(name string) string {
	words := splitWords(name)
	if len(words) == 0 {
		return name
	}

	var out strings.Builder

	out.WriteString(strings.ToLower(words[0][:1]) + words[0][1:])

	for _, w := range words[1:] {
		out.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}

	return out.String()
}

// toPascal converts name to PascalCase, preserving interior capitalisation.
func toPascal(name string) string {
	words := splitWords(name)

	var out strings.Builder

	for _, w := range words {
		out.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}

	return out.String()
}
```

Then change `(*RESTGenerator).toCamelCase` to `return toCamel(s)`, `(*SSEGenerator).toPascalCase` to `return toPascal(str)`, and the equivalents in `websocket.go` and `webtransport.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -v`
Expected: PASS, whole package. Existing tests asserting method names still hold — `users.get` and `data.delete` are unaffected by the boundary rule.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/
git commit -m "fix(client/typescript): preserve interior caps in case conversion"
```

---

### Task 11: Delete dead `generateEndpointMethod`

`rest.go:320-423` is unreachable — `Generate` builds output from the endpoint tree. It still carries the pre-`cc3364e` `path` collision and unconditional-`body` bugs, so leaving it invites a future caller to reintroduce both.

**Files:**
- Modify: `internal/client/generators/typescript/rest.go:319-423` (delete), and `generateMethodName` if it becomes unused

- [ ] **Step 1: Confirm it is unreachable**

Run:
```bash
grep -rn "generateEndpointMethod\|generateMethodName" internal/client/ --include=*.go
```
Expected: `generateEndpointMethod` appears only at its definition. Note whether `generateMethodName` has other callers; delete it only if it does not.

- [ ] **Step 2: Run the package tests to establish the baseline**

Run: `go test ./internal/client/generators/typescript/ -count=1`
Expected: PASS — this records the state the deletion must preserve.

- [ ] **Step 3: Delete the dead code**

Remove `generateEndpointMethod` in full. Remove `generateMethodName` only if Step 1 showed no other caller.

- [ ] **Step 4: Run tests to verify nothing regressed**

Run: `go test ./internal/client/generators/typescript/ -count=1 && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/rest.go
git commit -m "chore(client/typescript): remove unreachable generateEndpointMethod"
```

---

### Task 12: Fix abort-signal handling and error throws

`fetch_client.go:155` sets `const signal = requestConfig.signal || controller.signal`, so passing a signal discards the timeout entirely. `handleErrorResponse` throws an object literal, so `error.name === 'AbortError'` in `shouldRetry` can never match and `instanceof Error` fails for consumers.

**Files:**
- Modify: `internal/client/generators/typescript/fetch_client.go:150-156`, `:214-238`
- Test: `internal/client/generators/typescript/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/generators/typescript/ -run TestFetchClientCombinesSignals -v`
Expected: FAIL on all three assertions.

- [ ] **Step 3: Write minimal implementation**

Emit an `HTTPError` class in `GenerateBaseClient`, before `HTTPClient`:

```go
	buf.WriteString("/** Error thrown for non-2xx responses. */\n")
	buf.WriteString("export class HTTPError extends Error {\n")
	buf.WriteString("  readonly statusCode: number;\n")
	buf.WriteString("  readonly code: string;\n")
	buf.WriteString("  readonly details: unknown;\n\n")
	buf.WriteString("  constructor(statusCode: number, message: string, code: string, details: unknown) {\n")
	buf.WriteString("    super(message);\n")
	buf.WriteString("    this.name = 'HTTPError';\n")
	buf.WriteString("    this.statusCode = statusCode;\n")
	buf.WriteString("    this.code = code;\n")
	buf.WriteString("    this.details = details;\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")
```

Replace the throw in `handleErrorResponse`:

```go
	buf.WriteString("    throw new HTTPError(response.status, message, code, details);\n")
```

Replace the signal handling so the timeout always applies:

```go
	buf.WriteString("    // Combine the caller's signal with the timeout signal; using the\n")
	buf.WriteString("    // caller's alone would silently disable the timeout.\n")
	buf.WriteString("    const signal = requestConfig.signal\n")
	buf.WriteString("      ? (AbortSignal as any).any\n")
	buf.WriteString("        ? (AbortSignal as any).any([requestConfig.signal, controller.signal])\n")
	buf.WriteString("        : requestConfig.signal\n")
	buf.WriteString("      : controller.signal;\n\n")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/generators/typescript/ -run TestFetchClientCombinesSignals -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/fetch_client.go internal/client/generators/typescript/gate_test.go
git commit -m "fix(client/typescript): keep timeouts with caller signals, throw real errors"
```

---

### Task 13: Turn on the full gate and wire CI

Every prior task asserted the absence of one error class. This task asserts **zero** errors across the whole corpus, and makes the gate live in CI.

**Files:**
- Modify: `internal/client/generators/typescript/gate_test.go`
- Modify: `.github/workflows/go.yml`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify current state**

Run: `go test ./internal/client/generators/typescript/ -run TestGeneratedClientsTypeCheck -v`
Expected: PASS if Tasks 3–12 are complete. **If any fixture still reports errors, fix them now** — this is the gate the phase exists to satisfy. Do not weaken the assertion.

- [ ] **Step 3: Add Node to the CI test job**

In `.github/workflows/go.yml`, in the job containing `Run tests` (before that step):

```yaml
      - name: Set up Node
        uses: actions/setup-node@v4
        with:
          node-version: '20'

      - name: Install TypeScript
        run: npm install -g typescript@5.8.2
```

- [ ] **Step 4: Verify the full suite**

Run:
```bash
go test -short -race -timeout=10m $(go list ./... | grep -v '/bk/')
```
Expected: exit 0, no `FAIL`, no `DATA RACE`.

- [ ] **Step 5: Commit**

```bash
git add internal/client/generators/typescript/gate_test.go .github/workflows/go.yml
git commit -m "test(client/typescript): require generated clients to type-check in CI"
```

---

## Self-Review

**Spec coverage (Phase 1 scope).** Every Phase 1 item in the spec maps to a task: tsc harness → 1–2; `Client`/`APIName` → 4; dangling `AuthConfig` → 3; unquoted keys → 5; sorted iteration → 7; `encodeURIComponent` → 8; `insertIntoTree` → 9; shared case function → 10; dead `generateEndpointMethod` → 11; combined abort signals and real `Error` throws → 12. Task 6 (`require`) is **not** in the spec's Phase 1 list — it was found by measuring the baseline, and is included because the gate cannot go green without it.

**Deviation from spec, recorded.** The spec says duplicate operation IDs overwrite silently and should be fixed in Phase 1. Task 9 fixes the branch-clobber but *not* duplicate-ID detection, because making duplicates a generation error may break existing specs in this repo and needs its own decision. **Carry to Phase 2** as an explicit task, or raise it before starting Phase 2.

**Type consistency.** `typeCheck`/`findTSC` (Task 1) are used by Tasks 3, 4, 5, 6, 13. `gateFixtures`/`generateTo`/`baseSpec`/`baseConfig` (Task 2) are used by Tasks 3–7, 12, 13. `errorsMentioning` is defined once in Task 3 and reused by 4 and 6. `toCamel`/`toPascal` (Task 10) replace the four private methods and keep their existing call sites.

**Not covered here.** Phases 2–4 (type-generation depth, naming codec, streaming rework) get their own plans, written once this gate is green — their tasks depend on what the gate reports against a corrected baseline.
