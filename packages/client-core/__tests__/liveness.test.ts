import { describe, expect, it } from 'vitest';

import { manualScheduler } from '../src/invalidate';
import { forgeKeepalive, SubscriptionManager } from '../src/stream';
import { manualClock } from '../src/transport';
import { fakeSockets } from './harness';

/**
 * Keeping a socket alive, and getting it back when it was not.
 *
 * Two failures the manager could not previously survive, both of them silent.
 *
 * The first is a contract mismatch with the streaming extension. Its heartbeat
 * judges liveness by inbound traffic only -- `UpdateActivity` is called in the
 * read loop and nowhere else -- and the ping it sends is an application message
 * rather than a WebSocket control frame. A browser answers control pings for
 * free and cannot send one at all, so a client that only subscribes and listens
 * looks dead to the server and is closed on the interval. See
 * `extensions/streaming/heartbeat_test.go`, which pins that behaviour from the
 * other side.
 *
 * The second is the reconnect budget running out. Ten failed attempts and the
 * socket was abandoned for the life of the page, with nothing watching for the
 * network coming back to say otherwise.
 */

interface EventTargetLike {
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
}

/** An event target the test dispatches on, so no browser is involved. */
function fakeTarget(): EventTargetLike & { emit(type: string): void; readonly count: number } {
  const listeners = new Map<string, Set<() => void>>();

  return {
    addEventListener(type, listener) {
      const set = listeners.get(type) ?? new Set();
      set.add(listener);
      listeners.set(type, set);
    },
    removeEventListener(type, listener) {
      listeners.get(type)?.delete(listener);
    },
    emit(type) {
      for (const listener of [...(listeners.get(type) ?? [])]) listener();
    },
    get count() {
      let total = 0;
      for (const set of listeners.values()) total += set.size;

      return total;
    },
  };
}

function build(
  options: {
    attempts?: number;
    keepalive?: ((message: unknown) => unknown) | false;
    revive?: EventTargetLike | false;
  } = {},
) {
  const sockets = fakeSockets();
  const clock = manualClock();
  const release = manualScheduler();
  const errors: { error: unknown; context: string }[] = [];

  const subscriptions = new SubscriptionManager({
    connect: sockets.connect,
    sleep: clock.sleep,
    random: () => 0,
    backoff: { baseDelay: 1000, maxDelay: 8000, attempts: options.attempts ?? 10 },
    release: release.schedule,
    onError: (error, context) => errors.push({ error, context }),
    ...(options.keepalive === undefined ? {} : { keepalive: options.keepalive }),
    ...(options.revive === undefined ? {} : { revive: options.revive }),
  });

  return { subscriptions, sockets, clock, release, errors };
}

describe('answering the server keepalive', () => {
  it('sends a pong when the server pings', () => {
    const { subscriptions, sockets } = build();

    subscriptions.subscribe('/ws/orders', () => {});
    sockets.last().deliver({ type: 'system', event: 'ping', id: 'p1' });

    expect(sockets.last().sent).toEqual([{ type: 'system', event: 'pong' }]);
  });

  it('leaves an ordinary frame alone', () => {
    const { subscriptions, sockets } = build();

    subscriptions.subscribe('/ws/orders', () => {});
    sockets.last().deliver({ type: 'order.created', id: 'o1' });

    expect(sockets.last().sent).toEqual([]);
  });

  it('still delivers the keepalive to subscribers', () => {
    const { subscriptions, sockets } = build();
    const seen: unknown[] = [];

    subscriptions.subscribe('/ws/orders', (message) => seen.push(message));
    sockets.last().deliver({ type: 'system', event: 'ping' });

    // Answering is not swallowing. A decoder that wants to drop it still can,
    // and one that reports on transport frames still sees it.
    expect(seen).toEqual([{ type: 'system', event: 'ping' }]);
  });

  it('does not throw when the transport cannot send', () => {
    const sockets = fakeSockets();
    const clock = manualClock();

    const subscriptions = new SubscriptionManager({
      connect: (context) => {
        const connection = sockets.connect(context);

        // A receive-only transport, which is every transport that existed
        // before `send` was optional on the interface.
        return {
          onMessage: connection.onMessage.bind(connection),
          onClose: connection.onClose.bind(connection),
          close: connection.close.bind(connection),
        };
      },
      sleep: clock.sleep,
    });

    subscriptions.subscribe('/ws/orders', () => {});

    expect(() => sockets.last().deliver({ type: 'system', event: 'ping' })).not.toThrow();
  });

  it('can be replaced with an application policy', () => {
    const { subscriptions, sockets } = build({
      keepalive: (message) =>
        (message as { kind?: string }).kind === 'hb' ? { kind: 'hb-ack' } : undefined,
    });

    subscriptions.subscribe('/ws/orders', () => {});
    sockets.last().deliver({ kind: 'hb' });
    sockets.last().deliver({ type: 'system', event: 'ping' });

    // The default is replaced, not added to: the Forge ping is now none of the
    // manager's business.
    expect(sockets.last().sent).toEqual([{ kind: 'hb-ack' }]);
  });

  it('can be turned off entirely', () => {
    const { subscriptions, sockets } = build({ keepalive: false });

    subscriptions.subscribe('/ws/orders', () => {});
    sockets.last().deliver({ type: 'system', event: 'ping' });

    expect(sockets.last().sent).toEqual([]);
  });

  it('exports the default policy for an application composing its own', () => {
    expect(forgeKeepalive({ type: 'system', event: 'ping' })).toEqual({
      type: 'system',
      event: 'pong',
    });
    expect(forgeKeepalive({ type: 'order.created' })).toBeUndefined();
    expect(forgeKeepalive(null)).toBeUndefined();
    expect(forgeKeepalive('ping')).toBeUndefined();
  });
});

