# SSE Event Replay: Resumable Streams and Honest Gap Recovery

Date: 2026-08-08
Status: Approved, pending implementation plan

## Problem

A client whose stream drops and reconnects has missed events. It has no way to learn
*which* ones, so the only correct recovery available to it is to assume it missed
everything: `StreamBinder.recover` (`packages/client-core/src/live.ts:589`) invalidates
every list tag on the channel and refetches every registered live query.

That recovery is correct and deliberately blunt. Its own comment says so — a gap is every
event that did not arrive, so recovery has to be broader than any single event. The cost is
paid on every reconnect regardless of whether one event was missed or ten thousand: a laptop
lid closed for four seconds triggers the same full resync as an hour-long outage.

The server holds the information that would make a precise gap-fill possible and throws it
away. Commit `095a3887` added the transport primitive — `SendWithID`, `SendJSONWithID` and
`LastEventID` on the `Stream` interface — but nothing produces IDs and nothing reads the
resume position. The capability exists and has no callers.

### What the original finding got right, and what it got wrong

The finding that motivated `095a3887` claimed the missing event IDs are *why*
`StreamBinder.recover` must refetch. The first half is right and the second half names the
wrong transport.

Two independent facts, both established by reading the generated clients:

- **The SSE client is already built and waiting.** `internal/client/generators/typescript/sse.go`
  tracks `lastEventId` and sends it on reconnect as both a `Last-Event-ID` header and a
  `lastEventId` query parameter (`sse.go:307-356`). It has been sending a resume position all
  along to a server that ignored it. The SSE half of this work needs no client changes.

- **`StreamBinder.recover` is not on SSE.** Channels and rooms generate a *WebSocket* client
  (`channels.go`, `rooms.go`). WebSocket has no `Last-Event-ID`; that header is part of the
  SSE spec and has no analogue in the WebSocket protocol. Server-side event IDs are necessary
  for the live-query fix but not sufficient, because the live-query transport cannot carry the
  resume position without an application-level handshake.

So there are two paths needing two mechanisms, and only one of them is cheap today:

| Path | Transport | Resume mechanism | Client work |
| --- | --- | --- | --- |
| `EventStream` routes | SSE | `Last-Event-ID` (native) | already built |
| Live queries, channels, rooms | WebSocket | app-level resume frame | new |

**This spec covers the SSE path only.** WebSocket resume is deferred to a follow-up spec.
The consequence must be stated plainly: *the live-query refetch this work was originally
motivated by is not fixed by this spec.* What this spec delivers is the event log, the wire
contract, and the client-side recovery machinery — all three of which the WebSocket phase
reuses unchanged, and none of which can be designed well without a working transport to
prove them against.

## Goals

- A replayable event log with pluggable storage and a bounded in-memory default
- A resume that is *honest*: the server never implies it filled a gap it could not fill
- Fail-safe degradation — any failure, at any layer, reduces to today's full-resync behavior
- No behavior change for streams that do not opt in
- A wire contract the WebSocket phase can adopt without renegotiation

## Non-goals

- WebSocket resume (follow-up spec; the live-query path stays on full resync)
- Cross-instance replay in the default implementation (see Multi-instance, below)
- Exactly-once delivery. Replay is at-least-once and deduplicated by ID on the client
- Retrofitting IDs onto existing `Send`/`SendJSON` callers, which stay ID-free

## Decisions

Four decisions were taken before design, and the rest of this document follows from them:

1. **Scope is end-to-end** — root module, `packages/client-core`, and `extensions/streaming`.
2. **Best-effort replay with an explicit gap signal.** The server replays what it holds; when
   it cannot honor a resume position it says so, and the client falls back to full resync.
3. **Pluggable `EventLog` with a bounded in-memory default**, so single-instance deployments
   work with no configuration and multi-instance deployments can substitute a shared log.
4. **SSE first; WebSocket as phase 2.**

## Architecture

### Layer 1 — the `EventLog` contract (root)

Defined in `internal/router`, re-exported from the root `forge` package alongside
`Connection` and `Stream`, following the existing alias pattern in `streaming.go`.

```go
// LoggedEvent is one recorded event, as it will be replayed.
type LoggedEvent struct {
    ID    string
    Event string
    Data  []byte
}

type EventLog interface {
    // Append records an event on a channel and returns the ID assigned to it.
    Append(ctx context.Context, channel, event string, data []byte) (string, error)

    // Since returns the events recorded after id, in order.
    //
    // resumable reports whether id was still resolvable in the log. False means
    // the gap cannot be filled and the caller must fall back to a full resync;
    // events is empty in that case and must not be treated as "nothing missed".
    Since(ctx context.Context, channel, id string) (events []LoggedEvent, resumable bool, err error)
}
```

