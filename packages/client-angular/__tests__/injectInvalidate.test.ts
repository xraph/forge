import {
  ChangeDetectionStrategy,
  Component,
  EnvironmentInjector,
  createEnvironmentInjector,
  signal,
} from '@angular/core';
import { TestBed } from '@angular/core/testing';
import type { ComponentFixture } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { setClient } from '@forge-go/client-core';
import { injectInvalidate, injectQuery } from '../src';
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
import { configure, render, settle } from './harness-angular';

/**
 * Run the invalidator's pending batch, let the refetches it started settle,
 * and apply what changed.
 *
 * `invalidate` is deliberately not awaitable -- it marks queries stale and
 * returns -- so a test asserting on the requests it caused has to drive the
 * same scheduler the application's microtask would have driven.
 */
async function drive(fx: Harness, fixture?: ComponentFixture<unknown>): Promise<void> {
  fx.scheduler.flush();
  await settle(fixture);
}

/**
 * Two components on two argument variants of the same read.
 *
 * Fixed arguments rather than an `input`, because `injectQuery` opens its
 * record in a field initializer -- before Angular has set any input -- so a
 * required input would be read too early and a defaulted one would open a
 * third record for the default.
 */
@Component({
  selector: 'app-detail-one',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<i>{{ order.data()?.total ?? "-" }}</i>',
})
class DetailOne {
  readonly order = injectQuery<Order>(useOrderGet, { path: { id: 1 } });
}

@Component({
  selector: 'app-detail-two',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '<i>{{ order.data()?.total ?? "-" }}</i>',
})
class DetailTwo {
  readonly order = injectQuery<Order>(useOrderGet, { path: { id: 2 } });
}

