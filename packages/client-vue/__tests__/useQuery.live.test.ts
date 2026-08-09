import { defineComponent, effectScope, h, nextTick, ref } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { clientPlugin, useQuery } from '../src';
import { liveHarness, useOrderGet, useOrderList } from './harness';
import type { LiveHarness, Order } from './harness';

function withClient(fx: LiveHarness) {
  return { global: { plugins: [clientPlugin(fx.cache)] } };
}

/** Deliver a frame, commit it, and let the components re-render. */
async function emit(fx: LiveHarness, message: unknown): Promise<void> {
  fx.emit(message);
  await flushPromises();
}

/**
 * Let a deferred close actually happen.
 *
 * A socket whose last subscriber went away is not closed on the spot, so every
 * assertion about a *release* has to say when the deferral elapsed rather than
 * assume it has.
 */
function settleCloses(fx: LiveHarness): void {
  fx.closes.flush();
}

const List = defineComponent({
  props: { live: { type: Boolean, default: false } },
  setup(props) {
    const { data, status } = useQuery<Order[]>(useOrderList, undefined, {
      // A getter, so the composable follows the prop rather than reading it
      // once at setup -- which is what makes the toggle test a statement about
      // this adapter and not about remounting a component.
      live: () => props.live,
    });

    return () =>
      h('div', `${status.value}:${data.value?.map((order) => order.total).join(',') ?? '-'}`);
  },
});

