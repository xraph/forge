import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { decodeFrame, StreamBinder } from '../src/live';
import type { FrameDecoder } from '../src/live';
import { forgeStreamingDecoder } from '../src/streaming';
import { SubscriptionManager } from '../src/stream';
import type { StreamBinding } from '../src/stream';
import { manualClock } from '../src/transport';
import type { OperationMeta } from '../src/transport';
import { fakeSockets, fakeTransport, settleMicrotasks } from './harness';
import { schema } from './schema';

/**
 * The streaming extension's envelope, against the manifest a real channel
 * generates.
 *
 * Both halves of this file are copied rather than designed. The frames are the
 * JSON shape of `internal.Message` in
 * `extensions/streaming/internal/streaming.go` -- every field it marshals, in
 * its spelling, including the ones this decoder ignores -- and the bindings are
 * what `writeStreams` in
 * `internal/client/generators/typescript/opsmanifest.go` emits: `channel` is
 * the endpoint path, `message` is the AsyncAPI domain name. The defect this
 * pins was invisible precisely because each side was tested against its own
 * idea of the envelope, so a frame that neither half would accept from the
 * other passed both suites.
 *
 * `extensions/streaming/frame_test.go` asserts the Go bytes and names this
 * file; this file names it back.
 */
const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const streams: readonly StreamBinding[] = [
  {
    channel: '/ws/orders',
    message: 'order.created',
    entity: 'Order',
    intent: 'upsert',
    invalidates: ['Order[]'],
  },
  {
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: 'patch',
    invalidates: [],
  },
];

/** One `internal.Message`, marshalled. Nothing here is trimmed for the test. */
function frame(event: string, data: unknown, overrides: Record<string, unknown> = {}) {
  return {
    id: 'msg-1',
    type: 'message',
    event,
    channel_id: 'orders',
    user_id: 'u-1',
    data,
    timestamp: '2026-08-08T10:00:00Z',
    ...overrides,
  };
}

function harness(decode: FrameDecoder) {
  const transport = fakeTransport(() => [{ id: 7, total: 99 }]);
  const batches = manualScheduler();
  const frames = manualScheduler();
  const release = manualScheduler();
  const clock = manualClock();
  const sockets = fakeSockets();
  const unknown: { message: string; channel: string }[] = [];

  const cache = new QueryCache({ transport, entities: schema, scheduler: batches.schedule });
  const manager = new SubscriptionManager({
    connect: sockets.connect,
    sleep: clock.sleep,
    random: () => 0,
    release: release.schedule,
    principal: () => cache.owner,
  });
  const binder = new StreamBinder({
    cache,
    streams,
    manager,
    decode,
    scheduler: frames.schedule,
    onUnknown: (message, channel) => unknown.push({ message, channel }),
  });

  return { cache, binder, sockets, frames, batches, unknown };
}

/** Mount a live query so the orders socket is open and frames are accepted. */
async function connect(decode: FrameDecoder) {
  const kit = harness(decode);

  kit.cache.subscribe(orderList, undefined, () => undefined);
  kit.binder.subscribe(orderList);
  await settleMicrotasks();

  return kit;
}

