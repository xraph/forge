import { describe, expect, it, vi } from 'vitest';
import type { Transport, TransportRequest } from '@forge-go/client-core';
import { Revalidation, TransportControls } from '../src/control';
import { ops } from './harness';

/** The inner transport, counting what actually reached it. */
function inner(): Transport & { calls: TransportRequest[] } {
  const calls: TransportRequest[] = [];

  return {
    calls,
    execute(request) {
      calls.push(request);

      return Promise.resolve({ ok: true });
    },
  };
}

const request = { meta: ops.orderList, args: {} };

describe('the network conditions', () => {
  it('passes everything through when nothing is set', async () => {
    const controls = new TransportControls();
    const base = inner();

    await expect(controls.wrap(base).execute(request)).resolves.toEqual({ ok: true });
    expect(base.calls).toHaveLength(1);
  });

  /**
   * Offline has to fail *before* the inner transport, not after. A control
   * that let the request out and threw the answer away would still hit the
   * server, which is the one thing "offline" is supposed to prove it does not
   * need to.
   */
  it('fails offline without letting the request reach the wire', async () => {
    const controls = new TransportControls();
    const base = inner();

    controls.mode = 'offline';

    await expect(controls.wrap(base).execute(request)).rejects.toThrow(/offline/i);
    expect(base.calls).toHaveLength(0);
  });

  it('waits the injected latency before the request goes out', async () => {
    const slept: number[] = [];
    const controls = new TransportControls({
      sleep: (ms) => {
        slept.push(ms);

        return Promise.resolve();
      },
    });
    const base = inner();

    controls.latency = 750;

    await controls.wrap(base).execute(request);

    expect(slept).toEqual([750]);
    expect(base.calls).toHaveLength(1);
  });

  it('adds a delay of its own in slow mode, on top of the injected one', async () => {
    const slept: number[] = [];
    const controls = new TransportControls({
      sleep: (ms) => {
        slept.push(ms);

        return Promise.resolve();
      },
    });

    controls.mode = 'slow';
    controls.latency = 100;

    await controls.wrap(inner()).execute(request);

    expect(slept[0]).toBeGreaterThan(100);
  });

  /**
   * Armed once and disarmed on use. A toggle that stayed on would make every
   * subsequent request fail, which is indistinguishable from the bug you were
   * trying to reproduce.
   */
  it('fails exactly one request when fail next is armed', async () => {
    const controls = new TransportControls();
    const base = inner();
    const transport = controls.wrap(base);

    controls.failNext();

    await expect(transport.execute(request)).rejects.toThrow();
    expect(base.calls).toHaveLength(0);

    await expect(transport.execute(request)).resolves.toEqual({ ok: true });
    expect(base.calls).toHaveLength(1);
    expect(controls.armed).toBe(false);
  });

  it('fails with a status the retry policy can read', async () => {
    const controls = new TransportControls();

    controls.failNext(503);

    await controls
      .wrap(inner())
      .execute(request)
      .catch((error: { statusCode?: number }) => {
        expect(error.statusCode).toBe(503);
      });

    expect.assertions(1);
  });
});

describe('the revalidation toggles', () => {
  /**
   * `revalidateOnFocus` and friends are wired once at setup and then invisible,
   * and half of "why did this refetch" is one of them. The panel cannot toggle
   * a subscription it was never handed, so the application registers how to
   * start each one and this holds the stop.
   */
  it('starts switched off until something is registered', () => {
    const revalidation = new Revalidation({});

    expect(revalidation.enabled('focus')).toBe(false);
    expect(revalidation.registered('focus')).toBe(false);
  });

  it('starts a source on and stops it when toggled off', () => {
    const stop = vi.fn();
    const start = vi.fn(() => stop);
    const revalidation = new Revalidation({ focus: start });

    // Registering does not start it; the application decides the initial state.
    expect(start).not.toHaveBeenCalled();

    revalidation.toggle('focus');

    expect(start).toHaveBeenCalledTimes(1);
    expect(revalidation.enabled('focus')).toBe(true);

    revalidation.toggle('focus');

    expect(stop).toHaveBeenCalledTimes(1);
    expect(revalidation.enabled('focus')).toBe(false);
  });

  it('stops everything it started when disposed', () => {
    const stopFocus = vi.fn();
    const stopPoll = vi.fn();
    const revalidation = new Revalidation({
      focus: () => stopFocus,
      poll: () => stopPoll,
    });

    revalidation.toggle('focus');
    revalidation.toggle('poll');
    revalidation.dispose();

    expect(stopFocus).toHaveBeenCalledTimes(1);
    expect(stopPoll).toHaveBeenCalledTimes(1);
    expect(revalidation.enabled('focus')).toBe(false);
  });

  it('ignores a toggle for a source the application never registered', () => {
    const revalidation = new Revalidation({});

    expect(() => {
      revalidation.toggle('reconnect');
    }).not.toThrow();
    expect(revalidation.enabled('reconnect')).toBe(false);
  });
});
