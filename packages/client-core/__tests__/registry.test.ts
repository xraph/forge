import { describe, expect, it, vi } from 'vitest';

import { EntityStore } from '../src/store';
import { QueryRegistry } from '../src/registry';
import { queryKey } from '../src/tags';
import { schema } from './schema';

const listSpec = {
  operation: 'orderList',
  args: { query: { status: 'open' } },
  provides: ['Order[]'],
};

const key = queryKey(listSpec.operation, listSpec.args);

describe('QueryRegistry mounting', () => {
  it('ref-counts one entry for a query mounted from several places', () => {
    const registry = new QueryRegistry();

    const first = registry.mount(listSpec);
    const second = registry.mount(listSpec);

    expect(registry.size).toBe(1);
    expect(registry.get(key)?.mounts).toBe(2);
    expect(registry.queriesFor('Order[]')).toHaveLength(1);

    // One unmount, one watcher left: still indexed.
    first();

    expect(registry.get(key)?.mounts).toBe(1);
    expect(registry.queriesFor('Order[]')).toHaveLength(1);

    second();

    expect(registry.get(key)?.mounts).toBe(0);
    expect(registry.queriesFor('Order[]')).toEqual([]);
    // Every bucket, not just this tag: an emptied bucket is deleted too.
    expect(registry.indexedTags).toBe(0);
  });

  it('does not double-decrement when an unmount is called twice', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec);
    const unmount = registry.mount(listSpec);

    unmount();
    unmount();

    expect(registry.get(key)?.mounts).toBe(1);
    expect(registry.queriesFor('Order[]')).toHaveLength(1);
  });

  it('keys queries by operation and arguments, not by identity', () => {
    const registry = new QueryRegistry();

    registry.mount({ ...listSpec, args: { query: { status: 'open' } } });
    registry.mount({ ...listSpec, args: { query: { status: 'closed' } } });

    expect(registry.size).toBe(2);
    expect(registry.queriesFor('Order[]')).toHaveLength(2);
  });

  it('resolves provides templates against the query arguments at mount', () => {
    const registry = new QueryRegistry();

    registry.mount({
      operation: 'orderGet',
      args: { path: { id: 7 } },
      provides: ['Order:{id}'],
    });

    expect(registry.queriesFor('Order:7')).toHaveLength(1);
  });

  it('remembers an unmounted query without indexing it', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec)();

    expect(registry.size).toBe(1);
    expect(registry.mounted).toBe(0);
    expect(registry.indexedTags).toBe(0);
  });

  it('drops a query from the registry and from every tag', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec);

    expect(registry.drop(key)).toBe(true);
    expect(registry.size).toBe(0);
    expect(registry.indexedTags).toBe(0);
    expect(registry.drop(key)).toBe(false);
  });
});

describe('QueryRegistry settle', () => {
  it('adopts the entity keys the response normalized to as tags', () => {
    const registry = new QueryRegistry();
    const store = new EntityStore();

    registry.mount(listSpec);

    const { deps } = store.write(
      [{ id: 7, customer: { id: 'c-3', name: 'Ada' } }, { id: 8 }],
      schema,
      'Order',
    );

    registry.settle(key, { deps });

    // `normalize` already reports these, and they are already spelled the way
    // a mutation to `Order:7` invalidates them.
    expect(registry.queriesFor('Order:7')).toHaveLength(1);
    expect(registry.queriesFor('Customer:c-3')).toHaveLength(1);
    expect(registry.queriesFor('Order[]')).toHaveLength(1);
    expect(registry.get(key)?.tags).toEqual(new Set(['Order[]', 'Order:7', 'Order:8', 'Customer:c-3']));
  });

  it('unindexes a tag a later response no longer provides', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec);
    registry.settle(key, { deps: ['Order:7', 'Order:8'] });
    registry.settle(key, { deps: ['Order:8'] });

    expect(registry.queriesFor('Order:7')).toEqual([]);
    expect(registry.queriesFor('Order:8')).toHaveLength(1);
    expect(registry.queriesFor('Order[]')).toHaveLength(1);
  });

  it('resolves provides templates naming the response, and reports what cannot resolve', () => {
    const onUnresolved = vi.fn();
    const registry = new QueryRegistry({ onUnresolved });

    registry.mount({
      operation: 'orderCreate',
      provides: ['Order:{res.id}', 'Customer:{res.customerId}'],
      key: 'k',
    });

    // Neither resolves before the response arrives, and neither is reported:
    // warning at mount would fire on every mount of a healthy query.
    expect(registry.get('k')?.tags.size).toBe(0);
    expect(onUnresolved).not.toHaveBeenCalled();

    registry.settle('k', { response: { id: 9 } });

    expect(registry.queriesFor('Order:9')).toHaveLength(1);
    expect(onUnresolved).toHaveBeenCalledWith('Customer:{res.customerId}', expect.anything());
  });

  it('re-indexes a query that settles while unmounted, without indexing it', () => {
    const registry = new QueryRegistry();

    const unmount = registry.mount(listSpec);
    unmount();
    registry.settle(key, { deps: ['Order:7'] });

    expect(registry.indexedTags).toBe(0);
    expect(registry.get(key)?.tags.has('Order:7')).toBe(true);

    // Mounting picks up the tags it settled with while nobody watched.
    registry.mount(listSpec);

    expect(registry.queriesFor('Order:7')).toHaveLength(1);
  });

  it('ignores a settle for a query it does not know', () => {
    const registry = new QueryRegistry();

    expect(() => registry.settle('nope', { deps: ['Order:7'] })).not.toThrow();
  });

  it('keeps the previous value when settle omits one', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec);
    registry.settle(key, { value: [1, 2] });
    registry.settle(key, { deps: ['Order:7'] });

    expect(registry.get(key)?.value).toEqual([1, 2]);
  });
});

