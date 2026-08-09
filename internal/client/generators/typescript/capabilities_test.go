package typescript

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

// generateCapabilityFixture generates the "capabilities" gate fixture and
// returns the whole file set, so a test can assert on capabilities.ts and on
// what the rest of the client did or did not gain alongside it.
func generateCapabilityFixture(t *testing.T) map[string]string {
	t.Helper()

	out, err := NewGenerator().Generate(context.Background(), capabilitySpec(), baseConfig())
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}

	return out.Files
}

func capabilityFile(t *testing.T) string {
	t.Helper()

	files := generateCapabilityFixture(t)

	src, ok := files["src/capabilities.ts"]
	if !ok {
		t.Fatal("expected src/capabilities.ts to be emitted for a spec declaring scopes")
	}

	return src
}

// TestCapabilityFileStatesItIsNotASecurityBoundary is the test the whole
// feature exists under.
//
// The constraint is that capability gating is a UX affordance and never a
// security boundary, and that this must be impossible to misread at the call
// site -- which means the warning belongs in the generated file, where somebody
// reading capabilities.ts sees it, rather than only in documentation they may
// never open. Asserting on it makes deleting it a failing test rather than a
// silent edit.
func TestCapabilityFileStatesItIsNotASecurityBoundary(t *testing.T) {
	src := capabilityFile(t)

	for _, want := range []string{
		"NEVER A SECURITY BOUNDARY",
		"Authorization is enforced server-side",
		"Hiding a button is not access control",
		"Nothing here blocks a request",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated capabilities.ts must state %q", want)
		}
	}
}

// TestCapabilityUnionIsSortedAndDistinct pins the emitted union exactly.
//
// The scopes reach the generator through a Go map walk, and capabilitySpec
// declares users.create's two scopes unsorted, so a dropped sort produces a
// file that differs between runs -- which CI reports as drift in a file nobody
// edited.
func TestCapabilityUnionIsSortedAndDistinct(t *testing.T) {
	src := capabilityFile(t)

	want := "export type Capability =\n  | 'admin'\n  | 'users.read'\n  | 'users.write';\n"
	if !strings.Contains(src, want) {
		t.Errorf("capability union not emitted as expected; wanted:\n%s\ngot:\n%s", want, src)
	}
}

// TestRequiredCapabilitiesTableShape covers all four security shapes
// capabilitySpec declares, in one place, because what matters is as much what
// the table OMITS as what it contains.
func TestRequiredCapabilitiesTableShape(t *testing.T) {
	src := capabilityFile(t)

	table := section(t, src, "export const requiredCapabilities = {", "} as const satisfies")

	for _, want := range []string{
		// One alternative, one scope.
		`'users.get': [['users.read']]`,
		// Two scopes ANDed inside one alternative, sorted despite being
		// declared as {"users.write", "admin"}.
		`'users.create': [['admin', 'users.write']]`,
		// Two alternatives, ORed, each internally sorted and the pair ordered.
		`'uploads.create': [['admin'], ['users.read', 'users.write']]`,
	} {
		if !strings.Contains(table, want) {
			t.Errorf("requiredCapabilities missing %s; got:\n%s", want, table)
		}
	}

	// A scheme declaring no scopes means "authenticated, no particular scope".
	// The operation is not scope-gated and must be absent from the table
	// entirely -- present with an empty array would claim it is gated on
	// nothing, which is a different and wrong statement.
	for _, absent := range []string{"raw.create", "texts.get", "downloads.get"} {
		if strings.Contains(table, absent) {
			t.Errorf("requiredCapabilities must omit ungated operation %s; got:\n%s", absent, table)
		}
	}
}

// TestOperationUnionIncludesUngatedOperations is the counterpart to the
// omission above: an operation absent from the requirements table must still be
// a member of OperationName, so canCall() accepts it and answers true rather
// than failing to compile.
func TestOperationUnionIncludesUngatedOperations(t *testing.T) {
	src := capabilityFile(t)

	union := section(t, src, "export type OperationName =", ";")

	for _, want := range []string{"users.get", "texts.get", "downloads.get", "raw.create"} {
		if !strings.Contains(union, want) {
			t.Errorf("OperationName must include %q, gated or not; got:\n%s", want, union)
		}
	}
}

// TestCapabilitiesNotEmittedWithoutScopes covers the gate.
//
// A spec declaring no scope would produce `export type Capability = never` on a
// module whose every function is then uncallable. baseSpec has an auth scheme
// but no scopes anywhere, which is the realistic version of this case: most
// APIs authenticate without declaring per-route scopes at all.
func TestCapabilitiesNotEmittedWithoutScopes(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), baseSpec(), baseConfig())
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}

	if _, ok := out.Files["src/capabilities.ts"]; ok {
		t.Error("src/capabilities.ts must not be emitted for a spec declaring no scopes")
	}

	if strings.Contains(out.Files["src/index.ts"], "./capabilities") {
		t.Error("index.ts must not export a capabilities module that was not emitted")
	}
}

