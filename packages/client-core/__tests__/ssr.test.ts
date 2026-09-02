import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { isRef } from '../src/ref';
import {
  dehydrate,
  hydrate,
  hydrateBoundary,
  hydrationFailure,
  streamingDehydrator,
} from '../src/ssr';
import type { DehydratedState, DenormalizedState, NormalizedState } from '../src/ssr';
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

/**
 * A cache owned by `u-1`.
 *
 * Owned rather than anonymous because `dehydrate` asserts the principal against
 * the owner, and a cache with no owner could only ever be dehydrated for
 * `undefined` -- which would quietly turn every test below into a test of the
 * one case it is not trying to exercise.
 */
function cache(handler: (request: TransportRequest, call: number) => unknown): QueryCache {
  return cacheOwnedBy('u-1', handler);
}

/**
 * The same, for a named owner.
 *
 * Separate rather than a defaulted parameter: `undefined` is a *meaningful*
 * principal here -- an application that never calls `setPrincipal` -- and a
 * default would silently turn the test for it into a test for `u-1`.
 */
function cacheOwnedBy(
  principal: unknown,
  handler: (request: TransportRequest, call: number) => unknown,
  onError?: (error: unknown, context: string) => void,
): QueryCache {
  const client = new QueryCache({
    transport: fakeTransport(handler),
    entities: schema,
    scheduler: manualScheduler().schedule,
    ...(onError === undefined ? {} : { onError }),
  });

  client.setPrincipal(principal);

  return client;
}

describe('dehydrate, normalized', () => {
  it('emits the skeleton, the reachable records and the resolved tags', async () => {
    const client = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }]);

    await client.fetch(orderList);

    const state = dehydrate(client, { principal: 'u-1' }) as NormalizedState;

    expect(state.v).toBe(1);
    expect(state.mode).toBe('normalized');
    expect(state.principal).toBe('u-1');
    expect(state.queries).toHaveLength(1);
    expect(state.queries[0]?.operation).toBe('GET /orders');
    expect(state.queries[0]?.args).toBeUndefined();
    expect(state.queries[0]?.skeleton).toEqual([{ __ref: 'Order:7' }]);
    expect(state.queries[0]?.tags).toEqual(
      expect.arrayContaining(['Order[]', 'Order:7', 'Customer:c-3']),
    );
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
    const client = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', orders: [{ id: 7 }] } }]);

    await client.fetch(orderList);

    const state = dehydrate(client, { principal: 'u-1' }) as NormalizedState;

    expect(state.records['Customer:c-3']).toEqual({ id: 'c-3', orders: [{ __ref: 'Order:7' }] });
  });

  it('escapes a record field shaped like a reference', async () => {
    const client = cache(() => [{ id: 7, meta: { __ref: 'not a reference' } }]);

    await client.fetch(orderList);

    const state = dehydrate(client, { principal: 'u-1' }) as NormalizedState;

    expect(state.records['Order:7']).toEqual({ id: 7, meta: { ___ref: 'not a reference' } });
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

    await client.fetch(orderList);

    expect(() => dehydrate(client, { principal: 'u-2' })).toThrow(
      /\[forge\] dehydrate: principal does not match the cache owner/,
    );
  });

  it('accepts an unset principal on both sides', async () => {
    const client = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(dehydrate(client, { principal: undefined }).principal).toBeUndefined();
  });

  it('refuses a principal that cannot survive JSON', async () => {
    const owner = { id: 'u-1' };
    const client = cacheOwnedBy(owner, () => [{ id: 7, total: 99 }]);

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
        value: [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }],
        // A real reading of this cache's clock, since this fixture injects
        // none. Matched loosely on purpose: pinning the number would make the
        // test fail once a second.
        settledTime: expect.any(Number),
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
    const client = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', orders: [{ id: 7 }] } }]);

    await client.fetch(orderList);

    expect(() => dehydrate(client, { principal: 'u-1', mode: 'denormalized' })).toThrow(
      /cannot serialize a cyclic value/,
    );
    expect(() => dehydrate(client, { principal: 'u-1' })).not.toThrow();
  });
});

/** The generated `ops.ts` table, keyed as the generator keys it. */
const ops = { orderList, customerList };

/** Serialize and read back, exactly as an HTML round trip would. */
function transfer(state: DehydratedState): DehydratedState {
  return JSON.parse(JSON.stringify(state)) as DehydratedState;
}

