import { describe, expect, it, vi } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { OverlayStack, targetOf } from '../src/overlay';
import type { EntityPatch } from '../src/overlay';
import { makeRef } from '../src/ref';
import { EntityStore } from '../src/store';
import type { EntityKey } from '../src/types';
import type { OperationMeta } from '../src/transport';
import { deferred, fakeTransport, settleMicrotasks } from './harness';
import { schema } from './schema';

function host(): { store: EntityStore; stack: OverlayStack } {
  const store = new EntityStore();
  const stack = new OverlayStack(store);

  store.overlays = stack;

  return { store, stack };
}

function merge(fields: Record<string, unknown>): EntityPatch {
  return { kind: 'merge', source: fields };
}

function compute(fn: (prev: Record<string, unknown>) => Record<string, unknown>): EntityPatch {
  return { kind: 'merge', source: fn };
}

function patches(entries: [EntityKey, EntityPatch][]): Map<EntityKey, EntityPatch> {
  return new Map(entries);
}

function order(store: EntityStore, key: EntityKey): Record<string, unknown> | undefined {
  return store.read<Record<string, unknown> | undefined>(makeRef(key));
}

describe('the entity plane', () => {
  it('shows a merged patch over the base record', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open', total: 99 });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));

    // The record is still overlaid at read time, so it carries the OPTIMISTIC
    // symbol -- see store.ts. toEqual DOES compare symbol-keyed properties, so
    // an overlaid record is asserted with objectContaining rather than against
    // a plain literal.
    expect(order(store, 'Order:7')).toEqual(
      expect.objectContaining({ id: 7, status: 'shipped', total: 99 }),
    );
  });

  it('rebases: dropping the FIRST of two overlays keeps the second', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open', note: '' });

    const first = stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    stack.add(patches([['Order:7', merge({ note: 'gift' })]]));

    stack.take(first);

    // The second overlay is still live, so this is still an overlaid read.
    expect(order(store, 'Order:7')).toEqual(
      expect.objectContaining({ id: 7, status: 'open', note: 'gift' }),
    );
  });

  it('composes computed patches, and recomputes them on a refold', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, likes: 0 });

    const first = stack.add(
      patches([['Order:7', compute((prev) => ({ likes: (prev.likes as number) + 1 }))]]),
    );
    stack.add(patches([['Order:7', compute((prev) => ({ likes: (prev.likes as number) + 1 }))]]));

    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, likes: 2 }));

    stack.take(first);

    // Re-run against the reverted base, NOT rolled back by one. The second
    // overlay is still live, so this remains an overlaid read.
    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, likes: 1 }));
  });

  it('refolds over a base write, so a stream frame lands underneath the patch', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open', total: 99 });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    store.put('Order:7', { total: 120 }, 1);

    expect(order(store, 'Order:7')).toEqual(
      expect.objectContaining({ id: 7, status: 'shipped', total: 120 }),
    );
  });

  it('is a no-op over a record an evicting frame removed', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open' });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    store.evict('Order:7', 1);

    expect(order(store, 'Order:7')).toBeUndefined();
  });

  it('deletes, and restores on drop', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7 });

    const id = stack.add(patches([['Order:7', { kind: 'delete' }]]));
    expect(order(store, 'Order:7')).toBeUndefined();

    stack.take(id);
    expect(order(store, 'Order:7')).toEqual({ id: 7 });
  });

  it('creates a record that base never held', () => {
    const { store, stack } = host();

    stack.add(patches([['Order:~opt1', { kind: 'create', fields: { id: '~opt1', total: 99 } }]]));

    expect(order(store, 'Order:~opt1')).toEqual(
      expect.objectContaining({ id: '~opt1', total: 99 }),
    );
    expect(store.has('Order:~opt1')).toBe(false);
  });

  it('keeps the identity of records no overlay touches', () => {
    const { store, stack } = host();
    const { skeleton } = store.write(
      [{ id: 7 }, { id: 8 }],
      { Order: { idField: 'id' } },
      'Order',
    );
    const before = store.read<unknown[]>(skeleton);

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    const after = store.read<unknown[]>(skeleton);

    expect(after[1]).toBe(before[1]);
    expect(after[0]).not.toBe(before[0]);
  });

  it('reports a throwing compute patch and treats it as no change', () => {
    const report = vi.fn();
    const store = new EntityStore();
    const stack = new OverlayStack(store, report);
    store.overlays = stack;
    store.put('Order:7', { id: 7, status: 'open' });

    stack.add(
      patches([
        [
          'Order:7',
          compute(() => {
            throw new Error('boom');
          }),
        ],
      ]),
    );

    // Still overlaid -- the throwing patch is a no-op on the DATA, not a
    // removal of the overlay itself -- so this is still an overlaid read.
    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, status: 'open' }));
    expect(report).toHaveBeenCalledWith(expect.any(Error), 'optimistic');
  });

  it('promote writes merges into base and reports delete targets', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open' });
    store.put('Order:8', { id: 8 });

    const id = stack.add(
      patches([
        ['Order:7', merge({ status: 'shipped' })],
        ['Order:8', { kind: 'delete' }],
        ['Order:~opt1', { kind: 'create', fields: { id: '~opt1' } }],
      ]),
    );

    const buried = stack.promote(stack.take(id) as never);

    expect(store.getRecord('Order:7')?.data).toEqual({ id: 7, status: 'shipped' });
    expect(store.has('Order:8')).toBe(false);
    // A create is never promoted: the real entity arrives in the response.
    expect(store.has('Order:~opt1')).toBe(false);
    expect(buried).toEqual(['Order:8']);
  });

  it('promote evaluates a computed source against raw base, never the fold', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, likes: 0 });

    const first = stack.add(
      patches([['Order:7', compute((prev) => ({ likes: (prev.likes as number) + 1 }))]]),
    );
    stack.add(patches([['Order:7', compute((prev) => ({ likes: (prev.likes as number) + 1 }))]]));

    // Both increments are live: the reader sees them composed.
    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, likes: 2 }));

    // `first` is taken off the stack before it is promoted -- promote must
    // read `first`'s own compute source against RAW base, not against the
    // fold, which (with `first` gone but the second overlay still live)
    // already carries one increment. Reading through the fold here would
    // apply `first`'s increment a second time.
    stack.promote(stack.take(first) as never);

    expect(store.getRecord('Order:7')?.data).toEqual({ id: 7, likes: 1 });

    // The still-live second overlay refolds over the new base: 1 (written by
    // promote) + 1 (its own increment) = 2. A double-applying promote would
    // have left base at 2 and this would read 3.
    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, likes: 2 }));
  });

  it('promote never invokes a computed source when base is gone: merge over an absent record is a no-op', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, likes: 0 });
    const source = vi.fn((prev: Record<string, unknown>) => ({
      likes: (prev.likes as number) + 1,
    }));

    const id = stack.add(patches([['Order:7', { kind: 'merge', source }]]));
    store.evict('Order:7');

    const entry = stack.take(id) as never;
    stack.promote(entry);

    expect(source).not.toHaveBeenCalled();
    expect(store.has('Order:7')).toBe(false);
  });

  it('clear drops every overlay', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open' });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    stack.clear();

    expect(stack.empty).toBe(true);
    expect(order(store, 'Order:7')).toEqual({ id: 7, status: 'open' });
  });
});

