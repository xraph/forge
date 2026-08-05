import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import type { OperationMeta, Transport, TransportRequest } from '../src/transport';
import { deferred, fakeTransport, settleMicrotasks } from './harness';
import { schema } from './schema';

/**
 * Every cache in this file runs on the **default microtask scheduler**, and the
 * interleaving is driven by promise ordering rather than by an explicit flush.
 *
 * That is the whole point of the file. `manualScheduler` plus a `flush()` before
 * the fetch gate opens is a convenient way to assert what a batch contains, but
 * it silently answers the question these tests exist to ask: whether a response
 * dispatched *before* a write can land in the gap between the write and the
 * batch. Under a manual scheduler that gap is closed by the test itself, so a
 * suite written entirely against one cannot observe the defect.
 */

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const orderCreate: OperationMeta = {
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  provides: [],
  invalidates: ['Order[]'],
};

const orderPatch: OperationMeta = {
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  provides: [],
  invalidates: ['Order:{id}'],
};

/** A cache with no scheduler override: batches run on a real microtask. */
function cache(handler: Parameters<typeof fakeTransport>[0]) {
  const transport = fakeTransport(handler);

  return { transport, cache: new QueryCache({ transport, entities: schema }) };
}

describe('a response that predates a write never commits', () => {
  it('does not let a pre-create refetch overwrite a placed list', async () => {
    // The failure this covers is permanent. Placement means no refetch is
    // owed, so a pre-write response that overwrites the placed skeleton
    // deletes the created order from the list with nothing scheduled to put it
    // back -- and the escape hatch that is supposed to be the fast path
    // becomes the lossy one.
    const pending = deferred<unknown>();
    const { cache: queries, transport } = cache((request, call) => {
      if (request.meta === orderCreate) return { id: 9, total: 5 };
      if (call === 1) return pending.promise;

      return [{ id: 7, total: 99 }];
    });

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(queries.getState(orderList).data).toEqual([{ id: 7, total: 99 }]);

    // A refetch is now in flight, and knows nothing about what is about to
    // happen.
    const refetching = queries.refetch(orderList);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(2);

    await queries.mutate(orderCreate, { body: { total: 5 } }, {
      place: { 'Order[]': (created, current) => [created, ...(current as unknown[])] },
    });

    expect(queries.getState(orderList).data).toEqual([
      { id: 9, total: 5 },
      { id: 7, total: 99 },
    ]);

    // The pre-create list arrives.
    pending.resolve([{ id: 7, total: 99 }]);
    await settleMicrotasks();

    // Still placed. Before this was fixed the created order was gone here, for
    // good.
    expect(queries.getState(orderList).data).toEqual([
      { id: 9, total: 5 },
      { id: 7, total: 99 },
    ]);

    // Discarded, not restarted: placement already supplied the answer, so no
    // fourth request was spent rediscovering it.
    expect(transport.calls).toHaveLength(3);

    // And the caller awaiting the pre-empted refetch gets the current answer
    // rather than a rejection or the stale body.
    await expect(refetching).resolves.toEqual([
      { id: 9, total: 5 },
      { id: 7, total: 99 },
    ]);
  });

  it('does not let a pre-write refetch clobber the mutation’s own write', async () => {
    // The interleaving is set up by queue position, not by tick counting.
    //
    // `mutate` is called but not awaited, so its continuation is queued first;
    // the gate is then resolved, queuing the pre-write response behind it. The
    // mutation therefore commits `Order:7 total=100` and raises its
    // invalidation, and only *then* does the response that predates it get to
    // run. The batch is queued during the mutation's synchronous block, which
    // puts it behind the response -- so if staleness were only marked from the
    // batch, the pre-write body would already have committed by the time the
    // batch could do anything about it. That is exactly the window the default
    // microtask scheduler leaves open and a manual one does not.
    const pending = deferred<unknown>();
    const calls: TransportRequest[] = [];
    // Pre-registered rather than computed in a handler, so the number of
    // promise hops between dispatch and arrival is fixed and visible.
    const responses: unknown[] = [
      Promise.resolve([{ id: 7, total: 1 }]),
      pending.promise,
      Promise.resolve({ id: 7, total: 100 }),
      Promise.resolve([{ id: 7, total: 100 }]),
      Promise.resolve([{ id: 7, total: 100 }]),
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const queries = new QueryCache({ transport, entities: schema });
    const totals: unknown[] = [];

    queries.subscribe(orderList, undefined, () => {
      const record = queries.store.getRecord('Order:7');

      if (record !== undefined) totals.push(record.data['total']);
    });

    await settleMicrotasks();

    const refetching = queries.refetch(orderList);
    await settleMicrotasks();
    expect(calls).toHaveLength(2);

    const mutating = queries.mutate(orderPatch, { path: { id: 7 } });
    pending.resolve([{ id: 7, total: 1 }]);

    await mutating;
    expect(queries.store.getRecord('Order:7')?.data['total']).toBe(100);

    const seenBeforeTheRace = totals.length;

    await settleMicrotasks();

    // Never rolled back to 1, not in the store and not in any notification a
    // subscriber could have rendered from.
    expect(queries.store.getRecord('Order:7')?.data['total']).toBe(100);
    expect(totals.slice(seenBeforeTheRace)).not.toContain(1);
    expect(queries.getState(orderList).data).toEqual([{ id: 7, total: 100 }]);

    // Four requests: the initial load, the refetch that was thrown away, the
    // patch, and the one re-run in its place. Not five -- the batch must not
    // dispatch again for staleness the in-flight sequence has already taken
    // responsibility for.
    expect(calls).toHaveLength(4);

    await expect(refetching).resolves.toEqual([{ id: 7, total: 100 }]);
  });

  it('still refetches normally when nothing was in flight', async () => {
    // The counterpart to the assertion above: skipping the batch dispatch for
    // an in-flight request must not skip it for a query that has none.
    const { cache: queries, transport } = cache((request) =>
      request.meta === orderPatch ? { id: 7, total: 100 } : [{ id: 7, total: 100 }],
    );

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    await queries.mutate(orderPatch, { path: { id: 7 } });
    await settleMicrotasks();

    expect(transport.calls.map((call) => call.meta)).toEqual([
      orderList,
      orderPatch,
      orderList,
    ]);
  });
});

describe('a transport that throws synchronously', () => {
  it('does not install its rejection as the in-flight request forever', async () => {
    // `RestTransport` is `async` and cannot do this, but `Transport` is the
    // declared seam for the stream transports still to come. An inline
    // sequence body would run its own catch before the promise had been
    // recorded, and every later fetch would be served that first rejection
    // with no request made.
    let calls = 0;
    const transport: Transport = {
      execute: () => {
        calls++;

        throw new Error('sync boom');
      },
    };
    const queries = new QueryCache({ transport, entities: schema });

    for (let attempt = 0; attempt < 6; attempt++) {
      await expect(queries.fetch(orderList)).rejects.toThrow('sync boom');
    }

    await expect(queries.refetch(orderList)).rejects.toThrow('sync boom');

    expect(calls).toBe(7);
    expect(queries.getState(orderList).status).toBe('error');
  });
});

describe('abandoning a request', () => {
  it('rejects rather than resolving undefined when the cache is cleared', async () => {
    const pending = deferred<unknown>();
    const { cache: queries } = cache(() => pending.promise);

    queries.setPrincipal('user-a');

    const fetching = queries.fetch(orderList);
    await settleMicrotasks();

    queries.setPrincipal('user-b');
    pending.resolve([{ id: 7, total: 99 }]);

    // `undefined`, as though the server had returned nothing, is the quiet lie
    // that surfaces three layers away.
    await expect(fetching).rejects.toThrow(/abandoned/);
    expect(queries.store.size).toBe(0);
  });
});
