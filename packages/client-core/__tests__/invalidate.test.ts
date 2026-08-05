import { describe, expect, it, vi } from 'vitest';

import { Invalidator, manualScheduler } from '../src/invalidate';
import type { InvalidatorOptions } from '../src/invalidate';
import { QueryRegistry } from '../src/registry';
import type { QueryEntry } from '../src/registry';
import { queryKey } from '../src/tags';

/**
 * The whole chunk under test with no network and no timers.
 *
 * `execute` records the batches it was handed; the scheduler runs nothing
 * until `flush()`. Every coalescing assertion below is therefore about what
 * the code did, not about whether a machine got round to it in time.
 */
function harness(options: Partial<InvalidatorOptions> = {}) {
  const batches: string[][] = [];
  const scheduler = manualScheduler();
  const registry = new QueryRegistry();
  const invalidator = new Invalidator(registry, {
    execute: (batch) => batches.push(batch.map((entry) => entry.key)),
    scheduler: scheduler.schedule,
    onUnresolved: () => {},
    ...options,
  });

  function mount(operation: string, provides: string[], args = {}) {
    const key = queryKey(operation, args);
    const unmount = registry.mount({ operation, args, provides });

    return { key, unmount };
  }

  return { batches, scheduler, registry, invalidator, mount };
}

describe('invalidation', () => {
  it('hits exactly the queries carrying an invalidated tag', () => {
    const { batches, scheduler, invalidator, mount } = harness();

    const list = mount('orderList', ['Order[]']);
    const detail = mount('orderGet', ['Order:7'], { path: { id: 7 } });
    const unrelated = mount('customerList', ['Customer[]']);

    invalidator.settled({ invalidates: ['Order[]'] });
    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
    expect(invalidator.registry.get(detail.key)?.stale).toBe(false);
    expect(invalidator.registry.get(unrelated.key)?.stale).toBe(false);
  });

  it('resolves the mutation template before matching', () => {
    const { batches, scheduler, invalidator, mount } = harness();

    const customer = mount('customerGet', ['Customer:{id}'], { path: { id: 'c-3' } });
    mount('customerGet', ['Customer:{id}'], { path: { id: 'c-9' } });

    invalidator.settled({
      invalidates: ['Customer:{req.customerId}'],
      args: { body: { customerId: 'c-3' } },
    });
    scheduler.flush();

    expect(batches).toEqual([[customer.key]]);
  });

  it('skips a tag that resolves to nothing and reports it, keeping the rest', () => {
    const onUnresolved = vi.fn();
    const { batches, scheduler, invalidator, mount } = harness({ onUnresolved });

    const list = mount('orderList', ['Order[]']);

    invalidator.settled({ invalidates: ['Order[]', 'Customer:{customerId}'] });
    scheduler.flush();

    expect(onUnresolved).toHaveBeenCalledWith('Customer:{customerId}', 'invalidates');
    // Not `Customer:` -- which would match nothing, fire nothing, report nothing.
    expect(invalidator.registry.queriesFor('Customer:')).toEqual([]);
    expect(batches).toEqual([[list.key]]);
  });
});