describe('recovering a socket that gave up', () => {
  it('reopens on retry after the reconnect budget is exhausted', async () => {
    const { subscriptions, sockets, clock, errors } = build({ attempts: 2 });

    subscriptions.subscribe('/ws/orders', () => {});
    expect(sockets.opened).toHaveLength(1);

    // Two failed attempts, which is the whole budget.
    sockets.last().drop();
    await clock.advance(10_000);
    sockets.last().drop();
    await clock.advance(10_000);
    sockets.last().drop();
    await clock.advance(10_000);

    expect(errors.some((entry) => String(entry.error).includes('gave up'))).toBe(true);

    const abandoned = sockets.opened.length;

    subscriptions.retry();
    await clock.advance(10_000);

    expect(sockets.opened.length).toBeGreaterThan(abandoned);
    expect(sockets.last().closed).toBe(false);
  });

  it('resets the attempt budget so a later outage gets a full run', async () => {
    const { subscriptions, sockets, clock, errors } = build({ attempts: 2 });

    subscriptions.subscribe('/ws/orders', () => {});

    sockets.last().drop();
    await clock.advance(10_000);
    sockets.last().drop();
    await clock.advance(10_000);
    sockets.last().drop();
    await clock.advance(10_000);

    const first = errors.filter((entry) => String(entry.error).includes('gave up')).length;

    subscriptions.retry();
    await clock.advance(10_000);

    // A retry that inherited the exhausted counter would give up on the very
    // first drop rather than after another full budget.
    sockets.last().drop();
    await clock.advance(10_000);

    expect(errors.filter((entry) => String(entry.error).includes('gave up'))).toHaveLength(first);
  });

  it('does nothing to a healthy socket', async () => {
    const { subscriptions, sockets, clock } = build();

    subscriptions.subscribe('/ws/orders', () => {});
    const before = sockets.opened.length;

    subscriptions.retry();
    await clock.advance(10_000);

    expect(sockets.opened).toHaveLength(before);
    expect(sockets.last().closed).toBe(false);
  });

  it('retries when the revive target says the network is back', async () => {
    const target = fakeTarget();
    const { subscriptions, sockets, clock } = build({ attempts: 1, revive: target });

    subscriptions.subscribe('/ws/orders', () => {});

    sockets.last().drop();
    await clock.advance(10_000);
    sockets.last().drop();
    await clock.advance(10_000);

    const abandoned = sockets.opened.length;

    target.emit('online');
    await clock.advance(10_000);

    expect(sockets.opened.length).toBeGreaterThan(abandoned);
  });

  it('unhooks its listeners on closeAll', () => {
    const target = fakeTarget();
    const { subscriptions } = build({ revive: target });

    expect(target.count).toBeGreaterThan(0);

    subscriptions.closeAll();

    expect(target.count).toBe(0);
  });

  it('registers nothing when revive is off', () => {
    const target = fakeTarget();

    build({ revive: false });

    expect(target.count).toBe(0);
  });
});
