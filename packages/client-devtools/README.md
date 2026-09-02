# @forge-go/client-devtools

Why the cache did what it did.

A normalized cache with a tag graph has a characteristic failure mode, and it is
not "it doesn't work". It is *it did something surprising and there is no lever
to find out why* — the tarpit Relay and Apollo are known for. The surprise is
rarely a crash. A query refetched that should not have, or — far worse, because
it is silent — one did not refetch that should have, and the screen is quietly
wrong for as long as the session lasts.

The whole justification for *declaring* tags rather than guessing dependencies
is that the graph becomes inspectable. This package is where that is cashed in.

```ts
if (process.env.NODE_ENV !== 'production') {
  void import('@forge-go/client-devtools').then((devtools) => {
    globalThis.forge = devtools.attach(client);
  });
}
```

Written that way, a production build contains neither this package nor the
branch that would have loaded it. See **Zero production cost** below.

## The questions it answers

### Why did this query NOT refetch?

The one that matters. A missed invalidation produces no event, no error and no
request.

```ts
forge.whyNotRefetched('GET /orders');
```

```
outcome:     missed
reason:      `GET /orders` did not refetch because none of the 1 tag(s)
             mutation POST /orders raised are tags it carries. The two sets are
             disjoint.
invalidated: Order:9
carried:     Customer:c1, Order:1, Order:2, Order[]
matched:     (none)
nearest:     Order:9 vs Order[]  (instance-vs-collection)
suggestion:  the mutation invalidated the instance `Order:9` but this query
             provides the collection `Order[]`, and the two never intersect. A
             query only carries `Order:9` once a response has actually put that
             entity in its result — which a create never has. Add `Order[]` to
             the operation's Invalidates.
```

Five outcomes, because they are five different bugs and conflating any two of
them costs an afternoon:

| outcome | what it means |
|---|---|
| `missed` | the tag sets are disjoint. A declaration is wrong; read `nearest`. |
| `stale-while-unmounted` | they intersected, but nothing has the query mounted, so no request was made. **Correct behaviour**, and the most common false report of a broken cache. It refetches on the next mount. |
| `placed` | a placement callback answered. If the screen is wrong, the callback is wrong. |
| `refetched` | it did refetch. The problem is downstream of the cache. |
| `not-tracked` | this key is not one the cache holds. A key includes its arguments. |

`nearest` names how two tags that ought to have met failed to:
`instance-vs-collection`, `collection-vs-instance`, `different-instance`,
`case`, `scoped` — each with the declaration to change.

A sixth cause is reported separately because it is invisible by construction: a
tag template that **resolved to nothing and was skipped**. `Order:{res.id}`
against a response with no `id` invalidates nothing and says nothing.
`cause.unresolved` is where that shows up.

### Why did this query refetch?

```ts
forge.whyRefetched('GET /orders');
// `GET /orders` refetched because mutation PATCH /orders/{id} invalidated
// Order:1, Order[], of which it carries Order:1, Order[].
```

`reason` is `invalidation`, `mount` or `manual`; the cause is the mutation or
the stream frame batch responsible, recovered from the log.

Not sure which question you have? `forge.explain(key)` picks.

### What is in the cache for `Order:7`, and at what version?

```ts
forge.entity('Order:7');
// { key, type, id, version, frameAt, fields, refs, dependents }
```

`version` bumps only when the data actually moved, so a refetch that changed
nothing leaves it alone. `frameAt` is non-zero when a stream frame wrote it.
`refs` are the entities this record points at; references appear in `fields` as
`{__ref: 'Customer:c1'}`, which is what the cache genuinely holds.

### Which queries depend on this entity?

`forge.dependents('Customer:c1')`, or the `dependents` field of `entity`. This
reaches through nested references: a list of orders rendering
`order.customer.name` depends on `Customer:c1` without ever declaring it.

### Which sockets are open, for which channels, with what ref count?

```ts
forge.sockets();
// [{ endpoint: '/ws/orders', connected: true, refs: 2, opens: 1,
//    reconnecting: false, channels: [{ channel: '/ws/orders', handlers: 2 }] }]
```

Ten components on the same live query are one socket. `opens > 1` means it
dropped and came back — and a reconnect means frames were missed.

### And, without running anything

```ts
forge.wouldInvalidate(ops.orderCreate, { body: {} }, { id: 9 });
// { tags: ['Order:9'], unresolved: [], missed: ['Order:9'], hits: [...] }
```

