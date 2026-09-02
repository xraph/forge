import { defineComponent, effectScope, h, nextTick, ref } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { clientPlugin, useQuery } from '../src';
import type { UseQueryResult } from '../src';
import { harness, orderGet, orderList, useOrderGet, useOrderList, useOrderPatch } from './harness';
import type { Harness, Order } from './harness';

function withClient(fx: Harness) {
  return { global: { plugins: [clientPlugin(fx.cache)] } };
}

describe('useQuery', () => {
  it('renders its value, and re-renders when an entity it depends on changes', async () => {
    let total = 0;
    const fx = harness((request) => {
      total += 100;

      return { id: request.args.path?.['id'], total };
    });

    const Detail = defineComponent({
      setup() {
        const { data, status } = useQuery<Order>(useOrderGet, { path: { id: 1 } });

        return () => h('div', `${status.value}:${data.value === undefined ? '-' : data.value.total}`);
      },
    });

    const wrapper = mount(Detail, withClient(fx));

    // The subscription started the request synchronously, and `attach` read
    // the state back afterwards -- so even the very first paint is `pending`
    // rather than a flash of `idle` nobody asked for.
    expect(wrapper.text()).toBe('pending:-');

    await flushPromises();

    expect(wrapper.text()).toBe('success:100');

    // Patching order 1 invalidates `Order:1`, which is this query's own tag --
    // acquired both from `provides` and from the entity its response
    // normalized to.
    await fx.cache.mutate(useOrderPatch.meta, { path: { id: 1 } });
    fx.scheduler.flush();
    await flushPromises();

    expect(wrapper.text()).toBe('success:300');
  });

  it('serves two components on one query from a single request', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('div', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(
      defineComponent({ setup: () => () => h('div', [h(List), h(List)]) }),
      withClient(fx),
    );

    await flushPromises();

    expect(fx.transport.countOf(orderList)).toBe(1);
    expect(wrapper.text()).toBe('9999');
    // One entry with two listeners, not two entries: the tag index must not
    // fan one invalidation out into two refetches of identical data.
    expect(fx.cache.registry.mounted).toBe(1);
    expect(fx.cache.registry.get(fx.cache.key(orderList))?.mounts).toBe(1);
  });

  it('releases the subscription when the last consumer unmounts', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('div', String(data.value?.length ?? 0));
      },
    });

    const show = ref(2);
    const Shell = defineComponent({
      setup: () => () =>
        h('div', [show.value > 0 ? h(List) : null, show.value > 1 ? h(List) : null]),
    });

    mount(Shell, withClient(fx));
    await flushPromises();

    const key = fx.cache.key(orderList);

    expect(fx.cache.registry.get(key)?.mounts).toBe(1);

    // One of two consumers goes away: still mounted.
    show.value = 1;
    await flushPromises();
    expect(fx.cache.registry.get(key)?.mounts).toBe(1);
    expect(fx.cache.registry.mounted).toBe(1);

    // The last one: released.
    show.value = 0;
    await flushPromises();
    expect(fx.cache.registry.get(key)?.mounts).toBe(0);
    expect(fx.cache.registry.mounted).toBe(0);
  });

  it('releases the subscription when a bare effect scope stops', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    // No component anywhere: a store, a route guard, a plugin. This is why the
    // teardown hangs off `onScopeDispose` rather than `onUnmounted`.
    const scope = effectScope();
    let seen!: UseQueryResult<Order[]>;

    scope.run(() => {
      seen = useQuery<Order[]>(useOrderList, undefined, { client: fx.cache });
    });
    await flushPromises();

    const key = fx.cache.key(orderList);

    expect(seen.data.value?.[0]?.total).toBe(99);
    expect(fx.cache.registry.get(key)?.mounts).toBe(1);

    scope.stop();

    expect(fx.cache.registry.get(key)?.mounts).toBe(0);
    expect(fx.cache.registry.mounted).toBe(0);
  });

  it('releases exactly once, however the release is spelt', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    const scope = effectScope();
    let seen!: UseQueryResult<Order[]>;

    scope.run(() => {
      seen = useQuery<Order[]>(useOrderList, undefined, { client: fx.cache });
      // A second consumer of the same query, so the mount count would go
      // negative -- or the entry would be unlinked with a live subscriber --
      // if a double release were not idempotent.
      useQuery<Order[]>(useOrderList, undefined, { client: fx.cache });
    });
    await flushPromises();

    const key = fx.cache.key(orderList);

    expect(fx.cache.registry.get(key)?.mounts).toBe(1);

    seen.dispose();
    seen.dispose();
    scope.stop();

    expect(fx.cache.registry.get(key)?.mounts).toBe(0);
    expect(fx.cache.registry.mounted).toBe(0);
  });

  it('re-subscribes to the new query when a reactive argument changes', async () => {
    const fx = harness((request) => ({ id: request.args.path?.['id'], total: 7 }));

    const id = ref(1);
    const Detail = defineComponent({
      setup() {
        // The getter spelling: a fresh object literal every evaluation, which
        // is why the watcher is over the derived key rather than over this.
        const { data } = useQuery<Order>(useOrderGet, () => ({ path: { id: id.value } }));

        return () => h('div', String(data.value?.id ?? '-'));
      },
    });

    const wrapper = mount(Detail, withClient(fx));

    await flushPromises();
    expect(wrapper.text()).toBe('1');

    id.value = 2;
    await flushPromises();

    expect(wrapper.text()).toBe('2');
    expect(fx.cache.registry.get(fx.cache.key(orderGet, { path: { id: 1 } }))?.mounts).toBe(0);
    expect(fx.cache.registry.get(fx.cache.key(orderGet, { path: { id: 2 } }))?.mounts).toBe(1);
  });

  it('does not resurrect the subscription when an argument moves after dispose()', async () => {
    const fx = harness((request) => ({ id: request.args.path?.['id'], total: 7 }));

    const id = ref(1);
    let seen!: UseQueryResult<Order>;

    const Detail = defineComponent({
      setup() {
        seen = useQuery<Order>(useOrderGet, () => ({ path: { id: id.value } }));

        return () => h('div', String(seen.data.value?.id ?? '-'));
      },
    });

    mount(Detail, withClient(fx));
    await flushPromises();

    const first = fx.cache.key(orderGet, { path: { id: 1 } });

    expect(fx.cache.registry.get(first)?.mounts).toBe(1);

    // The documented escape hatch: release now, before the scope ends. It has
    // to release *both* halves -- a watcher left running would re-subscribe on
    // its next tick to a query the caller has finished with. The Angular
    // adapter's `destroy()` is held to exactly the same standard.
    seen.dispose();

    expect(fx.cache.registry.get(first)?.mounts).toBe(0);

    const requests = fx.transport.calls.length;

    id.value = 2;
    await flushPromises();

    expect(fx.cache.registry.mounted).toBe(0);
    expect(fx.cache.registry.get(fx.cache.key(orderGet, { path: { id: 2 } }))?.mounts ?? 0).toBe(0);
    expect(fx.transport.calls).toHaveLength(requests);
  });

  it('does not churn the subscription when an unrelated ref moves', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    const tick = ref(0);
    let seen!: UseQueryResult<Order[]>;

    const List = defineComponent({
      setup() {
        // An inline argument object: a new literal on every evaluation of the
        // getter, which is exactly how a caller writes it.
        seen = useQuery<Order[]>(useOrderList, () => ({ query: { status: 'open' } }));

        return () => h('div', `${tick.value}:${seen.data.value?.length ?? 0}`);
      },
    });

    const wrapper = mount(List, withClient(fx));

    await flushPromises();

    const entry = fx.cache.registry.get(fx.cache.key(orderList, { query: { status: 'open' } }));

    expect(entry?.mounts).toBe(1);

    const settled = seen.state.value;

    // Ten parent-driven re-renders. A key recomputed from the object's
    // identity would re-subscribe on each one, dropping the mount count to
    // zero every time and making the entry a candidate for LRU eviction.
    for (let i = 0; i < 10; i++) {
      tick.value = i + 1;
      await nextTick();
    }

    expect(wrapper.text()).toBe('10:1');
    // The identical snapshot, ten renders later.
    expect(seen.state.value).toBe(settled);
    expect(entry?.mounts).toBe(1);
    expect(fx.transport.countOf(orderList)).toBe(1);
  });

  it('keeps the last good value beside an error from a failed refetch', async () => {
    const fx = harness((_request, call) => {
      if (call > 0) throw new Error('boom');

      return [{ id: 1, total: 99 }];
    });

    let seen!: UseQueryResult<Order[]>;

    mount(
      defineComponent({
        setup() {
          seen = useQuery<Order[]>(useOrderList);

          return () => h('div', String(seen.status.value));
        },
      }),
      withClient(fx),
    );
    await flushPromises();

    const good = seen.data.value;

    await seen.refetch().catch(() => undefined);
    await flushPromises();

    expect(seen.status.value).toBe('error');
    expect((seen.error.value as Error).message).toBe('boom');
    // Stale data plus a warning beats an empty screen, and the value is the
    // same object it was: a failure does not invalidate identity.
    expect(seen.data.value).toBe(good);
  });

  it('reports a failed first fetch as an error state rather than throwing in setup', async () => {
    const fx = harness(() => {
      throw new Error('nope');
    });

    const List = defineComponent({
      setup() {
        const { status, error } = useQuery<Order[]>(useOrderList, undefined, { client: fx.cache });

        return () =>
          h('div', `${status.value}:${error.value === undefined ? '-' : (error.value as Error).message}`);
      },
    });

    const wrapper = mount(List);

    await flushPromises();

    expect(wrapper.text()).toBe('error:nope');
  });

  it('passes a per-call staleTime through to the cache', async () => {
    const fx = harness(() => [{ id: 1, total: 10 }]);

    const List = defineComponent({
      setup() {
        useQuery<Order[]>(useOrderList, undefined, { staleTime: 250 });

        return () => h('div');
      },
    });

    mount(List, withClient(fx));
    await flushPromises();

    expect(fx.cache.effectiveStaleTime(orderList)).toBe(250);
  });

  it('keeps the per-call staleTime after the handle rebuilds on a key change', async () => {
    const fx = harness((request) => ({ id: request.args.path?.['id'], total: 7 }));

    const id = ref(1);
    const Detail = defineComponent({
      setup() {
        useQuery<Order>(useOrderGet, () => ({ path: { id: id.value } }), { staleTime: 250 });

        return () => h('div');
      },
    });

    mount(Detail, withClient(fx));
    await flushPromises();

    expect(fx.cache.effectiveStaleTime(orderGet, { path: { id: 1 } })).toBe(250);

    // The rebuild trap: a component whose key changes tears down the handle
    // built at the first construction site and builds a new one at the
    // second. Missing `staleTime` there makes the option work only until the
    // arguments change, and then silently stop.
    id.value = 2;
    await flushPromises();

    expect(fx.cache.effectiveStaleTime(orderGet, { path: { id: 2 } })).toBe(250);
  });

  it('follows a staleTime that changes without the arguments changing', async () => {
    const fx = harness(() => [{ id: 1, total: 10 }]);

    const ms = ref(250);
    const List = defineComponent({
      setup() {
        useQuery<Order[]>(useOrderList, undefined, { staleTime: ms });

        return () => h('div');
      },
    });

    mount(List, withClient(fx));
    await flushPromises();

    expect(fx.cache.effectiveStaleTime(orderList)).toBe(250);

    // `live` beside it is already a `MaybeRefOrGetter`, so a caller reasonably
    // expects the same of this one. Read once at construction, a change that
    // does not also change the arguments is a silent no-op: the assertion above
    // still passes and the new value never reaches the cache.
    ms.value = 50;
    await flushPromises();

    expect(fx.cache.effectiveStaleTime(orderList)).toBe(50);
  });
});
