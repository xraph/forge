import { defineComponent, h, isReactive, isRef, toRaw } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { clientPlugin, useMutation, useQuery } from '../src';
import type { UseMutationResult, UseQueryResult } from '../src';
import { harness, orderGet, orderList, useOrderCreate, useOrderGet, useOrderList } from './harness';
import type { Harness, Order } from './harness';

/**
 * The tests this package exists to pass.
 *
 * The core returns the *same object* when nothing it can prove changed, and
 * that is what lets a child component skip a re-render. Vue's default
 * reactivity destroys it: `ref(state)` deep-proxies, so `data.value` would be
 * `reactive(array)` and `data.value[0]` a proxy of the entity -- structurally
 * identical to what the store holds, referentially a different object.
 *
 * The trap is that the obvious identity test cannot see the difference. Vue
 * caches one proxy per raw target in a `WeakMap`, so `reactive(x) === reactive(x)`
 * and *both* variants pass "the same object across two reads". Every
 * assertion below therefore compares what the component reads against what the
 * cache **holds**, or asks Vue directly whether it has been wrapped:
 * `toBe(cache.getState().data)`, `isReactive(...) === false`, `toRaw(v) === v`.
 * Those three fail immediately if `shallowRef` in `useQuery.ts` is changed to
 * `ref`, which is the only way this file is worth having.
 */

function mountWith(fx: Harness, setup: () => void) {
  return mount(
    defineComponent({
      setup() {
        setup();

        return () => h('div');
      },
    }),
    { global: { plugins: [clientPlugin(fx.cache)] } },
  );
}