/** A cache whose transport must never be reached. */
function offline(): QueryCache {
  return cacheOwnedBy(undefined, () => {
    throw new Error('a hydrated query must not fetch');
  });
}

describe('hydrate', () => {
  it('serves the hydrated value with no request, in normalized mode', async () => {
    const server = cacheOwnedBy(undefined, () => [
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);

    await server.fetch(orderList);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.getState(orderList).status).toBe('success');
    expect(client.getState(orderList).data).toEqual([
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);
  });

  it('serves the hydrated value with no request, in denormalized mode', async () => {
    const server = cacheOwnedBy(undefined, () => [
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);

    await server.fetch(orderList);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined, mode: 'denormalized' })), {
      ops,
    });

    expect(client.getState(orderList).data).toEqual([
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);
  });

  it('produces a store the entity graph is genuinely normalized into', async () => {
    const server = cacheOwnedBy(undefined, () => [
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);

    await server.fetch(orderList);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.store.has('Order:7')).toBe(true);
    expect(client.store.has('Customer:c-3')).toBe(true);
    expect(
      isRef((client.store.getRecord('Order:7')?.data as Record<string, unknown>).customer),
    ).toBe(true);
  });

  it('keeps reference-shaped response data as data', async () => {
    const server = cacheOwnedBy(undefined, () => [{ id: 7, meta: { __ref: 'not a reference' } }]);

    await server.fetch(orderList);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.getState(orderList).data).toEqual([
      { id: 7, meta: { __ref: 'not a reference' } },
    ]);
  });

  it('rebuilds an entity cycle as a cycle', async () => {
    const server = cacheOwnedBy(undefined, () => [
      { id: 7, total: 99, customer: { id: 'c-3', orders: [{ id: 7 }] } },
    ]);

    await server.fetch(orderList);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    const rows = client.getState(orderList).data as { customer: { orders: unknown[] } }[];

    expect(rows[0]?.customer.orders[0]).toBe(rows[0]);
  });

  it('carries a response-templated provides tag across, so a mutation still reaches it', async () => {
    const tagged: OperationMeta = { ...orderList, provides: ['Order[]', 'Batch:{res.0.id}'] };
    const server = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);

    await server.fetch(tagged);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), {
      ops: { orderList: tagged },
    });
    client.subscribe(tagged, undefined, () => undefined);

    expect(client.registry.queriesFor('Batch:7').map((entry) => entry.key)).toEqual([
      client.key(tagged),
    ]);
  });

  it('settles fresh by default and stale when asked', async () => {
    const server = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: undefined }));

    const fresh = offline();
    hydrate(fresh, state, { ops });
    expect(fresh.registry.get(fresh.key(orderList))?.stale).toBe(false);

    const verifying = offline();
    hydrate(verifying, state, { ops, stale: true });
    expect(verifying.registry.get(verifying.key(orderList))?.stale).toBe(true);
  });

  it('is idempotent: hydrating twice moves no version and keeps identity', async () => {
    const server = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });
    const first = client.getState(orderList).data;

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    expect(client.store.getRecord('Order:7')?.version).toBe(1);
    expect((client.getState(orderList).data as unknown[])[0]).toBe((first as unknown[])[0]);
  });

  it('refuses a payload belonging to another principal', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: 'u-1' }));
    const client = cacheOwnedBy('u-2', () => []);

    expect(() => hydrate(client, state, { ops })).toThrow(
      /\[forge\] hydrate: this payload belongs to a different principal/,
    );
  });

  it('refuses an unrecognised payload version', () => {
    const client = offline();

    expect(() =>
      hydrate(client, { v: 2, mode: 'normalized', records: {}, queries: [] } as never, { ops }),
    ).toThrow(/\[forge\] hydrate: unsupported payload version 2/);
  });

  it('refuses an operation the ops table does not name', async () => {
    const server = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    expect(() =>
      hydrate(client_(server), transfer(dehydrate(server, { principal: undefined })), {
        ops: { customerList },
      }),
    ).toThrow(/\[forge\] hydrate: no operation named GET \/orders/);
  });
});

/** A fresh offline cache, named so the assertion above reads in one line. */
function client_(_server: QueryCache): QueryCache {
  return offline();
}

