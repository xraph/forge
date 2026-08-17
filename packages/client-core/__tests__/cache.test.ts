import { describe, expect, it, vi } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { makeRef, markRewritten } from '../src/ref';
import type { OperationMeta } from '../src/transport';
import { deferred, fakeTransport, HttpFailure, settleMicrotasks } from './harness';
import { schema } from './schema';

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const orderGet: OperationMeta = {
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  invalidates: [],
};

const customerList: OperationMeta = {
  method: 'GET',
  path: '/customers',
  entity: 'Customer',
  provides: ['Customer[]'],
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

function cache(handler: Parameters<typeof fakeTransport>[0]) {
  const scheduler = manualScheduler();
  const transport = fakeTransport(handler);

  return {
    transport,
    scheduler,
    cache: new QueryCache({ transport, entities: schema, scheduler: scheduler.schedule }),
  };
}

describe('running a query', () => {
  it('normalizes the response, records its dependencies, and reads it back', async () => {
    const response = [
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
      { id: 8, total: 12, customer: { id: 'c-3', name: 'Ada' } },
    ];
    const { cache: queries } = cache(() => response);

    const value = await queries.fetch(orderList);

    expect(value).toEqual(response);
    expect(queries.store.getRecord('Order:7')?.data['total']).toBe(99);
    expect(queries.store.getRecord('Customer:c-3')?.data['name']).toBe('Ada');

    // The skeleton, not a document: the entities were lifted out, so the two
    // orders share one customer object rather than two copies of it.
    const list = value as { customer: unknown }[];
    expect(list[0]?.customer).toBe(list[1]?.customer);

    const entry = queries.registry.get(queries.key(orderList));
    expect(entry?.deps).toEqual(new Set(['Order:7', 'Order:8', 'Customer:c-3']));
    expect(entry?.tags.has('Order[]')).toBe(true);
    expect(entry?.tags.has('Order:7')).toBe(true);
  });

  it('serves a second read from cache, with the same object identity', async () => {
    const { cache: queries, transport } = cache(() => [{ id: 7, total: 99 }]);

    const first = await queries.fetch(orderList);
    const second = await queries.fetch(orderList);

    expect(second).toBe(first);
    expect(queries.getState(orderList).data).toBe(first);
    // Identity holds through the state object too, which is what
    // useSyncExternalStore requires of getSnapshot.
    expect(queries.getState(orderList)).toBe(queries.getState(orderList));
    expect(transport.calls).toHaveLength(1);
  });

  it('keeps the identity of an unchanged subtree across a refetch', async () => {
    let total = 99;
    const { cache: queries } = cache(() => [
      { id: 7, total, customer: { id: 'c-3', name: 'Ada' } },
    ]);

    const first = (await queries.fetch(orderList)) as { customer: unknown }[];
    const customer = first[0]?.customer;

    total = 120;

    const second = (await queries.refetch(orderList)) as { customer: unknown }[];

    expect(second).not.toBe(first);
    // The order changed; the customer did not, and did not re-render.
    expect(second[0]?.customer).toBe(customer);
  });

  // The other half of the test above. A refetch that changes something has to
  // move the container; a refetch that changes nothing must not. `useSync-
  // ExternalStore` re-renders on a new snapshot identity, so a poll returning
  // the same bytes used to re-render the whole subscribed tree even though
  // every record under it was untouched.
  it('keeps the container identity when a refetch returns identical data', async () => {
    // A fresh array of fresh objects per call, the way a real transport parses
    // a fresh response body.
    const { cache: queries } = cache(() => [
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
      { id: 8, total: 1, customer: { id: 'c-4', name: 'Grace' } },
    ]);

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    const first = queries.getState(orderList).data;

    await queries.refetch(orderList);

    expect(queries.getState(orderList).data).toBe(first);
  });

  it('reports pending, then success, to its subscribers', async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = cache(() => gate.promise);
    const seen: string[] = [];

    queries.subscribe(orderList, undefined, () => {
      seen.push(queries.getState(orderList).status);
    });

    await settleMicrotasks();
    expect(queries.getState(orderList).isFetching).toBe(true);

    gate.resolve([{ id: 7 }]);
    await settleMicrotasks();

    expect(seen).toEqual(['pending', 'success']);
    expect(queries.getState(orderList).data).toEqual([{ id: 7 }]);
  });

  it('keeps the last good value when a refetch fails', async () => {
    let fail = false;
    const { cache: queries } = cache(() => {
      if (fail) throw new HttpFailure(500);

      return [{ id: 7, total: 99 }];
    });

    const good = await queries.fetch(orderList);

    fail = true;
    await expect(queries.refetch(orderList)).rejects.toThrow('HTTP 500');

    const state = queries.getState(orderList);
    expect(state.status).toBe('error');
    expect(state.data).toBe(good);
  });
});