The `resumable` boolean is the whole design in one return value. The alternative — returning
an empty slice for both "nothing was missed" and "I cannot tell you what was missed" — makes
the dangerous case indistinguishable from the safe one at the call site, and the dangerous
case is the one that silently serves stale data. A separate return value cannot make ignoring
it a compile error, since `_` is always available; what it does is make ignoring it *visible*
in the source, which is the most a signature can do here.

### ID scheme

`<epoch>-<seq>`, where `epoch` identifies the log generation and `seq` is a per-channel
monotonic counter rendered in decimal. Example: `7f3a9c1e-42`.

Both components are decimal/hex text, so an ID is newline-free by construction and passes
`validSSEFieldValue` without a special case.

The epoch exists because `seq` alone is unsafe across a restart. A fresh process restarts its
counters at zero, so a client resuming from `41` would be told that events `42…` are its
missed events when they are in fact entirely different events that happen to reuse the
numbers. Comparing epochs turns that silent mis-replay into an honest `resumable=false`.

`Since` resolves `resumable` as follows, and every unresolvable case is false rather than a
guess:

| Condition | `resumable` |
| --- | --- |
| `id` is malformed | false |
| `id` epoch ≠ current epoch | false |
| `seq` older than the oldest retained entry | false |
| `seq` newer than the newest entry | false |
| otherwise | true, with events at `seq > id.seq` |

The "newer than newest" row covers a client that reconnects to an instance behind the one it
was talking to. It cannot be served correctly, so it is not served at all.

### In-memory default

A per-channel ring buffer bounded by both entry count and age, evicting on whichever binds
first. Defaults: 1024 entries per channel, 5 minutes. Both configurable. A count bound alone
lets a quiet channel retain events long past their usefulness; an age bound alone lets a busy
channel consume unbounded memory. The pairing is what makes the footprint predictable.

Eviction is what produces `resumable=false` for the expired case, so retention is a tuning
knob on *how often clients fall back*, never on correctness.

### Multi-instance

The in-memory log is per-process, so each instance has a distinct epoch and a reconnect
landing on a different instance resolves to `resumable=false` and a full resync. That is the
correct outcome, arrived at honestly, and it is exactly today's behavior — a multi-instance
deployment that does not configure a shared log is no worse off than before. A Redis or
NATS-backed `EventLog` sharing one epoch across instances is the supported upgrade and needs
no transport changes.

### Layer 2 — SSE replay wiring (root)

Opt-in per route, via a route option carrying both the log and the channel the route's
events belong to:

```go
router.EventStream("/orders/live", handler, forge.WithEventLog(log, channelFor))
```

`channelFor` derives the log channel from the request (`func(Context) string`), so one route
serving per-tenant or per-resource streams keeps a separate log partition per client rather
than replaying one client's events to another. A route whose stream is global returns a
constant. Handlers on a logged route append through the stream — `SendWithID` is not called
directly by application code; the wiring assigns IDs so that the log and the wire cannot
disagree about them.

A stream with no log configured behaves exactly as it does today.

On connect, before the handler runs:

1. Read `stream.LastEventID()`. Empty means a fresh client — no replay, no control event.
2. Call `log.Since(ctx, channel, id)`.
3. `resumable` with at least one event → replay each via `SendWithID`, then emit
   `forge.resumed`.
4. `!resumable` → emit `forge.gap` immediately, with no replay.
5. `resumable` with zero events → `forge.resumed` only on a log registered with
   `WithProducerEventLog`; otherwise `forge.gap`. A log written by connections alone records
   nothing while nobody is connected, so an empty result there cannot be told apart from
   nothing having been recorded, and the client that was the log's only writer is by
   construction at the head when it returns. Only a producer-written log can say "you missed
   nothing" and mean it.

Replay reads the log up to its current head and then subscribes from that head. Events
arriving during replay may therefore be delivered twice. That is deliberate: at-least-once
plus client-side dedup by ID is far simpler to make correct than an exactly-once handoff, and
`extensions/streaming` already has the dedup (`dedup.go`, sharded, keyed on message ID) that
makes duplicates harmless.

### Wire contract

Two reserved event names in a `forge.` namespace. Applications must not emit them; the
replay wiring is their only producer.

```
event: forge.resumed
data: {"from":"7f3a9c1e-41","count":12}

event: forge.gap
data: {"reason":"unresumable"}
```

`forge.resumed` is sent **after** the replayed batch, making it an end-of-replay marker: a
client that receives it knows both that the gap was filled and that the fill is complete.
`forge.gap` is sent immediately, with no replay.

