import fc from 'fast-check';
import { describe, expect, it } from 'vitest';

import { normalize } from '../src/normalize';
import { isRef } from '../src/ref';
import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { dehydrate, hydrate } from '../src/ssr';
import type { DehydratedState } from '../src/ssr';
import { EntityStore, denormalize } from '../src/store';
import type { OperationMeta } from '../src/transport';
import { schema } from './schema';

/**
 * Response trees, generated to hit the shapes hand-written cases miss:
 * arrays of arrays, nullable entity references, entities at varying depth,
 * the same entity twice, and objects that are not entities.
 *
 * Ids come from small pools on purpose. A generator drawing unique ids never
 * produces the collision that exercises merging, and merging is where the
 * store either preserves a field another view reads or silently drops it.
 */
const orderId = fc.constantFrom(1, 2, 3, 'a', 'b');
const customerId = fc.constantFrom('c-1', 'c-2');
const sku = fc.constantFrom('SKU-1', 'SKU-2');

const scalar = fc.oneof(
  fc.integer(),
  fc.string(),
  fc.boolean(),
  fc.constant(null),
  fc.double({ noNaN: true, noDefaultInfinity: true }),
);

/**
 * `__proto__` is not a data property: assigning it walks a setter instead of
 * creating a key, so neither the runtime nor this file's reference
 * implementation can round-trip it. It is out of scope, not a defect.
 *
 * `__ref` and its escape forms are drawn deliberately rather than left to
 * chance. They are the names that collide with the SSR wire encoding -- a
 * response may legitimately contain `{__ref: "anything"}`, which `normalize`
 * leaves inline and a naive revive pass would turn into a reference to nothing.
 * An arbitrary string generator will never produce one, which is exactly how
 * that collision stayed a comment in `ref.ts` instead of becoming a test.
 */
const propertyName = fc
  .oneof(
    { weight: 3, arbitrary: fc.string({ minLength: 1 }) },
    { weight: 1, arbitrary: fc.constantFrom('__ref', '___ref', '____ref', '__refs', '_ref') },
  )
  .filter((key) => key !== '__proto__');

/** A plain object or array with no entity in it, at arbitrary depth. */
const plain: fc.Arbitrary<unknown> = fc.letrec((tie) => ({
  node: fc.oneof(
    { depthSize: 'small' },
    scalar,
    fc.array(tie('node'), { maxLength: 4 }),
    fc.dictionary(propertyName, tie('node'), { maxKeys: 4 }),
  ),
})).node;

const lineItem = fc.record({
  sku,
  qty: fc.integer({ min: 0, max: 9 }),
});

/**
 * A type whose identity is not `id`, the `ForgeEntity()` escape. Empty and
 * null invoice numbers are generated on purpose: a declared type that does
 * not carry a usable identity has to stay inline rather than key a record
 * under `Invoice:` or `Invoice:null`.
 */
const invoice = fc.record(
  {
    invoiceNumber: fc.constantFrom('INV-1', 'INV-2', '', null),
    amount: fc.integer(),
  },
  { requiredKeys: ['amount'] },
);

/**
 * Orders and customers reference each other, so an entity turns up at several
 * depths, the same one turns up twice, and merging the duplicates produces the
 * mutual `Order → Customer → Orders[] → Order` cycle an ORM with eager
 * loading yields. The recursive fields go through `fc.oneof` with a depth
 * bias so the graph terminates instead of growing exponentially.
 */
const graph = fc.letrec((tie) => ({
  order: fc.record(
    {
      id: orderId,
      total: fc.integer(),
      // Nullable reference: the field is present and empty, which is not the
      // same as the field being absent.
      customer: fc.oneof({ depthSize: 'small' }, fc.constant(null), tie('customer')),
      invoice: fc.oneof({ depthSize: 'small' }, fc.constant(null), invoice),
      items: fc.oneof(
        fc.array(lineItem, { maxLength: 3 }),
        // Arrays of arrays.
        fc.array(fc.array(lineItem, { maxLength: 2 }), { maxLength: 2 }),
      ),
      related: fc.oneof(
        { depthSize: 'small' },
        fc.constant([]),
        fc.array(tie('order'), { maxLength: 2 }),
      ),
      notes: plain,
    },
    { requiredKeys: ['id'] },
  ),
  customer: fc.record(
    {
      id: customerId,
      name: fc.string(),
      // The back-edge. Without it no mutual Order/Customer cycle is ever
      // generated, and the cycle handling is only exercised by hand-written
      // cases.
      orders: fc.oneof(
        { depthSize: 'small' },
        fc.constant([]),
        fc.array(tie('order'), { maxLength: 2 }),
      ),
      meta: plain,
    },
    { requiredKeys: ['id'] },
  ),
}));

