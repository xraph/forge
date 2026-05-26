// registry_test.go
package contract

import (
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// unmarshalForTest is a private test helper that wraps yaml.Unmarshal.
// First introduced for the registry tests; reused by other test files in this package.
func unmarshalForTest(b []byte, v any) error { return yaml.Unmarshal(b, v) }

func mustManifest(t *testing.T, src string) *ContractManifest {
	t.Helper()
	var m ContractManifest
	if err := unmarshalForTest([]byte(src), &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return &m
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: users.list, kind: query, version: 1, capability: read }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	intent, ok := r.Intent("users", "users.list", 1)
	if !ok || intent.Name != "users.list" {
		t.Errorf("lookup failed: ok=%v intent=%+v", ok, intent)
	}
}

func TestRegistry_DuplicateContributor(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
`)
	_ = r.Register(m)
	if err := r.Register(m); err == nil {
		t.Error("expected duplicate-contributor error")
	}
}

func TestRegistry_HighestActiveIntentVersion(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: user.disable, kind: command, version: 1, capability: write,
      deprecated: { intentVersion: 1, removeAfter: "2026-09-01" } }
  - { name: user.disable, kind: command, version: 2, capability: write }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.HighestVersion("users", "user.disable")
	if !ok || got != 2 {
		t.Errorf("HighestVersion = %d, ok=%v", got, ok)
	}
}

// TestRegistry_ResolvesQueryRefsAtMergeTime guards the wire contract: a
// graph node declared with `data: queries.X` must surface to the shell
// with node.data.intent set (resolved from the named query), not just
// node.data.queryRef. Without resolution, the React shell's useContractQuery
// hook falls back to intent="" and the dashboard hits the transport's
// "intent  not registered" path the first time it tries to load page data.
func TestRegistry_ResolvesQueryRefsAtMergeTime(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: ops, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: health, kind: query, version: 1, capability: read }
queries:
  health:
    intent: health
graph:
  - route: /health
    intent: page.shell
    slots:
      main:
        - intent: resource.list
          data: queries.health
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	root, _, ok := r.MatchRoute("ops", "/health")
	if !ok {
		t.Fatal("expected /health route to match")
	}
	main := root.Slots["main"]
	if len(main) != 1 {
		t.Fatalf("main slot len = %d, want 1", len(main))
	}
	data := main[0].Data
	if data == nil {
		t.Fatal("main[0].Data is nil; expected resolved binding")
	}
	if data.Intent != "health" {
		t.Errorf("Data.Intent = %q, want \"health\" (resolved from queries.health)", data.Intent)
	}
	if data.QueryRef != "queries.health" {
		t.Errorf("Data.QueryRef = %q, want preserved \"queries.health\"", data.QueryRef)
	}
	if data.Kind != IntentKindQuery {
		t.Errorf("Data.Kind = %q, want %q (stamped from intent declaration)", data.Kind, IntentKindQuery)
	}
}

// TestRegistry_StampsKindOnInlineDataBindings guards the same shell
// contract for inline `data: { intent: X }` bindings — they also need
// Kind stamped so metric.counter can pick subscription vs query without
// chasing the manifest's intent table.
func TestRegistry_StampsKindOnInlineDataBindings(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: ops, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: metrics.summary, kind: subscription, version: 1, capability: read, mode: replace }
graph:
  - route: /live
    intent: page.shell
    slots:
      main:
        - intent: dashboard.grid
          slots:
            widgets:
              - intent: metric.counter
                data: { intent: metrics.summary }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	root, _, ok := r.MatchRoute("ops", "/live")
	if !ok {
		t.Fatal("expected /live route to match")
	}
	data := root.Slots["main"][0].Slots["widgets"][0].Data
	if data == nil || data.Intent != "metrics.summary" {
		t.Fatalf("inline binding lost: %+v", data)
	}
	if data.Kind != IntentKindSubscription {
		t.Errorf("Data.Kind = %q, want %q", data.Kind, IntentKindSubscription)
	}
}

// TestRegistry_ResolveQueryRefDoesNotMutateSource ensures the resolution
// step doesn't write back into the manifest the caller registered. The
// graph builder hands the same *ContractManifest pointer back via
// Contributor(); silently rewriting node.Data on it would corrupt
// subsequent reads (e.g. the contract/manifest endpoint that serves the
// raw manifest to remote dashboards expects the un-resolved YAML shape).
func TestRegistry_ResolveQueryRefDoesNotMutateSource(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: ops, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: health, kind: query, version: 1, capability: read }
queries:
  health:
    intent: health
graph:
  - route: /health
    intent: page.shell
    slots:
      main:
        - intent: resource.list
          data: queries.health
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	// The source manifest's main[0].Data should still carry only the
	// unresolved QueryRef — the merge step works on a deep copy.
	srcMain := m.Graph[0].Slots["main"]
	if srcMain[0].Data == nil || srcMain[0].Data.Intent != "" || srcMain[0].Data.QueryRef != "queries.health" {
		t.Errorf("source manifest mutated: Data=%+v", srcMain[0].Data)
	}
}

func TestRegistry_RejectsBadSlotFill(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - intent: page.shell
    slots:
      main:
        - { intent: action.button }   # not allowed in page.shell.main
`)
	if err := r.Register(m); err == nil {
		t.Error("expected slot-accept rejection")
	}
}