const patch: OperationMeta = {
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  provides: [],
  invalidates: ['Order:{id}', 'Order[]'],
};

describe('deriving the target from what a mutation invalidates', () => {
  it('finds the one entity-key tag', () => {
    expect(targetOf(patch, { path: { id: 7 } })).toBe('Order:7');
  });

  it('says create when no tag names an entity key', () => {
    const create: OperationMeta = {
      method: 'POST',
      path: '/orders',
      entity: 'Order',
      provides: [],
      invalidates: ['Order[]'],
    };

    expect(targetOf(create, {})).toBeUndefined();
  });

  it('is not fooled by a parameterised COLLECTION tag', () => {
    const archive: OperationMeta = {
      method: 'POST',
      path: '/orders/archive',
      entity: 'Order',
      provides: [],
      invalidates: ['Order[]:{req.archived}'],
    };

    expect(targetOf(archive, { body: { archived: true } })).toBeUndefined();
  });

  it('reports ambiguity rather than guessing between two entities', () => {
    const transfer: OperationMeta = {
      method: 'POST',
      path: '/orders/{id}/transfer',
      entity: 'Order',
      provides: [],
      invalidates: ['Order:{id}', 'Customer:{req.customerId}'],
    };

    expect(targetOf(transfer, { path: { id: 7 }, body: { customerId: 3 } })).toBe('ambiguous');
  });

  it('ignores a tag that resolves to nothing', () => {
    expect(targetOf(patch, {})).toBeUndefined();
  });
});

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const orderPatch: OperationMeta = {
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  provides: [],
  invalidates: ['Order:{id}', 'Order[]'],
};

