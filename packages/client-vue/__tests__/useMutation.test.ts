import { defineComponent, effectScope, h, ref } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import { clientPlugin, useMutation, useQuery } from '../src';
import type { UseMutationResult, UseQueryResult } from '../src';
import {
  deferred,
  harness,
  orderCreate,
  orderList,
  useOrderCreate,
  useOrderList,
} from './harness';
import type { Harness, Order } from './harness';

function withClient(fx: Harness) {
  return { global: { plugins: [clientPlugin(fx.cache)] } };
}

/** Mount a component whose only job is to run one composable. */
function mountWith(fx: Harness, setup: () => void) {
  return mount(
    defineComponent({
      setup() {
        setup();

        return () => h('div');
      },
    }),
    withClient(fx),
  );
}

describe('useMutation', () => {
  it('reports idle, pending and success around one call', async () => {
    const gate = deferred<unknown>();
    const fx = harness(() => gate.promise);

    let create!: UseMutationResult<Order>;

    const wrapper = mount(
      defineComponent({
        setup() {
          create = useMutation<Order>(useOrderCreate);

          return () => h('div', create.status.value);
        },
      }),
      withClient(fx),
    );

    expect(wrapper.text()).toBe('idle');

    const settled = create.mutate({ body: { total: 5 } });

    await flushPromises();
    expect(wrapper.text()).toBe('pending');
    expect(create.isPending.value).toBe(true);

    gate.resolve({ id: 9, total: 5 });
    await settled;
    await flushPromises();

    expect(wrapper.text()).toBe('success');
    expect(create.data.value).toEqual({ id: 9, total: 5 });
    expect(create.isPending.value).toBe(false);
  });

  it('records an error, and reset returns it to idle', async () => {
    const fx = harness(() => {
      throw new Error('conflict');
    });

    let create!: UseMutationResult<Order>;

    mountWith(fx, () => {
      create = useMutation<Order>(useOrderCreate);
    });

    let caught: unknown;

    await create.mutateAsync({ body: {} }).catch((error: unknown) => {
      caught = error;
    });
    await flushPromises();

    expect((caught as Error).message).toBe('conflict');
    expect(create.status.value).toBe('error');
    expect((create.error.value as Error).message).toBe('conflict');

    create.reset();

    expect(create.status.value).toBe('idle');
    expect(create.data.value).toBeUndefined();
  });

  it('resolves rather than rejecting when the mutation fails', async () => {
    const fx = harness(() => {
      throw new Error('conflict');
    });

    let create!: UseMutationResult<Order>;

    mountWith(fx, () => {
      create = useMutation<Order>(useOrderCreate);
    });

    // No `.catch`, exactly as the README's `@click` writes it.
    const resolved = await create.mutate({ body: {} });

    expect(resolved).toBeUndefined();
    // And the failure is not lost -- it is where the interface reads it.
    expect(create.status.value).toBe('error');
    expect((create.error.value as Error).message).toBe('conflict');
  });

  it('raises no unhandled rejection from the documented click handler', async () => {
    const fx = harness(() => {
      throw new Error('conflict');
    });

    const unhandled = vi.fn();

    process.on('unhandledRejection', unhandled);

    try {
      let create!: UseMutationResult<Order>;

      mountWith(fx, () => {
        create = useMutation<Order>(useOrderCreate);
      });

      // The spelling from the README: no await, no catch, fired from a handler.
      void create.mutate({ body: {} });

      await flushPromises();
      await new Promise((resolve) => {
        setImmediate(resolve);
      });

      expect(unhandled).not.toHaveBeenCalled();
      expect(create.status.value).toBe('error');
    } finally {
      process.off('unhandledRejection', unhandled);
    }
  });

  it('updates the queries the write invalidated', async () => {
    let total = 0;
    const fx = harness((request) => {
      if (request.meta === orderCreate) return { id: 2, total: 5 };

      total += 1;

      return [{ id: 1, total }];
    });

    let list!: UseQueryResult<Order[]>;
    let create!: UseMutationResult<Order>;

    const wrapper = mount(
      defineComponent({
        setup() {
          list = useQuery<Order[]>(useOrderList);
          create = useMutation<Order>(useOrderCreate);

          return () => h('div', String(list.data.value?.[0]?.total ?? '-'));
        },
      }),
      withClient(fx),
    );

    await flushPromises();
    expect(wrapper.text()).toBe('1');

    // `orderCreate` declares `Order[]`, so the list is refetched -- through the
    // list's own subscription, with no wiring in this composable at all.
    await create.mutate({ body: { total: 5 } });
    fx.scheduler.flush();
    await flushPromises();

    expect(fx.transport.countOf(orderList)).toBe(2);
    expect(wrapper.text()).toBe('2');
    expect(create.status.value).toBe('success');
  });

  it('does not publish a result into a scope that has already stopped', async () => {
    const gate = deferred<unknown>();
    const fx = harness(() => gate.promise);

    const scope = effectScope();
    let create!: UseMutationResult<Order>;

    scope.run(() => {
      create = useMutation<Order>(useOrderCreate, { client: fx.cache });
    });

    const settled = create.mutate({ body: {} });

    await flushPromises();
    expect(create.status.value).toBe('pending');

    scope.stop();
    gate.resolve({ id: 9, total: 5 });
    await settled;
    await flushPromises();

    // The write happened -- the cache has the entity -- but the disposed
    // scope's refs are left where they were rather than settling behind the
    // user's back.
    expect(create.status.value).toBe('pending');
  });

  it('lets the last of two concurrent calls win', async () => {
    const first = deferred<unknown>();
    const second = deferred<unknown>();
    const fx = harness((_request, call) => (call === 0 ? first.promise : second.promise));

    let create!: UseMutationResult<Order>;

    mountWith(fx, () => {
      create = useMutation<Order>(useOrderCreate);
    });

    const a = create.mutate({ body: { total: 1 } });
    const b = create.mutate({ body: { total: 2 } });

    // The second call was dispatched last, so its result is the one the
    // interface must end on however the two responses interleave.
    second.resolve({ id: 2, total: 2 });
    await b;
    first.resolve({ id: 1, total: 1 });
    await a;
    await flushPromises();

    expect(create.data.value).toEqual({ id: 2, total: 2 });
  });

  it('reads a reactive option at call time rather than at setup', async () => {
    const fx = harness(() => ({ id: 9, total: 5 }));

    const header = ref('one');
    let create!: UseMutationResult<Order>;

    mountWith(fx, () => {
      // A getter, so a `place` callback or a header that depends on reactive
      // state is current when the write actually runs -- not frozen as it was
      // at setup, which is the Vue-shaped version of the hazard React solves
      // with a ref published from an effect.
      create = useMutation<Order>(useOrderCreate, () => ({ headers: { 'x-test': header.value } }));
    });

    await create.mutate({ body: {} });
    header.value = 'two';
    await create.mutate({ body: {} });
    await flushPromises();

    expect(fx.transport.calls[0]?.headers?.['x-test']).toBe('one');
    expect(fx.transport.calls[1]?.headers?.['x-test']).toBe('two');
  });
});
