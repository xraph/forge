import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { applyFrames } from '../src/live';
import type { StreamFrame } from '../src/live';
import type { StreamBinding } from '../src/stream';
import type { OperationMeta, Transport, TransportRequest } from '../src/transport';
import { deferred, settleMicrotasks } from './harness';
import { schema } from './schema';

/**
 * The guarantee this file exists to demonstrate:
 *
 * > **A committed response never contains an entity older than a stream frame
 * > the client has already applied.**
 *
 * Requests are stamped at dispatch with the store's frame clock. Every frame
 * batch takes a fresh reading and stamps each record it writes. A response is
 * normalized *before* it is committed, and if any entity it carries has been
 * stamped by a frame since the request went out, the response is discarded and
 * the request re-run. Which of the two arrives last on the wire does not decide
 * it, because arrival order is not the order the facts happened in.
 *
 * Every cache below runs on the **default microtask scheduler**, for the reason
 * `ordering.test.ts` gives: a manual scheduler plus a flush in the right place
 * closes the very gap these tests exist to open.
 *
 * The frames are applied through `QueryCache.applyFrames` directly rather than
 * through a socket and a binder. That is deliberate -- the interleaving under
 * test is between a frame *commit* and a response *arrival*, and putting a
 * decoder, a scheduler and a fake socket in between would add promise hops
 * without adding anything to the question.
 */

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const created: StreamBinding = {
  channel: '/ws/orders',
  message: 'order.created',
  entity: 'Order',
  intent: 'upsert',
  invalidates: ['Order[]'],
};

const updated: StreamBinding = {
  channel: '/ws/orders',
  message: 'order.updated',
  entity: 'Order',
  intent: 'patch',
  invalidates: [],
};

const deleted: StreamBinding = {
  channel: '/ws/orders',
  message: 'order.deleted',
  entity: 'Order',
  intent: 'evict',
  invalidates: ['Order[]'],
};

function frame(binding: StreamBinding, payload: unknown): StreamFrame {
  return { binding, payload };
}

/** A transport handing out pre-registered responses, one per call, in order. */
function scripted(responses: readonly unknown[], frameRestarts?: number) {
  const queue = [...responses];
  const calls: TransportRequest[] = [];
  const transport: Transport = {
    execute: (request) => {
      calls.push(request);

      return Promise.resolve(queue.shift());
    },
  };

  return {
    calls,
    cache: new QueryCache({
      transport,
      entities: schema,
      ...(frameRestarts === undefined ? {} : { frameRestarts }),
    }),
  };
}

