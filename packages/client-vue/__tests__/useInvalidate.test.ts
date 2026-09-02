import { defineComponent, effectScope, h, nextTick, ref } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { setClient } from '@forge-go/client-core';
import { clientPlugin, useInvalidate, useQuery } from '../src';
import type { Invalidate } from '../src';
import {
  harness,
  orderGet,
  orderList,
  orderSearch,
  useOrderCreate,
  useOrderGet,
  useOrderList,
  useOrderSearch,
} from './harness';
import type { Harness, Order } from './harness';

function withClient(fx: Harness) {
  return { global: { plugins: [clientPlugin(fx.cache)] } };
}

/**
 * Run the invalidator's pending batch, let the refetches it started settle,
 * and paint what changed.
 *
 * `invalidate` is deliberately not awaitable -- it marks queries stale and
 * returns -- so a test asserting on the requests it caused has to drive the
 * same scheduler the application's microtask would have driven.
 */
async function settle(fx: Harness): Promise<void> {
  fx.scheduler.flush();
  await flushPromises();
}

describe('useInvalidate', () => {
  it('refreshes a list held by a sibling that this component cannot see', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('i', String(data.value?.[0]?.total ?? '-'));
      },
    });

    // The migration case exactly: a dialog that holds no query of its own,
    // imports no component, and still has to refresh what its write changed.
    const Dialog = defineComponent({
      setup() {
        invalidate = useInvalidate();

        return () => null;
      },
    });

    const wrapper = mount(
      defineComponent({ setup: () => () => h('div', [h(List), h(Dialog)]) }),
      withClient(fx),
    );

    await flushPromises();

    expect(wrapper.text()).toBe('1');

    invalidate(useOrderList);
    await settle(fx);

    expect(wrapper.text()).toBe('2');
  });

  it('reaches a query the tag graph cannot, because it declares no tags', async () => {
    const fx = harness((request) => {
      if (request.meta === orderSearch) return [{ id: 1, total: fx.transport.countOf(orderSearch) }];

      return { id: 2, total: 0 };
    });

    let invalidate!: Invalidate;

    const Search = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderSearch);

        invalidate = useInvalidate();

        return () => h('i', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(Search, withClient(fx));

    await flushPromises();

    expect(wrapper.text()).toBe('1');

    // A create declaring `invalidates: ['Order[]']`. The search carries
    // `Order:1` from its own response and nothing else, so the tag graph has
    // no edge to it and this write is invisible -- which is the whole defect.
    await fx.cache.mutate(useOrderCreate.meta, { body: { total: 7 } });
    await settle(fx);

    expect(fx.transport.countOf(orderSearch)).toBe(1);

    // Addressed by operation, it refetches regardless of what it declares.
    invalidate(useOrderSearch);
    await settle(fx);

    expect(fx.transport.countOf(orderSearch)).toBe(2);
    expect(wrapper.text()).toBe('2');
  });

  it('hits every argument variant when no arguments are given', async () => {
    const fx = harness((request) => ({
      id: request.args.path?.['id'],
      total: fx.transport.calls.length,
    }));

    let invalidate!: Invalidate;

    const Detail = defineComponent({
      props: { id: { type: Number, required: true } },
      setup(props) {
        const { data } = useQuery<Order>(useOrderGet, { path: { id: props.id } });

        return () => h('i', String(data.value?.total ?? '-'));
      },
    });

    const Toolbar = defineComponent({
      setup() {
        invalidate = useInvalidate();

        return () => null;
      },
    });

    mount(
      defineComponent({
        setup: () => () =>
          h('div', [h(Detail, { id: 1 }), h(Detail, { id: 2 }), h(Toolbar)]),
      }),
      withClient(fx),
    );

    await flushPromises();

    expect(fx.transport.countOf(orderGet)).toBe(2);

    invalidate(useOrderGet);
    await settle(fx);

    // Both variants, not just one, and not the whole cache either.
    expect(fx.transport.countOf(orderGet)).toBe(4);
  });

  it('targets exactly one variant when arguments are given', async () => {
    const fx = harness((request) => ({
      id: request.args.path?.['id'],
      total: fx.transport.calls.length,
    }));

    let invalidate!: Invalidate;

    const Detail = defineComponent({
      props: { id: { type: Number, required: true } },
      setup(props) {
        const { data } = useQuery<Order>(useOrderGet, { path: { id: props.id } });

        return () => h('i', String(data.value?.total ?? '-'));
      },
    });

    const Toolbar = defineComponent({
      setup() {
        invalidate = useInvalidate();

        return () => null;
      },
    });

    mount(
      defineComponent({
        setup: () => () =>
          h('div', [h(Detail, { id: 1 }), h(Detail, { id: 2 }), h(Toolbar)]),
      }),
      withClient(fx),
    );

    await flushPromises();

    expect(fx.transport.countOf(orderGet)).toBe(2);

    invalidate(useOrderGet, { path: { id: 1 } });
    await settle(fx);

    expect(fx.transport.countOf(orderGet)).toBe(3);
  });

  it('reads a ref or a getter for its arguments, at the moment of the call', async () => {
    const fx = harness((request) => ({
      id: request.args.path?.['id'],
      total: fx.transport.calls.length,
    }));

    const id = ref(1);
    let invalidate!: Invalidate;

    // The same getter the component handed `useQuery`, handed to `invalidate`.
    const args = () => ({ path: { id: id.value } });

    const Detail = defineComponent({
      props: { id: { type: Number, required: true } },
      setup(props) {
        const { data } = useQuery<Order>(useOrderGet, { path: { id: props.id } });

        return () => h('i', String(data.value?.total ?? '-'));
      },
    });

    const Toolbar = defineComponent({
      setup() {
        invalidate = useInvalidate();

        return () => null;
      },
    });

    mount(
      defineComponent({
        setup: () => () =>
          h('div', [h(Detail, { id: 1 }), h(Detail, { id: 2 }), h(Toolbar)]),
      }),
      withClient(fx),
    );

    await flushPromises();

    expect(fx.transport.countOf(orderGet)).toBe(2);

    invalidate(useOrderGet, args);
    await settle(fx);

    expect(fx.transport.countOf(orderGet)).toBe(3);

    // Read at the call, not captured at setup: moving the ref moves which
    // variant the *next* call names, and nothing about the last one.
    id.value = 2;

    invalidate(useOrderGet, args);
    await settle(fx);

    expect(fx.transport.countOf(orderGet)).toBe(4);
  });

  it('refetches a query fetched with no arguments at all', async () => {
    // The `open` args trap. `useQuery(useOrderList)` keys as `GET /orders`
    // while its registry entry holds `{}`, which re-derives as
    // `GET /orders|{}`. Refetching the entry's own `args` would open a second,
    // empty record and refresh nothing the component is watching.
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        invalidate = useInvalidate();

        return () => h('i', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(List, withClient(fx));

    await flushPromises();

    const size = fx.cache.size;

    invalidate(useOrderList);
    await settle(fx);

    expect(wrapper.text()).toBe('2');
    // No second record was opened behind the component's back.
    expect(fx.cache.size).toBe(size);
  });

  it('does not fetch an unmounted query now, and refetches it on its next mount', async () => {
    let served = 0;
    const fx = harness(() => ({ id: 1, total: ++served }));

    const visible = ref(true);
    let invalidate!: Invalidate;

    const Detail = defineComponent({
      setup() {
        const { data } = useQuery<Order>(useOrderGet, { path: { id: 1 } });

        return () => h('i', String(data.value?.total ?? '-'));
      },
    });

    const Shell = defineComponent({
      setup() {
        invalidate = useInvalidate();

        return () => h('div', [visible.value ? h(Detail) : null]);
      },
    });

    const wrapper = mount(Shell, withClient(fx));

    await flushPromises();

    expect(fx.transport.countOf(orderGet)).toBe(1);

    visible.value = false;
    await nextTick();

    invalidate(useOrderGet);
    await settle(fx);

    // Nobody is watching it, so nothing is fetched: a write must not stampede
    // the network for every list the user has navigated away from.
    expect(fx.transport.countOf(orderGet)).toBe(1);

    visible.value = true;
    await nextTick();
    await settle(fx);

    // The staleness was remembered, and paid for at the moment it matters.
    expect(fx.transport.countOf(orderGet)).toBe(2);
    expect(wrapper.text()).toBe('2');
  });

  it('resolves refetch only once the mounted query has settled', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        invalidate = useInvalidate();

        return () => h('i', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(List, withClient(fx));

    await flushPromises();

    expect(wrapper.text()).toBe('1');

    await invalidate.refetch(useOrderList);
    // `nextTick` and nothing else: the value was already in the `shallowRef`
    // when the promise resolved, and all that is left is the paint. No
    // scheduler flush, which is the point -- this is the spelling a dialog
    // uses before it closes.
    await nextTick();

    expect(wrapper.text()).toBe('2');
    expect(fx.transport.countOf(orderList)).toBe(2);

    // And the batch must not spend a second request on an answer on screen.
    await settle(fx);
    expect(fx.transport.countOf(orderList)).toBe(2);
  });

  it('forwards tags to the tag graph', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        invalidate = useInvalidate();

        return () => h('i', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(List, withClient(fx));

    await flushPromises();

    invalidate.tags(['Order[]']);
    await settle(fx);

    expect(wrapper.text()).toBe('2');
  });

  it('resolves its cache explicitly, then provided, then global', async () => {
    const globalFx = harness(() => [{ id: 1, total: 1 }]);
    const provided = harness(() => [{ id: 1, total: 2 }]);
    const explicit = harness(() => [{ id: 1, total: 3 }]);

    setClient(globalFx.cache);

    let byDefault!: Invalidate;
    let byOverride!: Invalidate;

    // One mounted list in each cache, so each has something to refresh.
    const Lists = defineComponent({
      setup() {
        useQuery<Order[]>(useOrderList, undefined, { client: globalFx.cache });
        useQuery<Order[]>(useOrderList, undefined, { client: provided.cache });
        useQuery<Order[]>(useOrderList, undefined, { client: explicit.cache });

        byDefault = useInvalidate();
        byOverride = useInvalidate(explicit.cache);

        return () => null;
      },
    });

    mount(Lists, withClient(provided));

    await flushPromises();

    expect(globalFx.transport.countOf(orderList)).toBe(1);

    await byDefault.refetch(useOrderList);
    await byOverride.refetch(useOrderList);

    expect(provided.transport.countOf(orderList)).toBe(2);
    expect(explicit.transport.countOf(orderList)).toBe(2);
    // The provider beat the global, and the override beat the provider.
    expect(globalFx.transport.countOf(orderList)).toBe(1);
  });

  it('works in a bare effect scope, where there is no provider to inject from', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    setClient(fx.cache);

    const scope = effectScope();

    // No component, so `inject` is never reached and the module-level client
    // is what a store or a router guard invalidates through. It holds nothing,
    // so stopping the scope has nothing to release.
    const invalidate = scope.run(() => {
      useQuery<Order[]>(useOrderList);

      return useInvalidate();
    }) as Invalidate;

    await flushPromises();

    expect(fx.transport.countOf(orderList)).toBe(1);

    await invalidate.refetch(useOrderList);

    expect(fx.transport.countOf(orderList)).toBe(2);

    scope.stop();
  });
});
