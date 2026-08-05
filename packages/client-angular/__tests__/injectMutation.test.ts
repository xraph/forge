import {
  ChangeDetectionStrategy,
  Component,
  EnvironmentInjector,
  createEnvironmentInjector,
  signal,
} from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it, vi } from 'vitest';
import { injectMutation, injectQuery } from '../src';
import type { InjectMutationResult } from '../src';
import { deferred, harness, orderCreate, orderList, useOrderCreate, useOrderList } from './harness';
import type { Order } from './harness';
import { configure, render, settle } from './harness-angular';

describe('injectMutation', () => {
  it('reports idle, pending and success around one call', async () => {
    const gate = deferred<unknown>();
    const fx = harness(() => gate.promise);

    @Component({
      selector: 'app-create',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ create.status() }}',
    })
    class Create {
      readonly create = injectMutation<Order>(useOrderCreate);
    }

    configure(fx);

    const fixture = render(Create);
    const { create } = fixture.componentInstance;

    expect(fixture.nativeElement.textContent).toBe('idle');

    const settled = create.mutate({ body: { total: 5 } });

    await settle(fixture);
    expect(fixture.nativeElement.textContent).toBe('pending');
    expect(create.isPending()).toBe(true);

    gate.resolve({ id: 9, total: 5 });
    await settled;
    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('success');
    expect(create.data()).toEqual({ id: 9, total: 5 });
    expect(create.isPending()).toBe(false);
  });

  it('records an error, and reset returns it to idle', async () => {
    const fx = harness(() => {
      throw new Error('conflict');
    });

    configure(fx);

    let create!: InjectMutationResult<Order>;

    TestBed.runInInjectionContext(() => {
      create = injectMutation<Order>(useOrderCreate);
    });

    let caught: unknown;

    await create.mutateAsync({ body: {} }).catch((error: unknown) => {
      caught = error;
    });
    await settle();

    expect((caught as Error).message).toBe('conflict');
    expect(create.status()).toBe('error');
    expect((create.error() as Error).message).toBe('conflict');

    create.reset();

    expect(create.status()).toBe('idle');
    expect(create.data()).toBeUndefined();
  });

  it('resolves rather than rejecting when the mutation fails', async () => {
    const fx = harness(() => {
      throw new Error('conflict');
    });

    configure(fx);

    let create!: InjectMutationResult<Order>;

    TestBed.runInInjectionContext(() => {
      create = injectMutation<Order>(useOrderCreate);
    });

    // No `.catch`, exactly as the README's `(click)` writes it.
    const resolved = await create.mutate({ body: {} });

    expect(resolved).toBeUndefined();
    // And the failure is not lost -- it is where the interface reads it.
    expect(create.status()).toBe('error');
    expect((create.error() as Error).message).toBe('conflict');
  });

  it('raises no unhandled rejection from the documented click handler', async () => {
    const fx = harness(() => {
      throw new Error('conflict');
    });

    const unhandled = vi.fn();

    process.on('unhandledRejection', unhandled);

    try {
      configure(fx);

      let create!: InjectMutationResult<Order>;

      TestBed.runInInjectionContext(() => {
        create = injectMutation<Order>(useOrderCreate);
      });

      // The spelling from the README: no await, no catch, fired from a handler.
      void create.mutate({ body: {} });

      await settle();
      await new Promise((resolve) => {
        setImmediate(resolve);
      });

      expect(unhandled).not.toHaveBeenCalled();
      expect(create.status()).toBe('error');
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

    @Component({
      selector: 'app-orders',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.data()?.[0]?.total ?? "-" }}',
    })
    class Orders {
      readonly orders = injectQuery<Order[]>(useOrderList);
      readonly create = injectMutation<Order>(useOrderCreate);
    }

    configure(fx);

    const fixture = render(Orders);

    await settle(fixture);
    expect(fixture.nativeElement.textContent).toBe('1');

    // `orderCreate` declares `Order[]`, so the list is refetched -- through the
    // list's own subscription, with no wiring in this binding at all.
    await fixture.componentInstance.create.mutate({ body: { total: 5 } });
    fx.scheduler.flush();
    await settle(fixture);

    expect(fx.transport.countOf(orderList)).toBe(2);
    expect(fixture.nativeElement.textContent).toBe('2');
    expect(fixture.componentInstance.create.status()).toBe('success');
  });

  it('does not publish a result into a context that has already been destroyed', async () => {
    const gate = deferred<unknown>();
    const fx = harness(() => gate.promise);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
    const create = injectMutation<Order>(useOrderCreate, { injector });

    const settled = create.mutate({ body: {} });

    await settle();
    expect(create.status()).toBe('pending');

    injector.destroy();
    gate.resolve({ id: 9, total: 5 });
    await settled;
    await settle();

    // The write happened -- the cache has the entity -- but the destroyed
    // context's signals are left where they were rather than settling behind
    // the user's back.
    expect(create.status()).toBe('pending');
  });

  it('lets the last of two concurrent calls win', async () => {
    const first = deferred<unknown>();
    const second = deferred<unknown>();
    const fx = harness((_request, call) => (call === 0 ? first.promise : second.promise));

    configure(fx);

    let create!: InjectMutationResult<Order>;

    TestBed.runInInjectionContext(() => {
      create = injectMutation<Order>(useOrderCreate);
    });

    const a = create.mutate({ body: { total: 1 } });
    const b = create.mutate({ body: { total: 2 } });

    // The second call was dispatched last, so its result is the one the
    // interface must end on however the two responses interleave.
    second.resolve({ id: 2, total: 2 });
    await b;
    first.resolve({ id: 1, total: 1 });
    await a;
    await settle();

    expect(create.data()).toEqual({ id: 2, total: 2 });
  });

  it('reads a reactive option at call time rather than at construction', async () => {
    const fx = harness(() => ({ id: 9, total: 5 }));

    configure(fx);

    const header = signal('one');
    let create!: InjectMutationResult<Order>;

    TestBed.runInInjectionContext(() => {
      // A getter, so a `place` callback or a header that depends on component
      // state is current when the write actually runs rather than frozen as it
      // was at construction.
      create = injectMutation<Order>(useOrderCreate, () => ({
        headers: { 'x-test': header() },
      }));
    });

    await create.mutate({ body: {} });
    header.set('two');
    await create.mutate({ body: {} });
    await settle();

    expect(fx.transport.calls[0]?.headers?.['x-test']).toBe('one');
    expect(fx.transport.calls[1]?.headers?.['x-test']).toBe('two');
  });
});