describe('deduplication', () => {
  it('makes one request for N subscribers mounting the same query', async () => {
    const gate = deferred<unknown>();
    const { cache: queries, transport } = cache(() => gate.promise);
    const listeners = [vi.fn(), vi.fn(), vi.fn(), vi.fn()];

    for (const listener of listeners) queries.subscribe(orderList, undefined, listener);

    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    gate.resolve([{ id: 7 }]);
    await settleMicrotasks();

    for (const listener of listeners) expect(listener).toHaveBeenCalled();
    expect(transport.calls).toHaveLength(1);
  });

  it('treats differently-ordered arguments as one query', async () => {
    const { cache: queries, transport } = cache(() => []);

    await queries.fetch(orderList, { query: { status: 'open', page: 1 } });
    await queries.fetch(orderList, { query: { page: 1, status: 'open' } });

    expect(transport.calls).toHaveLength(1);
    expect(queries.size).toBe(1);
  });

  it('shares the whole retry sequence, never resolving anyone from a failed attempt', async () => {
    // The transport fails the first call and succeeds on the second, so a
    // subscriber resolved from the first attempt would be observable as an
    // error while its neighbour got a value.
    const { cache: queries, transport } = cache((_request, call) => {
      if (call === 0) throw new HttpFailure(500);

      return [{ id: 7 }];
    });

    const first = queries.fetch(orderList);
    const second = queries.fetch(orderList);

    // Both joined one sequence. It fails, and both see the same failure.
    await expect(first).rejects.toThrow('HTTP 500');
    await expect(second).rejects.toThrow('HTTP 500');
    expect(transport.calls).toHaveLength(1);

    // The dead sequence is not adopted by the next caller.
    await expect(queries.fetch(orderList)).resolves.toEqual([{ id: 7 }]);
    expect(transport.calls).toHaveLength(2);
  });

  it('discards a response that predates an invalidation it was meant to reflect', async () => {
    const gates = [deferred<unknown>(), deferred<unknown>()];
    const { cache: queries, scheduler } = cache((_request, call) => gates[call]?.promise);

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    // The write lands while the read is still out. Resolving the read now
    // would show the list as it was before the write.
    queries.invalidate(['Order[]']);
    scheduler.flush();

    gates[0]?.resolve([{ id: 7, total: 99 }]);
    await settleMicrotasks();

    // Still nothing: the pre-write answer was thrown away and the query is
    // waiting on the request issued after it.
    expect(queries.getState(orderList).data).toBeUndefined();

    gates[1]?.resolve([{ id: 7, total: 99 }, { id: 8, total: 12 }]);
    await settleMicrotasks();

    expect(queries.getState(orderList).data).toHaveLength(2);
  });
});

