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
import { liveHarness, useOrderGet, useOrderList } from './harness';
import type { LiveHarness, Order } from './harness';
import { configure, render, settle } from './harness-angular';

/** Deliver a frame, commit it, and apply what it changed. */
async function emit(fx: LiveHarness, message: unknown): Promise<void> {
  fx.emit(message);
  await settle();
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

/** A list that declared itself live at the call site, once and for good. */
@Component({
  selector: 'app-list',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '{{ orders.status() }}:{{ orders.data()?.[0]?.total ?? "-" }}',
})
class LiveList {
  readonly orders = injectQuery<Order[]>(useOrderList, undefined, { live: true });
}

/** The same list, with `live` behind a signal the test moves. */
@Component({
  selector: 'app-toggle',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '{{ orders.status() }}:{{ orders.data()?.[0]?.total ?? "-" }}',
})
class ToggleList {
  readonly on = signal(false);
  // A getter, so the binding follows the signal rather than reading it once --
  // which is what makes the toggle test a statement about this adapter rather
  // than about re-creating the component.
  readonly orders = injectQuery<Order[]>(useOrderList, undefined, { live: () => this.on() });
}

/** And the same list with no mention of `live` at all. */
@Component({
  selector: 'app-plain',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '{{ orders.status() }}:{{ orders.data()?.[0]?.total ?? "-" }}',
})
class PlainList {
  readonly orders = injectQuery<Order[]>(useOrderList);
}

describe('injectQuery({live})', () => {
  it('updates from a frame, with no request behind it', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    configure(fx);

    const fixture = render(LiveList);

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('success:99');
    expect(fx.transport.calls).toHaveLength(1);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });
    fixture.detectChanges();

    // The whole claim of the design: the value moved, and not one request was
    // spent on it. `order.updated` is a `patch`, so it invalidates nothing.
    expect(fixture.nativeElement.textContent).toBe('success:100');
    expect(fx.transport.calls).toHaveLength(1);
  });

  it('is one subscription for two components on the same live query', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    @Component({
      selector: 'app-shell',
      imports: [LiveList],
      template: '<app-list /><app-list />',
    })
    class Shell {}

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    // One socket, and one subscription on it. A connection count that grows
    // with the component tree is precisely what the ref counting exists to
    // prevent, and a binding that subscribed per component rather than per
    // query would defeat it silently.
    expect(fx.opened).toHaveLength(1);
    expect(fx.manager.size).toBe(1);
    expect(fx.manager.connected('/ws/orders')).toBe(true);
  });

  it('is one channel for two different live queries on the same entity', async () => {
    const fx = liveHarness((request) =>
      request.meta.path === '/orders' ? [{ id: 7, total: 99 }] : { id: 7, total: 99 },
    );

    @Component({
      selector: 'app-detail',
      changeDetection: ChangeDetectionStrategy.OnPush,
      template: '{{ order.data()?.total ?? "-" }}',
    })
    class Detail {
      readonly order = injectQuery<Order>(useOrderGet, { path: { id: 7 } }, { live: true });
    }

    @Component({
      selector: 'app-shell',
      imports: [LiveList, Detail],
      template: '<app-list /><app-detail />',
    })
    class Shell {}

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);

    // Two distinct queries, two cache keys, two requests -- and one socket,
    // because `Order` is pushed on one channel and the manager multiplexes.
    expect(fx.transport.calls).toHaveLength(2);
    expect(fx.opened).toHaveLength(1);
    expect(fx.manager.size).toBe(1);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toBe('success:100100');
  });

  it('releases the socket when the last consumer is destroyed, and not before', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    @Component({
      selector: 'app-shell',
      imports: [LiveList],
      template: '@if (show() > 0) { <app-list /> } @if (show() > 1) { <app-list /> }',
    })
    class Shell {
      readonly show = signal(2);
    }

    configure(fx);

    const fixture = render(Shell);

    await settle(fixture);
    expect(fx.live()).toBe(1);

    fixture.componentInstance.show.set(1);
    await settle(fixture);
    settleCloses(fx);

    // The survivor still wants the channel.
    expect(fx.live()).toBe(1);

    fixture.componentInstance.show.set(0);
    await settle(fixture);
    settleCloses(fx);

    // The last one: released by that component's own `DestroyRef`.
    expect(fx.live()).toBe(0);
    expect(fx.manager.size).toBe(0);
  });

  it('releases when a bare injector is destroyed, with no component anywhere', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));

    injectQuery<Order[]>(useOrderList, undefined, { injector, live: true });

    await settle();
    expect(fx.live()).toBe(1);

    // `DestroyRef`, not `ngOnDestroy`: the lifetime is the injection context's,
    // and there is no component here to have one.
    injector.destroy();
    settleCloses(fx);

    expect(fx.live()).toBe(0);
  });

  it('releases the socket from the explicit destroy()', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));
    const live = signal(true);
    let seen!: InjectQueryResult<Order[]>;

    injector.runInContext(() => {
      seen = injectQuery<Order[]>(useOrderList, undefined, { live: () => live() });
    });

    await settle();
    expect(fx.live()).toBe(1);

    // "Release the subscription now" has to mean every half of it. A
    // `destroy()` that dropped the query and left the socket open would leave
    // a connection applying frames into the store on behalf of a binding the
    // caller has said it is finished with -- and leaving the `live` effect
    // running would re-acquire that socket on its next tick.
    seen.destroy();
    settleCloses(fx);

    expect(fx.live()).toBe(0);

    // The effect is stopped too, so moving the signal resurrects nothing.
    live.set(false);
    await settle();
    live.set(true);
    await settle();
    settleCloses(fx);

    expect(fx.live()).toBe(0);

    injector.destroy();
  });

  it('subscribes and unsubscribes as `live` toggles, without refetching', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    configure(fx);

    const fixture = render(ToggleList);

    await settle(fixture);

    expect(fx.opened).toHaveLength(0);
    expect(fx.transport.calls).toHaveLength(1);

    // Off -> on. A socket appears; the query is untouched.
    fixture.componentInstance.on.set(true);
    await settle(fixture);

    expect(fx.live()).toBe(1);
    expect(fx.transport.calls).toHaveLength(1);
    expect(fixture.nativeElement.textContent).toBe('success:99');

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toBe('success:100');

    // On -> off. The socket goes; the query keeps the value it has, and still
    // no second request -- `live` is not a hidden refetch trigger in either
    // direction.
    fixture.componentInstance.on.set(false);
    await settle(fixture);
    settleCloses(fx);

    expect(fx.live()).toBe(0);
    expect(fx.transport.calls).toHaveLength(1);
    expect(fixture.nativeElement.textContent).toBe('success:100');

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 250 } });
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toBe('success:100');
  });

  it('opts a query out entirely when `live` is not asked for', async () => {
    const fx = liveHarness(() => [{ id: 7, total: 99 }]);

    configure(fx);

    const fixture = render(PlainList);

    await settle(fixture);

    // The opt-in is real at the only level it can be: nothing subscribed, so
    // no socket was ever opened and there is no frame to arrive. The absence
    // of the word is the absence of the connection.
    expect(fx.opened).toHaveLength(0);
    expect(fx.manager.size).toBe(0);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 100 } });
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toBe('success:99');
    expect(fx.transport.calls).toHaveLength(1);
  });

  it('re-subscribes for the new principal rather than going deaf', async () => {
    let total = 99;
    const fx = liveHarness(() => [{ id: 7, total }]);

    configure(fx);

    const fixture = render(LiveList);

    await settle(fixture);

    const first = fx.opened[0];

    total = 5;
    fx.cache.setPrincipal('user-b');
    await settle(fixture);

    // The previous principal's socket is gone -- one that outlived the
    // identity change would push the previous session's entities into the new
    // one's store -- and a replacement was opened without the component doing
    // anything at all.
    expect(first?.closed).toBe(true);
    expect(fx.opened).toHaveLength(2);
    expect(fx.live()).toBe(1);

    await emit(fx, { type: 'order.updated', payload: { id: 7, total: 6 } });
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toBe('success:6');
  });
});
