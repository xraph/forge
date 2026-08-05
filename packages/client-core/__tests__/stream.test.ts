import { describe, expect, it } from 'vitest';

import { manualScheduler } from '../src/invalidate';
import { SubscriptionManager } from '../src/stream';
import { manualClock } from '../src/transport';
import { fakeSockets } from './harness';

/**
 * Nothing in this file touches a socket, a timer or the wall clock. The
 * transport is `fakeSockets`, the delay is `manualClock`, and the deferred
 * close is `manualScheduler` -- so every assertion below is about an ordering
 * the test chose rather than one it hoped for.
 */

function manager(options: Partial<Parameters<typeof build>[0]> = {}) {
  return build({ ...options });
}

function build(options: {
  principal?: () => unknown;
  endpointOf?: (channel: string) => string;
  attempts?: number;
}) {
  const sockets = fakeSockets();
  const clock = manualClock();
  const release = manualScheduler();
  const errors: { error: unknown; context: string }[] = [];
  const reconnects: { endpoint: string; channels: readonly string[] }[] = [];

  const subscriptions = new SubscriptionManager({
    connect: sockets.connect,
    sleep: clock.sleep,
    // No jitter: the delay a test asserts on should be the delay the policy
    // computes, not a sample from it.
    random: () => 0,
    backoff: { baseDelay: 1000, maxDelay: 8000, attempts: options.attempts ?? 10 },
    release: release.schedule,
    onError: (error, context) => errors.push({ error, context }),
    ...(options.principal === undefined ? {} : { principal: options.principal }),
    ...(options.endpointOf === undefined ? {} : { endpointOf: options.endpointOf }),
  });

  subscriptions.onReconnect = (endpoint, channels) => reconnects.push({ endpoint, channels });

  return { subscriptions, sockets, clock, release, errors, reconnects };
}

describe('ref counting', () => {
  it('shares one socket across subscribers and closes on the last release', () => {
    const { subscriptions, sockets, release } = manager();
    const seen: unknown[] = [];

    const first = subscriptions.subscribe('/ws/orders', (message) => seen.push(['a', message]));
    const second = subscriptions.subscribe('/ws/orders', (message) => seen.push(['b', message]));

    expect(sockets.opened).toHaveLength(1);
    expect(subscriptions.size).toBe(1);

    sockets.last().deliver({ type: 'order.created' });
    expect(seen).toHaveLength(2);

    first();
    release.flush();

    // One subscriber left: the socket is still open and still delivering.
    expect(sockets.last().closed).toBe(false);
    sockets.last().deliver({ type: 'order.updated' });
    expect(seen).toHaveLength(3);

    second();
    release.flush();

    expect(sockets.last().closed).toBe(true);
    expect(subscriptions.size).toBe(0);
    expect(sockets.opened).toHaveLength(1);
  });

  it('releases once however many times the release is called', () => {
    const { subscriptions, sockets, release } = manager();

    const one = subscriptions.subscribe('/ws/orders', () => undefined);
    const two = subscriptions.subscribe('/ws/orders', () => undefined);

    one();
    one();
    one();
    release.flush();

    expect(sockets.last().closed).toBe(false);

    two();
    release.flush();
    expect(sockets.last().closed).toBe(true);
  });

  it('multiplexes channels that share an endpoint, and counts them together', () => {
    const { subscriptions, sockets, release } = manager({ endpointOf: () => '/ws' });
    const orders: unknown[] = [];
    const shipments: unknown[] = [];

    const a = subscriptions.subscribe('/ws/orders', (_message, channel) => orders.push(channel));
    const b = subscriptions.subscribe('/ws/shipments', (_message, channel) =>
      shipments.push(channel),
    );

    expect(sockets.opened).toHaveLength(1);
    expect(sockets.last().context.channels).toEqual(['/ws/orders']);

    // One frame off the shared socket reaches both channels' subscribers, each
    // told which channel it was listening on -- the binder resolves the rest.
    sockets.last().deliver({ type: 'order.created' });
    expect(orders).toEqual(['/ws/orders']);
    expect(shipments).toEqual(['/ws/shipments']);

    a();
    release.flush();
    expect(sockets.last().closed).toBe(false);

    b();
    release.flush();
    expect(sockets.last().closed).toBe(true);
  });
});