describe('mutations', () => {
  it('settles, invalidates the queries that match, and leaves the others alone', async () => {
    const { cache: queries, transport, scheduler } = cache((request) =>
      request.meta === orderCreate ? { id: 9, total: 5 } : [],
    );

    queries.subscribe(orderList, undefined, () => undefined);
    queries.subscribe(customerList, undefined, () => undefined);
    await settleMicrotasks();

    const before = transport.calls.length;

    await queries.mutate(orderCreate, { body: { total: 5 } });
    scheduler.flush();
    await settleMicrotasks();

    const refetched = transport.calls.slice(before + 1).map((call) => call.meta);

    expect(refetched).toEqual([orderList]);
    expect(queries.store.getRecord('Order:9')?.data['total']).toBe(5);
  });

  it('reaches a list through the entity it displays, with no declared tag', async () => {
    const { cache: queries, transport, scheduler } = cache((request) =>
      request.meta === orderPatch ? { id: 7, total: 120 } : [{ id: 7, total: 99 }],
    );

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    const before = transport.calls.length;

    // `Order:7` is not in orderList's `provides`; it is there because the
    // response normalized to it.
    await queries.mutate(orderPatch, { path: { id: 7 } });

    // The response is committed before anything is refetched, so the list
    // already shows the new total -- the refetch confirms it rather than
    // being what produces it.
    expect(queries.store.getRecord('Order:7')?.data['total']).toBe(120);
    expect(queries.getState(orderList).data).toEqual([{ id: 7, total: 120 }]);

    scheduler.flush();
    await settleMicrotasks();

    expect(transport.calls.slice(before + 1).map((call) => call.meta)).toEqual([orderList]);
  });

  it('lets a placement callback answer for a query instead of refetching it', async () => {
    const { cache: queries, transport, scheduler } = cache((request) =>
      request.meta === orderCreate ? { id: 9, total: 5 } : [{ id: 7, total: 99 }],
    );

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    const before = transport.calls.length;

    await queries.mutate(orderCreate, { body: { total: 5 } }, {
      place: {
        'Order[]': (created, current) => [created, ...(current as unknown[])],
      },
    });
    scheduler.flush();
    await settleMicrotasks();

    // Placed, so no refetch.
    expect(transport.calls).toHaveLength(before + 1);
    expect(queries.getState(orderList).data).toEqual([
      { id: 9, total: 5 },
      { id: 7, total: 99 },
    ]);

    // The placed list is normalized like any other, so a later write to the
    // order it placed reaches it.
    queries.store.put('Order:9', { total: 6 });
    expect((queries.getState(orderList).data as { total: number }[])[0]?.total).toBe(6);
  });

  it('falls back to a refetch when placement declines', async () => {
    const { cache: queries, transport, scheduler } = cache((request) =>
      request.meta === orderCreate ? { id: 9, total: 5, status: 'draft' } : [],
    );

    queries.subscribe(orderList, { query: { status: 'open' } }, () => undefined);
    await settleMicrotasks();

    const before = transport.calls.length;

    await queries.mutate(orderCreate, { body: { total: 5 } }, {
      place: {
        // A filtered window the runtime cannot reason about, and the
        // application declines to guess either.
        'Order[]': (created, current, args) =>
          args.query?.['status'] === (created as { status: string }).status
            ? [created, ...(current as unknown[])]
            : undefined,
      },
    });
    scheduler.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(before + 2);
  });

  /**
   * A mutation's response commits entities that its declared tags do not
   * reach, and the subscribers displaying those entities have to be told.
   *
   * `orderCreate` declares `Order[]` and nothing else, and returns `Order:9`.
   * A detail query already displaying `Order:9` is in none of the tags being
   * refetched, so the *only* moment it can learn anything is the refresh
   * inside `mutate`. Refreshing without notifying does not merely delay that:
   * it consumes it. `record.state` advances to the new value, and every later
   * `notifyChanged()` compares the new state against the state it already
   * advanced to, finds them equal, and reports nothing -- so the query is
   * permanently stale against a cache holding the right answer.
   *
   * It hid behind React for a whole chunk. `useSyncExternalStore` re-reads
   * `getSnapshot` during every render, and the mutation's own
   * `pending -> success` re-renders the component that fired it, so the value
   * usually appeared anyway. A consumer that holds the last snapshot it was
   * handed -- Vue's `shallowRef`, an Angular signal -- has nothing to paper
   * over it with, and this is where the two adapters found it.
   */
  it('notifies a query whose entity the write changed but whose tags it never named', async () => {
    let written = 776;
    const { cache: queries, scheduler } = cache((request) => {
      if (request.meta === orderCreate) return { id: 9, total: ++written };

      return request.meta === orderGet ? { id: 9, total: 1 } : [{ id: 'c-3', name: 'Ada' }];
    });

    const detail = vi.fn();
    const unrelated = vi.fn();

    queries.subscribe(orderGet, { path: { id: 9 } }, detail);
    queries.subscribe(customerList, undefined, unrelated);
    await settleMicrotasks();

    expect(queries.getState(orderGet, { path: { id: 9 } }).data).toEqual({ id: 9, total: 1 });

    const settled = detail.mock.calls.length;

    await queries.mutate(orderCreate, { body: {} });

    // Before any scheduler flush and before any refetch: the commit alone is
    // enough, because the commit is all there will ever be for this query.
    expect(detail.mock.calls.length).toBeGreaterThan(settled);
    expect(queries.getState(orderGet, { path: { id: 9 } }).data).toEqual({ id: 9, total: 777 });

    // The change was reported *once* and is not still sitting unclaimed. With
    // the notification swallowed, this line is what used to hold and the two
    // above are what used to fail -- the refresh advanced `record.state`, so
    // `notifyChanged` compared the new state against the state it had already
    // advanced to and reported nothing, permanently.
    const reported = detail.mock.calls.length;

    queries.notifyChanged();
    scheduler.flush();
    await settleMicrotasks();

    expect(detail.mock.calls.length).toBe(reported);

    // And it stays selective. A second write to `Order:9` wakes the query
    // displaying it and nobody else: notification is by state-object identity,
    // so the customer list costs one memoized read and no render.
    //
    // Measured from the *second* mutation deliberately. The first refresh after
    // a batch settles can re-materialize a query that has not been read since
    // -- its `record.state` was captured before a sibling query's commit
    // invalidated the memo underneath it -- which is one notification, once,
    // and is neither introduced nor removed by notifying here. It is the same
    // artefact `notifyChanged()` has always had, since it is the same refresh.
    const before = unrelated.mock.calls.length;
    const beforeDetail = detail.mock.calls.length;

    await queries.mutate(orderCreate, { body: {} });

    expect(detail.mock.calls.length).toBeGreaterThan(beforeDetail);
    expect(unrelated.mock.calls.length).toBe(before);
    expect(queries.getState(orderGet, { path: { id: 9 } }).data).toEqual({ id: 9, total: 778 });
  });
});

