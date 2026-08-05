import { ChangeDetectionStrategy, Component, Input } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { injectMutation, injectQuery } from '../src';
import type { InjectMutationResult, InjectQueryResult } from '../src';
import { harness, orderGet, orderList, useOrderCreate, useOrderGet, useOrderList } from './harness';
import type { Order } from './harness';
import { configure, render, settle } from './harness-angular';

/**
 * The tests this package exists to pass.
 *
 * The core returns the *same object* when nothing it can prove changed, and
 * that is what lets an `OnPush` child skip its check. Angular will not rewrite
 * that object the way Vue's deep `ref` would -- a signal holds whatever it is
 * given -- so the hazard here is the binding itself: one `{...state()}` in a
 * `computed`, one `.map` over a list on the way to a template, and every read
 * mints a new object. That also breaks change detection outright rather than
 * merely making it wasteful, because Angular's default signal equality is
 * `Object.is`: a `computed` that rebuilds its value is *never* equal to itself,
 * so it notifies on every single read of the source.
 *
 * Every assertion below therefore compares what the component reads against
 * what the cache **holds** -- `toBe(cache.getState().data)` -- and against
 * itself across two reads. Both fail immediately if any `computed` in
 * `injectQuery.ts` starts copying, which was measured by making one do so.
 */

