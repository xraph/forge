import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { dehydrate } from '../src/ssr';
import type { DenormalizedState, NormalizedState } from '../src/ssr';
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
    expect(state.queries[0]?.args).toEqual({});
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
    const client = cache(() => [{ id: 7, total: 99, customer: { id: 'c-3', orders: [{ id: 7 }] } }]);

    await client.fetch(orderList);

    expect(() => dehydrate(client, { principal: 'u-1', mode: 'denormalized' })).toThrow(
      /cannot serialize a cyclic value/,
    );
    expect(() => dehydrate(client, { principal: 'u-1' })).not.toThrow();
  });
});