describe('identity partitioning', () => {
  it('drops the store on an identity change, and refetches what is being watched', async () => {
    let body: unknown = [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }];
    const { cache: queries, transport } = cache(() => body);

    queries.setPrincipal('user-a');
    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    expect(queries.store.has('Order:7')).toBe(true);
    expect(queries.getState(orderList).data).toHaveLength(1);

    body = [{ id: 42, total: 1 }];
    queries.setPrincipal('user-b');

    // Nothing from the previous session survives the switch, not even for the
    // instant before the refetch lands.
    expect(queries.store.has('Order:7')).toBe(false);
    expect(queries.store.has('Customer:c-3')).toBe(false);
    expect(queries.store.size).toBe(0);
    expect(queries.registry.size).toBe(1);
    expect(queries.getState(orderList).data).toBeUndefined();

    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
    expect(queries.getState(orderList).data).toEqual([{ id: 42, total: 1 }]);
    expect(queries.store.has('Order:7')).toBe(false);
  });

  it('drops an in-flight response for the principal that went away', async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = cache((_request, call) =>
      call === 0 ? gate.promise : [{ id: 42 }],
    );

    queries.setPrincipal('user-a');
    // Fetched but never watched: nothing will re-mount it, so if the response
    // is not abandoned it lands in the new principal's store unobserved.
    void queries.fetch(orderList).catch(() => undefined);
    await settleMicrotasks();

    queries.setPrincipal('user-b');

    gate.resolve([{ id: 7, total: 99 }]);
    await settleMicrotasks();

    expect(queries.store.has('Order:7')).toBe(false);
    expect(queries.store.size).toBe(0);
  });

  it('does nothing when the principal has not actually changed', async () => {
    const { cache: queries, transport } = cache(() => [{ id: 7 }]);

    queries.setPrincipal('user-a');
    await queries.fetch(orderList);
    queries.setPrincipal('user-a');

    expect(queries.store.has('Order:7')).toBe(true);
    expect(transport.calls).toHaveLength(1);
  });
});

