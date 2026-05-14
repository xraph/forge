# Slice (k) — Audit storage + live `audit.tail` subscription

**Status:** Active
**Branch:** `dashboard-contract-slice-a`
**Predecessors:** (a)–(j)

## Why

The audit emitter (slice b) writes to stdout/Logger via `LogAuditEmitter` and disappears. The vocabulary intent `audit.tail` (slice e) has had a React component that subscribes to a server intent named `audit.tail`, but that intent was never registered — clicking the audit widget would silently produce nothing. Slice (k) closes both gaps:

1. **Persistent (in-memory) audit storage** — every command run is stored in a ring buffer the dashboard can query.
2. **`audit.list` query intent** — paginated history of recent commands.
3. **`audit.tail` subscription intent** — live append-mode stream of new audit records, which is what the React `AuditTail` component is already wired to consume.
4. **`/audit` route** — top-level page in the manifest that combines history + live tail.

This unlocks the audit widget that's been a no-op since slice (e) and gives the dashboard real command observability without external infrastructure.

## Approach

### 1. `AuditStore` interface + in-memory impl

```go
type AuditStore interface {
    Append(rec AuditRecord)
    List(filter AuditFilter) []AuditRecord  // newest first, capped by Limit
    Subscribe() (<-chan AuditRecord, func())
}
```

In-memory implementation:
- Ring buffer (default cap 1000) protected by RWMutex.
- `List(filter)` walks newest→oldest, applying optional `User`, `Contributor`, `Intent`, `Result` filters; returns up to `filter.Limit` (default 200, max 1000).
- `Subscribe()` registers a fan-out channel; `Append()` non-blocking-sends to all subscribers (drops on slow consumers — telemetry, not authoritative).

The store lives in `extensions/dashboard/contract` next to `audit.go`.

### 2. `RecordingAuditEmitter`

Wraps any inner `AuditEmitter` and adds a store side-effect:

```go
func NewRecordingAuditEmitter(inner AuditEmitter, store AuditStore) AuditEmitter
```

Slice (b) wired `dispatcher.NewLoggerAuditEmitter(...)` into the extension. After slice (k) the wiring becomes `NewRecordingAuditEmitter(NewLoggerAuditEmitter(...), e.auditStore)` so log-line semantics stay and the store fills.

### 3. `audit.list` query handler

Lives in pilot at `extensions/dashboard/contract/pilot/audit.go`:

```go
type AuditListInput struct {
    Limit       int    `json:"limit,omitempty"`
    Contributor string `json:"contributor,omitempty"`
    Intent      string `json:"intent,omitempty"`
    User        string `json:"user,omitempty"`
    Result      string `json:"result,omitempty"`
}
type AuditListResponse struct {
    Records []AuditRecordDTO `json:"records"`
    Total   int              `json:"total"`
}
```

Records are projected to a wire-friendly DTO with RFC3339Nano timestamps and the same fields the React component renders.

### 4. `audit.tail` subscription handler

A `dispatcher.SubscriptionHandler` that calls `store.Subscribe()`, fans events out as `StreamEvent{Mode: ModeAppend, Payload: AuditRecordDTO}`. Cancellation closes the subscriber channel.

### 5. Manifest entry

Add to `pilot/manifest.yaml`:

```yaml
intents:
  - { name: audit.list, kind: query,        version: 1, capability: read }
  - { name: audit.tail, kind: subscription, version: 1, capability: read, mode: append }

queries:
  auditList:
    intent: audit.list
    cache: { staleTime: 5s }

graph:
  - route: /audit
    intent: page.shell
    title: Audit
    nav: { group: Operations, icon: history, priority: 23 }
    slots:
      main:
        - intent: audit.tail
          title: Live audit
          data:
            intent: audit.tail
          props:
            bufferSize: 200
```

This is the first time `audit.tail` appears in `page.shell.main`. Slot vocabulary needs an update: today `page.shell.main` accepts `[resource.list, resource.detail, dashboard.grid, form.edit, custom, iframe]` — extend with `audit.tail`.

The `audit.list` query stays available for future history-style pages; v1 of the audit page only ships the live tail because the React `AuditTail` component is already built around subscriptions and history-styled rendering would need a new component.

### 6. Wiring in `extension.go`

- `NewExtension` constructs `e.auditStore = contract.NewInMemoryAuditStore()`
- `e.auditEmitter = contract.NewRecordingAuditEmitter(<existing emitter>, e.auditStore)` after the existing `auditEmitter` selection block
- Pilot `Deps` gets a new `Audit AuditProvider` field carrying the store; `Register()` registers the two handlers when non-nil

## Tests

- **Store:** `TestAuditStore_AppendList` (round-trip), `TestAuditStore_Filter`, `TestAuditStore_RingTruncates`, `TestAuditStore_SubscribeBroadcasts`, `TestAuditStore_DropsSlowSubscriber`.
- **RecordingEmitter:** chains to inner + writes to store.
- **Pilot audit.list handler:** happy path, projection (timestamps formatted), CodeUnavailable when nil store, filter behavior.
- **Pilot audit.tail handler:** subscribes, emits events when store appends, cancellation cleans up.
- **Manifest:** loads with new intents, validates with new vocabulary entry.

## Out of scope

- Persistent storage backends (Postgres, SQLite). The interface is shaped to allow them; in-memory is the slice-(k) impl.
- Pagination cursors for `audit.list` (limit/offset would need a stable order key — slice (k2) when audit becomes a real history page).
- Per-tenant scoping. The store is global; multi-tenant filtering happens in handlers when we add tenant resolution to AuditRecord (slice l, separate concern).
- A history-rendering React component for `audit.list`. Slice (k) ships the live tail (`audit.tail`); a `resource.list`-style audit history page is a follow-on.

## Why not split

`audit.list` and `audit.tail` share the store and the projection helpers. The vocabulary update + manifest route benefits from being one PR with a single test pass. ~250 LOC total.