describe('useQuery({live})', () => {
  it('updates from a frame, with no request behind it', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    const wrapper = mount(List, { ...withClient(fx), props: { live: true } });
    await flushPromises();

    expect(wrapper.text()).toBe('success:99');
    expect(fx.transport.calls).toHaveLength(1);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });

    // The whole claim of the design: the value moved, and not one request was
    // spent on it. `order.updated` is a `patch`, so it invalidates nothing.
    expect(wrapper.text()).toBe('success:100');
    expect(fx.transport.calls).toHaveLength(1);
  });

  it('is one subscription for two components on the same live query', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    mount(
      defineComponent({
        setup: () => () => h('div', [h(List, { live: true }), h(List, { live: true })]),
      }),
      withClient(fx),
    );
    await flushPromises();

    // One socket, and one subscription on it. A connection count that grows
    // with the render tree is precisely what the ref counting exists to
    // prevent, and a composable that subscribed per component rather than per
    // query would defeat it silently.
    expect(fx.opened).toHaveLength(1);
    expect(fx.manager.size).toBe(1);
    expect(fx.manager.connected('/ws/orders')).toBe(true);
  });

  it('is one channel for two different live queries on the same entity', async () => {
    const fx = liveHarness((request) =>
      request.meta.path === '/orders' ? [{ id: 7, total: 99 }] : { id: 7, total: 99 },
    );

    const Detail = defineComponent({
      setup() {
        const { data } = useQuery<Order>(useOrderGet, { path: { id: 7 } }, { live: true });

        return () => h('div', String(data.value?.total ?? '-'));
      },
    });

    const wrapper = mount(
      defineComponent({ setup: () => () => h('div', [h(List, { live: true }), h(Detail)]) }),
      withClient(fx),
    );
    await flushPromises();

    // Two distinct queries, two cache keys, two requests -- and one socket,
    // because `Order` is pushed on one channel and the manager multiplexes.
    expect(fx.transport.calls).toHaveLength(2);
    expect(fx.opened).toHaveLength(1);
    expect(fx.manager.size).toBe(1);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });

    expect(wrapper.text()).toBe('success:100100');
  });

  it('releases the socket when the last consumer unmounts, and not before', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);
    const both = ref(true);

    mount(
      defineComponent({
        setup: () => () =>
          h('div', [h(List, { live: true }), both.value ? h(List, { live: true }) : null]),
      }),
      withClient(fx),
    );
    await flushPromises();

    expect(fx.live()).toBe(1);

    both.value = false;
    await nextTick();
    settleCloses(fx);

    // The survivor still wants the channel.
    expect(fx.live()).toBe(1);
  });

  it('releases the socket on the framework teardown path', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    const wrapper = mount(List, { ...withClient(fx), props: { live: true } });
    await flushPromises();

    expect(fx.live()).toBe(1);

    wrapper.unmount();
    settleCloses(fx);

    expect(fx.live()).toBe(0);
    expect(fx.manager.size).toBe(0);
  });

  it('releases the socket when a bare effect scope stops', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);
    const scope = effectScope();

    scope.run(() => {
      useQuery<Order[]>(useOrderList, undefined, { client: fx.cache, live: true });
    });
    await flushPromises();

    expect(fx.live()).toBe(1);

    // Scope, not component. `onScopeDispose` is what this composable registers
    // against, so a query created inside a store, a route guard or a detached
    // scope releases its socket when *that* ends -- there is no component here
    // to unmount.
    scope.stop();
    settleCloses(fx);

    expect(fx.live()).toBe(0);
  });

  it('releases the socket from the explicit dispose()', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);
    const scope = effectScope();
    let seen!: { dispose(): void };

    scope.run(() => {
      seen = useQuery<Order[]>(useOrderList, undefined, { client: fx.cache, live: true });
    });
    await flushPromises();

    // "Release the subscription now" has to mean both halves of it. A
    // `dispose` that dropped the query and left the socket open would leave a
    // connection applying frames into the store on behalf of a binding the
    // caller has said it is finished with.
    seen.dispose();
    settleCloses(fx);

    expect(fx.live()).toBe(0);

    scope.stop();
  });

  it('subscribes and unsubscribes as `live` toggles, without refetching', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);
    const live = ref(false);

    const wrapper = mount(
      defineComponent({ setup: () => () => h(List, { live: live.value }) }),
      withClient(fx),
    );
    await flushPromises();

    expect(fx.opened).toHaveLength(0);
    expect(fx.transport.calls).toHaveLength(1);

    // Off -> on. A socket appears; the query is untouched.
    live.value = true;
    await nextTick();

    expect(fx.live()).toBe(1);
    expect(fx.transport.calls).toHaveLength(1);
    expect(wrapper.text()).toBe('success:99');

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });

    expect(wrapper.text()).toBe('success:100');

    // On -> off. The socket goes; the query keeps the value it has, and still
    // no second request -- `live` is not a hidden refetch trigger in either
    // direction.
    live.value = false;
    await nextTick();
    settleCloses(fx);

    expect(fx.live()).toBe(0);
    expect(fx.transport.calls).toHaveLength(1);
    expect(wrapper.text()).toBe('success:100');

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 250 } });

    expect(wrapper.text()).toBe('success:100');
  });

  it('opts a query out entirely when `live` is not asked for', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    const wrapper = mount(List, withClient(fx));
    await flushPromises();

    // The opt-in is real at the only level it can be: nothing subscribed, so
    // no socket was ever opened and there is no frame to arrive. The absence
    // of the word is the absence of the connection.
    expect(fx.opened).toHaveLength(0);
    expect(fx.manager.size).toBe(0);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });

    expect(wrapper.text()).toBe('success:99');
    expect(fx.transport.calls).toHaveLength(1);
  });

  it('re-subscribes for the new principal rather than going deaf', async () => {
    let total = 99;
    const fx = liveHarness(() => [{ id: 7, total }]);

    const wrapper = mount(List, { ...withClient(fx), props: { live: true } });
    await flushPromises();

    const first = fx.opened[0];

    total = 5;
    fx.cache.setPrincipal('user-b');
    await flushPromises();

    // The previous principal's socket is gone -- one that outlived the
    // identity change would push the previous session's entities into the new
    // one's store -- and a replacement was opened without the component doing
    // anything at all.
    expect(first?.closed).toBe(true);
    expect(fx.opened).toHaveLength(2);
    expect(fx.live()).toBe(1);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 6 } });

    expect(wrapper.text()).toBe('success:6');
  });
});