func TestCapabilitiesExportedFromBarrel(t *testing.T) {
	files := generateCapabilityFixture(t)

	if !strings.Contains(files["src/index.ts"], "export * from './capabilities';") {
		t.Errorf("index.ts must re-export the capability module; got:\n%s", files["src/index.ts"])
	}
}

// TestCapabilitiesAddNoRuntimeDependency pins the property that lets this file
// be emitted in every generation mode.
//
// generatePackageJSON declares @forge-go/client-core only when hooks are
// enabled, so a capability module that imported anything would either break a
// hooks-off client at install time or force a dependency onto clients that
// today have none.
func TestCapabilitiesAddNoRuntimeDependency(t *testing.T) {
	files := generateCapabilityFixture(t)

	if strings.Contains(files["src/capabilities.ts"], "import ") {
		t.Error("capabilities.ts must import nothing: it is emitted in modes where no runtime dependency is declared")
	}

	if strings.Contains(files["package.json"], "client-core") {
		t.Error("emitting capabilities must not add a runtime dependency to package.json")
	}
}

// TestCapabilitiesEmittedForStreamOnlyScopes covers the AsyncAPI-only shape: a
// scope declared on a WebSocket route is still a scope this API has, so the
// union is emitted, but there are no REST operations for the per-operation half
// to be keyed by.
//
// Emitting an empty OperationName union would make it `never` and canCall a
// function no caller could pass an argument to, so that half is skipped
// entirely rather than emitted degenerate.
func TestCapabilitiesEmittedForStreamOnlyScopes(t *testing.T) {
	spec := wsSSESpec()
	spec.Endpoints = nil
	spec.WebSockets[0].Security = []client.SecurityRequirement{
		{SchemeName: "bearerAuth", Scopes: []string{"feed.subscribe"}},
	}

	src := NewCapabilityGenerator().Generate(spec, baseConfig())

	if !strings.Contains(src, "'feed.subscribe'") {
		t.Errorf("a scope declared on a WebSocket route must reach the union; got:\n%s", src)
	}

	if !strings.Contains(src, "export function can(") {
		t.Error("can() must be emitted whenever the union is")
	}

	// Matched as declarations rather than as bare names: the file header
	// legitimately discusses the predicates in prose, and a substring search
	// would find it there and report an emission that never happened.
	for _, absent := range []string{
		"export type OperationName",
		"export const requiredCapabilities",
		"export function canCall(",
		"export function missingCapabilities(",
	} {
		if strings.Contains(src, absent) {
			t.Errorf("%q must not be emitted for a spec with no REST endpoints; got:\n%s", absent, src)
		}
	}
}

// section returns the text between the first occurrence of open and the next
// occurrence of closing after it, so a table assertion cannot accidentally be
// satisfied by a substring living in a doc comment elsewhere in the file.
func section(t *testing.T, src, open, closing string) string {
	t.Helper()

	_, rest, found := strings.Cut(src, open)
	if !found {
		t.Fatalf("expected generated file to contain %q; got:\n%s", open, src)
	}

	body, _, found := strings.Cut(rest, closing)
	if !found {
		t.Fatalf("expected %q after %q; got:\n%s", closing, open, src)
	}

	return body
}