describe('referential identity', () => {
  it('hands the component the object the cache holds, not a proxy of it', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    let seen!: UseQueryResult<Order[]>;

    mountWith(fx, () => {
      seen = useQuery<Order[]>(useOrderList);
    });
    await flushPromises();

    const held = fx.cache.getState<Order[]>(orderList);

    // The snapshot, the array and the entity inside it: three levels, all of
    // them the cache's own objects. A deep `ref` fails all three.
    expect(seen.state.value).toBe(held);
    expect(seen.data.value).toBe(held.data);
    expect(seen.data.value?.[0]).toBe(held.data?.[0]);

    // Said a second way, in case a future refactor makes the cache hand out
    // something it has already wrapped: nothing on this path is reactive.
    expect(isReactive(seen.state.value)).toBe(false);
    expect(isReactive(seen.data.value)).toBe(false);
    expect(isReactive(seen.data.value?.[0])).toBe(false);
    expect(toRaw(seen.data.value)).toBe(seen.data.value);
    expect(toRaw(seen.data.value?.[0])).toBe(seen.data.value?.[0]);

    // Two reads, no writes between them. True of both variants -- Vue memoises
    // one proxy per target -- and recorded here only to show it is not what
    // the assertions above rely on.
    expect(seen.data.value).toBe(seen.data.value);
  });

  it('keeps a sibling entity identical when its neighbour changes', async () => {
    let total = 0;
    const fx = harness(() => {
      total += 10;

      // Order 2 is byte-for-byte what it was; order 1 is not. Both arrive as
      // fresh object literals every time, so anything short of real structural
      // sharing in the store fails this, and anything that re-wraps on the way
      // out of the store fails the `isReactive` assertions.
      return [
        { id: 1, total },
        { id: 2, total: 500 },
      ];
    });

    let seen!: UseQueryResult<Order[]>;

    mountWith(fx, () => {
      seen = useQuery<Order[]>(useOrderList);
    });
    await flushPromises();

    const before = seen.data.value;

    expect(before?.[1]).toEqual({ id: 2, total: 500 });

    await seen.refetch();
    await flushPromises();

    const after = seen.data.value;

    expect(fx.transport.countOf(orderList)).toBe(2);
    // The one that moved, moved.
    expect(after?.[0]).not.toBe(before?.[0]);
    expect(after?.[0]?.total).toBe(20);
    // The one that did not, did not -- and is still the store's raw object, so
    // a child component holding it as a prop sees no change at all.
    expect(after?.[1]).toBe(before?.[1]);
    expect(after?.[1]).toBe(fx.cache.getState<Order[]>(orderList).data?.[1]);
    expect(isReactive(after?.[1])).toBe(false);
  });

  /**
   * The payoff, and the one test in this file that a deep `ref` also passes --
   * measured, by running it against one. Vue hands back the same proxy for the
   * same target, so prop identity survives the wrapping even though the object
   * does not. It earns its place as the statement of what the identity is
   * *for*; the assertions above it are what actually pin the mechanism down.
   */
  it('does not re-render a child whose entity did not change', async () => {
    let total = 0;
    const fx = harness(() => {
      total += 10;

      return [
        { id: 1, total },
        { id: 2, total: 500 },
      ];
    });

    const renders = new Map<number, number>();

    /**
     * A child with a single object prop and **no slot children**, which is
     * what makes this test mean something. Vue's `shouldUpdateComponent` force-
     * updates a child of a hand-written render function when it has children;
     * with none, it falls through to a shallow prop comparison and skips the
     * child entirely when `order` is the same reference. That is precisely the
     * `React.memo` behaviour the core's identity guarantee is there to buy.
     */
    const Row = defineComponent({
      props: { order: { type: Object as () => Order, required: true } },
      setup(props) {
        return () => {
          renders.set(props.order.id, (renders.get(props.order.id) ?? 0) + 1);

          return h('span', String(props.order.total));
        };
      },
    });

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('div', (data.value ?? []).map((order) => h(Row, { key: order.id, order })));
      },
    });

    const wrapper = mount(List, { global: { plugins: [clientPlugin(fx.cache)] } });

    await flushPromises();

    expect(renders.get(1)).toBe(1);
    expect(renders.get(2)).toBe(1);

    wrapper.vm.$forceUpdate();
    await flushPromises();

    // The parent re-rendered; neither child did, because neither prop moved.
    expect(renders.get(1)).toBe(1);
    expect(renders.get(2)).toBe(1);

    // A refetch that changes order 1 and leaves order 2 alone.
    const seen = wrapper.text();

    expect(seen).toContain('500');

    await fx.cache.refetch(orderList);
    await flushPromises();

    expect(renders.get(1)).toBe(2);
    expect(renders.get(2)).toBe(1);
  });

  it('leaves a mutation result unwrapped too', async () => {
    const fx = harness((request, call) =>
      request.meta.method === 'GET' ? { id: 9, total: call === 0 ? 1 : 5 } : { id: 9, total: 5 },
    );

    let created!: UseMutationResult<Order>;
    let detail!: UseQueryResult<Order>;

    mountWith(fx, () => {
      created = useMutation<Order>(useOrderCreate);
      detail = useQuery<Order>(useOrderGet, { path: { id: 9 } });
    });
    await flushPromises();

    expect(detail.data.value?.total).toBe(1);

    await created.mutate({ body: { total: 5 } });
    await detail.refetch();
    await flushPromises();

    expect(created.data.value).toEqual({ id: 9, total: 5 });
    expect(isReactive(created.data.value)).toBe(false);
    expect(toRaw(created.data.value)).toBe(created.data.value);
    // The write's response was normalized into `Order:9`, and the refetch that
    // followed carried the same entity -- so the object the mutation reported
    // and the object the query is rendering are one object, through two
    // separate composables and two separate refs.
    expect(fx.transport.countOf(orderGet)).toBe(2);
    expect(created.data.value).toBe(detail.data.value);
  });

  it('returns refs, so a caller can watch a field without unwrapping a snapshot', async () => {
    const fx = harness((request) => ({ id: request.args.path?.['id'], total: 1 }));

    let seen!: UseQueryResult<Order>;

    mountWith(fx, () => {
      seen = useQuery<Order>(useOrderGet, { path: { id: 1 } });
    });
    await flushPromises();

    // The shape is Vue's, not React's: refs a template unwraps and a `watch`
    // can take directly, rather than one plain object rebuilt per change.
    expect(isRef(seen.data)).toBe(true);
    expect(isRef(seen.status)).toBe(true);
    expect(seen.status.value).toBe('success');
    expect(seen.data.value).toBe(fx.cache.getState<Order>(orderGet, { path: { id: 1 } }).data);
  });
});
