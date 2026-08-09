# Two SSE replay mechanisms now exist

**Date:** 2026-08-08
**Branch:** `fix/streaming-frame-decoder`

## The two mechanisms

**The router's event log** (`internal/router/eventlog*.go`,
`internal/router/streaming_sse_replay.go`). Positions are scalar,
`<epoch>-<seq>`. Storage is a bounded in-memory ring per channel, per process.
Entries are written either by the connections on the route (`WithEventLog`) or by
the application's own producer (`WithProducerEventLog`).

**The streaming extension's cursor replay** (`extensions/streaming/replay.go`).
A position is a vector — room ID to last delivered sequence — carried on the wire
as a base64url-encoded JSON token. Backlog comes from the `MessageStore`, and the
cursor is written by the producer, on every sequenced room message.

## Only one can own the `id:` field

An SSE event has one `id:`. When both mechanisms are active on a route,
`ErrEventIDAssignedByLog` settles it in the router's favour: `loggedStream`
refuses `SendWithID` / `SendJSONWithID`, the extension falls back to sending
without an ID, and that send routes through the router's `Send`, which emits the
router's scalar position instead.

Messages keep flowing. What does not reach the wire is the cursor.

As of this branch the refusal logs a warning once per stream, so the condition is
visible rather than silent. `WithEventLog`'s doc comment states the constraint.

## Why the vector matters

One SSE stream can carry many rooms, each advancing at its own rate. A scalar
position cannot encode where the client is in each of them, so a resume on a
multi-room stream cannot reconstruct the per-room set the client actually missed —
it replays the wrong set rather than failing.

## What the router provides that the extension does not

The router's replay has a wire contract the extension has no equivalent of:
`forge.resumed` (position resumed from, count delivered) and `forge.gap` (the gap
could not be filled), plus the client-side deferred recovery in
`packages/client-core` that waits for one of the two. That pair is how a client
learns whether its gap was actually filled, rather than assuming it was.

## Open question

Whether the extension's cursor should ride the router's id primitive and adopt the
`forge.resumed` / `forge.gap` contract — or whether the two should stay separate
and routes be required to pick one — is a question for whoever owns
`extensions/streaming`. Nothing here decides it.