describe('a frame that lands while a request is in flight', () => {
  it('is not overwritten by the response that predates it', async () => {
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [
      Promise.resolve([{ id: 7, total: 1 }]),
      // The refetch. Dispatched before the frame, answers after it, and carries
      // the pre-frame value -- which is not wrong of the server, merely old.
      gate.promise,
      Promise.resolve([{ id: 7, total: 100 }]),
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    /** Every value a subscriber could have rendered `Order:7` at. */
    const rendered: unknown[] = [];

    cache.subscribe(orderList, undefined, () => {
      const record = cache.store.getRecord('Order:7');

      if (record !== undefined) rendered.push(record.data['total']);
    });

    await settleMicrotasks();
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(1);

    const refetching = cache.refetch(orderList);
    await settleMicrotasks();
    expect(calls).toHaveLength(2);

    // The server pushes. This is newer than the answer already on its way.
    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(100);

    const seenBeforeTheRace = rendered.length;

    // And now the pre-frame response arrives.
    gate.resolve([{ id: 7, total: 1 }]);
    await settleMicrotasks();

    // Never rolled back to 1 -- not in the store, and not in any notification a
    // component could have rendered from.
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(100);
    expect(rendered.slice(seenBeforeTheRace)).not.toContain(1);

    // Discarded and re-run rather than committed. Three requests: the load, the
    // refetch that was thrown away, and the one dispatched after the frame.
    expect(calls).toHaveLength(3);

    await expect(refetching).resolves.toEqual([{ id: 7, total: 100 }]);
    expect(cache.getState(orderList).data).toEqual([{ id: 7, total: 100 }]);
  });

  it('converges in exactly one re-run, because the re-run postdates the frame', async () => {
    // The property that makes this a guarantee rather than a retry loop: the
    // second attempt is dispatched *after* the frame, so it carries a stamp the
    // frame cannot be newer than -- and it commits whatever it returns, even
    // the same stale body the first attempt was rejected for.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [
      Promise.resolve([{ id: 7, total: 1 }]),
      gate.promise,
      // The server is lagging: the re-run gets the old value too.
      Promise.resolve([{ id: 7, total: 1 }]),
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    void cache.refetch(orderList).catch(() => undefined);
    await settleMicrotasks();

    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);

    gate.resolve([{ id: 7, total: 1 }]);
    await settleMicrotasks();

    expect(calls).toHaveLength(3);
    // Committed, and the store now reads what the server actually holds. The
    // client is not claiming the frame is right forever -- only that it will not
    // be undone by an answer that was already out when it landed.
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(1);
  });

  it('holds for an unmounted query, which no tag index can reach', async () => {
    // The third of the three bugs of this shape: nothing is subscribed, so the
    // registry's tag index holds no entry for this query and an
    // invalidation-driven fix would never see it. The stamp comparison is on
    // the response's own contents against the store's own records, so mount
    // state is not part of the question.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [gate.promise, Promise.resolve([{ id: 7, total: 100 }])];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    const fetching = cache.fetch(orderList);
    await settleMicrotasks();

    expect(cache.registry.mounted).toBe(0);

    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);
    gate.resolve([{ id: 7, total: 1 }]);
    await settleMicrotasks();

    expect(calls).toHaveLength(2);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(100);
    await expect(fetching).resolves.toEqual([{ id: 7, total: 100 }]);
  });

  it('does not let a pre-delete response resurrect the row', async () => {
    // An eviction leaves no record to carry the stamp, so without a tombstone
    // the deleted order comes back -- which reads as a caching bug and is one.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [
      Promise.resolve([
        { id: 7, total: 1 },
        { id: 8, total: 2 },
      ]),
      gate.promise,
      Promise.resolve([{ id: 8, total: 2 }]),
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(cache.store.has('Order:7')).toBe(true);

    void cache.refetch(orderList).catch(() => undefined);
    await settleMicrotasks();

    applyFrames(cache, [frame(deleted, { id: 7 })]);
    expect(cache.store.has('Order:7')).toBe(false);

    gate.resolve([
      { id: 7, total: 1 },
      { id: 8, total: 2 },
    ]);
    await settleMicrotasks();

    expect(cache.store.has('Order:7')).toBe(false);
    expect(calls).toHaveLength(3);
    expect(cache.getState(orderList).data).toEqual([{ id: 8, total: 2 }]);
  });

  it('does not resurrect it through a query the delete’s tags cannot reach', async () => {
    // The test above passes on the tag path alone -- a delete declares
    // `Order[]` and the list carries it. This one removes that help: nothing is
    // mounted, so the tag index is empty, and the tombstone is the only thing
    // standing between the pre-delete response and a row that comes back from
    // the dead.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [gate.promise, Promise.resolve([])];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.store.put('Order:7', { id: 7, total: 1 });

    const fetching = cache.fetch(orderList);
    await settleMicrotasks();
    expect(cache.registry.mounted).toBe(0);

    applyFrames(cache, [frame(deleted, { id: 7 })]);
    gate.resolve([{ id: 7, total: 1 }]);
    await settleMicrotasks();

    expect(cache.store.has('Order:7')).toBe(false);
    expect(calls).toHaveLength(2);
    await expect(fetching).resolves.toEqual([]);
  });
});