describe('hydrate, keying', () => {
  const orderGet: OperationMeta = {
    method: 'GET',
    path: '/orders/{id}',
    entity: 'Order',
    provides: ['Order:{path.id}'],
    invalidates: [],
  };

  it('lands on the record a component asks for, for a query with arguments', async () => {
    const server = cacheOwnedBy(undefined, () => ({ id: 7, total: 99 }));

    await server.fetch(orderGet, { path: { id: 7 } });

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops: { orderGet } });

    expect(client.getState(orderGet, { path: { id: 7 } }).data).toEqual({ id: 7, total: 99 });
  });

  it('lands on the record a component asks for, for a query with none', async () => {
    const server = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const client = offline();

    hydrate(client, transfer(dehydrate(server, { principal: undefined })), { ops });

    // The record the payload restored and the record `getState` opens must be
    // one record, not two: `fetch(orderList)` keys `GET /orders` while the
    // record it creates holds `{}`, which re-derives as `GET /orders|{}`.
    expect(client.size).toBe(1);
    expect(client.getState(orderList).status).toBe('success');
  });
});

/**
 * The policy every framework's hydration boundary needs, tested once here
 * rather than three times over in the adapters.
 */
describe('hydrateBoundary', () => {
  async function payload(): Promise<DehydratedState> {
    const server = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);
    await server.fetch(orderList);

    return transfer(dehydrate(server, { principal: undefined }));
  }

  it('hydrates a payload the first time and skips it thereafter', async () => {
    const state = await payload();
    const client = cacheOwnedBy(undefined, () => []);

    hydrateBoundary(client, state, { ops });
    const first = client.getState(orderList).data;

    hydrateBoundary(client, state, { ops });

    expect(client.getState(orderList).data).toBe(first);
    expect(client.store.getRecord('Order:7')?.version).toBe(1);
  });

  it('hydrates the same payload again into a different cache', async () => {
    const state = await payload();
    const first = cacheOwnedBy(undefined, () => []);
    const second = cacheOwnedBy(undefined, () => []);

    hydrateBoundary(first, state, { ops });
    hydrateBoundary(second, state, { ops });

    expect(second.getState(orderList).status).toBe('success');
  });

  // A client running older code than the server is what every deploy produces
  // while stale JS is still cached. The queries just fetch.
  it('reports a version refusal and continues', () => {
    const reported: unknown[] = [];
    const client = cacheOwnedBy(undefined, () => [], (error) => reported.push(error));

    expect(() => hydrateBoundary(client, { v: 9 } as never, { ops })).not.toThrow();
    expect(reported).toHaveLength(1);
  });

  it('reports an unknown operation and continues', async () => {
    const server = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);
    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: undefined }));
    const reported: unknown[] = [];
    const client = cacheOwnedBy(undefined, () => [], (error) => reported.push(error));

    expect(() => hydrateBoundary(client, state, { ops: {} })).not.toThrow();
    expect(reported).toHaveLength(1);
  });

  // The one refusal that says something is wrong with whose data this is.
  // Fetching does not repair it, so it is not degraded past.
  it('rethrows a principal refusal', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);
    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: 'u-1' }));
    const client = cacheOwnedBy('u-2', () => []);

    expect(() => hydrateBoundary(client, state, { ops })).toThrow(/different principal/);
  });

  // React retries a render that threw. Marking the payload seen before the
  // rethrow would make the retry skip hydration, throw nothing, and render as
  // though it had worked, which turns the one refusal that must be loud into a
  // silent degrade.
  it('rethrows a principal refusal on every attempt, not just the first', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);
    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: 'u-1' }));
    const client = cacheOwnedBy('u-2', () => []);

    expect(() => hydrateBoundary(client, state, { ops })).toThrow();
    expect(() => hydrateBoundary(client, state, { ops })).toThrow();
  });

  it('does nothing at all without a payload', () => {
    const client = cacheOwnedBy(undefined, () => []);

    expect(() => hydrateBoundary(client, undefined, { ops })).not.toThrow();
    expect(client.getState(orderList).status).not.toBe('success');
  });
});

/**
 * Streaming SSR: one payload per flush, each carrying only what settled since
 * the last one.
 */