// TestCapabilityRuntimeAnswersUnderNode is the execution proof.
//
// Every other test in this file asserts on generated TEXT, which cannot catch a
// predicate that emits cleanly and then answers wrongly -- and the answers are
// the whole feature. This bundles the real generated module with esbuild and
// runs it under Node, exercising the three states the design turns on: nothing
// known yet, a scope set granted, and that set forgotten again.
//
// The driver is also type-checked before it runs, so the union types are proven
// to accept the arguments passed rather than merely to exist.
func TestCapabilityRuntimeAnswersUnderNode(t *testing.T) {
	out, err := NewGenerator().Generate(context.Background(), capabilitySpec(), baseConfig())
	if err != nil {
		t.Fatalf("generation failed: %v", err)
	}

	dir := t.TempDir()
	writeTree(t, dir, out.Files)

	// Every capability and operation name below is a literal checked against the
	// generated unions by tsc: a misspelling fails the type-check rather than
	// silently answering false, which is the property the union type exists for.
	driver := `
import {
  can,
  canCall,
  capabilitiesKnown,
  missingCapabilities,
  setCapabilities,
} from './capabilities';

const snapshot = () => ({
  known: capabilitiesKnown(),
  canRead: can('users.read'),
  canWrite: can('users.write'),
  // Gated on one scope.
  callGet: canCall('users.get'),
  // Gated on two scopes ANDed together.
  callCreate: canCall('users.create'),
  missingCreate: missingCapabilities('users.create'),
  // Gated on two alternatives of different sizes.
  missingUpload: missingCapabilities('uploads.create'),
  // Declares a scheme but no scope, so it is not scope-gated.
  callRaw: canCall('raw.create'),
  // Declares no security at all.
  callText: canCall('texts.get'),
});

const before = snapshot();

// A scope the specification never mentions, granted alongside a real one: a
// server may grant anything, and an unrecognised scope must be harmless.
setCapabilities(['users.read', 'scope.this.client.never.heard.of']);
const partial = snapshot();

setCapabilities(['users.read', 'users.write', 'admin']);
const full = snapshot();

setCapabilities();
const forgotten = snapshot();

console.log(JSON.stringify({ before, partial, full, forgotten }));
`

	writeTree(t, dir, map[string]string{"src/__driver_capabilities.ts": driver})

	if errs := typeCheck(t, dir); len(errs) != 0 {
		t.Fatalf("generated client + capability consumer must type-check cleanly, got:\n%s", strings.Join(errs, "\n"))
	}

	var result struct {
		Before    capabilitySnapshot `json:"before"`
		Partial   capabilitySnapshot `json:"partial"`
		Full      capabilitySnapshot `json:"full"`
		Forgotten capabilitySnapshot `json:"forgotten"`
	}

	if err := json.Unmarshal([]byte(runNodeDriver(t, dir, "src/__driver_capabilities.ts")), &result); err != nil {
		t.Fatalf("driver output was not the expected JSON: %v", err)
	}

	// Before anything is known: every answer false, and known is what tells a
	// caller that the falseness means "not yet" rather than "denied". This is
	// the decision the whole call-site shape rests on.
	assertSnapshot(t, "before", result.Before, capabilitySnapshot{
		Known: false, CanRead: false, CanWrite: false,
		CallGet: false, CallCreate: false,
		MissingCreate: []string{"admin", "users.write"},
		MissingUpload: []string{"admin"},
		// An operation requiring nothing is callable even before anything is
		// known -- there is no scope to be missing, so there is nothing to wait
		// for.
		CallRaw: true, CallText: true,
	})

	// One scope granted. users.get opens; users.create still needs both of its,
	// and reports exactly the ones outstanding.
	assertSnapshot(t, "partial", result.Partial, capabilitySnapshot{
		Known: true, CanRead: true, CanWrite: false,
		CallGet: true, CallCreate: false,
		MissingCreate: []string{"admin", "users.write"},
		// uploads.create is reachable via ['admin'] or via
		// ['users.read','users.write']. With users.read held, the second route
		// is one scope short and the first is one scope short, and the answer
		// names a shortest one rather than an arbitrary one.
		MissingUpload: []string{"admin"},
		CallRaw:       true, CallText: true,
	})

	assertSnapshot(t, "full", result.Full, capabilitySnapshot{
		Known: true, CanRead: true, CanWrite: true,
		CallGet: true, CallCreate: true,
		MissingCreate: []string{}, MissingUpload: []string{},
		CallRaw: true, CallText: true,
	})

	// Forgetting must return the module to the not-yet-known state exactly,
	// rather than to "known, and it is none" -- a sign-out that left
	// capabilitiesKnown() true would make every consumer render a confident
	// empty interface instead of a loading one.
	assertSnapshot(t, "forgotten", result.Forgotten, result.Before)
}

type capabilitySnapshot struct {
	Known         bool     `json:"known"`
	CanRead       bool     `json:"canRead"`
	CanWrite      bool     `json:"canWrite"`
	CallGet       bool     `json:"callGet"`
	CallCreate    bool     `json:"callCreate"`
	MissingCreate []string `json:"missingCreate"`
	MissingUpload []string `json:"missingUpload"`
	CallRaw       bool     `json:"callRaw"`
	CallText      bool     `json:"callText"`
}

func assertSnapshot(t *testing.T, stage string, got, want capabilitySnapshot) {
	t.Helper()

	for _, field := range []struct {
		name      string
		got, want bool
	}{
		{"capabilitiesKnown()", got.Known, want.Known},
		{"can('users.read')", got.CanRead, want.CanRead},
		{"can('users.write')", got.CanWrite, want.CanWrite},
		{"canCall('users.get')", got.CallGet, want.CallGet},
		{"canCall('users.create')", got.CallCreate, want.CallCreate},
		{"canCall('raw.create')", got.CallRaw, want.CallRaw},
		{"canCall('texts.get')", got.CallText, want.CallText},
	} {
		if field.got != field.want {
			t.Errorf("%s: %s = %v, want %v", stage, field.name, field.got, field.want)
		}
	}

	assertScopes(t, stage, "missingCapabilities('users.create')", got.MissingCreate, want.MissingCreate)
	assertScopes(t, stage, "missingCapabilities('uploads.create')", got.MissingUpload, want.MissingUpload)
}

func assertScopes(t *testing.T, stage, name string, got, want []string) {
	t.Helper()

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: %s = %v, want %v", stage, name, got, want)
	}
}