describe('a mutation that lost the race', () => {
  it('does not clobber the frame, and is never re-issued', async () => {
    // The mutation path is stamped exactly as the query path is, and converges
    // in the opposite way. Re-issuing a write is the duplicate-orders hazard --
    // the client cannot tell a request the server never saw from one it
    // processed -- so the response commits *around* the frame instead.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [gate.promise];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.store.put('Order:7', { id: 7, total: 1 });

    const patch: OperationMeta = {
      method: 'PATCH',
      path: '/orders/{id}',
      entity: 'Order',
      provides: [],
      invalidates: [],
    };

    const mutating = cache.mutate(patch, { path: { id: 7 } });
    await settleMicrotasks();
    expect(calls).toHaveLength(1);

    // The server pushes a newer value while this write is still out.
    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);

    // And the mutation's own response -- produced before the frame -- arrives.
    gate.resolve({ id: 7, total: 50 });
    const created = await mutating;

    // The frame stands, and exactly one request was made.
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(100);
    expect(calls).toHaveLength(1);

    // The caller is handed the current truth read back out of the store, not
    // its own superseded write.
    expect(created).toEqual({ id: 7, total: 100 });
  });

  it('still commits every entity the frame did not touch', async () => {
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [gate.promise];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    const bulk: OperationMeta = {
      method: 'POST',
      path: '/orders/bulk',
      entity: 'Order',
      provides: [],
      invalidates: [],
    };

    const mutating = cache.mutate(bulk, {});
    await settleMicrotasks();

    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);

    gate.resolve([
      { id: 7, total: 1 },
      { id: 8, total: 2 },
    ]);
    await mutating;

    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(100);
    expect(cache.store.getRecord('Order:8')?.data['total']).toBe(2);
    expect(calls).toHaveLength(1);
  });

  it('hands no undefined to the caller when its own entity was deleted mid-flight', async () => {
    // The narrow instance of the eviction defect, reached through the mutation
    // path rather than the skeleton one. Committing with `skip` leaves the
    // record absent, so reading the skeleton back resolves the mutation's *own*
    // entity to nothing -- and a scalar root then returns `undefined` typed as
    // `T`, which is a lie the type system cannot catch.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [gate.promise];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.store.put('Order:7', { id: 7, total: 1 });

    const patch: OperationMeta = {
      method: 'PATCH',
      path: '/orders/{id}',
      entity: 'Order',
      provides: [],
      invalidates: [],
    };

    const mutating = cache.mutate(patch, { path: { id: 7 } });
    await settleMicrotasks();

    // Somebody else deletes the order this write is editing.
    applyFrames(cache, [frame(deleted, { id: 7 })]);
    expect(cache.store.has('Order:7')).toBe(false);

    gate.resolve({ id: 7, total: 50 });
    const created = await mutating;

    // What the server said, not a corpse read back out of a store that no
    // longer holds the row.
    expect(created).toBeDefined();
    expect(created).toEqual({ id: 7, total: 50 });

    // And the delete stands: the mutation did not resurrect it.
    expect(cache.store.has('Order:7')).toBe(false);
    expect(calls).toHaveLength(1);
  });

  it('declines placement rather than splicing a deleted entity into a list', async () => {
    // The damaging half. `[created, ...current]` over an `undefined` `created`
    // produces `[undefined, {...}]`; `adopt` re-normalizes it, and a *literal*
    // `undefined` is deliberately not a hole -- only a dangling reference is --
    // so it survives into the rendered value and `data.map(o => o.id)` throws.
    //
    // The entity has to be one the store *held*, because that is what leaves a
    // tombstone: a delete for a key never cached is deliberately not remembered.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [
      Promise.resolve([
        { id: 7, total: 1 },
        { id: 8, total: 2 },
      ]),
      gate.promise,
      Promise.resolve([{ id: 8, total: 2 }]),
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });
    const placedWith: unknown[] = [];
    const rendered: unknown[][] = [];

    cache.subscribe(orderList, undefined, () => {
      rendered.push(cache.getState<unknown[]>(orderList).data ?? []);
    });
    await settleMicrotasks();
    expect(cache.store.has('Order:7')).toBe(true);

    const replace: OperationMeta = {
      method: 'PUT',
      path: '/orders/{id}',
      entity: 'Order',
      provides: [],
      invalidates: ['Order[]'],
    };

    const mutating = cache.mutate(replace, { path: { id: 7 } }, {
      place: {
        'Order[]': (made, current) => {
          placedWith.push(made);

          return [made, ...(current as unknown[])];
        },
      },
    });
    await settleMicrotasks();

    // Deleted out from under the write it is in the middle of.
    applyFrames(cache, [frame(deleted, { id: 7 })]);
    expect(cache.store.has('Order:7')).toBe(false);

    gate.resolve({ id: 7, total: 50 });
    await mutating;
    await settleMicrotasks();

    // Placement never ran, so nothing spliced a deleted row back into the list.
    expect(placedWith).toEqual([]);

    // Nothing any subscriber could have rendered contains a hole.
    for (const value of rendered) {
      expect(value).not.toContain(undefined);
      expect(() => value.map((order) => (order as { id: number }).id)).not.toThrow();
    }

    // The delete stands, and the list converged through the refetch that
    // declining placement fell back to.
    expect(cache.store.has('Order:7')).toBe(false);
    expect(cache.getState(orderList).data).toEqual([{ id: 8, total: 2 }]);
  });

  it('commits normally when no frame overtook it', async () => {
    const { cache, calls } = scripted([{ id: 7, total: 50 }]);

    const patch: OperationMeta = {
      method: 'PATCH',
      path: '/orders/{id}',
      entity: 'Order',
      provides: [],
      invalidates: [],
    };

    cache.store.put('Order:7', { id: 7, total: 1 });

    await expect(cache.mutate(patch, { path: { id: 7 } })).resolves.toEqual({ id: 7, total: 50 });
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(50);
    expect(calls).toHaveLength(1);
  });
});

