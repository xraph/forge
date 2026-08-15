import { defineComponent, h } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { dehydrate } from '@forge-go/client-core';
import type { DehydratedState } from '@forge-go/client-core';
import { HydrationBoundary, clientPlugin, useQuery } from '../src';
import { harness, orderList, useOrderList } from './harness';
import type { Harness, Order } from './harness';

const ops = { orderList };

const Orders = defineComponent({
  setup() {
    const { data, status } = useQuery<Order[]>(useOrderList);

    return () =>
      h('div', `${status.value}:${(data.value ?? []).map((order) => order.total).join(',')}`);
  },
});

/** A cache whose transport must never be reached. */
function offline(): Harness {
  return harness(() => {
    throw new Error('a hydrated query must not fetch');
  });
}

/**
 * One server render's worth of payload, put through JSON exactly as an HTML
 * response would.
 */
async function serverPayload(): Promise<DehydratedState> {
  const server = harness(() => [{ id: 7, total: 99 }]);

  await server.cache.fetch(orderList);

  return JSON.parse(JSON.stringify(dehydrate(server.cache, { principal: undefined })));
}

function boundary(fx: Harness, state: DehydratedState | undefined) {
  return mount(
    defineComponent({
      setup: () => () => h(HydrationBoundary, { state, ops }, () => [h(Orders)]),
    }),
    { global: { plugins: [clientPlugin(fx.cache)] } },
  );
}

describe('HydrationBoundary', () => {
  it('paints the server value on the first render, without fetching', async () => {
    const state = await serverPayload();
    const fx = offline();

    const wrapper = boundary(fx, state);

    // The assertion that matters is that this is already `success` before any
    // promise has settled. Hydration during render is what buys that; a
    // mounted hook would show `pending` here and flip afterwards.
    expect(wrapper.text()).toBe('success:99');

    await flushPromises();

    expect(wrapper.text()).toBe('success:99');
    expect(fx.transport.countOf(orderList)).toBe(0);
  });

  it('renders its children and no element of its own', async () => {
    const state = await serverPayload();

    expect(boundary(offline(), state).html()).toBe('<div>success:99</div>');
  });

  it('fetches normally when there is no payload', async () => {
    const fx = harness(() => [{ id: 7, total: 99 }]);

    const wrapper = boundary(fx, undefined);

    expect(wrapper.text()).toBe('pending:');

    await flushPromises();

    expect(wrapper.text()).toBe('success:99');
    expect(fx.transport.countOf(orderList)).toBe(1);
  });

  it('walks a payload once even though the boundary re-renders', async () => {
    const state = await serverPayload();
    const fx = offline();

    const wrapper = boundary(fx, state);

    await wrapper.vm.$forceUpdate();
    await flushPromises();

    expect(fx.cache.store.getRecord('Order:7')?.version).toBe(1);
  });

  it('refetches on mount when the payload is hydrated stale', async () => {
    const state = await serverPayload();
    const fx = harness(() => [{ id: 7, total: 120 }]);

    const wrapper = mount(
      defineComponent({
        setup: () => () => h(HydrationBoundary, { state, ops, stale: true }, () => [h(Orders)]),
      }),
      { global: { plugins: [clientPlugin(fx.cache)] } },
    );

    // Instant paint from the payload, before anything is refetched.
    expect(wrapper.text()).toBe('success:99');

    // The refetch is queued on the invalidation scheduler, which the harness
    // drives by hand so no test in this package waits on a real tick.
    fx.scheduler.flush();
    await flushPromises();

    expect(wrapper.text()).toBe('success:120');
    expect(fx.transport.countOf(orderList)).toBe(1);
  });
});