`missed` is tags no mounted query carries. Asking what a mutation would do must
not be answered by doing it, so this resolves the templates through the same
`resolveTags` the invalidator uses and reads the tag index — no request, no
write.

## Inspection does not mutate

Not one function on the *read* side calls `getState`, `fetch`, `read` or
`denormalize`, and this is less obvious than it sounds. `cache.getState(meta, args)` — the natural
way to read a query — calls `open`, which **moves the record to the back of the
LRU order** and creates one if it is missing, then rehydrates the skeleton,
building store memos, linking them into the reverse-dependency index and writing
the result onto the registry entry's `value`. An inspector built on it would
change which query is evicted next, change what a placement callback is handed
as `current`, and populate memos for queries nobody rendered — all only while
somebody has the panel open, which is the worst possible failure mode for a
debugging tool.

The whole read surface is map lookups and counters, and every snapshot is a
copy: a panel that writes to `snapshot.fields` cannot move the store. The test
suite asserts record count, record versions, the private LRU order and the full
registry state (including `value` **by identity**) are unchanged after a pass
over every read the API offers — and includes the probe showing those assertions
fail when `getState` is used instead.

There is a mutating half, and it is one file. `forge.actions` refetches,
invalidates, evicts, drops and clears, which is what a panel with buttons
needs. It lives in `actions.ts` and nowhere else, so the paragraph above stays
literally true of `inspect.ts` rather than approximately true of the package,
and so the entire set of calls that can move this cache fits on one screen.
Every one of them records what it did, so an action you took is in the same
log, in the same order, as the events it caused.

## The log is bounded

A fixed-size ring, 500 entries by default, allocated once and never grown. When
it fills, the oldest entry is overwritten and `dropped` counts how many are
gone, so a timeline that begins mid-story says so.

What is *in* an entry is bounded too, which is the half that is easy to miss.
Nothing is retained that could keep a page of data alive: no response body, no
error object, no rehydrated value. Tags are resolved and copied when the event
arrives, arguments are reduced to a truncated cache key, and an error becomes a
short string. The per-query bookkeeping is pruned against the cache's own LRU
cap rather than growing one entry per query key a search box ever produced.

## Frame capture, which is opt-in

Every other entry here is bounded by construction: tags are copied strings,
arguments are a truncated cache key, an error is a message. A stream frame's
payload is none of those, so capturing one is the single place the retention
rule is broken, and you have to ask for it.

    attach(client, { frames: { limit: 200 } })

Off by default; `frames: {}` turns it on at 200. Presence is the switch, not
the number. Turned on, `forge.frames()` gives you the last N decoded frames
with their channel, message, intent and body, which is what you want when a
frame arrives and the screen does not change. Payloads are copied at capture
and capped in depth and width, so nothing the ring holds can keep a store
record alive. It still costs memory, bounded by the limit you set.

## Across an identity change

`setPrincipal` drops the whole cache, so nothing recorded before it describes
the store that exists after. The inspector therefore does not pretend to span
the boundary: it records a `principal` entry, increments a session counter, and
stamps every entry with its session. Queries that re-mount and re-fetch under
the new identity are logged as **mounts**, not as refetches of a query whose
data no longer exists. The cache snapshot only ever shows the current
principal's data, because that is the only data there is.

## Zero production cost

The binding constraint, and it is checked against the **built output** rather
than argued from the source.

The core's seam is one nullable field on `QueryCache` and five optional calls.
An optional call does not evaluate its arguments when the callee is nullish, so
with no inspector attached there is not even an allocation — one property load
and one nullish check. **The core keeps no history**: every event is handed over
or forgotten, and the ring buffer, the causal attribution and the analysis all
live here.

`__tests__/zero-cost.test.ts` compiles the package with `tsc`, bundles three
fixture applications with esbuild exactly as a real build would, and reads the
bytes:

| fixture | gzipped |
|---|---|
| uses the runtime, never imports the devtools | 9325 B |
| the guarded dynamic import above, built for production | 9313 B |
| imports the devtools statically | 14573 B |
| imports the devtools and the panel statically | 18638 B |

The production bundle contains none of this package's marker strings; the
instrumented one contains all of them, which is what makes the absence
meaningful. The guarded bundle contains no `import(` at all — the branch folded
and the chunk was never emitted.

One thing that experiment established the hard way: write the guard as a **bare**
`process.env.NODE_ENV !== 'production'`. Spelt defensively as
`typeof process === 'undefined' || process?.env?.NODE_ENV !== 'production'`, the
second half still folds but the first cannot, the disjunction survives, and the
whole package ships.

