import {
  ChangeDetectionStrategy,
  Component,
  EnvironmentInjector,
  createEnvironmentInjector,
  signal,
} from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { injectQuery } from '../src';
import { harness, useOrderList } from './harness';
import type { Harness, Order } from './harness';
import { configure, render, settle } from './harness-angular';

/** A transport whose every response takes `latency` ms of fake time. */
function slowHarness(latency: number): Harness {
  let total = 0;

  return harness(async () => {
    await new Promise((resolve) => setTimeout(resolve, latency));
    total += 1;

    return [{ id: 1, total }];
  });
}

@Component({
  selector: 'app-polled',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '{{ orders.data()?.[0]?.total ?? "-" }}',
})
class Polled {
  readonly interval = signal<number | false>(false);
  readonly orders = injectQuery<Order[]>(useOrderList, undefined, {
    refetchInterval: () => this.interval(),
  });
}

describe('injectQuery refetchInterval', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('polls on the interval, and stops when its injector is destroyed', async () => {
    const fx = slowHarness(0);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));

    injectQuery<Order[]>(useOrderList, undefined, { injector, refetchInterval: 1_000 });

    await vi.advanceTimersByTimeAsync(0);
    expect(fx.transport.calls).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(3_000);
    expect(fx.transport.calls).toHaveLength(4);

    injector.destroy();

    await vi.advanceTimersByTimeAsync(3_000);
    expect(fx.transport.calls).toHaveLength(4);
  });

  // A timer restarted by each response fires at interval + latency instead
  // (xraph/forge#100).
  it('holds its cadence on a slow endpoint', async () => {
    const fx = slowHarness(400);

    configure(fx);

    const injector = createEnvironmentInjector([], TestBed.inject(EnvironmentInjector));

    injectQuery<Order[]>(useOrderList, undefined, { injector, refetchInterval: 1_000 });

    await vi.advanceTimersByTimeAsync(3_000);
    expect(fx.transport.calls).toHaveLength(4);

    injector.destroy();
  });

  it('reads false as off, and follows a signal', async () => {
    const fx = slowHarness(0);

    configure(fx);

    const fixture = render(Polled);

    await settle(fixture);
    await vi.advanceTimersByTimeAsync(5_000);
    expect(fx.transport.calls).toHaveLength(1);

    fixture.componentInstance.interval.set(1_000);
    await settle(fixture);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(fx.transport.calls).toHaveLength(3);

    fixture.componentInstance.interval.set(false);
    await settle(fixture);
    await vi.advanceTimersByTimeAsync(5_000);
    expect(fx.transport.calls).toHaveLength(3);

    fixture.destroy();
  });
});