const order = graph.order as fc.Arbitrary<unknown>;
const customer = graph.customer as fc.Arbitrary<unknown>;

/** What an operation can hand the runtime: an entity, a list, or a wrapper. */
const response = fc.oneof(
  fc.record({ value: order, type: fc.constant('Order' as const) }),
  fc.record({ value: fc.array(order, { maxLength: 4 }), type: fc.constant('Order' as const) }),
  // Rooted at the other side of the cycle.
  fc.record({ value: customer, type: fc.constant('Customer' as const) }),
  // Rooted at the type whose identity is not `id`.
  fc.record({ value: invoice, type: fc.constant('Invoice' as const) }),
  fc.record({ value: fc.array(invoice, { maxLength: 3 }), type: fc.constant('Invoice' as const) }),
  fc.record({
    value: fc.record({
      data: order,
      items: fc.array(order, { maxLength: 3 }),
      invoice: fc.oneof(fc.constant(null), invoice),
      meta: plain,
    }),
    type: fc.constant('Envelope' as const),
  }),
  // No declared type at all: nothing may be lifted.
  fc.record({ value: plain, type: fc.constant(undefined) }),
);

/**
 * The expected value of a round trip.
 *
 * It is not the input. When one entity occurs twice with different fields,
 * the store holds their union and both positions rehydrate to it -- that is
 * the whole point of a normalized cache, and asserting against the raw input
 * would be asserting that normalization did not happen. This recomputes the
 * union the same way the store does, from the input alone.
 *
 * The union can be cyclic where the input was not: `{id: 1, related: [{id: 1}]}`
 * is a finite tree, and there is exactly one `Order:1`, so the record that
 * results holds a reference to itself. Rebuilding therefore memoizes per key
 * the same way the store does, or it recurses forever.
 */
function expected(value: unknown, type: string | undefined): unknown {
  const merged = new Map<string, Record<string, unknown>>();

  collect(value, type);

  function collect(node: unknown, hint: string | undefined): void {
    if (node === null || typeof node !== 'object') return;

    if (Array.isArray(node)) {
      for (const item of node) collect(item, hint);

      return;
    }

    const obj = node as Record<string, unknown>;
    const meta = hint === undefined ? undefined : schema[hint];
    const idField = meta?.idField;
    const id = idField === undefined ? undefined : obj[idField];
    const key = isKeyable(id) ? `${hint}:${String(id)}` : undefined;

    for (const field of Object.keys(obj)) collect(obj[field], meta?.fields?.[field]);

    if (key === undefined) return;
    merged.set(key, { ...(merged.get(key) ?? {}), ...obj });
  }

  const built = new Map<string, Record<string, unknown>>();

  return rebuild(value, type);

  function rebuild(node: unknown, hint: string | undefined): unknown {
    if (node === null || typeof node !== 'object') return node;

    if (Array.isArray(node)) return node.map((item) => rebuild(item, hint));

    const obj = node as Record<string, unknown>;
    const meta = hint === undefined ? undefined : schema[hint];
    const idField = meta?.idField;
    const id = idField === undefined ? undefined : obj[idField];
    const key = isKeyable(id) ? `${hint}:${String(id)}` : undefined;

    if (key !== undefined) {
      const done = built.get(key);

      if (done !== undefined) return done;
    }

    const source =
      key === undefined ? obj : (merged.get(key) as Record<string, unknown>);
    const out: Record<string, unknown> = {};

    if (key !== undefined) built.set(key, out);

    for (const field of Object.keys(source)) {
      out[field] = rebuild(source[field], meta?.fields?.[field]);
    }

    return out;
  }
}

function isKeyable(id: unknown): id is string | number {
  if (typeof id === 'string') return id.length > 0;

  return typeof id === 'number' && Number.isFinite(id);
}

/** Every object identity reachable from a value, cycle-safe. */
function subtrees(value: unknown, out: object[] = [], seen = new Set<unknown>()): object[] {
  if (value === null || typeof value !== 'object' || seen.has(value)) return out;

  seen.add(value);
  out.push(value);

  for (const child of Array.isArray(value) ? value : Object.values(value)) {
    subtrees(child, out, seen);
  }

  return out;
}

/** The entity keys each record references, at any depth. */
function referenceGraph(store: EntityStore): Map<string, Set<string>> {
  const edges = new Map<string, Set<string>>();

  for (const key of [...store.keys()]) {
    const out = new Set<string>();

    (function walk(node: unknown): void {
      if (node === null || typeof node !== 'object') return;

      if (isRef(node)) {
        out.add(node.__ref);

        return;
      }

      for (const child of Array.isArray(node) ? node : Object.values(node)) walk(child);
    })(store.getRecord(key)?.data);

    edges.set(key, out);
  }

  return edges;
}