## Budget

| entry | gzipped | budget |
|---|---|---|
| inspection API | 5675 B | 6 kB |
| overlay | 2788 B | 3.5 kB |
| panel | 4594 B | 12 kB |
| inspection API and overlay together | 8495 B | 9 kB |

The design gives this package no budget, because it has no end user to protect:
it never reaches a production bundle. The numbers above exist to catch
*accidental* bloat — a charting library pulled in for a table — and are set so
that the inspector cannot quietly grow past the runtime it inspects.

`inspection API` was 5.5 kB and is now 6 kB. What grew it is the feature: an
action layer, an opt-in frame ring, a query detail read and a bulk record
read, 730 B between them. A budget raised because a dev-only package gained
capability is a budget doing its job. One raised because a *production*
package gained bytes would not be, and the two core budgets in the table below
were not touched.

`inspection API and overlay together` moved from 8.5 kB to 9 kB in the same
change and for the same reason. It is the two files above summed, so it grew
by what they grew by, and it landed 5 B under its old line. Five bytes is not
headroom, it is a tripwire: the next doc comment added to `index.js` sets it
off, and a budget that fails for a reason nobody can act on teaches people to
raise limits without reading them, which is the one thing a budget must never
teach.

## The overlay

```ts
import { mountOverlay } from '@forge-go/client-devtools/overlay';

const unmount = mountOverlay(forge);
```

A DOM panel in a shadow root: `document.createElement` and nothing else.
Deliberately **not a component** — a React panel forces React on a Vue
application, and a Vue one forces Vue on an Angular application. It is also
deliberately thinner than the API beneath it. A superb inspection API with a
plain panel beats the reverse: the API is what you call from the console at
three in the morning, what a test asserts on, and what somebody else's panel is
built from.

If you want the detail pane, the actions and the stream views, import
`/panel` instead. They are two entry points, not a base and an extension.

## The panel

```ts
import { mountPanel } from '@forge-go/client-devtools/panel';

const unmount = mountPanel(forge, { open: true });
```

The other entry point, and you pick one. `/overlay` is six read-only tables
and a filter box; `/panel` is that plus everything the overlay deliberately
does not have:

- **A detail pane.** Click a query and get its status, its mounts, its
  provides, tags and deps, and its last settled response as a `<details>`
  tree. Click an entity and get its version, the frame clock reading that last
  wrote it, its fields, its references and the queries that reach it, each with
  whether anything currently has it mounted.
- **An action bar**, which is the only part of either UI that writes.
  `refetch`, `invalidate` and `drop` hang off the selected query, `evict` off
  the selected entity, and `clear cache` sits in the global bar. Every one of
  them goes through `devtools.actions`, so a console session and a button
  press are the same call and land in the same log.
- **A streams tab**, which is `binderSnapshot` rendered: the bindings, the live
  queries and their ref counts, the queue depth, and the `recovering` badge
  naming the endpoints inside a post-reconnect gap window. That badge is the
  one thing here you cannot see any other way, because a client that silently
  missed frames looks exactly like one that did not.
- **A frames tab**, empty until you turn capture on, and honest about it: it
  tells you the call to make rather than showing nothing.

Neither entry point imports the other, so they have separate budgets and
neither can bloat the other. Same construction as the overlay in every other
respect: `document.createElement` in a shadow root, no framework, nothing an
Angular or a Vue application has to take on.

## What was added to the core

Seven things, all optional, all measured. The deltas are against `core, REST
only` (9122 B gzipped) and were taken by removing each one from the built
output and re-measuring:

| change | cost |
|---|---|
| `QueryCache.observer` + four emit sites | +64 B gzipped |
| `applyFrames` emit site, carrying the frame batch | +20 B gzipped (streams only) |
| `QueryRegistry.all()` | +8 B gzipped |
| `QueryCache.tracked()` | +11 B gzipped |
| `QueryCache.drop(key)` | +66 B gzipped |
| `socketSnapshot(manager)` | 0 B unless imported (+135 B when it is) |
| `binderSnapshot(binder)` | 0 B unless imported (+126 B when it is) |

The last two are free functions rather than methods precisely so they
tree-shake. The five above them cannot: a class method lands in every import
set that pulls the class in, which is why they are counted rather than
waved at.

Core totals now: **9122 B** gzipped REST-only against a 9.2 kB budget, and
**11856 B** with streams against 14.25 kB. The whole seam is 149 B of the
first figure and 164 B of the second.