describe('StrictMode', () => {
  it('leaves a live subscription after mount, unmount, mount', () => {
    const { subscriptions, sockets, release } = manager();
    const seen: unknown[] = [];

    // React development double-invokes an effect: subscribe, clean up,
    // subscribe again, with no chance for anything else to run in between.
    const first = subscriptions.subscribe('/ws/orders', (message) => seen.push(message));
    first();
    const second = subscriptions.subscribe('/ws/orders', (message) => seen.push(message));

    // The deferred close now runs, and must find the socket claimed again.
    release.flush();

    expect(sockets.opened).toHaveLength(1);
    expect(sockets.last().closed).toBe(false);

    sockets.last().deliver({ type: 'order.created' });
    expect(seen).toHaveLength(1);

    second();
    release.flush();
    expect(sockets.last().closed).toBe(true);
  });

  it('does not open a second socket for the phantom remount', () => {
    const { subscriptions, sockets, release } = manager();

    for (let cycle = 0; cycle < 5; cycle++) {
      const release1 = subscriptions.subscribe('/ws/orders', () => undefined);
      release1();
      const release2 = subscriptions.subscribe('/ws/orders', () => undefined);
      release.flush();
      release2();
      release.flush();
    }

    // Five mounts, five unmounts, and five sockets -- not ten. A naive
    // implementation opens one per phantom remount, which only shows up in
    // development and gets reported as "works in prod".
    expect(sockets.opened).toHaveLength(5);
  });

  it('defers the close of several sockets through one scheduled callback', () => {
    // `manualScheduler` holds exactly one queued flush, so a manager that
    // scheduled per socket would lose all but the last and leak the rest.
    const { subscriptions, sockets, release } = manager();

    const a = subscriptions.subscribe('/ws/orders', () => undefined);
    const b = subscriptions.subscribe('/ws/shipments', () => undefined);

    a();
    b();
    release.flush();

    expect(sockets.live()).toBe(0);
    expect(subscriptions.size).toBe(0);
  });
});

describe('reconnect', () => {
  it('backs off exponentially on the injected clock, and reports the gap once back', async () => {
    const { subscriptions, sockets, clock, reconnects } = manager();

    subscriptions.subscribe('/ws/orders', () => undefined);
    expect(sockets.opened).toHaveLength(1);

    sockets.last().drop();
    expect(subscriptions.connected('/ws/orders')).toBe(false);

    // Nothing yet: the first attempt is 1000ms out (half fixed, half jitter,
    // and the jitter source is pinned at zero).
    await clock.advance(499);
    expect(sockets.opened).toHaveLength(1);

    await clock.advance(1);
    expect(sockets.opened).toHaveLength(2);
    expect(subscriptions.connected('/ws/orders')).toBe(true);
    expect(reconnects).toEqual([{ endpoint: '/ws/orders', channels: ['/ws/orders'] }]);

    // The new socket delivers to the original subscriber: reconnecting is not a
    // resubscribe, and the handler was never re-registered.
    let seen = 0;
    subscriptions.subscribe('/ws/orders', () => {
      seen++;
    });
    sockets.last().deliver({ type: 'order.created' });
    expect(seen).toBe(1);
  });

  it('escalates the delay across consecutive failures and caps it', async () => {
    const { subscriptions, sockets, clock } = manager();

    subscriptions.subscribe('/ws/orders', () => undefined);

    // Each reopened socket drops before delivering anything, so nothing ever
    // proves the endpoint healthy and the backoff keeps climbing.
    const delays = [500, 1000, 2000, 4000, 4000];

    for (const [index, delay] of delays.entries()) {
      sockets.last().drop();

      await clock.advance(delay - 1);
      expect(sockets.opened).toHaveLength(index + 1);

      await clock.advance(1);
      expect(sockets.opened).toHaveLength(index + 2);
    }
  });

  it('restarts the backoff once the endpoint proves itself', async () => {
    const { subscriptions, sockets, clock } = manager();

    subscriptions.subscribe('/ws/orders', () => undefined);

    sockets.last().drop();
    await clock.advance(500);
    expect(sockets.opened).toHaveLength(2);

    sockets.last().drop();
    await clock.advance(1000);
    expect(sockets.opened).toHaveLength(3);

    // A frame is proof of life, so the next outage starts from the first rung
    // rather than from the third.
    sockets.last().deliver({ type: 'order.created' });
    sockets.last().drop();

    await clock.advance(499);
    expect(sockets.opened).toHaveLength(3);
    await clock.advance(1);
    expect(sockets.opened).toHaveLength(4);
  });

  it('gives up after the configured attempts rather than retrying forever', async () => {
    const { subscriptions, sockets, clock, errors } = manager({ attempts: 3 });

    subscriptions.subscribe('/ws/orders', () => undefined);

    for (let attempt = 0; attempt < 3; attempt++) {
      sockets.last().drop();
      await clock.advance(60000);
    }

    expect(sockets.opened).toHaveLength(4);

    sockets.last().drop();
    await clock.advance(60000);

    expect(sockets.opened).toHaveLength(4);
    expect(errors.map((entry) => String((entry.error as Error).message))).toContainEqual(
      expect.stringContaining('gave up reconnecting'),
    );
  });

  it('does not reconnect a socket nobody is subscribed to any more', async () => {
    const { subscriptions, sockets, clock, release } = manager();

    const stop = subscriptions.subscribe('/ws/orders', () => undefined);

    sockets.last().drop();
    stop();
    release.flush();

    await clock.advance(60000);

    expect(sockets.opened).toHaveLength(1);
  });

  it('does not report a gap on the first connect', () => {
    const { subscriptions, reconnects } = manager();

    subscriptions.subscribe('/ws/orders', () => undefined);

    // There is nothing to recover: the query is loading right now.
    expect(reconnects).toEqual([]);
  });

  it('ignores a frame from a connection it has already replaced', async () => {
    const { subscriptions, sockets, clock } = manager();
    const seen: unknown[] = [];

    subscriptions.subscribe('/ws/orders', (message) => seen.push(message));

    const stale = sockets.last();
    stale.drop();
    await clock.advance(1000);

    expect(sockets.opened).toHaveLength(2);

    stale.deliver({ type: 'order.created', payload: { id: 1 } });
    expect(seen).toEqual([]);

    sockets.last().deliver({ type: 'order.created', payload: { id: 2 } });
    expect(seen).toHaveLength(1);
  });
});