`reason` carries the single value `"unresumable"`. An earlier draft of this spec enumerated
`expired`, `epoch`, `malformed`, and `unknown`; the implementation deliberately does not,
because `EventLog.Since` reports resumability as a bool and the wiring therefore never
establishes which of the four applies. Naming one would be a guess presented as a diagnosis,
and a diagnosis is the one thing a log line is read as. The client treated all four
identically in any case, so the distinction bought nothing on the wire. A log that genuinely
knows the cause is free to widen the interface later; until it does, the honest answer is the
one value.

### Layer 3 — broker integration (`extensions/streaming`)

- Publishing appends to the log and broadcasts with the returned ID.
- SSE-backed connections send via `SendWithID`.
- `SessionSnapshot` (`session_store.go:10`) gains `LastEventIDs map[string]string`, a
  per-channel resume position. The snapshot already records `Channels` and `DisconnectedAt`
  but no position, which is precisely the field that makes a resumption able to resume.

This module is mid-refactor and does not currently compile (`MessageTypeError` and
`MessageTypeSystem` undefined in `extension.go`), so this layer is sequenced last and gated
on it building. Nothing in Layers 1, 2 or 4 depends on it.

### Layer 4 — conditional recovery (`packages/client-core`)

`StreamBinder.recover` becomes conditional. The difficulty is one of ordering: `onReconnect`
fires when the socket opens, which is *before* any control event can have arrived, so
recovery cannot simply be skipped on a flag that does not exist yet.

Recovery is therefore deferred rather than cancelled, resolving on the first of:

| Signal | Outcome |
| --- | --- |
| `forge.resumed` arrives | recovery cancelled — the gap was filled |
| `forge.gap` arrives | recovery runs immediately |
| `resumeGrace` elapses (default 1000ms) | recovery runs |
| control event malformed | recovery runs |

Only the first row is a behavior change. Every other path runs exactly today's
invalidate-and-refetch, so a server that does not implement replay, a dropped control event,
a malformed payload, and a transport that cannot carry the resume position all converge on
current behavior. The worst case is a refetch delayed by `resumeGrace`, never a refetch that
should have happened and did not.

`resumeGrace` is configurable on `SubscriptionManagerOptions`. Setting it to 0 disables
deferral and restores today's unconditional behavior exactly, which is the escape hatch if
the deferral proves problematic in practice.

## Testing

**`EventLog` contract tests**, written against the interface and run against the in-memory
implementation, so a future Redis-backed log inherits them: append-then-since round trip,
eviction by count, eviction by age, epoch mismatch, malformed ID, seq-ahead-of-head, and an
empty-but-resumable result distinguished from an unresumable one. That last pair is the
distinction the whole design rests on and deserves an explicit test.

**SSE wiring tests**: fresh connect emits no control event; resumable connect replays in
order then emits `forge.resumed` with a matching count; unresumable connect emits
`forge.gap` and no replayed events; a stream with no log configured is byte-identical to
today's output. That last one is the regression guard for the opt-in claim.

**Client tests** (`live.ts`): recovery cancelled on `forge.resumed`; recovery runs on
`forge.gap`; recovery runs on grace-window expiry with no control event; recovery runs on a
malformed control payload; `resumeGrace: 0` reproduces current behavior. The existing tests
use a manual clock (`manualClock().sleep`), so the grace window is testable without real
timers.

**Cross-module**: an end-to-end replay through the broker, gated on `extensions/streaming`
compiling.

## Risks

**`extensions/streaming` is a moving target.** It is red and actively being refactored by a
parallel workstream. Layer 3 is sequenced last and every other layer is independent of it, so
the work does not stall — but Layer 3's estimate is unreliable until that module builds, and
the `SessionSnapshot` change needs coordination with whoever is editing `session_store.go`.

**The headline problem stays open.** Live queries ride WebSocket and keep refetching on
reconnect until phase 2. Anyone reading commit `095a3887` or this spec's title could
reasonably assume otherwise, which is why it is stated in the Problem section, in the
Non-goals, and here.

**`resumeGrace` adds latency to genuine gap recovery.** A server that never sends control
events makes every reconnect refetch 1000ms later than it does today. Mitigated by the
`forge.gap` fast path for servers that do implement the contract, and by `resumeGrace: 0`
for those that do not.

## Sequencing

1. `EventLog` interface, in-memory implementation, contract tests — no dependencies
2. SSE replay wiring and the `forge.resumed` / `forge.gap` contract — depends on 1
3. `client-core` conditional recovery — depends on the wire contract in 2, not its code
4. Broker integration in `extensions/streaming` — depends on 1, 2, and that module compiling

Steps 1–3 are independently shippable and leave the system in a working state at each point.
