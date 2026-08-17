import { ChangeDetectionStrategy, Component, provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { dehydrate } from '@forge-go/client-core';
import type { DehydratedState } from '@forge-go/client-core';
import { injectQuery, provideClient, provideHydration } from '../src';
import { harness, orderList, useOrderList } from './harness';
import type { Harness, Order } from './harness';
import { render, settle } from './harness-angular';

const ops = { orderList };

@Component({
  selector: 'app-orders',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '{{ orders.status() }}:{{ orders.data()?.[0]?.total ?? "-" }}',
})
class Orders {
  readonly orders = injectQuery<Order[]>(useOrderList);
}

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

function configureWith(fx: Harness, state: DehydratedState | undefined, stale = false): void {
  TestBed.configureTestingModule({
    providers: [
      provideZonelessChangeDetection(),
      provideClient(fx.cache),
      provideHydration(state, ops, stale ? { stale: true } : {}),
    ],
  });
}

describe('provideHydration', () => {
  it('paints the server value on the first render, without fetching', async () => {
    const state = await serverPayload();
    const fx = offline();

    configureWith(fx, state);

    // Already settled on the very first detectChanges. The initializer ran
    // when the environment injector was created, which is before this
    // component existed to read anything.
    const fixture = render(Orders);

    expect(fixture.nativeElement.textContent).toBe('success:99');

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('success:99');
    expect(fx.transport.countOf(orderList)).toBe(0);
  });

  it('fetches normally when there is no payload', async () => {
    const fx = harness(() => [{ id: 7, total: 99 }]);

    configureWith(fx, undefined);

    const fixture = render(Orders);

    expect(fixture.nativeElement.textContent).toBe('pending:-');

    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('success:99');
    expect(fx.transport.countOf(orderList)).toBe(1);
  });

  it('refetches on mount when the payload is hydrated stale', async () => {
    const state = await serverPayload();
    const fx = harness(() => [{ id: 7, total: 120 }]);

    configureWith(fx, state, true);

    const fixture = render(Orders);

    expect(fixture.nativeElement.textContent).toBe('success:99');

    fx.scheduler.flush();
    await settle(fixture);

    expect(fixture.nativeElement.textContent).toBe('success:120');
    expect(fx.transport.countOf(orderList)).toBe(1);
  });

  it('walks the payload once, however many injectors read it', async () => {
    const state = await serverPayload();
    const fx = offline();

    configureWith(fx, state);
    render(Orders);

    expect(fx.cache.store.getRecord('Order:7')?.version).toBe(1);
  });
});