describe('what the guarantee deliberately does not do', () => {
  it('commits a response dispatched after the frame, without re-running it', async () => {
    // The counterpart assertion. A rule of "restart whenever a frame has ever
    // landed" would pass the tests above and refetch forever; the comparison is
    // against the *dispatch* stamp, so an answer that already postdates the
    // frame is simply the answer.
    const { cache, calls } = scripted([
      [{ id: 7, total: 1 }],
      [{ id: 7, total: 250 }],
    ]);

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);
    await settleMicrotasks();

    await cache.refetch(orderList);

    expect(calls).toHaveLength(2);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(250);
  });

  it('does not restart a response over a frame that touched a different entity', async () => {
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [Promise.resolve([{ id: 7, total: 1 }]), gate.promise];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    void cache.refetch(orderList).catch(() => undefined);
    await settleMicrotasks();

    // A different order entirely, patched -- so no tag is raised either, and the
    // stamp comparison is the only thing that could restart the request.
    applyFrames(cache, [frame(updated, { id: 9, total: 5 })]);

    gate.resolve([{ id: 7, total: 3 }]);
    await settleMicrotasks();

    expect(calls).toHaveLength(2);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(3);
    expect(cache.store.getRecord('Order:9')?.data['total']).toBe(5);
  });

  it('still restarts it when the frame’s own tags say membership moved', async () => {
    // Not the stamp path -- the tag path, which is chunk 3's and which a frame
    // reaches through the same `Invalidator` a mutation does. An `upsert`
    // declares `Order[]`, the mounted list carries it, and a list refetched
    // from before the create would be missing a row it should now contain.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [
      Promise.resolve([{ id: 7, total: 1 }]),
      gate.promise,
      Promise.resolve([
        { id: 9, total: 5 },
        { id: 7, total: 1 },
      ]),
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    void cache.refetch(orderList).catch(() => undefined);
    await settleMicrotasks();

    applyFrames(cache, [frame(created, { id: 9, total: 5 })]);

    gate.resolve([{ id: 7, total: 1 }]);
    await settleMicrotasks();

    expect(calls).toHaveLength(3);
    expect(cache.getState(orderList).data).toEqual([
      { id: 9, total: 5 },
      { id: 7, total: 1 },
    ]);
  });

  it('commits around the frames rather than looping, once the restart bound is spent', async () => {
    // The escape valve. A channel busy enough that a frame lands inside every
    // request window would restart the sequence forever under an unbounded
    // rule, and a query that never settles is worse than the defect avoided.
    // Past the bound the response commits -- but the entities a frame overtook
    // keep the frame's value, so nothing visibly reverts.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [
      Promise.resolve([
        { id: 7, total: 1 },
        { id: 8, total: 2 },
      ]),
      gate.promise,
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema, frameRestarts: 0 });

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    void cache.refetch(orderList).catch(() => undefined);
    await settleMicrotasks();

    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);

    gate.resolve([
      { id: 7, total: 1 },
      { id: 8, total: 22 },
    ]);
    await settleMicrotasks();

    // No third request, and the frame still stands for the entity it wrote.
    expect(calls).toHaveLength(2);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(100);
    // Everything the frame did not touch commits normally.
    expect(cache.store.getRecord('Order:8')?.data['total']).toBe(22);
  });
});