const orderDelete: OperationMeta = {
  method: 'DELETE',
  path: '/orders/{id}',
  entity: 'Order',
  provides: [],
  invalidates: ['Order:{id}', 'Order[]'],
};

function optimisticCache(handler: Parameters<typeof fakeTransport>[0]) {
  const scheduler = manualScheduler();
  const transport = fakeTransport(handler);

  return {
    scheduler,
    transport,
    cache: new QueryCache({ transport, entities: schema, scheduler: scheduler.schedule }),
  };
}

describe('optimistic mutations', () => {
  it('shows the patch before the server answers, and keeps it after', async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 7, status: 'open' }] : gate.promise,
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    const pending = queries.mutate(
      orderPatch,
      { path: { id: 7 }, body: { status: 'shipped' } },
      { optimistic: { status: 'shipped' } },
    );

    // Still overlaid -- the response has not landed yet -- so this record
    // carries the OPTIMISTIC symbol. See store.ts and the precedent in
    // overlay.test.ts's first `describe` block.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: 7, status: 'shipped' }),
    ]);

    gate.resolve({ id: 7, status: 'shipped' });
    await pending;

    // Settled: the overlay was taken and promoted, so this is a plain read.
    expect(queries.getState(orderList).data).toEqual([{ id: 7, status: 'shipped' }]);
  });

  it('reverts on failure, raises no tags, and schedules no refetch', async () => {
    const gate = deferred<unknown>();
    const { cache: queries, scheduler, transport } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 7, status: 'open' }] : gate.promise,
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);
    const calls = transport.calls.length;

    const pending = queries.mutate(
      orderPatch,
      { path: { id: 7 }, body: { status: 'shipped' } },
      { optimistic: { status: 'shipped' } },
    );

    // Still overlaid -- see the note in the previous test.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: 7, status: 'shipped' }),
    ]);

    gate.reject(new Error('nope'));
    await expect(pending).rejects.toThrow('nope');
    await settleMicrotasks();

    // The overlay was taken and never promoted, so base was never touched.
    expect(queries.getState(orderList).data).toEqual([{ id: 7, status: 'open' }]);
    expect(scheduler.pending()).toBe(false);
    expect(transport.calls.length).toBe(calls + 1);
  });

  it('keeps a later mutation when an EARLIER one fails', async () => {
    const first = deferred<unknown>();
    const second = deferred<unknown>();
    let writes = 0;
    const { cache: queries } = optimisticCache((request) => {
      if (request.meta.method === 'GET') return [{ id: 7, status: 'open', note: '' }];

      writes++;

      return writes === 1 ? first.promise : second.promise;
    });

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    const a = queries.mutate(
      orderPatch,
      { path: { id: 7 }, body: { status: 'shipped' } },
      { optimistic: { status: 'shipped' } },
    );
    const b = queries.mutate(
      orderPatch,
      { path: { id: 7 }, body: { note: 'gift' } },
      { optimistic: { note: 'gift' } },
    );

    // Both overlays are still live, composed onto one record.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: 7, status: 'shipped', note: 'gift' }),
    ]);

    first.reject(new Error('nope'));
    await expect(a).rejects.toThrow('nope');
    await settleMicrotasks();

    // The second survives, on the REVERTED base. `b` is still a live overlay,
    // so this is still an overlaid read.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: 7, status: 'open', note: 'gift' }),
    ]);

    second.resolve({ id: 7, status: 'open', note: 'gift' });
    await b;
  });

  it('does not flash a deleted row back when the server returns no content', async () => {
    const gate = deferred<unknown>();
    const seen: unknown[] = [];
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 7 }, { id: 8 }] : gate.promise,
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => {
      seen.push(queries.getState(orderList).data);
    });

    const pending = queries.mutate(orderDelete, { path: { id: 7 } }, { optimistic: 'delete' });

    expect(queries.getState(orderList).data).toEqual([{ id: 8 }]);

    gate.resolve(undefined);
    await pending;
    await settleMicrotasks();

    expect(queries.getState(orderList).data).toEqual([{ id: 8 }]);
    // Not once, at any point, did the row come back.
    for (const value of seen) expect(value).not.toContainEqual({ id: 7 });
  });

  it('does not resurrect a deleted row from a body-returning DELETE', async () => {
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 7 }, { id: 8 }] : { id: 7 },
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    await queries.mutate(orderDelete, { path: { id: 7 } }, { optimistic: 'delete' });

    expect(queries.getState(orderList).data).toEqual([{ id: 8 }]);
  });

  it('reports an ambiguous target and runs the mutation without an overlay', async () => {
    const errors: string[] = [];
    const scheduler = manualScheduler();
    const transport = fakeTransport(() => ({ id: 7 }));
    const queries = new QueryCache({
      transport,
      entities: schema,
      scheduler: scheduler.schedule,
      onError: (_error, context) => errors.push(context),
    });

    const transfer: OperationMeta = {
      method: 'POST',
      path: '/orders/{id}/transfer',
      entity: 'Order',
      provides: [],
      invalidates: ['Order:{id}', 'Customer:{req.customerId}'],
    };

    await queries.mutate(
      transfer,
      { path: { id: 7 }, body: { customerId: 3 } },
      { optimistic: { status: 'moved' } },
    );

    expect(errors).toContain('optimistic');
    expect(queries.overlays.empty).toBe(true);
  });

  it('drops every overlay when the principal changes', async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 7, status: 'open' }] : gate.promise,
    );

    await queries.fetch(orderList);
    const pending = queries.mutate(
      orderPatch,
      { path: { id: 7 }, body: { status: 'shipped' } },
      { optimistic: { status: 'shipped' } },
    );

    expect(queries.overlays.empty).toBe(false);

    queries.setPrincipal('someone-else');

    expect(queries.overlays.empty).toBe(true);

    gate.resolve({ id: 7, status: 'shipped' });
    await pending.catch(() => undefined);
  });

  it('keeps an in-flight overlay out of base when another mutation places over it', async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = optimisticCache((request) => {
      if (request.meta.method === 'GET') return [{ id: 7, status: 'open' }];
      if (request.meta.method === 'PATCH') return gate.promise;

      return { id: 9, status: 'new' };
    });

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    // A: still pending. Its overlay is live on Order:7 for the whole test.
    const pending = queries.mutate(
      orderPatch,
      { path: { id: 7 }, body: { status: 'shipped' } },
      { optimistic: { status: 'shipped' } },
    );

    const orderCreate: OperationMeta = {
      method: 'POST',
      path: '/orders',
      entity: 'Order',
      provides: [],
      invalidates: ['Order[]'],
    };

    // B: a placement callback, handed `current` including A's still-pending,
    // entity-plane-projected 'shipped' -- and `adopt` re-normalizes whatever
    // it returns straight into base.
    await queries.mutate(orderCreate, {}, {
      place: { 'Order[]': (created, current) => [created, ...(current as unknown[])] },
    });

    // Base must not carry A's unconfirmed field: `adopt` skipped Order:7
    // because A's overlay was still on the stack when it committed.
    expect(queries.store.getRecord('Order:7')?.data).toEqual({ id: 7, status: 'open' });

    gate.reject(new Error('nope'));
    await expect(pending).rejects.toThrow('nope');
    await settleMicrotasks();

    // A failed. Nothing was ever permanently corrupted by the leak -- the
    // record still reads as its original value.
    expect(queries.store.getRecord('Order:7')?.data).toEqual({ id: 7, status: 'open' });
  });

  it('declines placement for an optimistic delete instead of placing a hole', async () => {
    const placed = vi.fn();
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 7 }, { id: 8 }] : { id: 7 },
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    const result = await queries.mutate(
      orderDelete,
      { path: { id: 7 } },
      { optimistic: 'delete', place: { 'Order[]': placed } },
    );

    // Declined, not placed with a `[undefined, ...current]` hole: the
    // callback never ran, exactly as a raced delete already declines it.
    expect(placed).not.toHaveBeenCalled();
    // What the server actually said, not `undefined` from resolving the
    // mutation's own evicted root out of the skeleton.
    expect(result).toEqual({ id: 7 });
    expect(queries.getState(orderList).data).toEqual([{ id: 8 }]);
  });

  it('still dispatches and settles when a subscriber throws during the pre-dispatch push', async () => {
    const { cache: queries, transport } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 7, status: 'open' }] : { id: 7, status: 'shipped' },
    );

    await queries.fetch(orderList);

    let notifications = 0;
    queries.subscribe(orderList, undefined, () => {
      notifications++;

      // Only the FIRST notification is the pre-dispatch one `push` raises by
      // adding the overlay -- throwing there, before the request goes out,
      // is the hazard being guarded against.
      if (notifications === 1) throw new Error('subscriber boom');
    });

    const result = await queries.mutate(
      orderPatch,
      { path: { id: 7 }, body: { status: 'shipped' } },
      { optimistic: { status: 'shipped' } },
    );

    expect(result).toEqual({ id: 7, status: 'shipped' });
    expect(transport.calls.some((call) => call.meta.method === 'PATCH')).toBe(true);
    // More than the one throwing call: the mutation reached settle and
    // notified again, so it was not aborted by the earlier throw.
    expect(notifications).toBeGreaterThan(1);
  });
});