describe('the Forge streaming envelope', () => {
  // What the reorder bought. `type` is the transport kind, so the old
  // `type ?? event` order named every frame on every channel `message`, no
  // manifest row is keyed on `message`, and the whole channel was discarded.
  // Reading `event` first is correct for this envelope and still correct for
  // the two shapes that carry no `event` at all.
  it('is now readable by the default decoder', async () => {
    const { cache, sockets, frames, unknown } = await connect(decodeFrame);

    sockets.last().deliver(frame('order.created', { id: 9, total: 5 }));
    frames.flush();

    expect(unknown).toEqual([]);
    expect(cache.store.getRecord('Order:9')?.data).toEqual({ id: 9, total: 5 });
  });

  // Why forgeStreamingDecoder still exists after the reorder. The default has
  // no notion of a reserved transport kind, so a presence frame reaches it as
  // the name `presence` and is reported -- once per (channel, message) in
  // development -- for a frame that is working exactly as designed.
  it('still reports the extension’s transport frames, which the streaming decoder does not', async () => {
    const { sockets, frames, unknown } = await connect(decodeFrame);

    sockets.last().deliver({ id: 'm', type: 'presence', user_id: 'u-1', data: null });
    frames.flush();

    expect(unknown).toEqual([{ message: 'presence', channel: '/ws/orders' }]);
  });

  it('decodes to the binding the manifest declares, and applies it', async () => {
    const { cache, sockets, frames, unknown } = await connect(forgeStreamingDecoder());

    sockets.last().deliver(frame('order.created', { id: 9, total: 5 }));
    frames.flush();

    expect(unknown).toEqual([]);
    expect(cache.store.getRecord('Order:9')?.data).toEqual({ id: 9, total: 5 });
  });

  // `data`, not `payload`. Reading the wrong field would have produced a frame
  // that matched its binding and then upserted the envelope itself, which is a
  // worse failure than the one above because it writes.
  it('takes the payload from `data`', async () => {
    const { cache, sockets, frames } = await connect(forgeStreamingDecoder());

    sockets.last().deliver(frame('order.updated', { id: 7, total: 42 }));
    frames.flush();

    const record = cache.store.getRecord('Order:7')?.data as Record<string, unknown>;

    expect(record).toEqual({ id: 7, total: 42 });
    expect(record['user_id']).toBeUndefined();
  });

  // The reserved kinds, which is why the fallback is filtered. These are frames
  // the extension emits in normal operation, on a channel that is working; they
  // are not the server being ahead of the manifest, so they must not be
  // reported as though they were.
  it('drops transport frames without reporting them as unknown', async () => {
    const { sockets, frames, unknown } = await connect(forgeStreamingDecoder());
    const socket = sockets.last();

    for (const kind of ['presence', 'typing', 'join', 'leave', 'system', 'error', 'message']) {
      socket.deliver({ id: 'm', type: kind, user_id: 'u-1', data: null, timestamp: 'now' });
    }

    frames.flush();

    expect(unknown).toEqual([]);
  });

  // The drop is on the *fallback*, not on the frame. A domain event riding a
  // frame the extension marks `system` is still a domain event.
  it('honours an event name even when the transport kind is reserved', async () => {
    const { cache, sockets, frames } = await connect(forgeStreamingDecoder());

    sockets.last().deliver(frame('order.created', { id: 11 }, { type: 'system' }));
    frames.flush();

    expect(cache.store.getRecord('Order:11')?.data).toEqual({ id: 11 });
  });

  // What the fallback is for: this decoder replaces the default one rather than
  // sitting beside it, so a plain Forge WebSocket handler on another endpoint
  // keeps working after an application installs it globally.
  it('still reads the plain `type`/`payload` shape', async () => {
    const { cache, sockets, frames, unknown } = await connect(forgeStreamingDecoder());

    sockets.last().deliver({ type: 'order.created', payload: { id: 13 } });
    frames.flush();

    expect(unknown).toEqual([]);
    expect(cache.store.getRecord('Order:13')?.data).toEqual({ id: 13 });
  });

  it('ignores an envelope that is not an object, and one with no name at all', () => {
    const decode = forgeStreamingDecoder();

    expect(decode(null)).toBeUndefined();
    expect(decode('order.created')).toBeUndefined();
    expect(decode({ data: { id: 1 } })).toBeUndefined();
    expect(decode({ type: '', event: '', data: {} })).toBeUndefined();
  });
});

describe('channel resolution', () => {
  // The trap. `channel_id` is a logical id and a binding is keyed on the
  // endpoint path, so surfacing the id verbatim would override a channel that
  // matches with one that does not. Omitting it lets the arrival channel stand.
  it('leaves `channel_id` out of the decoded frame by default', () => {
    const decoded = forgeStreamingDecoder()(frame('order.created', { id: 9 }));

    expect(decoded).toEqual({ message: 'order.created', payload: { id: 9 } });
    expect(decoded).not.toHaveProperty('channel');
  });

  it('applies a mapping when one is supplied', () => {
    const decode = forgeStreamingDecoder({
      channelOf: (id) => (id === 'orders' ? '/ws/orders' : undefined),
    });

    expect(decode(frame('order.created', { id: 9 }))?.channel).toBe('/ws/orders');
  });

  // An id the mapping does not know is not a reason to guess. Falling through
  // to the arrival channel decodes correctly on a single-channel socket; a
  // guessed channel is a lookup miss on every socket.
  it('falls through to the arrival channel for an unmapped id', async () => {
    const { cache, sockets, frames, unknown } = await connect(
      forgeStreamingDecoder({ channelOf: () => undefined }),
    );

    sockets.last().deliver(frame('order.created', { id: 9, total: 5 }, { channel_id: 'unmapped' }));
    frames.flush();

    expect(unknown).toEqual([]);
    expect(cache.store.getRecord('Order:9')?.data).toEqual({ id: 9, total: 5 });
  });

  // A mapping that resolves to a channel carrying no such binding is a real
  // miss and is reported, rather than being papered over by a second lookup
  // against the arrival channel -- an override the caller asked for is an
  // override, and silently ignoring it would hide the misconfiguration.
  it('reports a mapped channel that binds nothing', async () => {
    const { sockets, frames, unknown } = await connect(
      forgeStreamingDecoder({ channelOf: () => '/ws/customers' }),
    );

    sockets.last().deliver(frame('order.created', { id: 9 }));
    frames.flush();

    expect(unknown).toEqual([{ message: 'order.created', channel: '/ws/customers' }]);
  });
});