// A generator that never produces the shape it was written for is a test
// that passes for the wrong reason. The first version of this file drew
// `Invoice` -- the non-`id` identity case -- zero times in 3999 runs, and
// had no Customer-to-Order back-edge at all, so no mutual cycle was ever
// generated and the cycle handling was only ever exercised by hand.
describe('generator coverage', () => {
  it('reaches the shapes it claims to', () => {
    const samples = fc.sample(response, { numRuns: 600, seed: 20260803 });

    let invoices = 0;
    let refused = 0;
    let mutualCycles = 0;

    for (const { value, type } of samples) {
      const store = new EntityStore();
      const { deps } = store.write(value, schema, type);

      if ([...deps].some((key) => key.startsWith('Invoice:'))) invoices++;

      // An Invoice-shaped object whose invoiceNumber cannot key a record.
      if (JSON.stringify(value).includes('"invoiceNumber":""')) refused++;

      const edges = referenceGraph(store);

      for (const [from, to] of edges) {
        if (!from.startsWith('Order:')) continue;

        for (const other of to) {
          if (other.startsWith('Customer:') && edges.get(other)?.has(from)) mutualCycles++;
        }
      }
    }

    expect(invoices).toBeGreaterThan(0);
    expect(refused).toBeGreaterThan(0);
    expect(mutualCycles).toBeGreaterThan(0);
  });
});

describe('round trip', () => {
  it('denormalize(normalize(x)) equals x, modulo entity merging', () => {
    fc.assert(
      fc.property(response, ({ value, type }) => {
        const store = new EntityStore();
        const { skeleton } = store.write(value, schema, type);

        expect(denormalize(skeleton, store)).toEqual(expected(value, type));
      }),
      { numRuns: 400 },
    );
  });

  it('leaves no entity data inline in the skeleton', () => {
    fc.assert(
      fc.property(response, ({ value, type }) => {
        const { skeleton, records, deps } = normalize(value, schema, type);

        // Every key the pass reported has a record behind it.
        for (const key of deps) expect(records.has(key)).toBe(true);

        // And every reference in the skeleton resolves.
        for (const node of subtrees(skeleton)) {
          for (const child of Array.isArray(node) ? node : Object.values(node)) {
            if (isRef(child)) expect(records.has(child.__ref)).toBe(true);
          }
        }

        if (isRef(skeleton)) expect(records.has(skeleton.__ref)).toBe(true);
      }),
      { numRuns: 400 },
    );
  });

  it('does not mutate the response it was given', () => {
    fc.assert(
      fc.property(response, ({ value, type }) => {
        const before = structuredClone(value);

        normalize(value, schema, type);

        expect(value).toEqual(before);
      }),
      { numRuns: 200 },
    );
  });

  it('writing the same response twice bumps no version', () => {
    fc.assert(
      fc.property(response, ({ value, type }) => {
        const store = new EntityStore();

        store.write(value, schema, type);
        const writes = store.version;
        store.write(value, schema, type);

        expect(store.version).toBe(writes);
      }),
      { numRuns: 400 },
    );
  });

  it('reads with no write between are referentially identical, everywhere', () => {
    fc.assert(
      fc.property(response, ({ value, type }) => {
        const store = new EntityStore();
        const { skeleton } = store.write(value, schema, type);

        const first = denormalize(skeleton, store);
        const second = denormalize(skeleton, store);

        expect(second).toBe(first);

        const a = subtrees(first);
        const b = subtrees(second);

        expect(b.length).toBe(a.length);
        for (let i = 0; i < a.length; i++) expect(b[i]).toBe(a[i]);
      }),
      { numRuns: 400 },
    );
  });

  it('a write to one entity leaves every subtree that excludes it identical', () => {
    fc.assert(
      fc.property(response, fc.integer(), ({ value, type }, bump) => {
        const store = new EntityStore();
        const { skeleton, deps } = store.write(value, schema, type);

        const keys = [...deps];

        fc.pre(keys.length > 0);

        const target = keys[Math.abs(bump) % keys.length] as string;
        const before = denormalize(skeleton, store);
        const beforeIds = new Set(subtrees(before));

        store.put(target, { __probe: bump });

        const after = denormalize(skeleton, store);

        // Anything that survived by identity must not contain the probe.
        for (const node of subtrees(after)) {
          if (!beforeIds.has(node)) continue;

          expect(containsProbe(node, bump)).toBe(false);
        }

        // And the change is visible.
        expect(JSON.stringify(store.getRecord(target)?.data)).toContain('__probe');
      }),
      { numRuns: 300 },
    );
  });

  // The other half of dependency tracking: a write nothing depends on must
  // recompute nothing at all. Without it, "only what changed" degrades to
  // "everything, on every write" and no test notices.
  it('a write to an unrelated entity recomputes nothing', () => {
    fc.assert(
      fc.property(response, ({ value, type }) => {
        const store = new EntityStore();
        const { skeleton, deps } = store.write(value, schema, type);

        const unrelated = 'Order:not-in-this-response';

        fc.pre(!deps.has(unrelated));

        const before = denormalize(skeleton, store);

        store.put(unrelated, { id: 'not-in-this-response' });

        expect(denormalize(skeleton, store)).toBe(before);
      }),
      { numRuns: 400 },
    );
  });
});