const orderCreate: OperationMeta = {
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  provides: [],
  invalidates: ['Order[]'],
};

const prepend = { 'Order[]': (made: unknown, current: unknown) => [made, ...(current as unknown[])] };

describe('optimistic create', () => {
  it('places a temp row immediately and replaces it with the real one', async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 8, total: 12 }] : gate.promise,
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    const pending = queries.mutate(
      orderCreate,
      { body: { total: 99 } },
      { optimistic: { total: 99 }, place: prepend },
    );

    // The temp row is still overlaid at this point, so it carries the
    // OPTIMISTIC symbol -- see the note at the top of the file.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: '~opt1', total: 99 }),
      { id: 8, total: 12 },
    ]);

    gate.resolve({ id: 9, total: 99 });
    await pending;
    await settleMicrotasks();

    expect(queries.getState(orderList).data).toEqual([
      { id: 9, total: 99 },
      { id: 8, total: 12 },
    ]);
    // The temp record never entered base.
    expect(queries.store.has('Order:~opt1')).toBe(false);
  });

  it('places concurrent creates in push order, and drops one cleanly', async () => {
    const first = deferred<unknown>();
    const second = deferred<unknown>();
    let writes = 0;
    const { cache: queries } = optimisticCache((request) => {
      if (request.meta.method === 'GET') return [{ id: 8 }];

      writes++;

      return writes === 1 ? first.promise : second.promise;
    });

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    const a = queries.mutate(
      orderCreate,
      { body: { total: 1 } },
      { optimistic: { total: 1 }, place: prepend },
    );
    const b = queries.mutate(
      orderCreate,
      { body: { total: 2 } },
      { optimistic: { total: 2 }, place: prepend },
    );

    // Both temp rows are still overlaid, so both carry the OPTIMISTIC symbol.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: '~opt2', total: 2 }),
      expect.objectContaining({ id: '~opt1', total: 1 }),
      { id: 8 },
    ]);

    first.reject(new Error('nope'));
    await expect(a).rejects.toThrow('nope');
    await settleMicrotasks();

    // The second create survives, rebased onto the list without the first --
    // still overlaid, so still carrying the symbol.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: '~opt2', total: 2 }),
      { id: 8 },
    ]);

    second.resolve({ id: 9, total: 2 });
    await b;
  });

  it('leaves an enveloped query alone and reports that it did', async () => {
    const errors: string[] = [];
    const scheduler = manualScheduler();
    const gate = deferred<unknown>();
    const transport = fakeTransport((request) =>
      request.meta.method === 'GET' ? { items: [{ id: 8 }], total: 1 } : gate.promise,
    );
    const queries = new QueryCache({
      transport,
      entities: schema,
      scheduler: scheduler.schedule,
      onError: (_error, context) => errors.push(context),
    });

    const paged: OperationMeta = {
      method: 'GET',
      path: '/paged-orders',
      entity: 'Order',
      rootType: 'Envelope',
      provides: ['Order[]'],
      invalidates: [],
    };

    await queries.fetch(paged);
    queries.subscribe(paged, undefined, () => undefined);

    const pending = queries.mutate(
      orderCreate,
      { body: { total: 99 } },
      { optimistic: { total: 99 }, place: prepend },
    );

    expect(queries.getState(paged).data).toEqual({ items: [{ id: 8 }], total: 1 });
    expect(errors).toContain('optimistic');

    gate.resolve({ id: 9, total: 99 });
    await pending;
  });

  it('keeps the projected value referentially stable while nothing moves', async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 8 }] : gate.promise,
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    const pending = queries.mutate(
      orderCreate,
      { body: { total: 99 } },
      { optimistic: { total: 99 }, place: prepend },
    );

    const once = queries.getState(orderList).data;
    const twice = queries.getState(orderList).data;

    expect(twice).toBe(once);

    gate.resolve({ id: 9, total: 99 });
    await pending;
  });

  it('reports nothing for a still-loading list, and places once it settles', async () => {
    const errors: string[] = [];
    const listGate = deferred<unknown>();
    const gate = deferred<unknown>();
    const transport = fakeTransport((request) =>
      request.meta.method === 'GET' ? listGate.promise : gate.promise,
    );
    const scheduler = manualScheduler();
    const queries = new QueryCache({
      transport,
      entities: schema,
      scheduler: scheduler.schedule,
      onError: (_error, context) => errors.push(context),
    });

    // The list is mounted but its first fetch has not answered yet: `base`
    // returns `undefined` until `record.settled`, which is not the same thing
    // as an enveloped query with the wrong shape.
    queries.subscribe(orderList, undefined, () => undefined);

    const pending = queries.mutate(
      orderCreate,
      { body: { total: 99 } },
      { optimistic: { total: 99 }, place: prepend },
    );

    expect(queries.getState(orderList).data).toBeUndefined();
    expect(errors).not.toContain('optimistic');

    listGate.resolve([{ id: 8, total: 12 }]);
    await settleMicrotasks();

    // Once the list has a value, the still-pending create is placed onto it.
    expect(queries.getState(orderList).data).toEqual([
      expect.objectContaining({ id: '~opt1', total: 99 }),
      { id: 8, total: 12 },
    ]);
    expect(errors).not.toContain('optimistic');

    gate.resolve({ id: 9, total: 99 });
    await pending;
  });

  it("keeps a settling query's registry value entity-plane only while a create overlay is live", async () => {
    const gate = deferred<unknown>();
    const { cache: queries } = optimisticCache((request) =>
      request.meta.method === 'GET' ? [{ id: 8, total: 12 }] : gate.promise,
    );

    await queries.fetch(orderList);
    queries.subscribe(orderList, undefined, () => undefined);

    // The overlay stays live -- its mutation never resolves in this test.
    const pending = queries.mutate(
      orderCreate,
      { body: { total: 99 } },
      { optimistic: { total: 99 }, place: prepend },
    );

    // Force the list through `settle` again while the overlay is still on
    // the stack, rather than relying on the order two adjacent lines in
    // `mutate` happen to run in.
    await queries.refetch(orderList);

    // `entry.value` is what a REAL placement callback is handed as `current`
    // on the settle path, and its return reaches `adopt` -> `store.commit`
    // unchanged. It must never carry the pending create's temp row.
    const entry = queries.registry.get(queries.key(orderList));

    expect(entry?.value).toEqual([{ id: 8, total: 12 }]);

    gate.resolve({ id: 9, total: 99 });
    await pending;
  });
});