describe('prefetching', () => {
  /**
   * A cache on the **default** scheduler, driven only by promise ordering.
   *
   * The manual scheduler above is the wrong instrument for this section. A
   * test that flushes the batch before releasing the fetch gate has already
   * decided the interleaving it was supposed to be probing; the sequence that
   * loses data is the one where a request dispatched before a write lands
   * after it, and reproducing that means resolving the gate by hand and
   * letting every batch fall where the real scheduler puts it.
   */
  function live(handler: Parameters<typeof fakeTransport>[0]) {
    const transport = fakeTransport(handler);

    return { transport, cache: new QueryCache({ transport, entities: schema }) };
  }

  it('refetches a prefetch whose response predates a mutation, once it mounts', async () => {
    const gate = deferred<unknown>();
    const { cache: queries, transport } = live((request, call) =>
      request.meta === orderCreate
        ? { id: 9, total: 5 }
        : call === 0
          ? gate.promise
          : [{ id: 7, total: 99 }, { id: 9, total: 5 }],
    );

    // Route preloading: the list is fetched with nothing mounted.
    const prefetch = queries.fetch(orderList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);

    // The write lands while that request is still out. Nothing is mounted, so
    // the tag index -- which holds mounted queries by construction -- has
    // nobody to report, and the in-flight request is never told.
    await queries.mutate(orderCreate, { body: { total: 5 } });

    // Only now does the pre-write answer arrive.
    gate.resolve([{ id: 7, total: 99 }]);
    await prefetch;
    await settleMicrotasks();

    // The route transition arrives and mounts the query.
    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    expect(queries.getState(orderList).data).toEqual([
      { id: 7, total: 99 },
      { id: 9, total: 5 },
    ]);
  });

  it('does not serve a cached prefetch that predates a mutation', async () => {
    const gate = deferred<unknown>();
    const { cache: queries, transport } = live((request, call) =>
      request.meta === orderCreate
        ? { id: 9, total: 5 }
        : call === 0
          ? gate.promise
          : [{ id: 7, total: 99 }, { id: 9, total: 5 }],
    );

    const prefetch = queries.fetch(orderList);
    await settleMicrotasks();

    await queries.mutate(orderCreate, { body: { total: 5 } });

    gate.resolve([{ id: 7, total: 99 }]);
    await prefetch;
    await settleMicrotasks();

    // Never mounted at all: `fetch` serves from cache only when the value is
    // current, and this one was fetched before the write.
    await expect(queries.fetch(orderList)).resolves.toEqual([
      { id: 7, total: 99 },
      { id: 9, total: 5 },
    ]);
    expect(transport.calls).toHaveLength(3);
  });
});

describe('bounded memory', () => {
  it('forgets the least recently used unwatched queries, and never a watched one', async () => {
    const scheduler = manualScheduler();
    const transport = fakeTransport(() => ({ id: 1 }));
    const queries = new QueryCache({
      transport,
      entities: schema,
      scheduler: scheduler.schedule,
      limit: 3,
    });

    const keep = queries.subscribe(orderGet, { path: { id: 0 } }, () => undefined);
    await settleMicrotasks();

    for (let id = 1; id <= 10; id++) {
      const release = queries.subscribe(orderGet, { path: { id } }, () => undefined);
      await settleMicrotasks();
      release();
    }

    expect(queries.size).toBeLessThanOrEqual(3);
    expect(queries.registry.size).toBeLessThanOrEqual(3);
    // The watched query survived every eviction.
    expect(queries.getState(orderGet, { path: { id: 0 } }).status).toBe('success');

    keep();
  });
});

describe('entity collection', () => {
  it('drops a record no cached query reaches', async () => {
    const { cache: queries } = cache(() => [{ id: 7, customer: { id: 'c-3', name: 'Ada' } }]);

    await queries.fetch(orderList);

    // Written straight into the store, so nothing's skeleton points at it.
    // A stream frame for an entity no mounted query holds arrives exactly
    // like this.
    queries.store.put('Order:999', { id: 999, total: 1 });

    expect(queries.collect()).toBe(1);
    expect(queries.store.getRecord('Order:999')).toBeUndefined();
    expect(queries.store.getRecord('Order:7')).toBeDefined();
    expect(queries.store.getRecord('Customer:c-3')).toBeDefined();
  });

  it('keeps everything a cached query still reaches, watched or not', async () => {
    const { cache: queries } = cache(() => [
      { id: 7, customer: { id: 'c-3', name: 'Ada' } },
      { id: 8, customer: { id: 'c-3', name: 'Ada' } },
    ]);

    // Unwatched, but still cached: `getState` can be called on it and would
    // read the store, so nothing under it is garbage yet.
    await queries.fetch(orderList);

    expect(queries.collect()).toBe(0);
    expect(queries.store.getRecord('Customer:c-3')).toBeDefined();
  });

  it('keeps a record only an optimistic overlay holds', async () => {
    const { cache: queries } = cache(() => ({ id: 1 }));

    // A pending write against a record no settled query reaches is still a
    // write somebody is going to roll back or confirm. Collecting it would
    // leave the overlay patching a row that no longer exists.
    queries.store.put('Order:1', { id: 1, total: 1 });
    void queries
      .mutate(orderPatch, { path: { id: 1 } }, { optimistic: { patch: { total: 5 } } })
      .catch(() => undefined);

    expect(queries.collect()).toBe(0);
    expect(queries.store.getRecord('Order:1')).toBeDefined();
  });

  it('collects when the query cap reaps an unwatched query', async () => {
    const scheduler = manualScheduler();
    const transport = fakeTransport((request) => ({
      id: request.args.path?.['id'],
      customer: { id: `c-${String(request.args.path?.['id'])}`, name: 'Ada' },
    }));
    const queries = new QueryCache({
      transport,
      entities: schema,
      scheduler: scheduler.schedule,
      limit: 3,
    });

    for (let id = 1; id <= 10; id++) {
      const release = queries.subscribe(orderGet, { path: { id } }, () => undefined);
      await settleMicrotasks();
      release();
    }

    // Three queries survive, each reaching one order and one customer, so the
    // store holds six records rather than the twenty it was handed.
    expect(queries.size).toBeLessThanOrEqual(3);
    expect(queries.store.size).toBeLessThanOrEqual(6);
  });
});