describe('streamingDehydrator', () => {
  it('carries everything on the first flush and nothing on a second', async () => {
    const client = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);
    const stream = streamingDehydrator(client, { principal: undefined });

    await client.fetch(orderList);

    const first = stream.flush() as NormalizedState;

    expect(first.queries).toHaveLength(1);
    expect(Object.keys(first.records)).toEqual(['Order:7']);

    expect(stream.flush()).toBeUndefined();
  });

  it('carries only the query that settled since the last flush', async () => {
    const client = cacheOwnedBy(undefined, (request) =>
      request.meta === orderList ? [{ id: 7, total: 99 }] : [{ id: 'c-9', name: 'Grace' }],
    );
    const stream = streamingDehydrator(client, { principal: undefined });

    await client.fetch(orderList);
    stream.flush();

    await client.fetch(customerList);

    const second = stream.flush() as NormalizedState;

    expect(second.queries).toHaveLength(1);
    expect(second.queries[0]?.operation).toBe('GET /customers');
    expect(Object.keys(second.records)).toEqual(['Customer:c-9']);
  });

  it('re-emits a record whose data changed between flushes', async () => {
    let total = 99;
    const client = cacheOwnedBy(undefined, () => [{ id: 7, total }]);
    const stream = streamingDehydrator(client, { principal: undefined });

    await client.fetch(orderList);
    stream.flush();

    total = 120;
    await client.refetch(orderList);

    const second = stream.flush() as NormalizedState;

    expect(second.records['Order:7']).toEqual({ id: 7, total: 120 });
  });

  // The property the whole helper rests on: applying the chunks in order lands
  // a client on exactly the cache a single non-streamed payload would have.
  it('hydrates chunk by chunk to the same place one payload would', async () => {
    const server = cacheOwnedBy(undefined, (request) =>
      request.meta === orderList ? [{ id: 7, total: 99 }] : [{ id: 'c-9', name: 'Grace' }],
    );
    const stream = streamingDehydrator(server, { principal: undefined });

    await server.fetch(orderList);
    const chunks = [stream.flush()];

    await server.fetch(customerList);
    chunks.push(stream.flush());

    const client = cacheOwnedBy(undefined, () => []);

    for (const chunk of chunks) {
      if (chunk !== undefined) hydrate(client, transfer(chunk), { ops: { orderList, customerList } });
    }

    expect(client.getState(orderList).data).toEqual([{ id: 7, total: 99 }]);
    expect(client.getState(customerList).data).toEqual([{ id: 'c-9', name: 'Grace' }]);
  });

  // Two queries over the same entity, so the second chunk's query references
  // a record the first chunk already shipped. Deduping across chunks is the
  // whole reason this exists rather than calling `dehydrate` per boundary.
  it('emits a record an earlier chunk already carried only once', async () => {
    const orderGet: OperationMeta = {
      method: 'GET',
      path: '/orders/{id}',
      entity: 'Order',
      provides: ['Order:{path.id}'],
      invalidates: [],
    };

    const client = cacheOwnedBy(undefined, (request) =>
      request.meta === orderList ? [{ id: 7, total: 99 }] : { id: 7, total: 99 },
    );
    const stream = streamingDehydrator(client, { principal: undefined });

    await client.fetch(orderList);
    stream.flush();

    await client.fetch(orderGet, { path: { id: 7 } });

    const second = stream.flush() as NormalizedState;

    expect(second.queries).toHaveLength(1);
    expect(second.queries[0]?.operation).toBe('GET /orders/{id}');
    expect(Object.keys(second.records)).toEqual([]);
  });

  it('streams a denormalized payload query by query', async () => {
    const client = cacheOwnedBy(undefined, (request) =>
      request.meta === orderList ? [{ id: 7, total: 99 }] : [{ id: 'c-9', name: 'Grace' }],
    );
    const stream = streamingDehydrator(client, { principal: undefined, mode: 'denormalized' });

    await client.fetch(orderList);

    expect((stream.flush() as DenormalizedState).queries).toHaveLength(1);
    expect(stream.flush()).toBeUndefined();

    await client.fetch(customerList);

    const second = stream.flush() as DenormalizedState;

    expect(second.queries).toHaveLength(1);
    expect(second.queries[0]?.operation).toBe('GET /customers');
  });

  it('refuses a principal that does not own the cache, on every flush', async () => {
    const client = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(() => streamingDehydrator(client, { principal: 'u-2' }).flush()).toThrow(
      /principal does not match the cache owner/,
    );
  });
});

