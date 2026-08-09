import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { isRef } from '../src/ref';
import { dehydrate, hydrate, hydrationFailure } from '../src/ssr';
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
): QueryCache {
  const client = new QueryCache({
    transport: fakeTransport(handler),
    entities: schema,
    scheduler: manualScheduler().schedule,
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