describe('tombstone expiry', () => {
  it('drops a tombstone once nothing that predates it is still in flight', async () => {
    // Deliberately disjoint: the list must not carry Order:1, or writing it
    // back would consume the tombstone by handing its stamp to the new record
    // and the test would pass without any expiry happening at all.
    const { cache: queries } = cache((request) =>
      request.meta === orderList ? [{ id: 55 }] : { id: 1 },
    );

    await queries.fetch(orderGet, { path: { id: 1 } });

    // A delete frame evicts with a stamp, which is what leaves the tombstone.
    queries.store.nextFrame();
    queries.store.evict('Order:1', queries.store.frameVersion);

    expect(queries.store.tombstones).toBe(1);

    // One request, dispatched after the delete, settling. Nothing older than
    // the tombstone is outstanding once it lands, so the stamp has no reader
    // left and is dropped rather than waiting to be pushed out by the cap.
    await queries.fetch(orderList);

    expect(queries.store.tombstones).toBe(0);
  });

  it('keeps a tombstone while a request that predates the delete is out', async () => {
    const gate = deferred<unknown>();
    // Three different answers, so no request in this test writes Order:1 back
    // and consumes the tombstone by handing its stamp to the new record.
    const { cache: queries } = cache((request) => {
      if (request.meta === orderList) return gate.promise;
      if (request.meta === orderPatch) return { id: 55 };

      return { id: 1 };
    });

    // The row that is about to be deleted has to exist first: `evict` leaves a
    // stamp only for a key it was actually holding.
    await queries.fetch(orderGet, { path: { id: 1 } });

    // Dispatched before the delete, so its reading of the frame clock predates
    // the tombstone. This is the request a tombstone exists for.
    const outstanding = queries.fetch(orderList);
    await settleMicrotasks();

    queries.store.nextFrame();
    queries.store.evict('Order:1', queries.store.frameVersion);

    expect(queries.store.tombstones).toBe(1);

    // Something dispatched *after* the delete lands while the first is still
    // out. That is the moment expiry runs, and the moment it must not fire:
    // the tombstone is the only thing standing between the outstanding
    // response and a resurrected row. The invalidation it raises stays queued,
    // because the scheduler in this harness is driven by hand.
    await queries.mutate(orderPatch, { path: { id: 1 } });

    expect(queries.store.tombstones).toBe(1);

    // Now the straddling request lands. Nothing older than the delete is left,
    // so the stamp has no reader and goes.
    gate.resolve([]);
    await outstanding;

    expect(queries.store.tombstones).toBe(0);
  });
});

describe('peek', () => {
  it('returns undefined for a query the cache has never opened, and opens nothing', () => {
    const { cache: client } = cache(() => [{ id: 7, total: 99 }]);

    expect(client.peek(orderList)).toBeUndefined();
    expect(client.size).toBe(0);
  });

  it('returns the same state object as getState once a record exists', async () => {
    const { cache: client } = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(client.peek(orderList)).toBe(client.getState(orderList));
  });

  it('is referentially stable across calls while nothing changes', async () => {
    const { cache: client } = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(client.peek(orderList)).toBe(client.peek(orderList));
  });
});