describe('principal', () => {
  it('never adopts a socket opened for a different identity', () => {
    let principal = 'user-a';
    const { subscriptions, sockets } = manager({ principal: () => principal });

    const first = subscriptions.subscribe('/ws/orders', () => undefined);
    expect(sockets.opened).toHaveLength(1);
    expect(sockets.last().context.principal).toBe('user-a');

    principal = 'user-b';
    subscriptions.subscribe('/ws/orders', () => undefined);

    expect(sockets.opened).toHaveLength(2);
    expect(sockets.opened[0]?.closed).toBe(true);
    expect(sockets.last().context.principal).toBe('user-b');

    first();
  });

  it('repartitions open sockets onto the new identity, keeping subscribers', () => {
    let principal: unknown = 'user-a';
    const { subscriptions, sockets, reconnects } = manager({ principal: () => principal });
    const seen: unknown[] = [];

    subscriptions.subscribe('/ws/orders', (message) => seen.push(message));

    const before = sockets.last();

    principal = 'user-b';
    subscriptions.repartition();

    expect(before.closed).toBe(true);
    expect(sockets.opened).toHaveLength(2);
    expect(sockets.last().context.principal).toBe('user-b');

    // The gap is reported, because the new session missed everything the old
    // socket would have carried and its store was just emptied.
    expect(reconnects).toEqual([{ endpoint: '/ws/orders', channels: ['/ws/orders'] }]);

    // The old socket is inert.
    before.deliver({ type: 'order.created' });
    expect(seen).toEqual([]);

    // The subscriber survived the swap.
    sockets.last().deliver({ type: 'order.created' });
    expect(seen).toHaveLength(1);
  });

  it('leaves sockets alone when the identity did not move', () => {
    const { subscriptions, sockets, reconnects } = manager({ principal: () => 'user-a' });

    subscriptions.subscribe('/ws/orders', () => undefined);
    subscriptions.repartition();

    expect(sockets.opened).toHaveLength(1);
    expect(sockets.last().closed).toBe(false);
    expect(reconnects).toEqual([]);
  });
});

describe('failures', () => {
  it('reports a transport error without closing anything', () => {
    const { subscriptions, sockets, errors } = manager();

    subscriptions.subscribe('/ws/orders', () => undefined);
    sockets.last().fail(new Error('frame too large'));

    expect(sockets.last().closed).toBe(false);
    expect(errors[0]?.context).toBe('stream /ws/orders');
  });

  it('does not let one subscriber’s throw cost the others their frame', () => {
    const { subscriptions, sockets, errors } = manager();
    const seen: unknown[] = [];

    subscriptions.subscribe('/ws/orders', () => {
      throw new Error('render exploded');
    });
    subscriptions.subscribe('/ws/orders', (message) => seen.push(message));

    sockets.last().deliver({ type: 'order.created' });

    expect(seen).toHaveLength(1);
    expect(errors[0]?.context).toBe('stream handler /ws/orders');
  });

  it('retries a connect factory that throws rather than abandoning the channel', async () => {
    const sockets = fakeSockets();
    const clock = manualClock();
    const errors: unknown[] = [];
    let fail = true;

    const subscriptions = new SubscriptionManager({
      connect: (context) => {
        if (fail) throw new Error('no token yet');

        return sockets.connect(context);
      },
      sleep: clock.sleep,
      random: () => 0,
      backoff: { baseDelay: 1000 },
      onError: (error) => errors.push(error),
    });

    subscriptions.subscribe('/ws/orders', () => undefined);

    expect(sockets.opened).toHaveLength(0);
    expect(errors).toHaveLength(1);

    fail = false;
    await clock.advance(1000);

    expect(sockets.opened).toHaveLength(1);
  });
});
