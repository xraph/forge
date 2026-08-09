# SSR `dehydrate` / `hydrate` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `dehydrate`/`hydrate` for the Forge web client runtime, so a server render emits real markup and a hydrating client starts warm instead of empty.

**Architecture:** A new `wire.ts` owns the JSON encoding — escaping data keys that collide with the `__ref` marker, detecting cycles, and reviving references through the existing `makeRef`/`markRewritten` primitives so `ref.ts`'s identity model is untouched. A new `ssr.ts` owns the policy — a reachability closure that emits only entities the exported queries reference, a required principal assertion on both sides, and two payload modes. `client-react` gains a hydration boundary that hydrates during render, and `getServerSnapshot` starts returning real state through a new non-opening `QueryCache.peek`.

**Tech Stack:** TypeScript (ES2020, `strict`), Vitest, fast-check, size-limit. React 18/19 for the adapter. No new runtime dependencies.

**Spec:** `docs/superpowers/specs/2026-08-08-ssr-dehydrate-hydrate-design.md`

## Global Constraints

- **Errors are plain `Error` with a `[forge] ` prefix.** This package has no custom error classes and gains none. See `src/client.ts:33`, `src/cache.ts:1093`.
- **`src/ref.ts` is not modified.** The revive pass uses the already-exported `makeRef` and `markRewritten`.
- **No new runtime dependencies** in `client-core` or `client-react`.
- **`client-core` ships no ambient Node types** (`tsconfig.json` sets `"types": []`). Nothing in `src/` may reference `process`, `Buffer`, or `require`.
- **Comments explain *why*, at the density of the surrounding files.** This codebase documents rejected alternatives inline; match it. Do not add comments that restate the code.
- **`client-react` resolves `@forge-go/client-core` from `file:../client-core`, i.e. from `dist/`.** Run `npm run build` in `client-core` before running `client-react` tests, every time core changes.
- **Commit after every task.** No `Co-Authored-By` trailers.
- Work from the package directory: `cd packages/client-core` or `cd packages/client-react`.

## File Structure

| File | Responsibility |
| --- | --- |
| `packages/client-core/src/wire.ts` | **New.** The JSON encoding alone: escape/unescape, cycle detection, reference revival. Knows nothing about `QueryCache`. |
| `packages/client-core/src/ssr.ts` | **New.** `dehydrate`/`hydrate`, the payload types, the reachability closure, the principal assertions. Knows nothing about escaping details. |
| `packages/client-core/src/cache.ts` | Add `peek`, `restore`, `settledQueries`; export `operationName`. |
| `packages/client-core/src/registry.ts` | `SettleResult.tags`. |
| `packages/client-core/src/client.ts` | `getServerState` on `QueryHandle`. |
| `packages/client-core/src/index.ts` | Exports. |
| `packages/client-core/package.json` | size-limit entries. |
| `packages/client-react/src/hydration.ts` | **New.** `ForgeHydrationBoundary`. |
| `packages/client-react/src/useQuery.ts` | `getServerSnapshot` via the handle. |
| `packages/client-react/src/index.ts` | Exports. |

---

### Task 1: The wire encoding

**Files:**
- Create: `packages/client-core/src/wire.ts`
- Test: `packages/client-core/__tests__/wire.test.ts`

**Interfaces:**
- Consumes: `isRef`, `makeRef`, `markRewritten` from `./ref`; `EntityKey`, `Ref` from `./types`.
- Produces:
  - `interface EncodeContext { readonly query: string; readonly entity?: string }`
  - `interface EncodeResult { readonly value: unknown; readonly refs: readonly EntityKey[] }`
  - `function encode(node: unknown, context: EncodeContext): EncodeResult`
  - `function assertAcyclic(node: unknown, context: EncodeContext): void`
  - `function revive(node: unknown): unknown`

- [ ] **Step 1: Write the failing test**

Create `packages/client-core/__tests__/wire.test.ts`:

```ts
import { describe, expect, it } from 'vitest';

import { makeRef, isRef, isRewritten } from '../src/ref';
import { assertAcyclic, encode, revive } from '../src/wire';

const where = { query: 'GET /orders({})' };

describe('encode', () => {
  it('emits a reference as a plain marker object and reports its key', () => {
    const { value, refs } = encode({ order: makeRef('Order:7') }, where);

    expect(value).toEqual({ order: { __ref: 'Order:7' } });
    expect(refs).toEqual(['Order:7']);
  });

  it('escapes response data that is shaped exactly like a reference', () => {
    const { value, refs } = encode({ meta: { __ref: 'not a reference' } }, where);

    expect(value).toEqual({ meta: { ___ref: 'not a reference' } });
    expect(refs).toEqual([]);
  });

  it('escapes an already-escape-shaped key, so the scheme nests', () => {
    expect(encode({ ___ref: 1, ____ref: 2 }, where).value).toEqual({ ____ref: 1, _____ref: 2 });
  });

  it('leaves every other key alone', () => {
    expect(encode({ __refs: 1, ref: 2, _ref: 3 }, where).value).toEqual({
      __refs: 1,
      ref: 2,
      _ref: 3,
    });
  });

  it('reports references found at any depth, deduplication left to the caller', () => {
    const { refs } = encode(
      { rows: [{ o: makeRef('Order:1') }, { o: makeRef('Order:2') }, makeRef('Order:1')] },
      where,
    );

    expect(refs).toEqual(['Order:1', 'Order:2', 'Order:1']);
  });

  it('allows the same object twice through different branches -- a DAG is not a cycle', () => {
    const shared = { n: 1 };

    expect(encode({ a: shared, b: shared }, where).value).toEqual({ a: { n: 1 }, b: { n: 1 } });
  });

  it('throws on a cycle, naming the query and the path', () => {
    const node: Record<string, unknown> = { id: 7 };
    node.self = node;

    expect(() => encode(node, where)).toThrow(/cyclic value/);
    expect(() => encode(node, where)).toThrow(/skeleton\.self/);
  });

  it('names the record when one is being encoded', () => {
    const node: Record<string, unknown> = {};
    node.meta = { self: node };

    expect(() => encode(node, { query: 'GET /orders({})', entity: 'Order:7' })).toThrow(
      /entity {2}Order:7/,
    );
    expect(() => encode(node, { query: 'GET /orders({})', entity: 'Order:7' })).toThrow(
      /data\.meta\.self/,
    );
  });

  it('reports an array index in the path', () => {
    const row: Record<string, unknown> = {};
    row.rows = [row];

    expect(() => encode(row, where)).toThrow(/skeleton\.rows\[0\]/);
  });
});

describe('assertAcyclic', () => {
  it('accepts an acyclic value', () => {
    expect(() => assertAcyclic({ a: [1, { b: 2 }] }, where)).not.toThrow();
  });

  it('throws on a cycle', () => {
    const node: Record<string, unknown> = {};
    node.self = node;

    expect(() => assertAcyclic(node, where)).toThrow(/cyclic value/);
  });
});

describe('revive', () => {
  it('mints a genuine reference the runtime recognises', () => {
    const revived = revive({ order: { __ref: 'Order:7' } }) as { order: unknown };

    expect(isRef(revived.order)).toBe(true);
  });

  it('unescapes data that was shaped like a reference, and does not mint one', () => {
    const revived = revive({ meta: { ___ref: 'not a reference' } }) as { meta: unknown };

    expect(revived.meta).toEqual({ __ref: 'not a reference' });
    expect(isRef(revived.meta)).toBe(false);
  });

  it('marks a container that has a reference beneath it', () => {
    const revived = revive({ rows: [{ __ref: 'Order:7' }] }) as { rows: object };

    expect(isRewritten(revived.rows)).toBe(true);
    expect(isRewritten(revived)).toBe(true);
  });

  it('leaves a container with no reference beneath it unmarked and by identity', () => {
    const input = { totals: { open: 3 } };
    const revived = revive(input) as { totals: object };

    expect(revived).toBe(input);
    expect(isRewritten(revived.totals)).toBe(false);
  });

  it('does not mark a container that only needed unescaping', () => {
    const revived = revive({ meta: { ___ref: 'x' } }) as object;

    expect(isRewritten(revived)).toBe(false);
  });

  it('ignores a marker-shaped object carrying anything but a lone string', () => {
    expect(isRef(revive({ __ref: 7 }))).toBe(false);
    expect(isRef(revive({ __ref: 'Order:7', extra: 1 }))).toBe(false);
  });

  it('round-trips through JSON', () => {
    const encoded = encode({ rows: [makeRef('Order:7'), { __ref: 'data' }] }, where);
    const revived = revive(JSON.parse(JSON.stringify(encoded.value))) as { rows: unknown[] };

    expect(isRef(revived.rows[0])).toBe(true);
    expect(revived.rows[1]).toEqual({ __ref: 'data' });
    expect(isRef(revived.rows[1])).toBe(false);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd packages/client-core && npx vitest run __tests__/wire.test.ts`
Expected: FAIL — `Failed to resolve import "../src/wire"`.

- [ ] **Step 3: Write the implementation**

Create `packages/client-core/src/wire.ts`:

```ts
import { isRef, makeRef, markRewritten } from './ref';
import type { EntityKey, Ref } from './types';

/**
 * The JSON encoding a dehydrated payload uses, and its inverse.
 *
 * `__ref` is the wire form of a reference and always has been -- see `Ref` --
 * but it cannot be the *recognition* rule, because a response may legitimately
 * contain an object of exactly that shape. `normalize` leaves such an object
 * inline, so it reaches the store as ordinary record data; a revive pass that
 * treated every `{__ref: string}` as a reference would mint one from it and
 * `denormalize` would resolve it to `undefined`. That is precisely the lossy
 * round-trip `ref.ts` refuses, reintroduced from the other direction.
 *
 * So the encoder escapes on the way out and the reviver unescapes on the way
 * in. Both directions are one walk, and the encoder's walk is needed anyway --
 * `dehydrate` has to collect the references it finds to build its reachability
 * closure, and has to notice a cycle, and both fall out of visiting every node.
 */

/** A key that would be read back as the marker, or as an escape of one. */
const COLLIDES = /^_*__ref$/;

/** A key this encoder escaped on the way out. */
const ESCAPED = /^_+__ref$/;

export interface EncodeContext {
  /** The query whose payload this is, for the cycle error. */
  readonly query: string;
  /** The record being encoded, when this is a record rather than a skeleton. */
  readonly entity?: string;
}

export interface EncodeResult {
  readonly value: unknown;
  /**
   * Every reference found, in encounter order and **not** deduplicated.
   *
   * The caller is walking a closure and already holds the set of keys it has
   * seen, so deduplicating here would build a second Set per node to answer a
   * question the caller has to answer again anyway.
   */
  readonly refs: readonly EntityKey[];
}

/** Copy `node` into a JSON-safe form, escaping keys and lifting references. */
export function encode(node: unknown, context: EncodeContext): EncodeResult {
  const refs: EntityKey[] = [];
  const route = new Set<object>();

  function walk(value: unknown, path: string): unknown {
    if (value === null || typeof value !== 'object') return value;

    if (isRef(value)) {
      const key = (value as Ref).__ref;
      refs.push(key);

      // A fresh literal rather than the frozen `Ref` itself. Emitting the
      // reference would put an object registered in `ref.ts`'s WeakSet into the
      // payload, which is harmless on the wire and confusing in a test.
      return { __ref: key };
    }

    if (route.has(value)) throw cyclic(context, path);

    route.add(value);

    const out = Array.isArray(value)
      ? value.map((element, index) => walk(element, `${path}[${index}]`))
      : walkObject(value as Record<string, unknown>, path);

    route.delete(value);

    return out;
  }

  function walkObject(value: Record<string, unknown>, path: string): Record<string, unknown> {
    const out: Record<string, unknown> = {};

    for (const key of Object.keys(value)) {
      out[COLLIDES.test(key) ? `_${key}` : key] = walk(value[key], `${path}.${key}`);
    }

    return out;
  }

  return { value: walk(node, rootPath(context)), refs };
}

/**
 * Throw if `node` is cyclic, without copying it.
 *
 * The denormalized payload mode ships a rehydrated value straight to
 * `JSON.stringify`, so it needs the cycle check and none of the escaping: it
 * contains no genuine references, and escaping a value nothing will unescape
 * would corrupt it.
 */
export function assertAcyclic(node: unknown, context: EncodeContext): void {
  const route = new Set<object>();

  function walk(value: unknown, path: string): void {
    if (value === null || typeof value !== 'object') return;

    if (route.has(value)) throw cyclic(context, path);

    route.add(value);

    if (Array.isArray(value)) {
      value.forEach((element, index) => walk(element, `${path}[${index}]`));
    } else {
      for (const key of Object.keys(value)) {
        walk((value as Record<string, unknown>)[key], `${path}.${key}`);
      }
    }

    route.delete(value);
  }

  walk(node, rootPath(context));
}

/**
 * Turn a decoded payload back into a skeleton the runtime recognises.
 *
 * `markRewritten` is applied to a container **only** where a reference occurs
 * beneath it. A container without one is deliberately left unmarked and, where
 * nothing about it changed, returned by identity -- which is what keeps
 * `EntityStore`'s "not rewritten means no walk" fast path intact for a hydrated
 * skeleton exactly as it is for a normalized one. Marking everything would be
 * correct and would void structural sharing for the whole response.
 */
export function revive(node: unknown): unknown {
  return reviveNode(node).value;
}

interface Revived {
  readonly value: unknown;
  /** Whether a reference was minted here or anywhere beneath. */
  readonly refs: boolean;
}

function reviveNode(node: unknown): Revived {
  if (node === null || typeof node !== 'object') return { value: node, refs: false };

  if (isMarker(node)) {
    return { value: makeRef((node as Record<string, unknown>).__ref as EntityKey), refs: true };
  }

  return Array.isArray(node)
    ? reviveArray(node)
    : reviveObject(node as Record<string, unknown>);
}

function reviveArray(node: unknown[]): Revived {
  const out = new Array<unknown>(node.length);
  let refs = false;
  let changed = false;

  for (let i = 0; i < node.length; i++) {
    const child = reviveNode(node[i]);

    out[i] = child.value;
    if (child.refs) refs = true;
    if (child.value !== node[i]) changed = true;
  }

  // Nothing moved, so nothing was minted either: a reference always differs
  // from the marker object it replaced.
  if (!changed) return { value: node, refs: false };

  return { value: refs ? markRewritten(out) : out, refs };
}

function reviveObject(node: Record<string, unknown>): Revived {
  const out: Record<string, unknown> = {};
  let refs = false;
  let changed = false;

  for (const key of Object.keys(node)) {
    const name = ESCAPED.test(key) ? key.slice(1) : key;
    const child = reviveNode(node[key]);

    out[name] = child.value;
    if (child.refs) refs = true;
    if (name !== key || child.value !== node[key]) changed = true;
  }

  if (!changed) return { value: node, refs: false };

  return { value: refs ? markRewritten(out) : out, refs };
}

/**
 * Whether a decoded object is the marker `encode` emits.
 *
 * Exactly one own key, named `__ref`, holding a string -- the shape
 * `makeRef` produces and the only shape the encoder ever emits unescaped.
 * Anything looser would claim objects the encoder escaped for a reason.
 */
function isMarker(node: object): boolean {
  if (Array.isArray(node)) return false;

  const keys = Object.keys(node);

  return (
    keys.length === 1 &&
    keys[0] === '__ref' &&
    typeof (node as Record<string, unknown>).__ref === 'string'
  );
}

function rootPath(context: EncodeContext): string {
  return context.entity === undefined ? 'skeleton' : 'data';
}

function cyclic(context: EncodeContext, path: string): Error {
  const headline =
    context.entity === undefined
      ? 'cannot serialize a cyclic value'
      : 'cannot serialize a cycle within one record';
  const entity = context.entity === undefined ? '' : `\n  entity  ${context.entity}`;

  return new Error(
    `[forge] dehydrate: ${headline}\n  query   ${context.query}${entity}\n  path    ${path}`,
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd packages/client-core && npx vitest run __tests__/wire.test.ts && npm run typecheck`
Expected: all PASS, no type errors.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/wire.ts packages/client-core/__tests__/wire.test.ts
git commit -m "feat(client): add the SSR wire encoding, with reference-shaped data escaped"
```

---

### Task 2: `SettleResult.tags`

**Files:**
- Modify: `packages/client-core/src/registry.ts:44-60` (the `SettleResult` interface) and `:306-328` (`settle`)
- Test: `packages/client-core/__tests__/registry.test.ts`

**Interfaces:**
- Produces: `SettleResult.tags?: Iterable<string>` — when present, `settle` uses these instead of resolving `provides` against a response.

- [ ] **Step 1: Write the failing test**

Append to `packages/client-core/__tests__/registry.test.ts`:

```ts
describe('settling with tags supplied', () => {
  it('uses the supplied tags instead of resolving provides against a response', () => {
    const registry = new QueryRegistry();

    registry.mount({ operation: 'orderList', args: {}, provides: ['Order:{res.id}'] })();
    registry.settle('orderList()', { tags: ['Order:7'], deps: ['Order:7'] });

    expect(registry.queriesFor('Order:7').map((entry) => entry.key)).toEqual(['orderList()']);
  });

  it('still unions the supplied tags with the entity dependencies', () => {
    const registry = new QueryRegistry();

    registry.mount({ operation: 'orderList', args: {}, provides: [] })();
    registry.settle('orderList()', { tags: ['Order[]'], deps: ['Order:1'] });

    const entry = registry.get('orderList()');

    expect([...(entry?.tags ?? [])].sort()).toEqual(['Order:1', 'Order[]']);
  });

  it('reports no unresolved template when tags are supplied', () => {
    const unresolved: string[] = [];
    const registry = new QueryRegistry({ onUnresolved: (template) => unresolved.push(template) });

    registry.mount({ operation: 'orderList', args: {}, provides: ['Order:{res.id}'] })();
    registry.settle('orderList()', { tags: ['Order:7'] });

    expect(unresolved).toEqual([]);
  });
});
```

Adjust the import block at the top of the file only if `QueryRegistry` is not already imported; it is.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd packages/client-core && npx vitest run __tests__/registry.test.ts -t "settling with tags supplied"`
Expected: FAIL — the query is not found under `Order:7`, because `Order:{res.id}` resolved to nothing.

- [ ] **Step 3: Write the implementation**

In `packages/client-core/src/registry.ts`, add to `SettleResult` after the `response` field:

```ts
  /**
   * The already-resolved tag set, bypassing `provides` resolution entirely.
   *
   * For `hydrate` in its normalized mode, which holds no response: `provides`
   * templates naming `{res.x}` cannot be resolved without one, and resolving
   * them to nothing would silently drop the tag, so a mutation would stop
   * reaching a query that displays what it changed. The tags were resolved on
   * the server, where the response existed, and are carried across instead.
   *
   * `response` is ignored when this is present. No caller supplies both.
   */
  readonly tags?: Iterable<string>;
```

Then replace the first statement of `settle` (`registry.ts:311-313`):

```ts
    const supplied = result.tags;
    const resolved =
      supplied === undefined
        ? resolveTags(entry.provides, { ...entry.args, response: result.response })
        : { tags: new Set(supplied), unresolved: [] as string[] };

    for (const template of resolved.unresolved) this.onUnresolved?.(template, entry);
```

The `retag` call two lines below already reads `resolved.tags`, so it needs no change.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd packages/client-core && npx vitest run __tests__/registry.test.ts && npm run typecheck`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/registry.ts packages/client-core/__tests__/registry.test.ts
git commit -m "feat(client): let a settle supply resolved tags instead of a response"
```

