import { defineComponent, effectScope, h } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { setClient } from '@forge-go/client-core';
import type { QueryCache } from '@forge-go/client-core';
import { clientPlugin, clientKey, provideClient, useClient, useQuery } from '../src';
import { harness, orderList, useOrderList } from './harness';
import type { Order } from './harness';

describe('client resolution', () => {
  it('falls back to the module-level client when nothing is provided', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    setClient(fx.cache);

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('div', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(List);

    await flushPromises();

    expect(wrapper.text()).toBe('99');
    expect(fx.transport.countOf(orderList)).toBe(1);
  });

  it('prefers a provided client over the module-level one', async () => {
    const global_ = harness(() => [{ id: 1, total: 1 }]);
    const scoped = harness(() => [{ id: 1, total: 2 }]);

    setClient(global_.cache);

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('div', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(List, { global: { plugins: [clientPlugin(scoped.cache)] } });

    await flushPromises();

    expect(wrapper.text()).toBe('2');
    expect(global_.transport.calls).toHaveLength(0);
  });

  it('takes a client from a parent component as well as from the app', async () => {
    const fx = harness(() => [{ id: 1, total: 42 }]);

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('div', String(data.value?.[0]?.total ?? '-'));
      },
    });

    // `provideClient` in a parent's setup: the subtree spelling, for an
    // application that talks to two backends from two branches of one tree.
    const Parent = defineComponent({
      setup() {
        provideClient(fx.cache);

        return () => h(List);
      },
    });

    const wrapper = mount(Parent);

    await flushPromises();

    expect(wrapper.text()).toBe('42');
  });

  it('prefers a per-call client over a provided one', async () => {
    const provided = harness(() => [{ id: 1, total: 1 }]);
    const explicit = harness(() => [{ id: 1, total: 2 }]);

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList, undefined, { client: explicit.cache });

        return () => h('div', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(List, { global: { plugins: [clientPlugin(provided.cache)] } });

    await flushPromises();

    expect(wrapper.text()).toBe('2');
    expect(provided.transport.calls).toHaveLength(0);
  });

  it('reports the missing configuration rather than fetching into a scratch cache', () => {
    const List = defineComponent({
      setup() {
        useQuery<Order[]>(useOrderList);

        return () => h('div');
      },
    });

    expect(() => mount(List)).toThrow(/no client configured/);
  });

  it('resolves outside a component, for a composable used in a bare scope', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    setClient(fx.cache);

    const scope = effectScope();
    let resolved: QueryCache | undefined;

    // No component instance, so there is nothing to `inject` from -- and Vue
    // must not be asked, or it logs `inject() can only be used inside setup()`
    // at a caller who did nothing wrong.
    scope.run(() => {
      resolved = useClient();
    });
    scope.stop();

    expect(resolved).toBe(fx.cache);
  });

  it('exposes the resolved client for work that reaches past the composables', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    let resolved: QueryCache | undefined;

    const List = defineComponent({
      setup() {
        // What an application calls to prefetch, to invalidate from an event
        // handler, or to `setPrincipal` on logout: the same answer the
        // composables resolve, resolved the same way.
        resolved = useClient();

        return () => h('div');
      },
    });

    mount(List, { global: { plugins: [clientPlugin(fx.cache)] } });

    expect(resolved).toBe(fx.cache);
  });

  it('accepts the injection key directly, for a test fixture or a custom provider', async () => {
    const fx = harness(() => [{ id: 1, total: 7 }]);

    const List = defineComponent({
      setup() {
        const { data } = useQuery<Order[]>(useOrderList);

        return () => h('div', String(data.value?.[0]?.total ?? '-'));
      },
    });

    const wrapper = mount(List, {
      global: { provide: { [clientKey as symbol]: fx.cache } },
    });

    await flushPromises();

    expect(wrapper.text()).toBe('7');
  });
});
