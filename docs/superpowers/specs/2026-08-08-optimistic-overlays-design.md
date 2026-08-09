# Optimistic overlays

Status: approved, not implemented
Package: `@forge-go/client-core`, the three framework adapters, `internal/client/generators/typescript`

## The problem this solves twice

A mutation today is request, settle, invalidate. Nothing appears until the
server answers.

The common fix is worse than the gap. Every mutation hand-writes three
functions — a cache update, a rollback, and a settle handler — and each is a
place to be subtly wrong. The rollback is the worst of the three, because the
inverse it records was computed against a base that already included whatever
*other* optimistic mutation was pending at the time. When that earlier mutation
fails first, the inverse restores a state that never existed.

So there are two requirements, and the second is the one that shapes the
design:

1. A caller **declares what changes** and writes no rollback logic at all.
2. Concurrent optimistic mutations against one entity **rebase**: when the
   *first* fails, the second's effect survives, correctly, on the reverted
   base.

## The model: two planes, one fold, no undo

**An overlay is never applied to the base store.** What a subscriber sees is
recomputed on demand:

```
effectiveRecord(key)  = fold(baseRecord(key),  [patches for key,      in push order])
projectedValue(query) = fold(baseValue(query), [placements matching q, in push order])
```

Rollback is `stack.take(id)` followed by a refold of the keys that overlay
touched. There is no inverse operation anywhere in the design, so there is
nothing to record and nothing to get wrong.

**Rebase falls out of the fold.** Dropping `o1` and refolding re-applies `o2`'s
patch to the freshly-reverted base. `o2` survives because it was stored as a
*patch*, not as a precomputed value.

That is the one rule a caller has to respect, and it is why a computed patch is
a function:

```ts
optimistic: (o) => ({ likes: o.likes + 1 })   // re-run on every refold — composes
optimistic: { likes: order.likes + 1 }        // captured at call time — does not
```

Two planes, because **membership is not entity state**. Plane one is
`EntityKey → EntityPatch`. Plane two is the mutation's existing `Placement`
callbacks, re-run against the projected `current` at fold time.

### Patch kinds

```ts
type MergeSource =
  | Record<string, unknown>
  | ((prev: Record<string, unknown>) => Record<string, unknown>);

type EntityPatch =
  | { readonly kind: 'merge';  readonly source: MergeSource }
  | { readonly kind: 'create'; readonly fields: Record<string, unknown> }
  | { readonly kind: 'delete' };
```

### What the option accepts

```ts
/** One explicitly targeted patch. The escape hatch for multi-entity writes. */
export interface OptimisticPatch<E = unknown> {
  readonly key: EntityKey;
  readonly patch: Partial<E> | ((prev: E) => Partial<E>) | 'delete';
}

/** `MutateOptions.optimistic`. */
export type OptimisticSpec<E = unknown> =
  | Partial<E>                    // merge into the derived target
  | ((prev: E) => Partial<E>)     // computed merge, re-run on every refold
  | 'delete'                      // delete the derived target
  | readonly OptimisticPatch<E>[]; // explicit keys, no derivation
```

`merge` over a base record that **does not exist is a no-op**, not a
resurrection. That single rule is what lets an evicting stream frame beat a
pending local edit without a special case.

## What the caller writes

```ts
// update — the change itself, and nothing else
update.mutate({ path: { id: 7 }, body: { status: 'shipped' } },
              { optimistic: { status: 'shipped' } });

// delete — list membership included; no placement, no refetch to wait for
remove.mutate({ path: { id: 7 } }, { optimistic: 'delete' });

// create — the patch is the row; `place` says where it goes, as it already does
create.mutate({ body: { total: 99 } }, {
  optimistic: { total: 99, status: 'open' },
  place: { 'Order[]': (order, current) => [order, ...current] },
});

// escape hatch — several entities, explicit keys
mutate(args, { optimistic: [
  { key: 'Order:7',    patch: { status: 'shipped' } },
  { key: 'Customer:3', patch: (c) => ({ openOrders: c.openOrders - 1 }) },
]});
```

No rollback function, no cache-update function, no settle handler, in any of
them.

### The target is derived, not declared

From `meta.invalidates`, take the templates that are **entity-key shaped**
(contain `:`, do not end `[]`) and resolve each through `resolveTag` against the
call's args.

| matches | meaning | behaviour |
|---|---|---|
| exactly one | update or delete | that key is the target |
| zero | create | mint a temp key from `meta.entity` |
| more than one | ambiguous | no overlay is pushed; reported through `onError` |

