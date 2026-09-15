import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { binderSnapshot, StreamBinder } from '../src/live';
import { SubscriptionManager } from '../src/stream';
import type { StreamBinding } from '../src/stream';
import { manualClock } from '../src/transport';
import { fakeSockets, fakeTransport } from './harness';

const streams: readonly StreamBinding[] = [
  { kind: 'duplex', channel: '/api/v1/query/live/ws', send: 'SendMessage', receive: 'ReceiveMessage' },
  { channel: '/ws/orders', message: 'orderUpdated', entity: 'Order', intent: 'upsert', invalidates: [] },
];

function build() {
  const sockets = fakeSockets();
  const clock = manualClock();
  const cache = new QueryCache({ transport: fakeTransport(() => []), entities: { Order: { idField: 'id' } } });
  const manager = new SubscriptionManager({
    connect: sockets.connect,
    sleep: clock.sleep,
    random: () => 0,
    release: manualScheduler().schedule,
  });
  const binder = new StreamBinder({ cache, streams, manager, scheduler: (flush) => flush(), sleep: clock.sleep });

  return { binder, sockets, manager };
}

describe('a duplex channel', () => {
  // Guards that a raw subscription is the manager's subscription: same socket,
  // same hello, same release.
  it('subscribes raw with a hello and hands frames over undecoded', () => {
    const { binder, sockets } = build();
    const seen: unknown[] = [];

    const release = binder.raw('/api/v1/query/live/ws', (message) => seen.push(message), {
      hello: { action: 'subscribe', data: { id: 'q1' } },
      goodbye: { action: 'unsubscribe', data: { id: 'q1' } },
    });
    sockets.last().open();
    sockets.last().deliver({ type: 'snapshot', subscriptionId: 'q1', payload: { rows: [] } });

    expect(sockets.last().sent).toEqual([{ action: 'subscribe', data: { id: 'q1' } }]);
    expect(seen).toEqual([{ type: 'snapshot', subscriptionId: 'q1', payload: { rows: [] } }]);

    release();
    expect(sockets.last().sent).toEqual([
      { action: 'subscribe', data: { id: 'q1' } },
      { action: 'unsubscribe', data: { id: 'q1' } },
    ]);
  });

  // Guards the generated table as the single source of channel names.
  it('refuses a channel that is not a duplex binding', () => {
    const { binder } = build();

    expect(() => binder.raw('/ws/orders', () => {})).toThrow(/not a duplex channel/);
    expect(() => binder.raw('/ws/nowhere', () => {})).toThrow(/not a duplex channel/);
  });

  // Guards the entity path from a frame it must never see.
  it('keeps duplex frames out of the entity store', () => {
    const { binder, sockets } = build();

    binder.raw('/api/v1/query/live/ws', () => {}, { hello: { id: 'q1' } });
    sockets.last().open();
    sockets.last().deliver({ type: 'orderUpdated', payload: { id: 'o1', total: 3 } });

    expect(binderSnapshot(binder).queued).toBe(0);
  });

  it('lists the duplex binding in the snapshot', () => {
    const { binder } = build();

    const channels = binderSnapshot(binder).channels.map((c) => c.channel).sort();
    expect(channels).toEqual(['/api/v1/query/live/ws', '/ws/orders']);
  });
});