describe('the frame stamp itself', () => {
  it('survives a later response merging fields into the record', async () => {
    // A response that legitimately commits must not erase the record's memory
    // of having been overtaken, or the *next* in-flight request loses the
    // protection.
    const { cache } = scripted([[{ id: 7, total: 1 }]]);

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);
    const stamped = cache.store.frameVersion;

    cache.store.put('Order:7', { note: 'from a later response' });

    expect(cache.store.getRecord('Order:7')?.frameAt).toBe(stamped);
    expect(cache.store.racedSince(['Order:7'], stamped - 1)).toEqual(['Order:7']);
  });

  it('is recorded even when the frame changed nothing, and bumps no version', () => {
    const { cache } = scripted([]);

    cache.store.put('Order:7', { id: 7, total: 5 });
    const before = cache.store.getRecord('Order:7');

    applyFrames(cache, [frame(updated, { id: 7, total: 5 })]);

    const after = cache.store.getRecord('Order:7');

    // Same data, same version -- so nothing re-renders -- but the record now
    // knows a frame confirmed it, and an older response cannot move it.
    expect(after?.data).toEqual(before?.data);
    expect(after?.version).toBe(before?.version);
    expect(after?.frameAt).toBe(cache.store.frameVersion);
  });

  it('keeps the siblings of a raced entity rather than losing them to a failed re-run', async () => {
    // Only the raced keys are stale; every other entity in the rejected
    // response is at least as new as the store's. Discarding them wholesale
    // loses them for good when the re-run then fails, leaving the query on
    // `status: 'error'` holding data older than an answer it did receive.
    const gate = deferred<unknown>();
    const calls: TransportRequest[] = [];
    const responses: unknown[] = [
      Promise.resolve([
        { id: 7, total: 1 },
        { id: 8, total: 2 },
      ]),
      gate.promise,
      Promise.reject(new Error('gateway timeout')),
    ];
    const transport: Transport = {
      execute: (request) => {
        calls.push(request);

        return responses.shift() as Promise<unknown>;
      },
    };
    const cache = new QueryCache({ transport, entities: schema });

    cache.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    void cache.refetch(orderList).catch(() => undefined);
    await settleMicrotasks();

    applyFrames(cache, [frame(updated, { id: 7, total: 100 })]);

    gate.resolve([
      { id: 7, total: 1 },
      { id: 8, total: 222 },
    ]);
    await settleMicrotasks();

    expect(calls).toHaveLength(3);
    expect(cache.getState(orderList).status).toBe('error');

    // The frame still stands for the entity it wrote...
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(100);
    // ...and the sibling that arrived in the rejected response was not thrown
    // away with it.
    expect(cache.store.getRecord('Order:8')?.data['total']).toBe(222);
  });

  it('bounds the tombstones rather than growing one per delete forever', () => {
    const { cache } = scripted([]);

    // Deletes for keys this client never cached. There is no request that could
    // be carrying a record it never asked for, so there is nothing to protect
    // and nothing worth remembering.
    for (let i = 0; i < 5000; i++) {
      applyFrames(cache, [frame(deleted, { id: `ghost-${String(i)}` })]);
    }

    expect(cache.store.size).toBe(0);
    expect(cache.store.tombstones).toBe(0);

    // Deletes for keys it did hold are remembered, but capped.
    for (let i = 0; i < 5000; i++) {
      const key = `Order:${String(i)}`;
      cache.store.put(key, { id: i, total: 1 });
      applyFrames(cache, [frame(deleted, { id: i })]);
    }

    expect(cache.store.size).toBe(0);
    expect(cache.store.tombstones).toBeLessThanOrEqual(256);

    // And the most recent delete -- the only one any in-flight request could
    // still be racing -- is the one that survived.
    expect(cache.store.frameStamp('Order:4999')).toBeGreaterThan(0);
  });

  it('hands a tombstone’s stamp to the record that replaces it', () => {
    const { cache } = scripted([]);

    cache.store.put('Order:7', { id: 7, total: 5 });
    applyFrames(cache, [frame(deleted, { id: 7 })]);

    const stamp = cache.store.frameVersion;
    expect(cache.store.frameStamp('Order:7')).toBe(stamp);

    // The row comes back through a response dispatched after the delete. The
    // stamp moves onto the record, so the tombstone map does not grow with
    // every entity the session ever deleted.
    cache.store.put('Order:7', { id: 7, total: 9 });

    expect(cache.store.getRecord('Order:7')?.frameAt).toBe(stamp);
    expect(cache.store.frameStamp('Order:7')).toBe(stamp);
  });
});