Ambiguity is **reported and skipped, never thrown**. Throwing would reject the
mutation before it was dispatched, and `mutate` swallows rejections by design —
so the write would silently not happen, which is a far worse failure than not
being optimistic. Skipping means the mutation runs exactly as it does today and
the developer gets a message naming the explicit `key` form. This is the same
decision `Invalidator` makes for a tag template that resolves to nothing, and
for the same reason.

`PATCH /orders/{id}` and `DELETE /orders/{id}` carry `Order:{id}` from derived
same-entity invalidation, so both land on the one-match branch by construction.
`POST /orders` carries only `Order[]`, so it lands on the create branch. This is
the manifest already knowing the answer; nothing new is generated for it.

Temp keys are `Order:~opt1`, minted from a counter on the stack. The `~opt`
prefix is a module constant.

### Optimistic delete needs no placement

Dropping the record from the effective view makes every settled skeleton
rehydrate the now-dangling reference as a **hole** — `store.ts:416` drops a
dangling reference from an array rather than pushing `undefined`, because the
stream eviction path already needed exactly that. The row leaves every list on
screen with no callback and no refetch, and comes back on failure by the same
mechanism running backwards.

## Where the code lives

### `src/overlay.ts` — new, ~350 lines

Owns the stack, the patch kinds, target derivation, both folds, and the
projection memo. It is a self-contained algebra with one entry point and no
query-lifecycle logic, which is the justification for not putting it in
`cache.ts` — already the largest file in the runtime at 1094 lines.

```ts
export const OPTIMISTIC: unique symbol;

export interface OverlayEntry {
  readonly id: number;
  readonly patches: ReadonlyMap<EntityKey, EntityPatch>;
  readonly place: Readonly<Record<string, Placement>> | undefined;
  /** This mutation's `invalidates`, resolved against its args at push time. */
  readonly tags: readonly string[];
  /** The minted key, for a create. Read to supply `created` to placements. */
  readonly created: EntityKey | undefined;
}

export class OverlayStack {
  get version(): number;          // bumps on every push, take and refold
  get empty(): boolean;           // the early-out every hot path checks first
  keys(): ReadonlySet<EntityKey>; // every key any live overlay touches

  push(spec: OptimisticSpec, meta: OperationMeta, args: TagContext): number;
  take(id: number): OverlayEntry | undefined;
  clear(): void;

  /** base + patches, or undefined when the fold deletes the record. */
  effective(key: EntityKey): EntityRecord | undefined;
  /** Recompute the fold for these keys after a base write. */
  rebase(keys: Iterable<EntityKey>): void;

  /** base value + matching placements, memoized by (version, base identity). */
  project(key: string, base: unknown, entry: QueryEntry | undefined): unknown;
  /** Whether any live overlay reaches this query. Drives `isOptimistic`. */
  affects(entry: QueryEntry | undefined): boolean;
}
```

### `src/store.ts` — +~15 lines

An optional `overlays` field typed as a **structural** `OverlayLayer` interface
declared in `store.ts`, exactly as `cache.ts` declares `LiveBinding` rather than
importing `StreamBinder`, and for the same reason.

- `materializeKey` resolves through `this.overlays?.effective(key) ?? this.records.get(key)`.
- `materializeKey` stamps `out[OPTIMISTIC] = true` when the key is overlaid.
- `put` and `evict` call `this.overlays?.rebase([key])` after writing base, so
  the fold is recomputed against the new base.

Nothing else in the store changes. `frameStamp`, `racedSince`, `graves` and the
frame clock are all about **base** writes and overlays are not writes.

### `src/cache.ts` — +~50 lines

- `readonly overlays = new OverlayStack()`.
- `MutateOptions.optimistic`.
- `mutate` pushes before dispatch and promotes/drops on settle (below).
- `read` is split into `base(record)` and `value(record)`.
- `snapshot` gains `isOptimistic`.

**`entry.value` stays the base value, deliberately.** Projection happens on the
way out to subscribers, not into the registry. If `entry.value` were projected,
a *real* placement callback at settle would receive a `current` containing
another mutation's temp entity, and `adopt` normalizes whatever a callback
returns straight into the base store — permanently writing `Order:~opt2` into
base. So:

```ts
private base(record):  reads the skeleton, refreshes entry.value   // internal, placement
private value(record): overlays.project(key, this.base(record), entry)  // subscribers
```

`snapshot`, `fetch`, `refetch` and `drop` return `value`. `mutate`'s `created`
and the registry's `value` are `base`.

### Bundle

`mutate` references the stack statically, so `overlay.ts` lands in the REST-only
budget whether or not an application uses it. That is the right trade: unlike
streams, optimism is a mutation *option*, not a subsystem, and making it
installable would be DX tax paid for bytes.