---

### Task 3: `QueryCache.peek`, `settledQueries` and `restore`

**Files:**
- Modify: `packages/client-core/src/cache.ts` — add three methods after `getState` (`cache.ts:271`), and export `operationName` (`cache.ts:1056`)
- Test: `packages/client-core/__tests__/cache.test.ts`

**Interfaces:**
- Consumes: `SettleResult.tags` from Task 2.
- Produces:
  - `interface CachedQuery { readonly key: string; readonly meta: OperationMeta; readonly args: TagContext; readonly skeleton: unknown }`
  - `QueryCache.peek<T>(meta: OperationMeta, args?: TagContext): QueryState<T> | undefined`
  - `QueryCache.settledQueries(): CachedQuery[]`
  - `QueryCache.restore(meta: OperationMeta, args: TagContext | undefined, input: RestoreInput): void`
  - `interface RestoreInput { readonly skeleton: unknown; readonly tags?: Iterable<string>; readonly response?: unknown; readonly stale?: boolean }`
  - `export function operationName(meta: OperationMeta): string` (package-internal; not re-exported from `index.ts`)

- [ ] **Step 1: Write the failing test**

Append to `packages/client-core/__tests__/cache.test.ts`:

```ts
describe('peek', () => {
  it('returns undefined for a query the cache has never opened, and opens nothing', () => {
    const { cache } = cache_(() => [{ id: 7, total: 99 }]);

    expect(cache.peek(orderList)).toBeUndefined();
    expect(cache.size).toBe(0);
  });

  it('returns the same state object as getState once a record exists', async () => {
    const { cache } = cache_(() => [{ id: 7, total: 99 }]);

    await cache.fetch(orderList);

    expect(cache.peek(orderList)).toBe(cache.getState(orderList));
  });

  it('is referentially stable across calls while nothing changes', async () => {
    const { cache } = cache_(() => [{ id: 7, total: 99 }]);

    await cache.fetch(orderList);

    expect(cache.peek(orderList)).toBe(cache.peek(orderList));
  });
});

describe('settledQueries', () => {
  it('lists only the queries that settled successfully', async () => {
    const { cache } = cache_((request) => {
      if (request.meta === customerList) throw new HttpFailure(500);

      return [{ id: 7, total: 99 }];
    });

    await cache.fetch(orderList);
    await cache.fetch(customerList).catch(() => undefined);
    cache.getState(orderGet, { path: { id: 1 } });

    expect(cache.settledQueries().map((query) => query.key)).toEqual([cache.key(orderList)]);
  });

  it('reports the skeleton the store holds, not the response', async () => {
    const { cache } = cache_(() => [{ id: 7, total: 99 }]);

    await cache.fetch(orderList);

    expect(cache.settledQueries()[0]?.skeleton).toEqual([{ __ref: 'Order:7' }]);
  });
});

describe('restore', () => {
  it('settles a query from a skeleton with no request', () => {
    const { cache, transport } = cache_(() => [{ id: 7, total: 99 }]);

    cache.store.put('Order:7', { id: 7, total: 99 });
    cache.restore(orderList, undefined, { skeleton: [makeRef('Order:7')], tags: ['Order[]'] });

    expect(cache.getState(orderList).status).toBe('success');
    expect(cache.getState(orderList).data).toEqual([{ id: 7, total: 99 }]);
    expect(transport.calls).toHaveLength(0);
  });

  it('records the skeleton dependencies, so a write to the entity is seen', () => {
    const { cache } = cache_(() => []);

    cache.store.put('Order:7', { id: 7, total: 99 });
    cache.restore(orderList, undefined, { skeleton: [makeRef('Order:7')], tags: ['Order[]'] });

    expect([...(cache.registry.get(cache.key(orderList))?.deps ?? [])]).toEqual(['Order:7']);
  });

  it('leaves the entry fresh by default and stale when asked', () => {
    const { cache } = cache_(() => []);

    cache.store.put('Order:7', { id: 7, total: 99 });
    cache.restore(orderList, undefined, { skeleton: [makeRef('Order:7')], tags: ['Order[]'] });
    expect(cache.registry.get(cache.key(orderList))?.stale).toBe(false);

    cache.restore(orderGet, { path: { id: 7 } }, {
      skeleton: makeRef('Order:7'),
      tags: ['Order:7'],
      stale: true,
    });
    expect(cache.registry.get(cache.key(orderGet, { path: { id: 7 } }))?.stale).toBe(true);
  });

  it('notifies the subscribers of a query it settles', () => {
    const { cache } = cache_(() => []);
    let notified = 0;

    cache.store.put('Order:7', { id: 7, total: 99 });
    cache.subscribe(orderList, undefined, () => {
      notified++;
    });

    const before = notified;
    cache.restore(orderList, undefined, { skeleton: [makeRef('Order:7')], tags: ['Order[]'] });

    expect(notified).toBeGreaterThan(before);
  });
});
```

Add to the imports at the top of `cache.test.ts`:

```ts
import { makeRef } from '../src/ref';
```

and rename the local helper `cache` to `cache_` throughout the file **only if** the new tests shadow it. The file already declares `function cache(handler)` at module scope; the new `describe` blocks call it as `cache_`, so add an alias next to the existing helper rather than renaming every call site:

```ts
const cache_ = cache;
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd packages/client-core && npx vitest run __tests__/cache.test.ts -t "peek"`
Expected: FAIL — `cache.peek is not a function`.

- [ ] **Step 3: Write the implementation**

In `packages/client-core/src/cache.ts`, add after the `getState` method (`cache.ts:271`):

```ts
  /**
   * This query's state **without opening a record for it.**
   *
   * `getState` routes through `open`, which creates the record if it is new --
   * correct for a subscriber, wrong for two callers that must not have side
   * effects. A server render asks about every query on the page, including ones
   * this request never fetched, and on a server the cache may be shared; and a
   * dehydrated payload is read, not fetched. `undefined` means "nothing is
   * cached", which is a different answer from `idle`.
   *
   * Deliberately does not move the record's LRU position either: a peek is not
   * a use, and letting a server render reorder the eviction queue would make
   * which query gets evicted depend on render order.
   */
  peek<T = unknown>(meta: OperationMeta, args?: TagContext): QueryState<T> | undefined {
    const record = this.records.get(this.key(meta, args));

    if (record === undefined) return undefined;

    return this.snapshot(record) as QueryState<T>;
  }

  /**
   * Every query that settled successfully, as `dehydrate` reads them.
   *
   * Pending and failed queries are absent by construction. A pending query has
   * no skeleton to serialize, and a failed one would hydrate a client into a
   * failure the server observed and the client cannot meaningfully retry --
   * both are better left for the client to fetch normally.
   */
  settledQueries(): CachedQuery[] {
    const out: CachedQuery[] = [];

    for (const record of this.records.values()) {
      if (!record.settled || record.status !== 'success') continue;

      out.push({
        key: record.key,
        meta: record.meta,
        args: record.args,
        skeleton: record.skeleton,
      });
    }

    return out;
  }

  /**
   * Settle a query from a skeleton, with no request behind it.
   *
   * The seam `hydrate` writes through. Everything a settle normally does apart
   * from the request: install the skeleton, mark the record successful, record
   * the dependencies, retag the registry entry and notify.
   *
   * `deps` are recomputed from the skeleton against the live store rather than
   * carried in the payload -- `dependencies` is exact, costs one memoized walk,
   * and does not have to trust what arrived over the wire.
   *
   * Merges rather than replaces. A query the cache already holds is re-settled
   * against the hydrated skeleton, and the records behind it went through `put`,
   * which keeps the previous object for identical data. Hydrating the same
   * payload twice therefore moves no version and changes no identity.
   */
  restore(meta: OperationMeta, args: TagContext | undefined, input: RestoreInput): void {
    const record = this.open(meta, args);

    record.skeleton = input.skeleton;
    record.settled = true;
    record.status = 'success';
    record.error = undefined;
    record.fetching = false;

    const value = this.read(record);

    this.registry.settle(record.key, {
      value,
      deps: this.store.dependencies(input.skeleton),
      ...(input.tags === undefined ? {} : { tags: input.tags }),
      ...(input.response === undefined ? {} : { response: input.response }),
    });

    const entry = this.registry.get(record.key);

    if (input.stale === true && entry !== undefined) this.registry.markStale(entry);

    this.notify(record);
  }
```

Add the two interfaces above the `QueryCache` class, next to `LiveBinding` (`cache.ts:84`):

```ts
/** One settled query, as `dehydrate` reads it out of the cache. */
export interface CachedQuery {
  readonly key: string;
  readonly meta: OperationMeta;
  readonly args: TagContext;
  readonly skeleton: unknown;
}

/** What `QueryCache.restore` installs. See that method. */
export interface RestoreInput {
  readonly skeleton: unknown;
  /** Resolved tags, for a payload that carries no response. */
  readonly tags?: Iterable<string>;
  /** The response, for a payload that does. Ignored when `tags` is present. */
  readonly response?: unknown;
  /** Settle behind the server, so a mount refetches. */
  readonly stale?: boolean;
}
```

Finally, export the existing `operationName` helper by changing `cache.ts:1056` from `function operationName(` to `export function operationName(`. Add one line to its doc comment:

```
 * Exported within the package so `ssr.ts` names an operation on the wire
 * exactly as the cache keys it. It is not part of the public API.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd packages/client-core && npx vitest run && npm run typecheck`