describe('hydrationFailure', () => {
  it('names the reason a refusal carries, so nothing has to match a message', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: 'u-1' }));

    expect(reasonOf(() => hydrate(cacheOwnedBy('u-2', () => []), state, { ops }))).toBe(
      'principal',
    );
    expect(
      reasonOf(() => hydrate(offline(), { v: 9 } as never, { ops })),
    ).toBe('version');
    expect(
      reasonOf(() =>
        hydrate(offline(), { v: 1, mode: 'martian', queries: [] } as never, { ops }),
      ),
    ).toBe('version');

    const anonymous = cacheOwnedBy(undefined, () => [{ id: 7, total: 99 }]);
    await anonymous.fetch(orderList);

    expect(
      reasonOf(() =>
        hydrate(offline(), transfer(dehydrate(anonymous, { principal: undefined })), {
          ops: { customerList },
        }),
      ),
    ).toBe('operation');
  });

  it('answers undefined for anything it did not raise', () => {
    expect(hydrationFailure(new Error('something else'))).toBeUndefined();
    expect(hydrationFailure('a string')).toBeUndefined();
    expect(hydrationFailure(null)).toBeUndefined();
    expect(hydrationFailure({ forgeHydration: 'invented' })).toBeUndefined();
  });

  it('leaves the cache untouched when it refuses before writing', async () => {
    const server = cache(() => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = transfer(dehydrate(server, { principal: 'u-1' }));
    const client = cacheOwnedBy('u-2', () => []);

    expect(() => hydrate(client, state, { ops })).toThrow();
    expect(client.store.size).toBe(0);
    expect(client.size).toBe(0);
  });
});

/** The reason a thrown refusal carried, or undefined if it did not throw. */
function reasonOf(run: () => void): unknown {
  try {
    run();
  } catch (error) {
    return hydrationFailure(error);
  }

  return undefined;
}


describe('carrying the settle time across hydration', () => {
  /** A cache with a clock the test moves by hand. */
  function timed(now: () => number, handler: () => unknown): QueryCache {
    const client = new QueryCache({
      transport: fakeTransport(handler),
      entities: schema,
      scheduler: manualScheduler().schedule,
      now,
    });

    client.setPrincipal('u-1');

    return client;
  }

  it("hydrates with the server's settle time, not the client's clock", async () => {
    const server = timed(() => 1_000, () => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = dehydrate(server, { principal: 'u-1' }) as NormalizedState;

    expect(state.queries[0]?.settledTime).toBe(1_000);

    // The page then sits in a CDN for a hundred seconds before anyone loads it.
    const client = timed(() => 101_000, () => [{ id: 7, total: 99 }]);

    hydrate(client, state, { ops: { 'GET /orders': orderList } });

    // Without this, the record is stamped at hydration and a staleTime of ten
    // seconds treats hundred-second-old data as fresh for ten seconds more.
    expect(client.settledTimeOf(orderList)).toBe(1_000);
  });

  it('carries it through a denormalized payload too', async () => {
    const server = timed(() => 2_000, () => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = dehydrate(server, {
      principal: 'u-1',
      mode: 'denormalized',
    }) as DenormalizedState;

    expect(state.queries[0]?.settledTime).toBe(2_000);

    const client = timed(() => 50_000, () => [{ id: 7, total: 99 }]);

    hydrate(client, state, { ops: { 'GET /orders': orderList } });

    expect(client.settledTimeOf(orderList)).toBe(2_000);
  });

  it('falls back to the local clock for a payload that carries no settle time', async () => {
    const server = timed(() => 1_000, () => [{ id: 7, total: 99 }]);

    await server.fetch(orderList);

    const state = dehydrate(server, { principal: 'u-1' }) as NormalizedState;

    // A payload written by a client built before this field existed. It must
    // hydrate exactly as it did then, stamped on arrival.
    const older = {
      ...state,
      queries: state.queries.map(({ settledTime, ...rest }) => rest),
    } as NormalizedState;

    const client = timed(() => 77_000, () => [{ id: 7, total: 99 }]);

    hydrate(client, older, { ops: { 'GET /orders': orderList } });

    expect(client.settledTimeOf(orderList)).toBe(77_000);
  });
});
