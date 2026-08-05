import { afterEach, describe, expect, it, vi } from 'vitest';

import { configureClient, getClient, mutation, query, setClient } from '../src/client';
import { manualScheduler } from '../src/invalidate';
import { RestTransport } from '../src/transport';
import type { OperationMeta } from '../src/transport';
import { fakeClient, HttpFailure, settleMicrotasks } from './harness';
import { schema } from './schema';

/**
 * The two lines a generated `hooks.ts` contains for these operations.
 *
 * Bound at module scope, exactly as generation emits them -- before any
 * application has configured anything, which is why the bindings resolve their
 * cache when they are called rather than when they are created.
 */
const ops = {
  orderList: {
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    provides: ['Order[]'],
    invalidates: [],
  },
  orderCreate: {
    method: 'POST',
    path: '/orders',
    entity: 'Order',
    provides: [],
    invalidates: ['Order[]'],
  },
} as const satisfies Record<string, OperationMeta>;

const useOrderList = query<{ id: number; total: number }[]>(ops.orderList);
const useOrderCreate = mutation<{ id: number }>(ops.orderCreate);

afterEach(() => {
  setClient(undefined);
});

function wire(handler: Parameters<typeof fakeClient>[0]) {
  const client = fakeClient(handler);
  const scheduler = manualScheduler();
  const cache = configureClient({
    transport: new RestTransport({ client, sleep: () => Promise.resolve(), random: () => 0 }),
    entities: schema,
    scheduler: scheduler.schedule,
  });

  return { client, scheduler, cache };
}

describe('generated bindings', () => {
  it('tags each binding with its kind and operation', () => {
    expect(useOrderList.kind).toBe('query');
    expect(useOrderList.meta).toBe(ops.orderList);
    expect(useOrderCreate.kind).toBe('mutation');
    expect(useOrderCreate.meta).toBe(ops.orderCreate);
  });

  it('refuses to invent a cache rather than caching into a scratch one', () => {
    expect(() => useOrderList().getState()).toThrow(/no client configured/);
    expect(() => getClient()).toThrow(/no client configured/);
  });

  it('runs the whole stack: subscribe, request, normalize, read back', async () => {
    const { client } = wire(() => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }]);
    const handle = useOrderList({ query: { status: 'open' } });
    const listener = vi.fn();

    const release = handle.subscribe(listener);
    await settleMicrotasks();

    expect(client.calls[0]?.url).toBe('/orders?status=open');
    expect(handle.getState().data).toEqual([
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);
    expect(listener).toHaveBeenCalled();

    // Two handles for the same arguments are one query.
    expect(useOrderList({ query: { status: 'open' } }).key).toBe(handle.key);
    expect(useOrderList({ query: { status: 'closed' } }).key).not.toBe(handle.key);

    release();
  });

  it('runs a mutation, then refetches the query it invalidated', async () => {
    const { client, scheduler } = wire((config) =>
      config.method === 'POST' ? { id: 9, total: 5 } : [{ id: 7, total: 99 }],
    );
    const handle = useOrderList();
    const release = handle.subscribe(() => undefined);

    await settleMicrotasks();
    const before = client.calls.length;

    await expect(useOrderCreate({ body: { total: 5 } })).resolves.toEqual({ id: 9, total: 5 });

    scheduler.flush();
    await settleMicrotasks();

    expect(client.calls).toHaveLength(before + 2);
    expect(client.calls[before]?.method).toBe('POST');
    expect(client.calls[before + 1]?.method).toBe('GET');

    release();
  });

  it('retries the query on a 500 and not the mutation', async () => {
    let failGet = true;
    const { client } = wire((config) => {
      if (config.method === 'GET' && failGet) {
        failGet = false;

        throw new HttpFailure(500);
      }

      if (config.method === 'POST') throw new HttpFailure(500);

      return [{ id: 7, total: 99 }];
    });

    await expect(useOrderList().fetch()).resolves.toEqual([{ id: 7, total: 99 }]);
    expect(client.calls.filter((call) => call.method === 'GET')).toHaveLength(2);

    await expect(useOrderCreate({ body: {} })).rejects.toThrow('HTTP 500');
    expect(client.calls.filter((call) => call.method === 'POST')).toHaveLength(1);
  });

  it('takes an explicit cache in preference to the configured default', async () => {
    const { cache: fallback } = wire(() => []);
    const isolated = configureClient({
      transport: { execute: () => Promise.resolve([{ id: 1, total: 1 }]) },
      entities: schema,
    });

    setClient(fallback);

    await useOrderList(undefined, { client: isolated }).fetch();

    expect(isolated.store.has('Order:1')).toBe(true);
    expect(fallback.store.size).toBe(0);
  });
});
