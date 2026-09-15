import { describe, expect, it } from 'vitest';

import { manualScheduler } from '../src/invalidate';
import { socketSnapshot, SubscriptionManager } from '../src/stream';
import { manualClock } from '../src/transport';
import { fakeSockets } from './harness';

function build(attempts = 10) {
  const sockets = fakeSockets();
  const clock = manualClock();
  const release = manualScheduler();
  const errors: { error: unknown; context: string }[] = [];
  const subscriptions = new SubscriptionManager({
    connect: sockets.connect,
    sleep: clock.sleep,
    random: () => 0,
    backoff: { baseDelay: 1000, maxDelay: 8000, attempts },
    release: release.schedule,
    onError: (error, context) => errors.push({ error, context }),
  });

  return { subscriptions, sockets, clock, release, errors };
}

describe('a subscription that speaks first', () => {
  // Guards the contract live query depends on: the server learns what the
  // client wants from a frame, and it must arrive once the socket is open,
  // not while it is still connecting where a real WebSocket drops it.
  it('sends hello after the transport opens, not before', () => {
    const { subscriptions, sockets } = build();

    subscriptions.subscribe('/ws/live', () => {}, { hello: { action: 'subscribe', data: { id: 'q1' } } });

    expect(sockets.last().sent).toEqual([]);
    sockets.last().open();
    expect(sockets.last().sent).toEqual([{ action: 'subscribe', data: { id: 'q1' } }]);
  });

  it('sends hello immediately when the socket is already open', () => {
    const { subscriptions, sockets } = build();

    subscriptions.subscribe('/ws/live', () => {});
    sockets.last().open();
    subscriptions.subscribe('/ws/live', () => {}, { hello: () => ({ action: 'subscribe', data: { id: 'q2' } }) });

    expect(sockets.last().sent).toEqual([{ action: 'subscribe', data: { id: 'q2' } }]);
  });

  // Guards recovery: a reconnected socket is a fresh server-side session, so
  // every open subscription must reintroduce itself, in the order it was made.
  it('resends every hello after a reconnect, in subscription order', async () => {
    const { subscriptions, sockets, clock } = build();

    subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'first' } });
    subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'second' } });
    sockets.last().open();
    sockets.last().drop('gone');
    await clock.advance(1000);
    sockets.last().open();

    expect(sockets.opened.length).toBe(2);
    expect(sockets.last().sent).toEqual([{ id: 'first' }, { id: 'second' }]);
  });

  it('sends goodbye on release only while connected', () => {
    const { subscriptions, sockets } = build();

    const first = subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'a' }, goodbye: { bye: 'a' } });
    const second = subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'b' }, goodbye: { bye: 'b' } });
    sockets.last().open();
    first();
    expect(sockets.last().sent).toEqual([{ id: 'a' }, { id: 'b' }, { bye: 'a' }]);

    sockets.last().drop('gone');
    second();
    expect(sockets.last().sent).toEqual([{ id: 'a' }, { id: 'b' }, { bye: 'a' }]);
  });

  // Guards the failure mode being fixed: a frame nobody can send must not
  // vanish quietly.
  it('refuses hello on a transport that cannot send', () => {
    const sockets = fakeSockets();
    const subscriptions = new SubscriptionManager({
      connect: (context) => {
        const connection = sockets.connect(context);
        return {
          onMessage: connection.onMessage.bind(connection),
          onClose: connection.onClose.bind(connection),
          close: connection.close.bind(connection),
        };
      },
    });

    expect(() => subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'x' } })).toThrow(/cannot send/);
  });

  it('never gives up when attempts is Infinity', async () => {
    const { subscriptions, sockets, clock, errors } = build(Number.POSITIVE_INFINITY);

    subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'q' } });
    for (let i = 0; i < 40; i++) {
      sockets.last().drop('gone');
      await clock.advance(8000);
    }

    expect(sockets.opened.length).toBe(41);
    expect(errors.filter((e) => String(e.error).includes('gave up'))).toEqual([]);
  });

  it('reports which channels carry a hello in the snapshot', () => {
    const { subscriptions } = build();

    subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'q' } });
    subscriptions.subscribe('/ws/orders', () => {});

    const [socket] = socketSnapshot(subscriptions).filter((s) => s.endpoint === '/ws/live');
    expect(socket?.channels).toEqual([{ channel: '/ws/live', handlers: 1, hello: true }]);
  });

  // Guards against the greet-then-say double send: a transport with no
  // onOpen is ready the instant `connect` returns, so `open()` itself greets
  // the brand-new subscriber synchronously. `subscribe` must not send the
  // same hello a second time on top of that.
  it('sends hello exactly once on a transport with no onOpen', () => {
    const sockets = fakeSockets();
    const subscriptions = new SubscriptionManager({
      connect: (context) => {
        const connection = sockets.connect(context);
        return {
          onMessage: connection.onMessage.bind(connection),
          onClose: connection.onClose.bind(connection),
          close: connection.close.bind(connection),
          send: (message: unknown) => connection.send?.(message),
        };
      },
    });

    subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'once' } });

    expect(sockets.last().sent).toEqual([{ id: 'once' }]);
  });

  // Guards the ordering StreamBinder's recovery depends on: a consumer that
  // reacts to onReconnect by refetching must see the reintroduction go out
  // first, never learn about the reconnect before the server does.
  it('reports onReconnect only after the reconnected socket has been greeted', async () => {
    const { subscriptions, sockets, clock } = build();
    let sentAtReconnect: readonly unknown[] | undefined;
    let reconnectCalls = 0;

    subscriptions.onReconnect = () => {
      reconnectCalls++;
      sentAtReconnect = [...sockets.last().sent];
    };

    subscriptions.subscribe('/ws/live', () => {}, { hello: { id: 'again' } });
    sockets.last().open();
    sockets.last().drop('gone');
    await clock.advance(1000);

    // The new connection exists but has not reported open yet: no reconnect
    // report and no hello, either.
    expect(reconnectCalls).toBe(0);

    sockets.last().open();

    expect(reconnectCalls).toBe(1);
    expect(sentAtReconnect).toEqual([{ id: 'again' }]);
  });

  // Guards the failure mode being fixed: a goodbye nobody can send must not
  // vanish quietly either, exactly like hello above.
  it('refuses goodbye on a transport that cannot send', () => {
    const sockets = fakeSockets();
    const subscriptions = new SubscriptionManager({
      connect: (context) => {
        const connection = sockets.connect(context);
        return {
          onMessage: connection.onMessage.bind(connection),
          onClose: connection.onClose.bind(connection),
          close: connection.close.bind(connection),
        };
      },
    });

    expect(() => subscriptions.subscribe('/ws/live', () => {}, { goodbye: { id: 'x' } })).toThrow(
      /cannot send/,
    );
  });
});
