import { ChangeDetectionStrategy, Component, provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { setClient } from '@forge-go/client-core';
import type { QueryCache } from '@forge-go/client-core';
import { FORGE_CLIENT, injectForgeClient, injectQuery, provideForgeClient } from '../src';
import { harness, orderList, useOrderList } from './harness';
import type { Order } from './harness';
import { configure, render, settle } from './harness-angular';

@Component({
  selector: 'app-list',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '{{ orders.data()?.[0]?.total ?? "-" }}',
})
class List {
  readonly orders = injectQuery<Order[]>(useOrderList);
}

describe('client resolution', () => {
  it('falls back to the module-level client when nothing is provided', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    setClient(fx.cache);
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection()] });

    const fixture = render(List);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('99');
    expect(fx.transport.countOf(orderList)).toBe(1);
  });

  it('prefers an injected client over the module-level one', async () => {
    const globalFx = harness(() => [{ id: 1, total: 1 }]);
    const scoped = harness(() => [{ id: 1, total: 2 }]);

    setClient(globalFx.cache);
    configure(scoped);

    const fixture = render(List);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('2');
    expect(globalFx.transport.calls).toHaveLength(0);
  });

  it('takes a client from a component-level provider as well as the root', async () => {
    const rootFx = harness(() => [{ id: 1, total: 1 }]);
    const branch = harness(() => [{ id: 1, total: 42 }]);

    @Component({
      selector: 'app-branch',
      changeDetection: ChangeDetectionStrategy.OnPush,
      imports: [List],
      providers: [provideForgeClient(branch.cache)],
      template: '<app-list />',
    })
    class Branch {}

    configure(rootFx);

    const fixture = render(Branch);

    await settle(fixture);

    // The component's own injector wins for its subtree: the spelling for an
    // application talking to two backends from two routes.
    expect(fixture.nativeElement.textContent).toBe('42');
    expect(rootFx.transport.calls).toHaveLength(0);
  });

  it('prefers a per-call client over an injected one', async () => {
    const provided = harness(() => [{ id: 1, total: 1 }]);
    const explicit = harness(() => [{ id: 1, total: 2 }]);

    @Component({
      selector: 'app-explicit',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ orders.data()?.[0]?.total ?? "-" }}',
    })
    class Explicit {
      readonly orders = injectQuery<Order[]>(useOrderList, undefined, { client: explicit.cache });
    }

    configure(provided);

    const fixture = render(Explicit);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('2');
    expect(provided.transport.calls).toHaveLength(0);
  });

  it('reports the missing configuration rather than fetching into a scratch cache', () => {
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection()] });

    expect(() => render(List)).toThrow(/no client configured/);
  });

  it('exposes the resolved client for work that reaches past the bindings', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    configure(fx);

    // What an application calls to prefetch, to invalidate from an event
    // handler, or to `setPrincipal` on logout: the same answer the bindings
    // resolve, resolved the same way.
    const resolved: QueryCache = TestBed.runInInjectionContext(() => injectForgeClient());

    expect(resolved).toBe(fx.cache);
    // And the token itself, for an application that would rather inject the
    // cache the way it injects everything else.
    expect(TestBed.inject(FORGE_CLIENT)).toBe(fx.cache);
  });

  it('needs no injection context when the cache is handed over explicitly', async () => {
    const fx = harness(() => [{ id: 1, total: 99 }]);

    // Outside `runInInjectionContext` entirely: the override is checked before
    // `inject` is ever reached, so a caller who already has a cache is not
    // forced into a context to use it.
    expect(injectForgeClient(fx.cache)).toBe(fx.cache);
  });
});