describe('injectInvalidate', () => {
  it('refreshes a list held by a sibling that this component cannot see', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.data()?.[0]?.total ?? "-" }}',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);
    }

    // The migration case exactly: a dialog that holds no query of its own,
    // imports no component, and still has to refresh what its write changed.
    @Component({
      selector: 'app-dialog',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '',
    })
    class Dialog {
      readonly invalidate = (invalidate = injectInvalidate());
    }

    @Component({
      selector: 'app-shell',
      imports: [List, Dialog],
      template: '<app-list /><app-dialog />',
    })
    class Shell {}

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('1');

    invalidate(useOrderList);
    await drive(fx, fixture);

    expect(fixture.nativeElement.textContent).toBe('2');
  });

  it('reaches a query the tag graph cannot, because it declares no tags', async () => {
    const fx = harness((request) => {
      if (request.meta === orderSearch) return [{ id: 1, total: fx.transport.countOf(orderSearch) }];

      return { id: 2, total: 0 };
    });

    @Component({
      selector: 'app-search',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ results.data()?.[0]?.total ?? "-" }}',
    })
    class Search {
      readonly results = injectQuery<Order[]>(useOrderSearch);
      readonly invalidate = injectInvalidate();
    }

    configure(fx);

    const fixture = render(Search);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('1');

    // A create declaring `invalidates: ['Order[]']`. The search carries
    // `Order:1` from its own response and nothing else, so the tag graph has
    // no edge to it and this write is invisible -- which is the whole defect.
    await fx.cache.mutate(useOrderCreate.meta, { body: { total: 7 } });
    await drive(fx, fixture);

    expect(fx.transport.countOf(orderSearch)).toBe(1);

    // Addressed by operation, it refetches regardless of what it declares.
    fixture.componentInstance.invalidate(useOrderSearch);
    await drive(fx, fixture);

    expect(fx.transport.countOf(orderSearch)).toBe(2);
    expect(fixture.nativeElement.textContent).toBe('2');
  });

  it('hits every argument variant when no arguments are given', async () => {
    const fx = harness((request) => ({
      id: request.args.path?.['id'],
      total: fx.transport.calls.length,
    }));

    @Component({
      selector: 'app-shell',
      imports: [DetailOne, DetailTwo],
      template: '<app-detail-one /><app-detail-two />',
    })
    class Shell {
      readonly invalidate = injectInvalidate();
    }

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    expect(fx.transport.countOf(orderGet)).toBe(2);

    fixture.componentInstance.invalidate(useOrderGet);
    await drive(fx, fixture);

    // Both variants, not just one, and not the whole cache either.
    expect(fx.transport.countOf(orderGet)).toBe(4);
  });

  it('targets exactly one variant when arguments are given', async () => {
    const fx = harness((request) => ({
      id: request.args.path?.['id'],
      total: fx.transport.calls.length,
    }));

    @Component({
      selector: 'app-shell',
      imports: [DetailOne, DetailTwo],
      template: '<app-detail-one /><app-detail-two />',
    })
    class Shell {
      readonly invalidate = injectInvalidate();
    }

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    expect(fx.transport.countOf(orderGet)).toBe(2);

    fixture.componentInstance.invalidate(useOrderGet, { path: { id: 1 } });
    await drive(fx, fixture);

    expect(fx.transport.countOf(orderGet)).toBe(3);
  });

  it('reads a signal for its arguments, at the moment of the call', async () => {
    const fx = harness((request) => ({
      id: request.args.path?.['id'],
      total: fx.transport.calls.length,
    }));

    @Component({
      selector: 'app-shell',
      imports: [DetailOne, DetailTwo],
      template: '<app-detail-one /><app-detail-two />',
    })
    class Shell {
      readonly which = signal(1);
      readonly invalidate = injectInvalidate();

      // The same shape of getter a component hands `injectQuery`, handed to
      // `invalidate` instead.
      readonly args = (): { path: { id: number } } => ({ path: { id: this.which() } });
    }

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    expect(fx.transport.countOf(orderGet)).toBe(2);

    const shell = fixture.componentInstance;

    shell.invalidate(useOrderGet, shell.args);
    await drive(fx, fixture);

    expect(fx.transport.countOf(orderGet)).toBe(3);

    // Read at the call, not captured at construction: moving the signal moves
    // which variant the *next* call names, and nothing about the last one. It
    // is also not a tracked read, so nothing here re-runs on the change.
    shell.which.set(2);

    shell.invalidate(useOrderGet, shell.args);
    await drive(fx, fixture);

    expect(fx.transport.countOf(orderGet)).toBe(4);
  });

  it('refetches a query fetched with no arguments at all', async () => {
    // The `open` args trap. `injectQuery(useOrderList)` keys as `GET /orders`
    // while its registry entry holds `{}`, which re-derives as
    // `GET /orders|{}`. Refetching the entry's own `args` would open a second,
    // empty record and refresh nothing the component is watching.
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.data()?.[0]?.total ?? "-" }}',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);
      readonly invalidate = injectInvalidate();
    }

    configure(fx);

    const fixture = render(List);

    await settle(fixture);

    const size = fx.cache.size;

    fixture.componentInstance.invalidate(useOrderList);
    await drive(fx, fixture);

    expect(fixture.nativeElement.textContent).toBe('2');
    // No second record was opened behind the component's back.
    expect(fx.cache.size).toBe(size);
  });

  it('does not fetch a destroyed query now, and refetches it on its next mount', async () => {
    let served = 0;
    const fx = harness(() => ({ id: 1, total: ++served }));

    @Component({
      selector: 'app-detail',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ order.data()?.total ?? "-" }}',
    })
    class Detail {
      readonly order = injectQuery<Order>(useOrderGet, { path: { id: 1 } });
    }

    @Component({
      selector: 'app-shell',
      imports: [Detail],
      template: '@if (visible()) { <app-detail /> }',
    })
    class Shell {
      readonly visible = signal(true);
      readonly invalidate = injectInvalidate();
    }

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    expect(fx.transport.countOf(orderGet)).toBe(1);

    fixture.componentInstance.visible.set(false);
    await settle(fixture);

    fixture.componentInstance.invalidate(useOrderGet);
    await drive(fx, fixture);

    // Nobody is watching it, so nothing is fetched: a write must not stampede
    // the network for every list the user has navigated away from.
    expect(fx.transport.countOf(orderGet)).toBe(1);

    fixture.componentInstance.visible.set(true);
    await settle(fixture);
    await drive(fx, fixture);

    // The staleness was remembered, and paid for at the moment it matters.
    expect(fx.transport.countOf(orderGet)).toBe(2);
    expect(fixture.nativeElement.textContent).toBe('2');
  });

  it('resolves refetch only once the mounted query has settled', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.data()?.[0]?.total ?? "-" }}',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);
      readonly invalidate = injectInvalidate();
    }

    configure(fx);

    const fixture = render(List);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('1');

    await fixture.componentInstance.invalidate.refetch(useOrderList);
    // A `detectChanges` and nothing else: the value was already in the signal
    // when the promise resolved, and all that is left is the paint. No
    // scheduler flush, which is the point -- this is the spelling a dialog
    // uses before it closes.
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toBe('2');
    expect(fx.transport.countOf(orderList)).toBe(2);

    // And the batch must not spend a second request on an answer on screen.
    await drive(fx, fixture);
    expect(fx.transport.countOf(orderList)).toBe(2);
  });

  it('forwards tags to the tag graph', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    @Component({
      selector: 'app-list',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.data()?.[0]?.total ?? "-" }}',
    })
    class List {
      readonly orders = injectQuery<Order[]>(useOrderList);
      readonly invalidate = injectInvalidate();
    }

    configure(fx);

    const fixture = render(List);

    await settle(fixture);

    fixture.componentInstance.invalidate.tags(['Order[]']);
    await drive(fx, fixture);

    expect(fixture.nativeElement.textContent).toBe('2');
  });

  it('resolves its cache explicitly, then injected, then global', async () => {
    const globalFx = harness(() => [{ id: 1, total: 1 }]);
    const provided = harness(() => [{ id: 1, total: 2 }]);
    const explicit = harness(() => [{ id: 1, total: 3 }]);

    setClient(globalFx.cache);

    // One mounted list in each cache, so each has something to refresh.
    @Component({
      selector: 'app-lists',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '',
    })
    class Lists {
      readonly a = injectQuery<Order[]>(useOrderList, undefined, { client: globalFx.cache });
      readonly b = injectQuery<Order[]>(useOrderList, undefined, { client: provided.cache });
      readonly c = injectQuery<Order[]>(useOrderList, undefined, { client: explicit.cache });

      readonly byDefault = injectInvalidate();
      readonly byOverride = injectInvalidate({ client: explicit.cache });
    }

    configure(provided);

    const fixture = render(Lists);

    await settle(fixture);

    expect(globalFx.transport.countOf(orderList)).toBe(1);

    await fixture.componentInstance.byDefault.refetch(useOrderList);
    await fixture.componentInstance.byOverride.refetch(useOrderList);

    expect(provided.transport.countOf(orderList)).toBe(2);
    expect(explicit.transport.countOf(orderList)).toBe(2);
    // The injector beat the global, and the override beat the injector.
    expect(globalFx.transport.countOf(orderList)).toBe(1);
  });

  it('takes an injector, for a call site with no ambient context', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
    const list = injectQuery<Order[]>(useOrderList, undefined, { injector });

    // The escape hatch for `ngOnInit` or a callback. Unlike its siblings this
    // registers no teardown, so the injector's lifetime is not part of the
    // bargain -- it is only where the cache is looked up.
    const invalidate = injectInvalidate({ injector });

    await settle();

    expect(list.data()?.[0]?.total).toBe(1);

    await invalidate.refetch(useOrderList);

    expect(fx.transport.countOf(orderList)).toBe(2);
    expect(list.data()?.[0]?.total).toBe(2);

    injector.destroy();
  });

  it('needs no injection context at all when handed a cache', async () => {
    let served = 0;
    const fx = harness(() => [{ id: 1, total: ++served }]);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));

    injectQuery<Order[]>(useOrderList, undefined, { injector });

    await settle();

    // `injectClient` returns the override before it reaches `inject`, so this
    // one call is legal from a plain function, a service method or a test --
    // anywhere a cache is already in hand.
    const invalidate = injectInvalidate({ client: fx.cache });

    await invalidate.refetch(useOrderList);

    expect(fx.transport.countOf(orderList)).toBe(2);

    injector.destroy();
  });
});
