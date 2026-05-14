# Slice (f) — Streaming Extension Migration

> Companion design doc to [DESIGN.md](DESIGN.md). Slice (f) is the first real external contributor migrating from the legacy templ-based contributor system to the new contract YAML.

## Context

Slices (a)–(e.5) shipped the contract package, dispatcher, security stack, pilot contributor, React shell, the v1 vocabulary on shadcn/Base UI, and the docs. The streaming extension is the only **external** contributor today. Its dashboard surface (6 routes, 4 widgets, 1 settings page, 5 mutations) is implemented as a templ-rendering `LocalContributor` at [extensions/streaming/dashboard/](../../streaming/dashboard/).

Slice (f) ports that surface to the new contract: a YAML manifest declaring the routes + intents, typed dispatcher handlers calling the existing `Manager` interface, and a small wire-up in `extensions/streaming/extension.go` to register both the legacy and the contract paths during the migration window.

The legacy `/dashboard/ext/streaming/*` URLs keep working (templ paths stay registered). The new path is `/dashboard/contract/streaming-contract/*`. Once stakeholders are happy, a follow-on slice will retire the templ paths globally (across `streaming-contract` and `core-contract` and CoreContributor) — that retirement is **explicitly out of scope here**.

## Architecture Decisions (locked in)

| Decision | Choice | Rationale |
|---|---|---|
| Contributor name | `streaming-contract` | Matches the slice (c) pilot pattern (`core-contract`). Name is namespaced so legacy `streaming` keeps coexisting. |
| Sub-package layout | `extensions/streaming/contract/` | Mirrors the pilot at `extensions/dashboard/contract/pilot/`. Keeps migration code adjacent to the manager it calls without polluting the main streaming package. |
| Handlers | All five Mutations as `command` kind; all reads as `query` kind. No subscriptions in v1 (today's contributor polls). | Matches today's behavior. Subscriptions are a later optimization once we measure traffic. |
| Settings page | Read-only `dashboard.grid` of config fields | Today's settings page is read-only; preserve the contract. Mutable config is future work. |
| Presence page | `resource.list` over a flattened presence-per-user query | Today's templ does the per-user iteration server-side; we move that into a single query handler. |
| Playground page | Five `form.edit` cards in a `dashboard.grid` | Each form binds an `op` (the command intent). Submission shows the response (success or error) inline. |
| Detail navigation | `resource.list` + `detailDrawer` slot for `/rooms`. No separate `/rooms/{id}` route in the YAML | Detail-drawer is the v1 admin pattern shadcn provides; deep-linkable detail routes are a follow-on. |
| Registration | Streaming extension registers via the `dashboard.ContractContributorAware` interface (new this slice — small) | Keeps the streaming code unaware of the dashboard's contract registry shape; the dashboard discovers contract contributors at startup the same way it discovers legacy contributors. |
| Tests | 1 unit test per handler + 1 e2e test for the wired-up registration | Same TDD discipline as the pilot. |

## Scope

### In scope (this slice)
- `extensions/streaming/contract/` sub-package with:
  - `manifest.yaml` declaring 6 routes + ~14 intents.
  - `types.go` for the wire shapes the handlers return (mostly `*internal.ManagerStats` etc. directly).
  - `handlers/` files grouped by topic: `stats.go`, `connections.go`, `rooms.go`, `channels.go`, `presence.go`, `playground.go`, `config.go`.
  - `streaming.go` — `Register(disp, deps) error` entry point that loads the YAML, registers all handlers, and returns.
- A `dashboard.ContractContributorAware` interface in `extensions/dashboard/contributor/` so the dashboard can discover contributors at startup.
- Wire-up in `extensions/streaming/extension.go` so the streaming extension implements `ContractContributorAware` and gets registered automatically.
- Wire-up in `extensions/dashboard/extension.go` to discover and register external contract contributors during dashboard startup.
- Unit tests for each handler.
- E2E test that exercises the registered streaming-contract through the contract HTTP handler.

### Out of scope (future slices)
- **Slice (g)**: retire the legacy `/dashboard/ext/streaming/*` templ paths once the team has flipped to the contract path in production.
- **Slice (h)**: retire `CoreContributor`'s templ pages (Overview/Health/Metrics/Services/Traces/Extensions). Bigger because every dashboard deployment depends on them.
- Subscriptions for live updates (today's contributor polls; sub-streams are a future optimization).
- Mutable settings (form.edit for config). Today's settings page is read-only and we preserve that.
- Deep-linked `/rooms/{id}` route (detailDrawer covers the immediate use case).

## Vocabulary mapping

| Legacy route | Contract route | Intent tree |
|---|---|---|
| `/` | `/streaming-contract/` | `page.shell` → `dashboard.grid` (cols=4) → 7× `metric.counter` (one per ManagerStats field) |
| `/connections` | `/streaming-contract/connections` | `page.shell` → `resource.list` (data: connections.list, columns: connID, userID, rooms, subs, lastActivity, status) |
| `/rooms` | `/streaming-contract/rooms` | `page.shell` → `resource.list` (data: rooms.list, columns: id, name, members, created, private, archived) + `detailDrawer` slot → `resource.detail` (data: rooms.detail using parent.id) + a row-level admin section showing `rooms.members` and `rooms.moderation` |
| `/rooms/{id}` | (covered by detailDrawer) | — |
| `/channels` | `/streaming-contract/channels` | `page.shell` → `resource.list` (data: channels.list, columns: channelID, name, subCount, messageCount, created) |
| `/presence` | `/streaming-contract/presence` | `page.shell` → `resource.list` (data: presence.list, columns: userID, status, lastSeen, rooms) |
| `/playground` | `/streaming-contract/playground` | `page.shell` → `dashboard.grid` (cols=2) → 5× `form.edit` (one per mutation: create-room, delete-room, send-message, set-presence, kick-connection) |
| `/settings` | (settings descriptor unchanged; settings page handled by dashboard's existing settings registry) | The `streaming.config` query intent backs a read-only display; can be embedded in any page later |

## Intent declarations (YAML preview)

```yaml
schemaVersion: 1
contributor:
  name: streaming-contract
  envelope: { supports: [v1], preferred: v1 }
  capabilities: [streaming.read, streaming.write]

intents:
  # Reads
  - { name: stats,             kind: query, version: 1, capability: read }
  - { name: connections.list,  kind: query, version: 1, capability: read }
  - { name: rooms.list,        kind: query, version: 1, capability: read }
  - { name: rooms.detail,      kind: query, version: 1, capability: read }
  - { name: rooms.members,     kind: query, version: 1, capability: read }
  - { name: rooms.moderation,  kind: query, version: 1, capability: read }
  - { name: channels.list,     kind: query, version: 1, capability: read }
  - { name: presence.list,     kind: query, version: 1, capability: read }
  - { name: config,            kind: query, version: 1, capability: read }

  # Mutations
  - { name: rooms.create,         kind: command, version: 1, capability: write, requires: { all: [scope:streaming.write] } }
  - { name: rooms.delete,         kind: command, version: 1, capability: write, requires: { all: [scope:streaming.write] } }
  - { name: rooms.send-message,   kind: command, version: 1, capability: write, requires: { all: [scope:streaming.write] } }
  - { name: presence.set,         kind: command, version: 1, capability: write, requires: { all: [scope:streaming.write] } }
  - { name: connections.kick,     kind: command, version: 1, capability: write, requires: { all: [scope:streaming.write], any: [role:admin, role:moderator] } }
```

Routes follow under the `graph:` key; full YAML lives at `extensions/streaming/contract/manifest.yaml`.

## Files Affected

### New (this slice)

```
extensions/streaming/contract/
  doc.go
  manifest.yaml
  streaming.go              # Register(disp, deps) entry point
  deps.go                   # Deps struct {Manager, Config}
  types.go                  # response payloads (ConnectionsList, RoomsList, ...)
  stats.go                  # statsHandler
  connections.go            # connectionsListHandler, kickConnectionHandler
  rooms.go                  # roomsListHandler, roomDetailHandler, membersHandler, moderationHandler, createRoomHandler, deleteRoomHandler, sendMessageHandler
  channels.go               # channelsListHandler
  presence.go               # presenceListHandler, setPresenceHandler
  config.go                 # configHandler
  *_test.go                 # one test file per topic group
  e2e_test.go               # full HTTP round-trip through the contract handler
```

### Modified

- `extensions/streaming/extension.go` — implement `dashboard.ContractContributorAware` (returns a `pilot.Register`-style function that the dashboard calls during startup).
- `extensions/dashboard/extension.go` — during contributor discovery, also call `ContractContributorAware`-implementing extensions to register their contract handlers + manifest. Same loop that today calls `DashboardAware.DashboardContributor()` gets a sibling branch for `ContractContributorAware`.
- `extensions/dashboard/contributor/types.go` (or similar) — declare the `ContractContributorAware` interface.

### Reused (do not duplicate)

- `extensions/streaming/internal.Manager` — every read and write goes through this existing interface.
- `extensions/dashboard/contract/dispatcher.RegisterQuery / RegisterCommand` — the typed wrappers.
- `extensions/dashboard/contract/loader.Load + Validate` — the same YAML loader the pilot uses.
- `pilot.Deps` style adapter pattern — the same shape, scoped to streaming's data sources.

## Verification

1. **Unit tests** under `contract/*_test.go`:
   - Each handler called with a fixture Manager produces the expected payload.
   - Mutation handlers return `&contract.Error{Code: CodeNotFound}` / `CodeBadRequest` for the right error paths.
   - The presence-list handler correctly aggregates per-connection presence calls.

2. **E2E test** (`contract/e2e_test.go`):
   - Stand up dispatcher + contract registry + streaming.Register in-process.
   - POST `kind=query` for each intent against the contract HTTP handler; assert envelope shape.
   - POST `kind=command` for `rooms.create`; assert idempotency-key persistence works (returns the same response on a duplicate).

3. **Go tests + vet** — entire repo green: `go test ./...`, `go vet ./...`.

4. **Shell verification** (manual, not automated):
   - Run dashboard + streaming, browse to `/dashboard/contract/app/streaming-contract/rooms`.
   - Verify the rooms page renders, click a row, see the detail drawer, click "Create Room" in the playground page, see a success result.

## Out of Scope — Future Slices

- **(g)** Retire `/dashboard/ext/streaming/*` once the team has cut over.
- **(h)** Migrate `CoreContributor` pages and retire the dashboard's own templ files (~2,266 LOC).
- **(i)** Add subscriptions to streaming-contract for real-time stats / room activity.
- **(j)** Add deep-linked detail routes (e.g., `/rooms/<id>`) once the React shell supports `dataKey` URL parameters.
- **(k)** Mutable settings page using `form.edit`.