Expected: all PASS, including every pre-existing test.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/cache.ts packages/client-core/__tests__/cache.test.ts
git commit -m "feat(client): add peek, settledQueries and restore to the query cache"
```

---

### Task 4: `dehydrate`

**Files:**
- Create: `packages/client-core/src/ssr.ts`
- Test: `packages/client-core/__tests__/ssr.test.ts`

**Interfaces:**
- Consumes: `encode`, `assertAcyclic` from `./wire`; `operationName`, `CachedQuery`, `QueryCache` from `./cache`.
- Produces:
  - `type DehydratedState = NormalizedState | DenormalizedState`
  - `interface NormalizedState { readonly v: 1; readonly mode: 'normalized'; readonly principal?: string | number | null; readonly records: Readonly<Record<EntityKey, unknown>>; readonly queries: readonly NormalizedQuery[] }`
  - `interface DenormalizedState { readonly v: 1; readonly mode: 'denormalized'; readonly principal?: string | number | null; readonly queries: readonly DenormalizedQuery[] }`
  - `interface NormalizedQuery { readonly operation: string; readonly args: TagContext; readonly skeleton: unknown; readonly tags: readonly string[] }`
  - `interface DenormalizedQuery { readonly operation: string; readonly args: TagContext; readonly value: unknown }`
  - `interface DehydrateOptions { readonly principal: string | number | null | undefined; readonly mode?: 'normalized' | 'denormalized'; readonly include?: readonly string[] }`
  - `function dehydrate(cache: QueryCache, options: DehydrateOptions): DehydratedState`

- [ ] **Step 1: Write the failing test**

Create `packages/client-core/__tests__/ssr.test.ts`:

```ts
import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { dehydrate } from '../src/ssr';
import type { NormalizedState, DenormalizedState } from '../src/ssr';
import type { OperationMeta, TransportRequest } from '../src/transport';
import { fakeTransport } from './harness';
import { schema } from './schema';

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const customerList: OperationMeta = {
  method: 'GET',
  path: '/customers',
  entity: 'Customer',
  provides: ['Customer[]'],
  invalidates: [],
};

function cache(handler: (request: TransportRequest, call: number) => unknown): QueryCache {
  const scheduler = manualScheduler();

  return new QueryCache({
    transport: fakeTransport(handler),
    entities: schema,
    scheduler: scheduler.schedule,
  });
}

describe('dehydrate, normalized', () => {
  it('emits the skeleton, the reachable records and the resolved tags', async () => {
    const client = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }]);

    await client.fetch(orderList);

    const state = dehydrate(client, { principal: 'u-1' }) as NormalizedState;

    expect(state.v).toBe(1);
    expect(state.mode).toBe('normalized');
    expect(state.principal).toBe('u-1');
    expect(state.queries).toEqual([
      {
        operation: 'GET /orders',
        args: {},
        skeleton: [{ __ref: 'Order:7' }],
        tags: expect.arrayContaining(['Order[]', 'Order:7', 'Customer:c-3']),
      },
    ]);
    expect(state.records['Order:7']).toEqual({
      id: 7,
      total: 99,
      customer: { __ref: 'Customer:c-3' },
    });
    expect(state.records['Customer:c-3']).toEqual({ id: 'c-3', name: 'Ada' });
  });

  it('survives JSON', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(() => JSON.stringify(dehydrate(client, { principal: 'u-1' }))).not.toThrow();
  });

  it('emits an entity cycle between records without difficulty', async () => {
    const client = cache(() => [
      { id: 7, total: 99, customer: { id: 'c-3', orders: [{ id: 7 }] } },
    ]);

    await client.fetch(orderList);

    const state = dehydrate(client, { principal: 'u-1' }) as NormalizedState;

    expect(state.records['Customer:c-3']).toEqual({
      id: 'c-3',
      orders: [{ __ref: 'Order:7' }],
    });
  });
});

describe('dehydrate, the reachability closure', () => {
  it('omits an entity no exported query references', async () => {
    const client = cache((request) =>
      request.meta === orderList ? [{ id: 7, total: 99 }] : [{ id: 'c-9', name: 'Grace' }],
    );

    await client.fetch(orderList);
    await client.fetch(customerList);

    const state = dehydrate(client, {
      principal: 'u-1',
      include: [client.key(orderList)],
    }) as NormalizedState;

    expect(Object.keys(state.records)).toEqual(['Order:7']);
    expect(state.queries).toHaveLength(1);
  });

  it('never reads the store wholesale: an orphaned record is not emitted', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);
    client.store.put('Order:999', { id: 999, secret: 'another request' });

    const state = dehydrate(client, { principal: 'u-1' }) as NormalizedState;

    expect(Object.keys(state.records)).toEqual(['Order:7']);
  });

  it('throws for an include naming a key the cache does not hold', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(() => dehydrate(client, { principal: 'u-1', include: ['GET /nope()'] })).toThrow(
      /\[forge\] dehydrate: no settled query for GET \/nope\(\)/,
    );
  });

  it('omits a query that failed', async () => {
    const client = cache((request) => {
      if (request.meta === customerList) throw new Error('boom');

      return [{ id: 7, total: 99 }];
    });

    await client.fetch(orderList);
    await client.fetch(customerList).catch(() => undefined);

    expect(dehydrate(client, { principal: 'u-1' }).queries).toHaveLength(1);
  });
});

describe('dehydrate, the principal', () => {
  it('throws when it does not match the cache owner', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);

    client.setPrincipal('u-1');
    await client.fetch(orderList);

    expect(() => dehydrate(client, { principal: 'u-2' })).toThrow(
      /\[forge\] dehydrate: principal does not match the cache owner/,
    );
  });

  it('accepts an unset principal on both sides', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(dehydrate(client, { principal: undefined }).principal).toBeUndefined();
  });

  it('refuses a principal that cannot survive JSON', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);
    const owner = { id: 'u-1' };

    client.setPrincipal(owner);
    await client.fetch(orderList);

    expect(() => dehydrate(client, { principal: owner as never })).toThrow(
      /\[forge\] dehydrate: principal must be a string, number, null or undefined/,
    );
  });
});