describe('coalescing', () => {
  it('turns N invalidated queries in one tick into one batch', () => {
    const { batches, scheduler, invalidator, mount } = harness();

    const a = mount('orderList', ['Order[]']);
    const b = mount('orderCount', ['Order[]']);
    const c = mount('orderStats', ['Order[]']);

    invalidator.settled({ invalidates: ['Order[]'] });

    expect(batches).toEqual([]);
    expect(scheduler.pending()).toBe(true);

    scheduler.flush();

    expect(batches).toHaveLength(1);
    expect(batches[0]).toEqual([a.key, b.key, c.key]);
  });

  it('refetches a query hit by two tags once', () => {
    const { batches, scheduler, invalidator, registry, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    registry.settle(list.key, { deps: ['Order:7'] });
    invalidator.settled({ invalidates: ['Order[]', 'Order:7'] });
    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
  });

  it('coalesces several mutations settling in the same tick', () => {
    const { batches, scheduler, invalidator, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    invalidator.settled({ invalidates: ['Order[]'] });
    invalidator.settled({ invalidates: ['Order[]'] });
    invalidator.settled({ invalidates: ['Order[]'] });
    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
  });

  it('schedules a fresh batch for the next tick', () => {
    const { batches, scheduler, invalidator, registry, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    invalidator.settled({ invalidates: ['Order[]'] });
    scheduler.flush();
    registry.settle(list.key, {});

    invalidator.settled({ invalidates: ['Order[]'] });
    scheduler.flush();

    expect(batches).toEqual([[list.key], [list.key]]);
  });

  it('runs one batch per microtask under the default scheduler', async () => {
    // Deterministic by ordering, not by timing: the invalidations below are
    // synchronous, so the batch is queued before this test awaits, and a
    // microtask queued first runs first. No sleeping, nothing to lose a race.
    const { batches, invalidator, mount } = harness({ scheduler: undefined });

    const a = mount('orderList', ['Order[]']);
    const b = mount('orderCount', ['Order[]']);

    invalidator.settled({ invalidates: ['Order[]'] });
    invalidator.settled({ invalidates: ['Order[]'] });

    expect(batches).toEqual([]);

    await Promise.resolve();

    expect(batches).toEqual([[a.key, b.key]]);
  });

  it('drops a query that unmounted between the invalidation and the flush', () => {
    const { batches, scheduler, invalidator, mount } = harness();

    const staying = mount('orderList', ['Order[]']);
    const leaving = mount('orderCount', ['Order[]']);

    invalidator.settled({ invalidates: ['Order[]'] });
    leaving.unmount();
    scheduler.flush();

    expect(batches).toEqual([[staying.key]]);
  });
});

describe('unmounted queries', () => {
  it('refetches on the next mount, and not before', () => {
    const { batches, scheduler, invalidator, registry, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    registry.settle(list.key, { value: ['a'] });
    list.unmount();

    invalidator.settled({ invalidates: ['Order[]'] });
    scheduler.flush();

    // Nobody is watching it, so nothing was fetched.
    expect(batches).toEqual([]);
    expect(scheduler.pending()).toBe(false);

    mount('orderList', ['Order[]']);
    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
    expect(registry.get(list.key)?.stale).toBe(true);
  });

  it('does not refetch a query that mounts having missed nothing', () => {
    const { batches, scheduler, invalidator, registry, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    registry.settle(list.key, {});
    list.unmount();

    invalidator.settled({ invalidates: ['Customer[]'] });
    mount('orderList', ['Order[]']);
    scheduler.flush();

    expect(batches).toEqual([]);
  });

  it('refetches once no matter how many invalidations it missed', () => {
    const { batches, scheduler, invalidator, registry, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    registry.settle(list.key, { deps: ['Order:7'] });
    list.unmount();

    invalidator.settled({ invalidates: ['Order[]'] });
    invalidator.settled({ invalidates: ['Order:7'] });
    invalidator.settled({ invalidates: ['Order[]'] });

    mount('orderList', ['Order[]']);
    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
  });
});

describe('placement', () => {
  const created = { id: 9, status: 'open' };

  function placed() {
    const h = harness();
    const list = h.mount('orderList', ['Order[]'], { query: { status: 'open' } });

    h.registry.settle(list.key, { value: [{ id: 7 }] });

    return { ...h, list };
  }

  it('skips the refetch when a callback returns a list', () => {
    const { batches, scheduler, invalidator, registry, list } = placed();

    invalidator.settled({
      invalidates: ['Order[]'],
      response: created,
      place: { 'Order[]': (entity, current) => [entity, ...(current as unknown[])] },
    });
    scheduler.flush();

    expect(batches).toEqual([]);
    expect(registry.get(list.key)?.value).toEqual([created, { id: 7 }]);
    expect(registry.get(list.key)?.stale).toBe(false);
  });

  it('hands the callback the query arguments, so it can decline per query', () => {
    const { batches, scheduler, invalidator, mount, registry } = harness();

    const open = mount('orderList', ['Order[]'], { query: { status: 'open' } });
    const closed = mount('orderList', ['Order[]'], { query: { status: 'closed' } });

    registry.settle(open.key, { value: [] });
    registry.settle(closed.key, { value: [] });

    invalidator.settled({
      invalidates: ['Order[]'],
      response: created,
      place: {
        'Order[]': (entity, current, args) =>
          args.query?.['status'] === (entity as typeof created).status
            ? [...(current as unknown[]), entity]
            : undefined,
      },
    });
    scheduler.flush();

    // The filtered list the new order does not belong to falls back; the one
    // it does belong to placed it without a request.
    expect(batches).toEqual([[closed.key]]);
    expect(registry.get(open.key)?.value).toEqual([created]);
  });

  it('falls back to a refetch when the callback returns undefined', () => {
    const { batches, scheduler, invalidator, list } = placed();

    invalidator.settled({
      invalidates: ['Order[]'],
      response: created,
      place: { 'Order[]': () => undefined },
    });
    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
  });

  it('refetches when only some of the tags that matched have a callback', () => {
    const { batches, scheduler, invalidator, registry, list } = placed();

    registry.settle(list.key, { deps: ['Order:7'], value: [{ id: 7 }] });

    invalidator.settled({
      invalidates: ['Order[]', 'Order:7'],
      response: created,
      place: { 'Order[]': (entity, current) => [entity, ...(current as unknown[])] },
    });
    scheduler.flush();

    // `Order:7` is genuinely unhandled. Placing the other one would leave the
    // query looking updated while being wrong.
    expect(batches).toEqual([[list.key]]);
  });

  it('does not take the batch down when a callback throws', () => {
    const onError = vi.fn();
    const h = harness({ onError });

    const thrower = h.mount('orderList', ['Order[]']);
    const other = h.mount('orderCount', ['Order[]']);

    h.registry.settle(thrower.key, { value: [] });
    h.registry.settle(other.key, { value: [] });

    h.invalidator.settled({
      invalidates: ['Order[]'],
      response: created,
      place: {
        'Order[]': (_entity, current) => {
          if ((current as unknown[]).length === 0) throw new Error('boom');

          return [];
        },
      },
    });
    h.scheduler.flush();

    expect(onError).toHaveBeenCalledTimes(2);
    expect(onError.mock.calls[0]?.[1]).toBe('place Order[]');
    // A throw is the same answer as `undefined`: refetch, and report it.
    expect(h.batches).toEqual([[thrower.key, other.key]]);
  });

  it('reports what it placed', () => {
    const onPlace = vi.fn();
    const h = harness({ onPlace });
    const list = h.mount('orderList', ['Order[]']);

    h.registry.settle(list.key, { value: [] });
    h.invalidator.settled({
      invalidates: ['Order[]'],
      created,
      place: { 'Order[]': (entity) => [entity] },
    });

    expect(onPlace).toHaveBeenCalledWith(
      expect.objectContaining({ key: list.key }) as QueryEntry,
      [created],
    );
  });

  it('does not refetch a placed query when it mounts again', () => {
    const { batches, scheduler, invalidator, mount, list } = placed();

    invalidator.settled({
      invalidates: ['Order[]'],
      response: created,
      place: { 'Order[]': (entity) => [entity] },
    });
    scheduler.flush();

    // Without stamping the placement forward, the invalidation it just handled
    // would still look unobserved here and undo the escape hatch.
    list.unmount();
    mount('orderList', ['Order[]'], { query: { status: 'open' } });
    scheduler.flush();

    expect(batches).toEqual([]);
  });
});

describe('flush', () => {
  it('runs the pending batch on demand and leaves the scheduled one empty', () => {
    const { batches, scheduler, invalidator, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    invalidator.settled({ invalidates: ['Order[]'] });
    invalidator.flush();

    expect(batches).toEqual([[list.key]]);

    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
  });

  it('does not call the executor with an empty batch', () => {
    const { batches, scheduler, invalidator } = harness();

    invalidator.settled({ invalidates: ['Order[]'] });
    scheduler.flush();

    expect(batches).toEqual([]);
  });

  it('reports an executor that throws rather than losing it to the microtask queue', () => {
    const onError = vi.fn();
    const { scheduler, invalidator, mount } = harness({
      onError,
      execute: () => {
        throw new Error('transport is down');
      },
    });

    mount('orderList', ['Order[]']);
    invalidator.settled({ invalidates: ['Order[]'] });

    expect(() => scheduler.flush()).not.toThrow();
    expect(onError).toHaveBeenCalledWith(expect.any(Error) as Error, 'execute');
  });

  it('invalidates already-resolved tags directly', () => {
    const { batches, scheduler, invalidator, mount } = harness();

    const list = mount('orderList', ['Order[]']);

    invalidator.invalidate(['Order[]']);
    scheduler.flush();

    expect(batches).toEqual([[list.key]]);
  });
});