Budgeted ~1.2 kB gzipped against 2.6 kB of current headroom (6.39 kB actual
against a 9 kB limit), with its own `size-limit` entry so the cost is visible
rather than absorbed into an existing line.

## The ordering rules

1. **Overlays never write base**, therefore a rollback and a refetch commute.
   The rollback/refetch race stops needing an ordering decision instead of
   getting one.
2. **An overlay outranks base regardless of what wrote it**, until discarded. A
   `patch` frame on `Order:7` lands underneath a pending local edit and the user
   keeps seeing their edit. Overlays take no part in the frame clock.
3. **An evicting frame wins.** `merge` over a missing base is a no-op, so the
   row disappears and nothing resurrects it.
4. **Success promotes, then commits**, in one synchronous block:
   1. promote `merge` patches (`put` the source **evaluated against base
      alone**) and `delete` patches (`evict`); **`create` patches are not
      promoted** — the real entity arrives in the response and a temp record
      must never enter base
   2. commit the staged response, with promoted delete targets added to the
      existing `skip` set, so a body-returning `DELETE` cannot resurrect
   3. `refresh(true)` — makes `entry.value` current for placement
   4. observer, then `invalidator.settled(...)` — the real row is placed here
   5. drop the overlay, then `refresh(true)` again

   Promotion is load-bearing. Without it a `204 No Content` delete flashes the
   row back between settle and refetch, which is the most common bug in shipped
   implementations of this feature. Server truth still wins, because the
   response commits *after* promotion. Dropping the overlay **last** means there
   is never an instant with neither the temp row nor the real one.

   Evaluating a `merge` against base alone is what makes concurrent computed
   patches correct. Two pending `likes + 1`; the second settles first; promoting
   `compute(base)` writes 1, refolding the first over it displays 2. When the
   first settles it promotes `compute(1)` and writes 2. If it fails instead,
   base stays 1 and 1 is displayed. Where the server computed against a
   different order, the invalidation refetch reconciles; that is unknowable
   client-side.
5. **Failure drops, and owes nothing.** Base was never touched, so no
   invalidation is raised and no refetch is scheduled. The drop happens before
   the rethrow, so `mutateAsync`'s rejection and `mutate`'s deliberate swallow
   both observe a clean cache. The `mutate` never rejects contract is untouched
   and no new rejection surface is introduced.
6. **`setPrincipal` and `clear` drop every overlay.** A pending edit belongs to
   the identity that made it.
7. **Refolds are total, never incremental.** Any change to the stack or to the
   base recomputes from base, for the affected keys only.

## Projection, precisely

`project(key, base, entry)`:

1. `stack.empty` → return `base`. This is the universal case.
2. `entry === undefined` or `base` is **not an array** → return `base`, and
   report once through `onError` if any live overlay's tags match. `Placement`
   returns `unknown[]`, so an enveloped query has no shape for it to produce;
   `adopt` has the same limitation today and it is documented rather than
   widened here.
3. Otherwise, for each live overlay in push order whose `tags` intersect
   `entry.tags`, run every matching callback with `created` = the temp entity
   read through `store.read(makeRef(overlay.created))`, `current` = the value
   accumulated so far, `args` = `entry.args`. All-or-nothing per overlay,
   matching `Invalidator.place`: an overlay whose matched tags are not all
   covered by callbacks contributes nothing. A throw is reported and treated as
   `undefined`.
4. Memoize on `(record.key) → {version, base, projected}` and return the cached
   `projected` when both the stack version and the base identity are unchanged.
   That memo is what preserves referential stability for `useSyncExternalStore`.

`makeRef` rather than a `{__ref}` literal: references are identified by object
identity, not by the property.

## Adapters

`QueryState.isOptimistic`, computed once in `QueryCache.snapshot` as
`overlays.affects(entry)` — `entry.deps ∩ stack.keys() ≠ ∅`, or any live
overlay's tags intersecting `entry.tags` — with an early-out to `false` on an
empty stack. Added to the state-identity comparison alongside the other four
fields.

Plus the exported `OPTIMISTIC` symbol, stamped on materialized overlaid records,
so one row in a list of fifty can dim itself. Symbol-keyed, so it is invisible
to `Object.keys`, `JSON.stringify`, spread, and the deep-equality in `equal()`.

Honest accounting on *one code path, not two*:

- **React** gets it free. `useQuery` returns the `QueryState` object straight
  through.
- **Vue** and **Angular** need **one line each**, because both enumerate fields
  into `computed()` (`useQuery.ts:237`, `injectQuery.ts:323`). One line, no
  logic, no per-framework reimplementation.