describe('referential identity', () => {
  it('hands the component the object the cache holds, unchanged', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    configure(fx);

    const seen = TestBed.runInInjectionContext(() => injectQuery<Order[]>(useOrderList));

    await settle();

    const held = fx.cache.getState<Order[]>(orderList);

    // The snapshot, the array and the entity inside it: three levels, all of
    // them the cache's own objects.
    expect(seen.state()).toBe(held);
    expect(seen.data()).toBe(held.data);
    expect(seen.data()?.[0]).toBe(held.data?.[0]);

    // And stable across reads, which is what `Object.is` equality downstream
    // of these signals depends on. A `computed` that spread its value would
    // return a new object here and mark every consumer dirty forever.
    expect(seen.data()).toBe(seen.data());
    expect(seen.state()).toBe(seen.state());
  });

  it('keeps a sibling entity identical when its neighbour changes', async () => {
    let total = 0;
    const fx = harness(() => {
      total += 10;

      // Order 2 is byte-for-byte what it was; order 1 is not. Both arrive as
      // fresh object literals every time, so anything short of real structural
      // sharing fails this.
      return [
        { id: 1, total },
        { id: 2, total: 500 },
      ];
    });

    configure(fx);

    const seen = TestBed.runInInjectionContext(() => injectQuery<Order[]>(useOrderList));

    await settle();

    const before = seen.data();

    expect(before?.[1]).toEqual({ id: 2, total: 500 });

    await seen.refetch();
    await settle();

    const after = seen.data();

    expect(fx.transport.countOf(orderList)).toBe(2);
    // The one that moved, moved.
    expect(after?.[0]).not.toBe(before?.[0]);
    expect(after?.[0]?.total).toBe(20);
    // The one that did not, did not -- and is still the store's own object, so
    // a child holding it as an input sees no change at all.
    expect(after?.[1]).toBe(before?.[1]);
    expect(after?.[1]).toBe(fx.cache.getState<Order[]>(orderList).data?.[1]);
    // The container too: a binding that rebuilt the list -- `[...data]`, a
    // `.map`, a `computed` assembling view models -- would satisfy every
    // element assertion above and still hand every consumer a new array on
    // each read.
    expect(after).toBe(fx.cache.getState<Order[]>(orderList).data);
  });

  /**
   * The payoff. `OnPush` plus an object input is Angular's `React.memo`: the
   * child is only checked when an input reference changes or one of its own
   * signals moves, so an unchanged entity costs nothing at all.
   *
   * Also the one test in this file that survives a copying binding -- measured,
   * by making `data` a `computed` that rebuilds its array. The elements stay
   * identical through a shallow copy, so `@for` still hands each row the same
   * object. It earns its place as the statement of what identity is *for*; the
   * assertions around it are what pin the mechanism down.
   */
  it('does not check a child whose entity did not change', async () => {
    let total = 0;
    const fx = harness(() => {
      total += 10;

      return [
        { id: 1, total },
        { id: 2, total: 500 },
      ];
    });

    const renders = new Map<number, number>();

    @Component({
      selector: 'app-row',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '<span>{{ count() }}</span>',
    })
    class Row {
      /**
       * The decorator spelling rather than `input.required<Order>()`, because
       * these components are compiled by the JIT compiler and a signal input
       * is recognised only by the ahead-of-time one -- it is discovered by
       * reading the class body, which JIT never sees. The change-detection
       * semantics under test are identical either way: Angular compares an
       * input's new value with `Object.is` and leaves an `OnPush` child clean
       * when it has not moved.
       */
      @Input({ required: true }) order!: Order;

      count(): number {
        const order = this.order;

        renders.set(order.id, (renders.get(order.id) ?? 0) + 1);

        return order.total;
      }
    }

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      imports: [Row],
      template: '@for (order of orders.data() ?? []; track order.id) { <app-row [order]="order" /> }',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);
    }

    configure(fx);

    const fixture = render(List);

    await settle(fixture);

    expect(renders.get(1)).toBe(1);
    expect(renders.get(2)).toBe(1);

    // A check of the whole tree with nothing changed: neither child is dirty,
    // so neither template runs.
    fixture.detectChanges();
    expect(renders.get(1)).toBe(1);
    expect(renders.get(2)).toBe(1);

    // A refetch that changes order 1 and leaves order 2 alone.
    await fx.cache.refetch(orderList);
    await settle(fixture);

    expect(renders.get(1)).toBe(2);
    expect(renders.get(2)).toBe(1);
    expect(fixture.nativeElement.textContent).toContain('500');
  });

  it('leaves a mutation result uncopied too', async () => {
    const fx = harness((request, call) =>
      request.meta.method === 'GET' ? { id: 9, total: call === 0 ? 1 : 5 } : { id: 9, total: 5 },
    );

    configure(fx);

    let created!: InjectMutationResult<Order>;
    let detail!: InjectQueryResult<Order>;

    TestBed.runInInjectionContext(() => {
      created = injectMutation<Order>(useOrderCreate);
      detail = injectQuery<Order>(useOrderGet, { path: { id: 9 } });
    });
    await settle();

    expect(detail.data()?.total).toBe(1);

    await created.mutate({ body: { total: 5 } });
    await detail.refetch();
    await settle();

    expect(created.data()).toEqual({ id: 9, total: 5 });
    expect(created.data()).toBe(created.data());
    // The write's response was normalized into `Order:9`, and the refetch that
    // followed carried the same entity -- so the object the mutation reported
    // and the object the query is rendering are one object, through two
    // separate bindings and two separate signals.
    expect(fx.transport.countOf(orderGet)).toBe(2);
    expect(created.data()).toBe(detail.data());
  });

  it('does not notify a derived signal when the field it reads did not move', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    configure(fx);

    const seen = TestBed.runInInjectionContext(() => injectQuery<Order[]>(useOrderList));

    await settle();

    const settledData = seen.data();

    // A refetch moves `isFetching` twice and produces a structurally identical
    // array. `data` is a `computed` over the snapshot, and Angular compares
    // with `Object.is`, so the entity a template reads never changes identity
    // and nothing that depends on it alone is marked dirty.
    await seen.refetch();
    await settle();

    expect(fx.transport.countOf(orderList)).toBe(2);
    expect(seen.data()?.[0]).toBe(settledData?.[0]);
    expect(seen.data()).toBe(fx.cache.getState<Order[]>(orderList).data);
    expect(seen.isFetching()).toBe(false);
  });
});