describe('settledQueries', () => {
  it('lists only the queries that settled successfully', async () => {
    const { cache: client } = cache((request) => {
      if (request.meta === customerList) throw new HttpFailure(500);

      return [{ id: 7, total: 99 }];
    });

    await client.fetch(orderList);
    await client.fetch(customerList).catch(() => undefined);
    client.getState(orderGet, { path: { id: 1 } });

    expect(client.settledQueries().map((query) => query.key)).toEqual([client.key(orderList)]);
  });

  it('reports the skeleton the store holds, not the response', async () => {
    const { cache: client } = cache(() => [{ id: 7, total: 99 }]);

    await client.fetch(orderList);

    expect(client.settledQueries()[0]?.skeleton).toEqual([{ __ref: 'Order:7' }]);
  });
});

/**
 * A one-element list skeleton, built the way `revive` builds one.
 *
 * The `markRewritten` is not decoration. A container with no mark is the
 * identity function on read, so an unmarked array of references rehydrates to
 * the references themselves rather than to the records -- see
 * `EntityStore.materializeNode`, which returns an unmarked node untouched.
 */
function skeletonOf(key: string): unknown {
  return markRewritten([makeRef(key)]);
}

describe('restore', () => {
  it('settles a query from a skeleton with no request', () => {
    const { cache: client, transport } = cache(() => [{ id: 7, total: 99 }]);

    client.store.put('Order:7', { id: 7, total: 99 });
    client.restore(orderList, undefined, { skeleton: skeletonOf('Order:7'), tags: ['Order[]'] });

    expect(client.getState(orderList).status).toBe('success');
    expect(client.getState(orderList).data).toEqual([{ id: 7, total: 99 }]);
    expect(transport.calls).toHaveLength(0);
  });

  it('records the skeleton dependencies, so a write to the entity is seen', () => {
    const { cache: client } = cache(() => []);

    client.store.put('Order:7', { id: 7, total: 99 });
    client.restore(orderList, undefined, { skeleton: skeletonOf('Order:7'), tags: ['Order[]'] });

    expect([...(client.registry.get(client.key(orderList))?.deps ?? [])]).toEqual(['Order:7']);
  });

  it('leaves the entry fresh by default and stale when asked', () => {
    const { cache: client } = cache(() => []);

    client.store.put('Order:7', { id: 7, total: 99 });
    client.restore(orderList, undefined, { skeleton: skeletonOf('Order:7'), tags: ['Order[]'] });
    expect(client.registry.get(client.key(orderList))?.stale).toBe(false);

    client.restore(
      orderGet,
      { path: { id: 7 } },
      { skeleton: makeRef('Order:7'), tags: ['Order:7'], stale: true },
    );
    expect(client.registry.get(client.key(orderGet, { path: { id: 7 } }))?.stale).toBe(true);
  });

  it('notifies the subscribers of a query it settles', () => {
    const { cache: client } = cache(() => []);
    let notified = 0;

    client.store.put('Order:7', { id: 7, total: 99 });
    client.subscribe(orderList, undefined, () => {
      notified++;
    });

    const before = notified;
    client.restore(orderList, undefined, { skeleton: skeletonOf('Order:7'), tags: ['Order[]'] });

    expect(notified).toBeGreaterThan(before);
  });
});

describe('restore, staleness', () => {
  it('refetches on the first mount when hydrated stale', async () => {
    const { cache: client, transport, scheduler } = cache(() => [{ id: 7, total: 120 }]);

    client.store.put('Order:7', { id: 7, total: 99 });
    client.restore(orderList, undefined, {
      skeleton: skeletonOf('Order:7'),
      tags: ['Order[]'],
      stale: true,
    });

    client.subscribe(orderList, undefined, () => undefined);
    scheduler.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);
  });

  it('does not refetch on the first mount when hydrated fresh', async () => {
    const { cache: client, transport, scheduler } = cache(() => [{ id: 7, total: 120 }]);

    client.store.put('Order:7', { id: 7, total: 99 });
    client.restore(orderList, undefined, { skeleton: skeletonOf('Order:7'), tags: ['Order[]'] });

    client.subscribe(orderList, undefined, () => undefined);
    scheduler.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(0);
  });
});