function containsProbe(node: object, bump: number): boolean {
  return (node as Record<string, unknown>).__probe === bump;
}

/**
 * One operation per root typename, so `rootType` matches the sample.
 *
 * The path doubles as the operation name, which is what `dehydrate` writes onto
 * the wire and `hydrate` looks back up.
 */
function metaFor(type: string | undefined): OperationMeta {
  // `undefined` is one of the shapes the generator draws on purpose: a response
  // with no declared type, where nothing may be lifted and the skeleton is the
  // whole value. It still has to survive the round trip.
  return {
    method: 'GET',
    path: `/${(type ?? 'untyped').toLowerCase()}`,
    provides: [],
    invalidates: [],
    ...(type === undefined ? {} : { entity: type, rootType: type }),
  };
}

/** A cache whose transport must never be reached: these properties fetch nothing. */
function ssrCache(): QueryCache {
  return new QueryCache({
    transport: {
      execute: () => {
        throw new Error('the SSR properties issue no requests');
      },
    },
    entities: schema,
    scheduler: manualScheduler().schedule,
  });
}

/** A server cache holding one settled query for this sample. */
function rendered(
  value: unknown,
  type: string | undefined,
): { cache: QueryCache; meta: OperationMeta } {
  const cache = ssrCache();
  const meta = metaFor(type);
  const { skeleton } = cache.store.write(value, schema, type);

  cache.restore(meta, undefined, { skeleton, tags: [] });

  return { cache, meta };
}

/**
 * Whether a tree contains a negative zero anywhere.
 *
 * The generated values are trees, so this needs no cycle guard.
 */
function hasNegativeZero(node: unknown): boolean {
  if (typeof node === 'number') return Object.is(node, -0);
  if (node === null || typeof node !== 'object') return false;
  if (Array.isArray(node)) return node.some(hasNegativeZero);

  return Object.values(node as Record<string, unknown>).some(hasNegativeZero);
}

/**
 * The same responses, minus the ones JSON itself cannot carry.
 *
 * `-0` serializes as `0` and comes back as `0`. That is a property of JSON
 * rather than of this encoding -- `structuredClone` preserves it and
 * `JSON.stringify` never has -- so it is out of scope here for the same reason
 * `__proto__` is, and is recorded in the docs rather than worked around.
 */
const jsonSafe = response.filter(({ value }) => !hasNegativeZero(value));

describe('the SSR round trip', () => {
  it('hydrates to the value the server rendered, through JSON', () => {
    fc.assert(
      fc.property(jsonSafe, ({ value, type }) => {
        const server = rendered(value, type);
        const expected = server.cache.getState(server.meta).data;

        const wire = JSON.parse(
          JSON.stringify(dehydrate(server.cache, { principal: undefined })),
        ) as DehydratedState;

        const client = ssrCache();
        hydrate(client, wire, { ops: { op: server.meta } });

        expect(client.getState(server.meta).data).toEqual(expected);
      }),
      { numRuns: 150 },
    );
  });

  it('recomputes the dependency set it started with', () => {
    fc.assert(
      fc.property(jsonSafe, ({ value, type }) => {
        const server = rendered(value, type);
        const before = [
          ...(server.cache.registry.get(server.cache.key(server.meta))?.deps ?? []),
        ].sort();

        const wire = JSON.parse(
          JSON.stringify(dehydrate(server.cache, { principal: undefined })),
        ) as DehydratedState;

        const client = ssrCache();
        hydrate(client, wire, { ops: { op: server.meta } });

        expect([...(client.registry.get(client.key(server.meta))?.deps ?? [])].sort()).toEqual(
          before,
        );
      }),
      { numRuns: 150 },
    );
  });
});