describe('QueryRegistry invalidation stamps', () => {
  it('remembers an invalidation that arrived while the query was unmounted', () => {
    const registry = new QueryRegistry();
    const onStale = vi.fn();

    registry.onStale = onStale;

    const unmount = registry.mount(listSpec);
    // Settling folds the entity dependencies into the tag set, which is what
    // makes `Order:7` reach this query at all.
    registry.settle(key, { deps: ['Order:7'] });
    unmount();

    // A tab switch, then a write from somewhere else.
    registry.invalidated(['Order:7']);
    expect(onStale).not.toHaveBeenCalled();

    registry.mount(listSpec);

    expect(registry.get(key)?.stale).toBe(true);
    expect(onStale).toHaveBeenCalledTimes(1);
  });

  it('does not stamp a tag no remembered query carries', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec)();
    registry.settle(key, { deps: ['Order:7'] });

    // `Order[]` and `Order:7` are carried; the rest of the store is not.
    registry.invalidated(['Order:7', 'Order:8', 'Order:9', 'Customer:c-3']);

    expect(registry.stampedTags).toBe(1);

    // A hundred writes to entities nothing displays leave nothing behind. The
    // old bound -- "the API's tag vocabulary" -- did not hold once entity deps
    // became tags, which they are from the first settle.
    for (let id = 100; id < 200; id++) registry.invalidated([`Order:${id}`]);

    expect(registry.stampedTags).toBe(1);
  });

  it('forgets a stamp when the last query carrying its tag is dropped', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec)();
    registry.mount({ operation: 'orderDetail', args: { path: { id: 7 } }, key: 'detail' })();
    registry.settle(key, { deps: ['Order:7'] });
    registry.settle('detail', { deps: ['Order:7'] });

    registry.invalidated(['Order:7']);
    expect(registry.stampedTags).toBe(1);

    // One of two carriers: the stamp is still live for the other.
    registry.drop('detail');
    expect(registry.stampedTags).toBe(1);

    registry.drop(key);
    expect(registry.stampedTags).toBe(0);

    // And a query mounting afterwards is not stale, because it never saw the
    // invalidation and there is nothing left claiming it did.
    registry.mount(listSpec);
    expect(registry.get(key)?.stale).toBe(false);
  });

  it('forgets a stamp for a tag a later response stopped providing', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec);
    registry.settle(key, { deps: ['Order:7'] });
    registry.invalidated(['Order:7']);

    expect(registry.stampedTags).toBe(1);

    registry.settle(key, { deps: ['Order:8'] });

    expect(registry.stampedTags).toBe(0);
  });

  it('clears the stamps and the carrier counts together', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec);
    registry.settle(key, { deps: ['Order:7'] });
    registry.invalidated(['Order:7']);
    registry.clear();

    expect(registry.stampedTags).toBe(0);

    // Not merely emptied: a stamp written after a clear must still find a
    // carrier, which a leftover count would fake.
    registry.invalidated(['Order:7']);
    expect(registry.stampedTags).toBe(0);
  });
});

describe('QueryRegistry dispatch stamps', () => {
  it('marks a response stale when the invalidation landed after it was dispatched', () => {
    const registry = new QueryRegistry();
    const unmount = registry.mount(listSpec);

    // Nothing is watching, which is the case with no second line of defence:
    // an unmounted query is not in the tag index, so the holder of its
    // in-flight request is never told the answer went stale.
    unmount();

    const startedAt = registry.stamp;

    registry.invalidated(['Order[]']);
    registry.settle(key, { value: ['pre-write'], startedAt });

    expect(registry.get(key)?.stale).toBe(true);

    // And it says so on mount, rather than serving the pre-write value.
    const onStale = vi.fn();
    registry.onStale = onStale;
    registry.mount(listSpec);

    expect(onStale).toHaveBeenCalledTimes(1);
  });

  it('leaves a response dispatched after the invalidation current', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec)();
    registry.invalidated(['Order[]']);

    // The refetch the invalidation asked for: dispatched afterwards, so its
    // answer is the one the write produced.
    registry.settle(key, { value: ['post-write'], startedAt: registry.stamp });

    expect(registry.get(key)?.stale).toBe(false);
  });

  it('stamps the reading now when the caller has no request behind it', () => {
    const registry = new QueryRegistry();

    registry.mount(listSpec)();
    registry.invalidated(['Order[]']);
    registry.settle(key, { value: ['placed'] });

    expect(registry.get(key)?.stale).toBe(false);
  });
});