- `useMutation` and `injectMutation` need no new state. `isPending` already
  means "my overlay is live".

## Generator

`facades.go` emits type arguments on mutations:

```ts
import type { Order } from './types';

export const useOrderUpdate = mutation<Order, Order>(ops.orderUpdate);
```

- `MutationBinding<TResponse, TEntity = unknown>`; `mutation<TResponse, TEntity>`.
- `optimistic` is typed
  `Partial<TEntity> | ((prev: TEntity) => Partial<TEntity>) | 'delete' | OptimisticPatch[]`.
  A misspelled field is a compile error rather than a silent no-op.
- Type names come from `Endpoint.RootType` and `Endpoint.Entity.Type`, rendered
  through the same PascalCase renderer `types.ts` uses.
- The `import type` list is the sorted union of referenced names, for the
  determinism tests.
- An endpoint with no named response type keeps the bare `mutation(ops.x)`.
- **Queries are left untyped in this change.** A strict improvement, but
  unrelated bytes.

This is sound only because the field-naming gap is closed in code:
`opsmanifest.go:171` renames `idField` through `tsFieldName`,
`renameEntityFields` renames the edge keys, and `RestTransport` forwards both
codecs (`transport.ts:297`). Records hold TS-cased fields, so `Partial<Order>`
merges into a record with no translation layer. Both `README.md` and
`not-yet-shipped.mdx` still list that gap as open; correcting them is a separate
change and is noted, not folded in.

## Tests

New `packages/client-core/__tests__/overlay.test.ts`, plus additions to
`cache.test.ts`. Vitest, matching the existing style. Deterministic throughout —
`manualScheduler` and a `fakeTransport` holding explicit deferreds, no timers
and no sleeps.

| case | asserts |
|---|---|
| rebase | two overlays on `Order:7`; drop the **first**; the second's effect survives on the reverted base |
| computed patches compose | `+1`, `+1`, drop the first → `+1`; not `+2`, not `0` |
| frame during overlay (`patch`) | frame writes base, overlay still on top; drop → frame's value |
| frame during overlay (`evict`) | row disappears; the overlay does not resurrect it |
| 204 delete | the row never reappears between settle and refetch |
| body-returning delete | the echoed entity is skipped and does not resurrect |
| concurrent creates | placed in push order; dropping the first leaves the second correct |
| create settles | temp record never enters base; real row is placed with no gap |
| failure | base untouched, no tags raised, no refetch scheduled, `mutate` resolves `undefined` |
| `isOptimistic` | true for a reached query, false elsewhere, false on an empty stack |
| `OPTIMISTIC` marker | present on the overlaid record, absent from siblings, invisible to `Object.keys` |
| principal change | every overlay dropped |
| structural sharing | pushing an overlay on `Order:7` does not move `Order:8`'s subtree identity |
| target derivation | one match, zero matches, ambiguous-throws |
| non-array query | placement skipped and reported, base returned unchanged |

## Declined, on purpose

- **`optimistic: true`** (use the request body as the patch). The body conforms
  to a *request* schema, not the entity's, so it would write junk fields onto
  the record — and there is no runtime filter available, because the entities
  table lists only ids and edges. `optimistic: { status: 'shipped' }` is five
  characters more, typed, and exact.
- **Keep-on-failure / retry policy.** Reintroduces unbounded pending state.
- **Imperative `cache.optimistic()` outside a mutation.** No caller yet; the
  stack supports it later without redesign.
- **Widening `Placement` to enveloped queries.** A pre-existing limitation of
  `adopt`, not this change's business.
- **Tombstones on a promoted delete.** Tombstones are frame-stamped; a promoted
  delete leaves none, so an in-flight `GET` dispatched before the delete can
  resurrect. Today's non-optimistic delete has exactly the same hole, so this
  neither adds nor removes one.
- **Unrelated `cache.ts` decomposition.** Real, but out of scope.

## Documentation

- `packages/client-core/README.md`: replace the bare `- Optimistic overlays.`
  gap bullet with a section, and update the four-chunks opening paragraph and
  the size table.
- `docs/content/docs/web-client/not-yet-shipped.mdx`: remove the *Optimistic
  writes are not built* section and move it into the shipped list. The closing
  line — "The gap is two designed features" — needs a recount; capability gating
  shipped separately, so after this change it is one.
- Mirror with `pnpm docs:import <forge-repo-path> forge v1` from
  `/Users/rexraphael/Work/xraph/website`. That repo's `content/docs/forge/v1/**`
  is generated and must never be hand-edited.
