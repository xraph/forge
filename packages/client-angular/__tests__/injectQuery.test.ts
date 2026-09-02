import {
  ChangeDetectionStrategy,
  Component,
  EnvironmentInjector,
  createEnvironmentInjector,
  signal,
} from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { injectQuery } from '../src';
import type { InjectQueryResult } from '../src';
import { harness, orderGet, orderList, useOrderGet, useOrderList, useOrderPatch } from './harness';
import type { Order } from './harness';
import { configure, render, settle } from './harness-angular';

describe('injectQuery', () => {
  it('renders its value, and updates when an entity it depends on changes', async () => {
    let total = 0;
    const fx = harness((request) => {
      total += 100;

      return { id: request.args.path?.['id'], total };
    });

    @Component({
      selector: 'app-detail',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ order.status() }}:{{ order.data()?.total ?? "-" }}',
    })
    class Detail {
      readonly order = injectQuery<Order>(useOrderGet, { path: { id: 1 } });
    }

    configure(fx);

    const fixture = render(Detail);

    // The subscription started the request synchronously, and the binding read
    // the state back afterwards -- so even the first paint is `pending` rather
    // than a flash of `idle` nobody asked for.
    expect(fixture.nativeElement.textContent).toBe('pending:-');

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('success:100');

    // Patching order 1 invalidates `Order:1`, which is this query's own tag --
    // acquired both from `provides` and from the entity its response
    // normalized to.
    await fx.cache.mutate(useOrderPatch.meta, { path: { id: 1 } });
    fx.scheduler.flush();
    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('success:300');
  });

  it('serves two components on one query from a single request', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '<i>{{ orders.data()?.[0]?.total ?? "-" }}</i>',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);
    }

    @Component({
      selector: 'app-shell',
      imports: [List],
      template: '<app-list /><app-list />',
    })
    class Shell {}

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    expect(fx.transport.countOf(orderList)).toBe(1);
    expect(fixture.nativeElement.textContent).toBe('9999');
    // One entry with two listeners, not two entries: the tag index must not
    // fan one invalidation out into two refetches of identical data.
    expect(fx.cache.registry.mounted).toBe(1);
    expect(fx.cache.registry.get(fx.cache.key(orderList))?.mounts).toBe(1);
  });

  it('releases the subscription when the last consumer is destroyed', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.data()?.length ?? 0 }}',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);
    }

    @Component({
      selector: 'app-shell',
      imports: [List],
      template: '@if (show() > 0) { <app-list /> } @if (show() > 1) { <app-list /> }',
    })
    class Shell {
      readonly show = signal(2);
    }

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    const key = fx.cache.key(orderList);

    expect(fx.cache.registry.get(key)?.mounts).toBe(1);

    // One of two consumers goes away: still mounted.
    fixture.componentInstance.show.set(1);
    await settle(fixture);
    expect(fx.cache.registry.get(key)?.mounts).toBe(1);
    expect(fx.cache.registry.mounted).toBe(1);

    // The last one: released, by that component's own `DestroyRef`.
    fixture.componentInstance.show.set(0);
    await settle(fixture);
    expect(fx.cache.registry.get(key)?.mounts).toBe(0);
    expect(fx.cache.registry.mounted).toBe(0);
  });

  it('releases when a bare injector is destroyed, with no component anywhere', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    configure(fx);

    // A service's injector, a lazy route's, or one made by hand for a scope
    // Angular has no other name for. This is why the teardown hangs off
    // `DestroyRef` rather than `ngOnDestroy`.
    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
    const seen = injectQuery<Order[]>(useOrderList, undefined, { injector });

    await settle();

    const key = fx.cache.key(orderList);

    expect(seen.data()?.[0]?.total).toBe(99);
    expect(fx.cache.registry.get(key)?.mounts).toBe(1);

    injector.destroy();

    expect(fx.cache.registry.get(key)?.mounts).toBe(0);
    expect(fx.cache.registry.mounted).toBe(0);
  });

  it('releases exactly once, however the release is spelt', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
    const seen = injectQuery<Order[]>(useOrderList, undefined, { injector });

    // A second consumer of the same query, so the mount count would go
    // negative -- or the entry would be unlinked with a live subscriber -- if
    // a double release were not idempotent.
    injectQuery<Order[]>(useOrderList, undefined, { injector });

    await settle();

    const key = fx.cache.key(orderList);

    expect(fx.cache.registry.get(key)?.mounts).toBe(1);

    seen.destroy();
    seen.destroy();
    injector.destroy();

    expect(fx.cache.registry.get(key)?.mounts).toBe(0);
    expect(fx.cache.registry.mounted).toBe(0);
  });

  it('re-subscribes to the new query when a reactive argument changes', async () => {
    const fx = harness((request) => ({ id: request.args.path?.['id'], total: 7 }));

    @Component({
      selector: 'app-detail',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ order.data()?.id ?? "-" }}',
    })
    class Detail {
      readonly id = signal(1);
      // The getter spelling: a fresh object literal on every evaluation, which
      // is why the effect watches the derived key rather than this.
      readonly order = injectQuery<Order>(useOrderGet, () => ({ path: { id: this.id() } }));
    }

    configure(fx);

    const fixture = render(Detail);

    await settle(fixture);
    expect(fixture.nativeElement.textContent).toBe('1');

    fixture.componentInstance.id.set(2);
    // Twice, and both are load-bearing rather than a sleep in disguise: the
    // first cycle runs the effect that notices the new key and subscribes, and
    // the second renders the response that subscription then fetched.
    await settle(fixture);
    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('2');
    expect(fx.cache.registry.get(fx.cache.key(orderGet, { path: { id: 1 } }))?.mounts).toBe(0);
    expect(fx.cache.registry.get(fx.cache.key(orderGet, { path: { id: 2 } }))?.mounts).toBe(1);
  });

  it('does not resurrect the subscription when an argument moves after destroy()', async () => {
    const fx = harness((request) => ({ id: request.args.path?.['id'], total: 7 }));

    @Component({
      selector: 'app-detail',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ order.data()?.id ?? "-" }}',
    })
    class Detail {
      readonly id = signal(1);
      readonly order = injectQuery<Order>(useOrderGet, () => ({ path: { id: this.id() } }));
    }

    configure(fx);

    const fixture = render(Detail);

    await settle(fixture);

    const first = fx.cache.key(orderGet, { path: { id: 1 } });

    expect(fx.cache.registry.get(first)?.mounts).toBe(1);

    // The documented escape hatch: release now, before the context is
    // destroyed. It has to release *both* halves -- a watcher left running
    // re-subscribes on its next tick to a query the caller has finished with.
    fixture.componentInstance.order.destroy();

    expect(fx.cache.registry.get(first)?.mounts).toBe(0);

    const requests = fx.transport.calls.length;

    fixture.componentInstance.id.set(2);
    await settle(fixture);
    await settle(fixture);

    expect(fx.cache.registry.mounted).toBe(0);
    expect(fx.cache.registry.get(fx.cache.key(orderGet, { path: { id: 2 } }))?.mounts ?? 0).toBe(0);
    expect(fx.transport.calls).toHaveLength(requests);
  });

  it('does not churn the subscription when an unrelated signal moves', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ tick() }}:{{ orders.data()?.length ?? 0 }}',
    })
    class List {
      readonly tick = signal(0);
      readonly orders = injectQuery<Order[]>(useOrderList, () => ({ query: { status: 'open' } }));
    }

    configure(fx);

    const fixture = render(List);

    await settle(fixture);

    const entry = fx.cache.registry.get(fx.cache.key(orderList, { query: { status: 'open' } }));

    expect(entry?.mounts).toBe(1);

    const settledState = fixture.componentInstance.orders.state();

    // Ten change-detection cycles driven by something else entirely. A key
    // recomputed from the arguments object's identity would re-subscribe on
    // each one, dropping the mount count to zero every time and making the
    // entry a candidate for LRU eviction while it is on screen.
    for (let i = 0; i < 10; i++) {
      fixture.componentInstance.tick.set(i + 1);
      await settle(fixture);
    }

    expect(fixture.nativeElement.textContent).toBe('10:1');
    expect(fixture.componentInstance.orders.state()).toBe(settledState);
    expect(entry?.mounts).toBe(1);
    expect(fx.transport.countOf(orderList)).toBe(1);
  });

  it('keeps the last good value beside an error from a failed refetch', async () => {
    const fx = harness((_request, call) => {
      if (call > 0) throw new Error('boom');

      return [{ id: 1, total: 99 }];
    });

    configure(fx);

    let seen!: InjectQueryResult<Order[]>;

    TestBed.runInInjectionContext(() => {
      seen = injectQuery<Order[]>(useOrderList);
    });
    await settle();

    const good = seen.data();

    await seen.refetch().catch(() => undefined);
    await settle();

    expect(seen.status()).toBe('error');
    expect((seen.error() as Error).message).toBe('boom');
    // Stale data plus a warning beats an empty screen, and the value is the
    // same object it was: a failure does not invalidate identity.
    expect(seen.data()).toBe(good);
  });

  it('reports a failed first fetch as an error state rather than throwing', async () => {
    const fx = harness(() => {
      throw new Error('nope');
    });

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.status() }}:{{ errorText() }}',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);

      errorText(): string {
        const error = this.orders.error();

        return error === undefined ? '-' : (error as Error).message;
      }
    }

    configure(fx);

    const fixture = render(List);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('error:nope');
  });

  it('passes a per-call staleTime through to the cache', async () => {
    const fx = harness(() => [{ id: 1, total: 10 }]);

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList, undefined, { staleTime: 250 });
    }

    configure(fx);

    const fixture = render(List);

    await settle(fixture);

    expect(fx.cache.effectiveStaleTime(orderList)).toBe(250);
  });

  it('keeps the per-call staleTime after the handle rebuilds on a key change', async () => {
    const fx = harness((request) => ({ id: request.args.path?.['id'], total: 7 }));

    @Component({
      selector: 'app-detail',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ order.data()?.id ?? "-" }}',
    })
    class Detail {
      readonly id = signal(1);
      readonly order = injectQuery<Order>(
        useOrderGet,
        () => ({ path: { id: this.id() } }),
        { staleTime: 250 },
      );
    }

    configure(fx);

    const fixture = render(Detail);

    await settle(fixture);

    expect(fx.cache.effectiveStaleTime(orderGet, { path: { id: 1 } })).toBe(250);

    // The rebuild trap: a component whose key changes tears down the handle
    // built at the first construction site and builds a new one at the
    // second. Missing `staleTime` there makes the option work only until the
    // arguments change, and then silently stop.
    fixture.componentInstance.id.set(2);
    await settle(fixture);
    await settle(fixture);

    expect(fx.cache.effectiveStaleTime(orderGet, { path: { id: 2 } })).toBe(250);
  });
});