describe('dehydrate, denormalized', () => {
  it('emits the rehydrated value and no records', async () => {
    const client = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }]);

    await client.fetch(orderList);

    const state = dehydrate(client, {
      principal: 'u-1',
      mode: 'denormalized',
    }) as DenormalizedState;

    expect(state.mode).toBe('denormalized');
    expect(state.queries).toEqual([
      {
        operation: 'GET /orders',
        args: {},
        value: [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }],
      },
    ]);
    expect(state).not.toHaveProperty('records');
  });

  it('passes reference-shaped response data through untouched', async () => {
    const client = cache(() => [{ id: 7, meta: { __ref: 'not a reference' } }]);

    await client.fetch(orderList);

    const state = dehydrate(client, {
      principal: 'u-1',
      mode: 'denormalized',
    }) as DenormalizedState;

    expect(state.queries[0]?.value).toEqual([{ id: 7, meta: { __ref: 'not a reference' } }]);
  });

  it('throws on an entity cycle, which normalized mode serializes fine', async () => {
    const client = cache(() => [
      { id: 7, total: 99, customer: { id: 'c-3', orders: [{ id: 7 }] } },
    ]);

    await client.fetch(orderList);

    expect(() => dehydrate(client, { principal: 'u-1', mode: 'denormalized' })).toThrow(
      /cannot serialize a cyclic value/,
    );
    expect(() => dehydrate(client, { principal: 'u-1' })).not.toThrow();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd packages/client-core && npx vitest run __tests__/ssr.test.ts`
Expected: FAIL — `Failed to resolve import "../src/ssr"`.

- [ ] **Step 3: Write the implementation**

Create `packages/client-core/src/ssr.ts`:

```ts
import { operationName } from './cache';
import type { CachedQuery, QueryCache } from './cache';
import type { TagContext } from './tags';
import type { EntityKey } from './types';
import { assertAcyclic, encode } from './wire';

/**
 * Serializing a cache for a server render, and reading it back.
 *
 * The payload is data embedded in an HTML response, so what it may contain is
 * a property of this module rather than a caution in the documentation.
 * `dehydrate` never reads the store wholesale: the record set is *built* by a
 * reachability walk from the exported queries, so an entity no exported query
 * references cannot appear in the payload -- not because a rule forbids it, but
 * because nothing ever put it there. That is what makes a module-level cache
 * shared across concurrent server requests survivable rather than a leak.
 *
 * Both sides also assert the principal. `dehydrate` refuses to serialize for
 * anyone but the cache's current owner, and `hydrate` refuses a payload that
 * belongs to someone else -- which is what a payload cached at a CDN and served
 * to the wrong session runs into.
 */

/** One query in a normalized payload. */
export interface NormalizedQuery {
  readonly operation: string;
  readonly args: TagContext;
  readonly skeleton: unknown;
  /**
   * The resolved tag set.
   *
   * Carried because this mode holds no response, and `provides` templates
   * naming `{res.x}` cannot be resolved without one. See `SettleResult.tags`.
   */
  readonly tags: readonly string[];
}

/** One query in a denormalized payload. */
export interface DenormalizedQuery {
  readonly operation: string;
  readonly args: TagContext;
  readonly value: unknown;
}

export interface NormalizedState {
  readonly v: 1;
  readonly mode: 'normalized';
  readonly principal?: string | number | null;
  readonly records: Readonly<Record<EntityKey, unknown>>;
  readonly queries: readonly NormalizedQuery[];
}

export interface DenormalizedState {
  readonly v: 1;
  readonly mode: 'denormalized';
  readonly principal?: string | number | null;
  readonly queries: readonly DenormalizedQuery[];
}

export type DehydratedState = NormalizedState | DenormalizedState;

export interface DehydrateOptions {
  /**
   * Who this payload's data belongs to. **Required**, and asserted against the
   * cache's owner.
   *
   * Constrained to a scalar. That is not arbitrary: `setPrincipal` compares
   * with `===`, so an object principal already re-clears the cache on every
   * call that mints a fresh one -- the store's working contract is a scalar,
   * and this states it. `undefined` is encoded as the key's absence, which is
   * what `JSON.stringify` does with it anyway.
   */
  readonly principal: string | number | null | undefined;
  /**
   * `normalized` (the default) dedupes an entity several queries share and is
   * the smallest wire form. `denormalized` ships each query's rehydrated value
   * and needs no revive pass, at the cost of duplicating shared entities -- and
   * it cannot express a query whose value contains an entity cycle, because
   * `denormalize` rebuilds such a graph as a real cycle and no JSON encoding of
   * one exists.
   */
  readonly mode?: 'normalized' | 'denormalized';
  /** Cache keys to export. Every settled query, when absent. */
  readonly include?: readonly string[];
}

export function dehydrate(cache: QueryCache, options: DehydrateOptions): DehydratedState {
  const { principal } = options;

  if (!scalar(principal)) {
    throw new Error(
      '[forge] dehydrate: principal must be a string, number, null or undefined, ' +
        'so that it survives JSON and compares by value',
    );
  }

  if (!Object.is(principal, cache.owner)) {
    throw new Error(
      '[forge] dehydrate: principal does not match the cache owner -- ' +
        'this cache holds another identity’s data',
    );
  }

  const exported = select(cache, options.include);

  return options.mode === 'denormalized'
    ? denormalized(cache, exported, principal)
    : normalized(cache, exported, principal);
}

/**
 * The queries to export: those named, or every settled one.
 *
 * A named key the cache does not hold throws rather than exporting nothing. A
 * typo that silently ships an empty payload is the defect found in production,
 * where it presents as SSR having quietly stopped working.
 */
function select(cache: QueryCache, include: readonly string[] | undefined): CachedQuery[] {
  const settled = cache.settledQueries();

  if (include === undefined) return settled;

  const byKey = new Map(settled.map((query) => [query.key, query]));

  return include.map((key) => {
    const query = byKey.get(key);

    if (query === undefined) throw new Error(`[forge] dehydrate: no settled query for ${key}`);

    return query;
  });
}

function normalized(
  cache: QueryCache,
  exported: readonly CachedQuery[],
  principal: string | number | null | undefined,
): NormalizedState {
  const queries: NormalizedQuery[] = [];
  const records: Record<EntityKey, unknown> = {};
  const seen = new Set<EntityKey>();
  // Each pending key remembers the query that reached it, so a cycle inside a
  // record can name the query whose payload would have carried it.
  const pending: { key: EntityKey; from: string }[] = [];

  const enqueue = (keys: readonly EntityKey[], from: string): void => {
    for (const key of keys) {
      if (seen.has(key)) continue;

      seen.add(key);
      pending.push({ key, from });
    }
  };

  for (const query of exported) {
    const encoded = encode(query.skeleton, { query: query.key });

    enqueue(encoded.refs, query.key);

    queries.push({
      operation: operationName(query.meta),
      args: query.args,
      skeleton: encoded.value,
      tags: [...(cache.registry.get(query.key)?.tags ?? [])],
    });
  }

  while (pending.length > 0) {
    const { key, from } = pending.pop() as { key: EntityKey; from: string };
    const record = cache.store.getRecord(key);

    // A reference the store no longer holds -- evicted between the fetch and
    // this call. It rehydrates to nothing on the client exactly as it does
    // here, which is the behaviour `denormalize` already specifies for a hole.
    if (record === undefined) continue;

    const encoded = encode(record.data, { query: from, entity: key });

    records[key] = encoded.value;
    enqueue(encoded.refs, from);
  }

  return {
    v: 1,
    mode: 'normalized',
    ...(principal === undefined ? {} : { principal }),
    records,
    queries,
  };
}

function denormalized(
  cache: QueryCache,
  exported: readonly CachedQuery[],
  principal: string | number | null | undefined,
): DenormalizedState {
  const queries = exported.map((query) => {
    // The cache retains no raw responses -- `settle` reads one to resolve tags
    // and does not keep it -- so this is the response as the store now holds
    // it, merges included. `store.write` re-normalizes it into the same records.
    const value = cache.store.read(query.skeleton);

    assertAcyclic(value, { query: query.key });

    return { operation: operationName(query.meta), args: query.args, value };
  });

  return {
    v: 1,
    mode: 'denormalized',
    ...(principal === undefined ? {} : { principal }),
    queries,
  };
}

function scalar(value: unknown): value is string | number | null | undefined {
  return (
    value === undefined || value === null || typeof value === 'string' || typeof value === 'number'
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd packages/client-core && npx vitest run __tests__/ssr.test.ts && npm run typecheck`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/ssr.ts packages/client-core/__tests__/ssr.test.ts
git commit -m "feat(client): add dehydrate, emitting only entities the exported queries reach"
```

---

### Task 5: `hydrate`

**Files:**
- Modify: `packages/client-core/src/ssr.ts`
- Test: `packages/client-core/__tests__/ssr.test.ts`

**Interfaces:**
- Consumes: `revive` from `./wire`; `QueryCache.restore` from Task 3.
- Produces:
  - `interface HydrateOptions { readonly ops: Readonly<Record<string, OperationMeta>>; readonly stale?: boolean }`
  - `function hydrate(cache: QueryCache, state: DehydratedState, options: HydrateOptions): void`

- [ ] **Step 1: Write the failing test**

Append to `packages/client-core/__tests__/ssr.test.ts`:

```ts
import { hydrate } from '../src/ssr';
import { isRef } from '../src/ref';

/** The generated `ops.ts` table, keyed as the generator keys it. */
const ops = { orderList, customerList };

/** Serialize and read back, exactly as an HTML round trip would. */
function transfer(state: ReturnType<typeof dehydrate>): ReturnType<typeof dehydrate> {
  return JSON.parse(JSON.stringify(state)) as ReturnType<typeof dehydrate>;
}

describe('hydrate', () => {
  it('serves the hydrated value with no request, in normalized mode', async () => {
    const server = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }]);

    await server.fetch(orderList);

    const client = cache(() => {
      throw new Error('the client must not fetch');
    });

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.getState(orderList).status).toBe('success');
    expect(client.getState(orderList).data).toEqual([
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);
  });

  it('serves the hydrated value with no request, in denormalized mode', async () => {
    const server = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }]);

    await server.fetch(orderList);

    const client = cache(() => {
      throw new Error('the client must not fetch');
    });

    hydrate(
      client,
      transfer(dehydrate(server, { principal: undefined, mode: 'denormalized' })),
      { ops },
    );

    expect(client.getState(orderList).data).toEqual([
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);
  });

  it('produces a store the entity graph is genuinely normalized into', async () => {
    const server = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }]);

    await server.fetch(orderList);

    const client = cache(() => []);

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.store.has('Order:7')).toBe(true);
    expect(client.store.has('Customer:c-3')).toBe(true);
    expect(isRef((client.store.getRecord('Order:7')?.data as Record<string, unknown>).customer))
      .toBe(true);
  });

  it('keeps reference-shaped response data as data', async () => {
    const server = cache(() => [{ id: 7, meta: { __ref: 'not a reference' } }]);

    await server.fetch(orderList);

    const client = cache(() => []);

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.getState(orderList).data).toEqual([
      { id: 7, meta: { __ref: 'not a reference' } },
    ]);
  });

  it('rebuilds an entity cycle as a cycle', async () => {
    const server = cache(() => [
      { id: 7, total: 99, customer: { id: 'c-3', orders: [{ id: 7 }] } },
    ]);

    await server.fetch(orderList);

    const client = cache(() => []);

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    const rows = client.getState(orderList).data as { customer: { orders: unknown[] } }[];

    expect(rows[0]?.customer.orders[0]).toBe(rows[0]);
  });

  it('carries a response-templated provides tag across, so a mutation still reaches it', async () => {
    const listWithResponseTag: OperationMeta = {
      ...orderList,
      provides: ['Order[]', 'Batch:{res.0.id}'],
    };
    const server = cache(() => [{ id: 7, total: 99 }]);

    await server.fetch(listWithResponseTag);

    const client = cache(() => []);

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), {
      ops: { orderList: listWithResponseTag },
    });

    expect(
      client.registry.queriesFor('Batch:7').map((entry) => entry.key),
    ).toEqual([client.key(listWithResponseTag)]);
  });

  it('settles fresh by default and stale when asked', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: undefined }));

    const fresh = cache(() => []);
    hydrate(fresh, state, { ops });
    expect(fresh.registry.get(fresh.key(orderList))?.stale).toBe(false);

    const verifying = cache(() => []);
    hydrate(verifying, state, { ops, stale: true });
    expect(verifying.registry.get(verifying.key(orderList))?.stale).toBe(true);
  });

  it('is idempotent: hydrating twice keeps the identity of what did not move', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: undefined }));
    const client = cache(() => []);

    hydrate(client, state, { ops });
    const first = client.getState(orderList).data;

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.getState(orderList).data).toEqual(first);
    expect(client.store.getRecord('Order:7')?.version).toBe(1);
  });

  it('refuses a payload belonging to another principal', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);

    server.setPrincipal('u-1');
    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: 'u-1' }));
    const client = cache(() => []);

    client.setPrincipal('u-2');

    expect(() => hydrate(client, state, { ops })).toThrow(
      /\[forge\] hydrate: this payload belongs to a different principal/,
    );
  });

  it('refuses an unrecognised payload version', () => {
    const client = cache(() => []);

    expect(() =>
      hydrate(client, { v: 2, mode: 'normalized', records: {}, queries: [] } as never, { ops }),
    ).toThrow(/\[forge\] hydrate: unsupported payload version 2/);
  });

  it('refuses an operation the ops table does not name', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const client = cache(() => []);

    expect(() =>
      hydrate(client, transfer(dehydrate(server, { principal: undefined })), {
        ops: { customerList },
      }),
    ).toThrow(/\[forge\] hydrate: no operation named GET \/orders/);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd packages/client-core && npx vitest run __tests__/ssr.test.ts -t hydrate`
Expected: FAIL — `hydrate is not exported by ../src/ssr`.

- [ ] **Step 3: Write the implementation**

In `packages/client-core/src/ssr.ts`, extend the imports:

```ts
import { assertAcyclic, encode, revive } from './wire';
import type { OperationMeta } from './transport';
```

and append:

```ts
export interface HydrateOptions {
  /**
   * The generated `ops.ts` table, passed verbatim.
   *
   * A cache record holds an `OperationMeta` and needs it to refetch, to
   * `watchLive` and to drive the transport -- and that is route metadata living
   * in the generated manifest, not in the store, so it cannot be reconstructed
   * from a payload. Serializing it instead would make this argument unnecessary
   * at the cost of putting the route table into every HTML response, to
   * duplicate what the client bundle already ships.
   *
   * Keyed however the generator keys it; the values are what matter, and they
   * are re-indexed below by the same `method path` the cache keys operations by.
   */
  readonly ops: Readonly<Record<string, OperationMeta>>;
  /**
   * Settle every hydrated query behind the server, so a mount refetches.
   *
   * Off by default, which is right for a dynamically rendered page: the server
   * fetched the data milliseconds earlier. A statically generated or ISR page
   * wants it on -- instant paint, then a verifying refetch.
   */
  readonly stale?: boolean;
}

export function hydrate(
  cache: QueryCache,
  state: DehydratedState,
  options: HydrateOptions,
): void {
  if (state.v !== 1) {
    throw new Error(`[forge] hydrate: unsupported payload version ${String(state.v)}`);
  }

  if (!Object.is(state.principal, cache.owner)) {
    throw new Error(
      '[forge] hydrate: this payload belongs to a different principal -- ' +
        'set the principal before hydrating, and never hydrate a payload built for someone else',
    );
  }

  const index = new Map<string, OperationMeta>();

  for (const meta of Object.values(options.ops)) index.set(operationName(meta), meta);

  const metaFor = (operation: string): OperationMeta => {
    const meta = index.get(operation);

    if (meta === undefined) throw new Error(`[forge] hydrate: no operation named ${operation}`);

    return meta;
  };

  const stale = options.stale === true ? { stale: true } : {};

  if (state.mode === 'normalized') {
    // Records first. A skeleton restored before the entity it references would
    // read as a hole, and `restore` reads its value as it settles.
    for (const [key, data] of Object.entries(state.records)) {
      cache.store.put(key, revive(data) as Record<string, unknown>);
    }

    for (const query of state.queries) {
      cache.restore(metaFor(query.operation), query.args, {
        skeleton: revive(query.skeleton),
        tags: query.tags,
        ...stale,
      });
    }

    return;
  }

  if (state.mode === 'denormalized') {
    for (const query of state.queries) {
      const meta = metaFor(query.operation);
      const { skeleton } = cache.store.write(
        query.value,
        cache.entities,
        meta.rootType ?? meta.entity,
      );

      cache.restore(meta, query.args, { skeleton, response: query.value, ...stale });
    }

    return;
  }

  throw new Error(
    `[forge] hydrate: unrecognised payload mode ${String((state as { mode: unknown }).mode)}`,
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd packages/client-core && npx vitest run && npm run typecheck`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/ssr.ts packages/client-core/__tests__/ssr.test.ts
git commit -m "feat(client): add hydrate, reviving a payload into a warm cache"
```

---

### Task 6: The round-trip property, and the generator that can actually hit the collision

**Files:**
- Modify: `packages/client-core/__tests__/roundtrip.property.test.ts`

**Interfaces:**
- Consumes: `dehydrate`, `hydrate` from Task 4 and Task 5.

- [ ] **Step 1: Write the failing test**

In `packages/client-core/__tests__/roundtrip.property.test.ts`, replace the `propertyName` arbitrary (currently a filtered `fc.string`) with:

```ts
/**
 * `__proto__` is not a data property: assigning it walks a setter instead of
 * creating a key, so neither the runtime nor this file's reference
 * implementation can round-trip it. It is out of scope, not a defect.
 *
 * `__ref` and its escape forms are drawn deliberately rather than left to
 * chance. They are the names that collide with the SSR wire encoding, and an
 * arbitrary string generator will never produce one -- which is exactly how a
 * response legitimately containing `{__ref: ...}` stayed a comment in `ref.ts`
 * instead of becoming a test.
 */
const propertyName = fc
  .oneof(
    { weight: 3, arbitrary: fc.string({ minLength: 1 }) },
    { weight: 1, arbitrary: fc.constantFrom('__ref', '___ref', '____ref', '__refs', '_ref') },
  )
  .filter((key) => key !== '__proto__');
```

Then append a new `describe` block at the end of the file:

```ts
describe('the SSR round trip', () => {
  it('hydrates to the value the server rendered, through JSON', () => {
    fc.assert(
      fc.property(response, (value) => {
        const server = ssrCache();

        server.store.write(value, schema, 'Order');
        server.restore(orderList, undefined, {
          skeleton: server.store.stage(value, schema, 'Order').skeleton,
          tags: [],
        });

        const expected = server.getState(orderList).data;
        const wire = JSON.parse(JSON.stringify(dehydrate(server, { principal: undefined })));

        const client = ssrCache();
        hydrate(client, wire, { ops: { orderList } });

        expect(client.getState(orderList).data).toEqual(expected);
      }),
      { numRuns: 200 },
    );
  });

  it('recomputes the same dependency set it started with', () => {
    fc.assert(
      fc.property(response, (value) => {
        const server = ssrCache();
        const staged = server.store.stage(value, schema, 'Order');

        server.store.write(value, schema, 'Order');
        server.restore(orderList, undefined, { skeleton: staged.skeleton, tags: [] });

        const before = [...(server.registry.get(server.key(orderList))?.deps ?? [])].sort();
        const wire = JSON.parse(JSON.stringify(dehydrate(server, { principal: undefined })));

        const client = ssrCache();
        hydrate(client, wire, { ops: { orderList } });

        expect([...(client.registry.get(client.key(orderList))?.deps ?? [])].sort()).toEqual(
          before,
        );
      }),
      { numRuns: 200 },
    );
  });
});
```

Add to the top of the file:

```ts
import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { dehydrate, hydrate } from '../src/ssr';
import type { OperationMeta } from '../src/transport';

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  rootType: 'Order',
  provides: [],
  invalidates: [],
};

/** A cache with a transport that must never be reached: these tests fetch nothing. */
function ssrCache(): QueryCache {
  return new QueryCache({
    transport: {
      execute: () => {
        throw new Error('the SSR property tests issue no requests');
      },
    },
    entities: schema,
    scheduler: manualScheduler().schedule,
  });
}
```

If the existing arbitrary that generates whole response trees is not named `response`, use whatever the file calls it; do not rename it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd packages/client-core && npx vitest run __tests__/roundtrip.property.test.ts`
Expected: FAIL initially only if something is genuinely broken. If they pass immediately, that is the correct outcome for this task — the property is a regression net over Tasks 1–5, and the *generator* change is the new coverage. Confirm the generator change is live by temporarily reverting the `COLLIDES` regex in `wire.ts` to `/^__ref$/`, re-running, and observing a failure; then restore it.

- [ ] **Step 3: Verify the collision is genuinely covered**

Run: `cd packages/client-core && npx vitest run __tests__/roundtrip.property.test.ts`
Expected: PASS with the correct `wire.ts`, FAIL with the sabotaged one. Restore `wire.ts` before continuing.

- [ ] **Step 4: Run the whole suite**

Run: `cd packages/client-core && npx vitest run && npm run typecheck`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/__tests__/roundtrip.property.test.ts
git commit -m "test(client): assert the SSR round trip, with reference-shaped keys generated"
```

---

### Task 7: `getServerState` on the query handle, and the package exports

**Files:**
- Modify: `packages/client-core/src/client.ts:72-90` (`QueryHandle`) and `:104-120` (`query`)
- Modify: `packages/client-core/src/index.ts`
- Modify: `packages/client-core/package.json`
- Test: `packages/client-core/__tests__/client.test.ts`

**Interfaces:**
- Consumes: `QueryCache.peek` from Task 3.
- Produces: `QueryHandle<T>.getServerState(): QueryState<T>`.

- [ ] **Step 1: Write the failing test**

Append to `packages/client-core/__tests__/client.test.ts`:

```ts
describe('the server snapshot', () => {
  it('is idle for a query the cache has nothing for, and opens no record', () => {
    const client = cache(() => [{ id: 7, total: 99 }]);
    const handle = query(orderList)(undefined, { client });

    expect(handle.getServerState()).toEqual({
      status: 'idle',
      data: undefined,
      error: undefined,
      isFetching: false,
    });
    expect(client.size).toBe(0);
  });

  it('is the same object every call, so useSyncExternalStore does not tear', () => {
    const client = cache(() => [{ id: 7, total: 99 }]);
    const handle = query(orderList)(undefined, { client });

    expect(handle.getServerState()).toBe(handle.getServerState());
  });

  it('returns what the cache holds once it holds something', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);
    const handle = query(orderList)(undefined, { client });

    await client.fetch(orderList);

    expect(handle.getServerState().data).toEqual([{ id: 7, total: 99 }]);
  });
});
```

Match the local helper names already used in `client.test.ts`; if it builds its cache differently, use its existing helper rather than introducing one.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd packages/client-core && npx vitest run __tests__/client.test.ts -t "the server snapshot"`
Expected: FAIL — `handle.getServerState is not a function`.

- [ ] **Step 3: Write the implementation**

In `packages/client-core/src/client.ts`, add a frozen constant above `QueryHandle`:

```ts
/**
 * The server snapshot for a query the cache holds nothing for.
 *
 * A module-level frozen constant because `getServerSnapshot` must be
 * referentially stable under a harder condition than `getSnapshot`: it is asked
 * about queries no record exists for, so there is no per-record memo to lean on.
 */
const IDLE: QueryState<never> = Object.freeze({
  status: 'idle' as const,
  data: undefined,
  error: undefined,
  isFetching: false,
});
```

Add to the `QueryHandle` interface, after `getState`:

```ts
  /**
   * The snapshot a server render sees, and the one a hydrating client's first
   * pass must match.
   *
   * `peek` rather than `getState`: this is called for queries the cache has
   * never opened, and opening a record as a side effect of a *render* is wrong
   * twice over -- on a server the cache may be shared between concurrent
   * requests, and a discarded render would leave an entry behind. `undefined`
   * from `peek` means nothing is cached, which is `idle`.
   *
   * Real data here is only correct because hydration exists: React compares
   * this against the client's first pass and treats a difference as a mismatch,
   * so a hydration boundary must have run above the component. With one, both
   * sides read the same warm cache and the server emits real markup.
   */
  getServerState(): QueryState<T>;
```

And in `query`'s returned handle, after `getState`:

```ts
      getServerState: () => cache.peek<T>(meta, args) ?? (IDLE as QueryState<T>),
```

In `packages/client-core/src/index.ts`, add after the `QueryCache` export block:

```ts
export { dehydrate, hydrate } from './ssr';
export type {
  DehydratedState,
  DehydrateOptions,
  DenormalizedQuery,
  DenormalizedState,
  HydrateOptions,
  NormalizedQuery,
  NormalizedState,
} from './ssr';
```

and extend the existing `export type { ... } from './cache'` list with `CachedQuery` and `RestoreInput`.

Update the module doc comment at the top of `index.ts` by adding a paragraph before the closing one:

```
 * **Server rendering**: `dehydrate` serializes a cache for an HTML response and
 * `hydrate` reads it back. What may cross that boundary is a property of the
 * API rather than a caution in the docs -- the payload holds only the entities
 * the exported queries actually reference, and both sides assert the principal.
```

In `packages/client-core/package.json`, add a size-limit entry after the `stream binding` one:

```json
    {
      "name": "ssr",
      "path": "dist/index.js",
      "import": "{ dehydrate, hydrate }",
      "limit": "1.5 kB",
      "gzip": true
    },
```

and raise the `core with streams` limit from `"14 kB"` to `"15 kB"`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd packages/client-core && npx vitest run && npm run typecheck && npm run size`
Expected: all tests PASS; every size-limit entry within budget. If `core, REST only` (9 kB) has moved at all, stop — `dehydrate`/`hydrate` must tree-shake out of that import set, and a regression there means something in `cache.ts` grew more than intended.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/client.ts packages/client-core/src/index.ts \
        packages/client-core/package.json packages/client-core/__tests__/client.test.ts
git commit -m "feat(client): give a query handle a real server snapshot, and export the SSR surface"
```

---

### Task 8: The React hydration boundary and the server snapshot

**Files:**
- Create: `packages/client-react/src/hydration.ts`
- Modify: `packages/client-react/src/useQuery.ts:33-81` and `:173`
- Modify: `packages/client-react/src/index.ts`
- Modify: `packages/client-react/package.json` (size-limit)
- Test: `packages/client-react/__tests__/ssr.test.tsx`

**Interfaces:**
- Consumes: `dehydrate`, `hydrate`, `DehydratedState`, `HydrateOptions` from `@forge-go/client-core`; `QueryHandle.getServerState` from Task 7.
- Produces:
  - `interface ForgeHydrationBoundaryProps { readonly state: DehydratedState | undefined; readonly ops: Readonly<Record<string, OperationMeta>>; readonly client?: QueryCache; readonly stale?: boolean; readonly children?: ReactNode }`
  - `function ForgeHydrationBoundary(props: ForgeHydrationBoundaryProps): ReactNode`

- [ ] **Step 1: Build the core so the adapter resolves it**

Run: `cd packages/client-core && npm run build`
Expected: `dist/ssr.js`, `dist/wire.js` and updated `dist/index.d.ts` exist.

- [ ] **Step 2: Write the failing test**

Create `packages/client-react/__tests__/ssr.test.tsx`:

```tsx
import { StrictMode } from 'react';
import { renderToString } from 'react-dom/server';
import { act, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { dehydrate } from '@forge-go/client-core';
import type { DehydratedState } from '@forge-go/client-core';
import { ForgeHydrationBoundary, ForgeProvider, useQuery } from '../src';
import { harness, orderList, useOrderList } from './harness';
import type { Order } from './harness';

const ops = { orderList };

function Orders(): JSX.Element {
  const { status, data } = useQuery<Order[]>(useOrderList);

  return (
    <ul data-testid="orders" data-status={status}>
      {(data ?? []).map((order) => (
        <li key={order.id}>{order.total}</li>
      ))}
    </ul>
  );
}

/** A server render for one request: its own cache, prefetched, then dehydrated. */
async function serverRender(): Promise<{ html: string; state: DehydratedState }> {
  const server = harness(() => [{ id: 7, total: 99 }]);

  await server.cache.fetch(orderList);

  const state = dehydrate(server.cache, { principal: undefined });
  const html = renderToString(
    <ForgeProvider client={server.cache}>
      <ForgeHydrationBoundary state={state} ops={ops}>
        <Orders />
      </ForgeHydrationBoundary>
    </ForgeProvider>,
  );

  return { html, state: JSON.parse(JSON.stringify(state)) as DehydratedState };
}

describe('server rendering', () => {
  it('emits the data rather than the loading branch', async () => {
    const { html } = await serverRender();

    expect(html).toContain('data-status="success"');
    expect(html).toContain('99');
  });
});

describe('hydrating', () => {
  it('renders the server data on the first pass and issues no request', async () => {
    const { state } = await serverRender();
    const client = harness(() => {
      throw new Error('a hydrated query must not fetch');
    });

    await act(async () => {
      render(
        <ForgeProvider client={client.cache}>
          <ForgeHydrationBoundary state={state} ops={ops}>
            <Orders />
          </ForgeHydrationBoundary>
        </ForgeProvider>,
      );
    });

    expect(screen.getByTestId('orders').dataset.status).toBe('success');
    expect(screen.getByText('99')).toBeTruthy();
    expect(client.transport.calls).toHaveLength(0);
  });

  it('hydrates once under StrictMode, whose renders are double-invoked', async () => {
    const { state } = await serverRender();
    const client = harness(() => {
      throw new Error('a hydrated query must not fetch');
    });

    await act(async () => {
      render(
        <StrictMode>
          <ForgeProvider client={client.cache}>
            <ForgeHydrationBoundary state={state} ops={ops}>
              <Orders />
            </ForgeHydrationBoundary>
          </ForgeProvider>
        </StrictMode>,
      );
    });

    expect(client.cache.store.getRecord('Order:7')?.version).toBe(1);
  });

  it('refetches on mount when hydrated stale', async () => {
    const { state } = await serverRender();
    const client = harness(() => [{ id: 7, total: 120 }]);

    await act(async () => {
      render(
        <ForgeProvider client={client.cache}>
          <ForgeHydrationBoundary state={state} ops={ops} stale>
            <Orders />
          </ForgeHydrationBoundary>
        </ForgeProvider>,
      );
    });

    expect(client.transport.calls).toHaveLength(1);
  });

  it('renders children unchanged when there is nothing to hydrate', async () => {
    const client = harness(() => [{ id: 7, total: 99 }]);

    await act(async () => {
      render(
        <ForgeProvider client={client.cache}>
          <ForgeHydrationBoundary state={undefined} ops={ops}>
            <Orders />
          </ForgeHydrationBoundary>
        </ForgeProvider>,
      );
    });

    expect(screen.getByTestId('orders')).toBeTruthy();
  });
});
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd packages/client-react && npx vitest run __tests__/ssr.test.tsx`
Expected: FAIL — `ForgeHydrationBoundary` is not exported.

- [ ] **Step 4: Write the implementation**

Create `packages/client-react/src/hydration.ts`:

```ts
import type { ReactNode } from 'react';
import { hydrate } from '@forge-go/client-core';
import type { DehydratedState, OperationMeta, QueryCache } from '@forge-go/client-core';
import { useForgeClient } from './context';

/**
 * Which payloads have already been hydrated into which cache.
 *
 * Keyed on the cache first because the same payload legitimately hydrates two
 * of them -- this component renders on the server as well, against the cache
 * that produced the payload, and again on the client against a fresh one.
 *
 * An optimisation rather than a correctness requirement: `hydrate` merges, and
 * a record written with identical data keeps its previous object and bumps no
 * version. What this buys is that StrictMode's double-invoked render does not
 * walk the payload twice.
 */
const hydrated = new WeakMap<QueryCache, WeakSet<object>>();

export interface ForgeHydrationBoundaryProps {
  /** The payload from `dehydrate`, after whatever transport carried it. */
  readonly state: DehydratedState | undefined;
  /** The generated `ops.ts` table, passed verbatim. */
  readonly ops: Readonly<Record<string, OperationMeta>>;
  /** Use this cache rather than the provided or configured one. */
  readonly client?: QueryCache;
  /** Settle the hydrated queries behind the server, so mounting refetches. */
  readonly stale?: boolean;
  readonly children?: ReactNode;
}

/**
 * Hydrate a payload into the cache this subtree reads from.
 *
 * **It hydrates during render, not in an effect.** Children read `getSnapshot`
 * during their own render, which happens after this one returns, so a
 * render-phase hydrate is visible to them on the first pass. An effect runs
 * after the tree commits: the first paint would be the loading branch and then
 * flip, which is a visible flash and, on the hydration pass, exactly the
 * mismatch this component exists to remove.
 *
 * Rendering no element of its own is deliberate. A wrapper would change the DOM
 * the server and client compare, for a component whose entire job is to make
 * those two agree.
 */
export function ForgeHydrationBoundary(props: ForgeHydrationBoundaryProps): ReactNode {
  const client = useForgeClient(props.client);
  const { state } = props;

  if (state !== undefined) {
    let seen = hydrated.get(client);

    if (seen === undefined) {
      seen = new WeakSet<object>();
      hydrated.set(client, seen);
    }

    if (!seen.has(state)) {
      seen.add(state);
      hydrate(client, state, {
        ops: props.ops,
        ...(props.stale === true ? { stale: true } : {}),
      });
    }
  }

  return props.children ?? null;
}
```

In `packages/client-react/src/useQuery.ts`, delete the `IDLE` constant and the `serverSnapshot` function together with their comment block (`:33-81`), and replace the `useSyncExternalStore` call at `:173` with:

```ts
  const state = useSyncExternalStore(handle.subscribe, handle.getState, handle.getServerState);
```

Replace the deleted comment block with a short one above the `useQuery` export:

```ts
/**
 * The server snapshot now comes from the handle, which reads it out of the
 * cache with `peek` -- see `QueryHandle.getServerState`. This file used to hold
 * a frozen `idle` constant instead, because no store serialisation existed and
 * returning server-fetched data would have been a guaranteed hydration mismatch
 * rather than an optimisation. With `ForgeHydrationBoundary` above the tree,
 * both sides read the same warm cache and a server render emits real markup.
 */
```

In `packages/client-react/src/index.ts`, add:

```ts
export { ForgeHydrationBoundary } from './hydration';
export type { ForgeHydrationBoundaryProps } from './hydration';
```

and replace the final line of the module doc comment (`Streaming (\`live: true\`), devtools and SSR hydration land in later chunks.`) with:

```
 * SSR is here: `ForgeHydrationBoundary` hydrates a payload from `dehydrate`
 * during render, and a server render emits real markup rather than a spinner.
```

In `packages/client-react/package.json`, raise the `adapter` size-limit from `"2 kB"` to `"2.5 kB"`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd packages/client-react && npx vitest run && npm run typecheck && npm run size`
Expected: all PASS, including every pre-existing test in `useQuery.test.tsx`, `useQuery.live.test.tsx`, `context.test.tsx` and `useMutation.test.tsx`.

- [ ] **Step 6: Commit**

```bash
git add packages/client-react/src/hydration.ts packages/client-react/src/useQuery.ts \
        packages/client-react/src/index.ts packages/client-react/package.json \
        packages/client-react/__tests__/ssr.test.tsx
git commit -m "feat(react): hydrate during render, and emit real markup from a server render"
```

---

### Task 9: Documentation

**Files:**
- Create: `docs/content/docs/web-client/ssr.mdx`
- Modify: `docs/content/docs/web-client/meta.json`
- Modify: `docs/content/docs/web-client/not-yet-shipped.mdx`
- Modify: `packages/client-core/README.md`
- Modify: `packages/client-react/README.md`

- [ ] **Step 1: Add the page to the sidebar**

In `docs/content/docs/web-client/meta.json`, insert `"ssr"` into `pages` immediately after `"adapters"`:

```json
    "---Browser runtime---",
    "runtime",
    "adapters",
    "ssr",
    "devtools",
```

- [ ] **Step 2: Write the SSR page**

Create `docs/content/docs/web-client/ssr.mdx`. It must cover, in this order:

1. Frontmatter: `title: Server rendering`, `description: Prefetch on the server, hydrate on the client, and emit real markup`, `icon: Server`.
2. The Next.js App Router example from the spec's "Package placement" section, verbatim — a server component building a per-request cache, calling `setPrincipal`, prefetching, and passing `dehydrate(...)` to a client component that wraps its tree in `<ForgeHydrationBoundary state={state} ops={ops}>`.
3. **A `<Callout type="warn">` on the principal**: `dehydrate` requires it, asserts it against the cache owner, and `hydrate` refuses a payload built for anyone else. State plainly that a payload is server state embedded in an HTML response, and that the payload holds only the entities the exported queries reference — an entity nothing exported points at cannot be in it.
4. `include`, for exporting a subset.
5. The two modes as a table: `normalized` (default; smallest; dedupes shared entities; serializes entity cycles) and `denormalized` (no revive pass; duplicates shared entities; **cannot** serialize a query whose value contains an entity cycle).
6. Freshness: `stale` on both `hydrate` and the boundary, with the SSR-versus-SSG framing from the spec.
7. A short "Why a per-request cache" section: the module-level cache is shared between concurrent server requests, and while the reachability closure means one request cannot export another's entities, a per-request cache is still the correct shape — pointing at `ForgeProvider`.
8. A closing note that Vue and Angular can call `dehydrate`/`hydrate` directly but ship no boundary component.

- [ ] **Step 3: Correct `not-yet-shipped.mdx`**

Delete the whole `## SSR `dehydrate` / `hydrate` is not built` section, including its claim about `packages/nextjs-plugin`.

Add to the `## Smaller gaps in the runtime` list:

```md
- **SSR ships for React only.** `dehydrate`/`hydrate` are framework-agnostic and the Vue and Angular adapters can call them directly, but neither ships a hydration boundary component or a server-snapshot path. There is also no streamed-payload injection helper: multiple boundaries work, but flushing a payload mid-stream is the application's job.
- **A denormalized payload cannot carry an entity cycle.** `dehydrate`'s default `normalized` mode serializes `Order → Customer → Orders[] → Order` without difficulty, because it closes through references. `mode: 'denormalized'` ships the rehydrated value, which *is* such a cycle, and throws.
```

Add to the `## What is shipped` list, after the React/Vue/Angular adapters line:

```md
- SSR `dehydrate`/`hydrate`, with a reachability-closed payload and principal assertions on both sides
```

Then re-read the page's opening paragraph and closing line (`The gap is two designed features and a handful of runtime edges…`) and correct the count: one designed feature remains unbuilt (capability gating), not two.

- [ ] **Step 4: Update `packages/client-core/README.md`**

Delete the first bullet of `## Known gaps, deliberately left to later chunks` — the one beginning `SSR revival.`

Add a `## Server rendering` section after `## Stream binding`, covering:
- the `dehydrate`/`hydrate` signatures;
- the reachability closure as the security property, in the README's voice — that the payload is *built* by a walk rather than read off the store, so an entity nothing exported references cannot be in it;
- the `__ref` collision and the escape scheme, because that is the non-obvious part and the README is where this codebase records non-obvious parts;
- the two modes and the entity-cycle limitation of the denormalized one;
- that `deps` are recomputed from the skeleton rather than trusted from the wire, and `version`/`frameAt` are not carried because the frame clock is per session.

In the size-budget section, add a sentence recording that `core with streams` moved from 14 kB to 15 kB to admit `ssr.ts`, that `dehydrate`/`hydrate` tree-shake out of an application that never imports them, and that the two application-facing budgets — `core, REST only` at 9 kB and the per-surface entries — did not move.

- [ ] **Step 5: Update `packages/client-react/README.md`**

Add a `## Server rendering` section: the boundary, that it hydrates during render and why, the `ops` prop, `stale`, and that `getServerSnapshot` now returns real state through `peek`. Remove any line claiming SSR is a later chunk.

- [ ] **Step 6: Verify the docs build**

Run: `cd docs && npm run build` (or `pnpm build`, matching whatever the `docs` package declares)
Expected: the build succeeds and `web-client/ssr` appears in the generated sidebar.

- [ ] **Step 7: Commit**

```bash
git add docs/content/docs/web-client packages/client-core/README.md packages/client-react/README.md
git commit -m "docs(client): document SSR dehydrate/hydrate and retire the not-yet-shipped section"
```

---

### Task 10: Full verification and the website mirror

**Files:**
- No source changes. Mirrors generated docs into `/Users/rexraphael/Work/xraph/website`.

- [ ] **Step 1: Run everything, from a clean build**

```bash
cd packages/client-core && npm run build && npx vitest run && npm run typecheck && npm run size
```

Expected: build clean, all tests PASS, no type errors, every size-limit entry within budget.

- [ ] **Step 2: Run the adapter against the freshly built core**

```bash
cd packages/client-react && npx vitest run && npm run typecheck && npm run size
```

Expected: all PASS. Also run the Vue and Angular adapters, which consume the same `dist` and must not have regressed:

```bash
cd packages/client-vue && npx vitest run
cd packages/client-angular && npx vitest run
```

- [ ] **Step 3: Mirror the docs to the website**

```bash
cd /Users/rexraphael/Work/xraph/website && pnpm docs:import /Users/rexraphael/Work/xraph/forge forge v1
```

Expected: `content/docs/forge/v1/web-client/ssr.mdx` appears and `not-yet-shipped.mdx` is updated. **That tree is generated — never hand-edit it.** If the import reports a failure, fix the source under `docs/content/docs/` in the forge repo and re-run rather than touching the mirror.

- [ ] **Step 4: Commit the mirror**

```bash
cd /Users/rexraphael/Work/xraph/website
git add content/docs/forge/v1
git commit -m "docs(forge): mirror v1 web-client SSR page"
```

- [ ] **Step 5: Report**

State plainly: the test counts for each package, whether every size budget held, and which budget figure moved and why.

---

## Self-Review

**Spec coverage.** Every spec section maps to a task: the revive pass and escaping → Task 1; `SettleResult.tags` → Task 2; `peek`/`restore`/`settledQueries` → Task 3; the payload, the closure, the principal, the modes and the cycle errors → Tasks 4–5; the round-trip property and the generator fix → Task 6; `getServerState`, exports and budgets → Task 7; the boundary and `getServerSnapshot` → Task 8; every documentation change including the `nextjs-plugin` correction → Task 9; the website mirror → Task 10.

**Type consistency.** `EncodeContext`/`EncodeResult` (Task 1) are consumed unchanged in Task 4. `RestoreInput` (Task 3) is what `hydrate` passes in Task 5. `CachedQuery` (Task 3) is what `select` returns in Task 4. `HydrateOptions` (Task 5) is what the boundary spreads in Task 8. `operationName` is exported in Task 3 and imported in Tasks 4 and 5.

**Known judgement calls left to the implementer.** Task 6 says to match the existing arbitrary's name rather than assuming `response`, and Task 7 says to match `client.test.ts`'s existing cache helper. Both are named explicitly because guessing them from here would be inventing an API that already exists.
