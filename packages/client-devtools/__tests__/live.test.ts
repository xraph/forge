import { manualScheduler, StreamBinder, SubscriptionManager } from '@forge-go/client-core';
import type {
  StreamBinding,
  StreamConnect,
  StreamConnection,
  StreamConnectContext,
} from '@forge-go/client-core';
import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import { counter, harness, ops } from './harness';

/**
 * The stream half: a frame batch is a cause like any other, and the connection
 * panel answers a question `size` and `connected` cannot.
 */
const streams: StreamBinding[] = [
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

interface Fake extends StreamConnection {
  readonly context: StreamConnectContext;
  deliver(message: unknown): void;
}

function sockets(): { connect: StreamConnect; opened: Fake[] } {
  const opened: Fake[] = [];

  const connect: StreamConnect = (context) => {
    let messages: ((message: unknown) => void) | undefined;
    const connection: Fake = {
      context,
      onMessage: (handler) => {
        messages = handler;
      },
      onClose: () => undefined,
      close: () => undefined,
      deliver: (message) => messages?.(message),
    };

    opened.push(connection);

    return connection;
  };

  return { connect, opened };
}

function wire(cache: ReturnType<typeof harness>['cache']) {
  const factory = sockets();
  const frames = manualScheduler();
  const release = manualScheduler();
  const manager = new SubscriptionManager({
    connect: factory.connect,
    release: release.schedule,
    principal: () => cache.owner,
  });
  const binder = new StreamBinder({ cache, streams, manager, scheduler: frames.schedule });

  return { factory, frames, release, manager, binder };
}

describe('a frame batch is a cause', () => {
  it('records the frames, their tags, and the queries they reached', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const live = wire(h.cache);

    const stopQuery = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    const stopLive = h.cache.watchLive(ops.orderList);
    await h.settle();

    live.factory.opened[0]?.deliver({ type: 'order.created', payload: { id: 9, total: 30 } });
    live.frames.flush();
    h.flush();
    await h.settle();

    const batch = devtools.log().find((entry) => entry.kind === 'frames');

    expect(batch).toMatchObject({ kind: 'frames', frames: 1, tags: ['Order[]'] });

    const hit = devtools
      .log()
      .find((entry) => entry.kind === 'invalidated' && entry.query === h.cache.key(ops.orderList));

    expect(hit?.kind === 'invalidated' && hit.cause).toBe(batch?.seq);

    const report = devtools.whyRefetched(h.cache.key(ops.orderList));

    expect(report?.cause?.label).toBe('1 stream frame');
    expect(report?.summary).toContain('stream frame');

    stopLive();
    stopQuery();
    devtools.dispose();
  });

  it('explains a patch frame that correctly refetches nothing', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const live = wire(h.cache);

    const stopQuery = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    const stopLive = h.cache.watchLive(ops.orderList);
    await h.settle();

    // A patch declares no invalidations by design: the store has the new value
    // and no request is owed. A developer watching the network tab sees nothing
    // happen and reports a broken cache.
    live.factory.opened[0]?.deliver({ type: 'order.updated', payload: { id: 1, total: 99 } });
    live.frames.flush();
    h.flush();
    await h.settle();

    expect(devtools.entity('Order:1')?.fields['total']).toBe(99);

    const report = devtools.whyNotRefetched(h.cache.key(ops.orderList));

    expect(report.outcome).toBe('missed');
    expect(report.cause.label).toBe('1 stream frame');
    expect(report.invalidated).toEqual([]);

    stopLive();
    stopQuery();
    devtools.dispose();
  });
});

describe('the connection panel', () => {
  it('reports which sockets are open, for which channels, with what ref count', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const live = wire(h.cache);

    expect(devtools.sockets()).toEqual([]);

    const first = h.cache.watchLive(ops.orderList);
    const second = h.cache.watchLive(ops.orderList);
    await h.settle();

    const open = devtools.sockets();

    expect(open).toHaveLength(1);
    expect(open[0]).toMatchObject({
      endpoint: '/ws/orders',
      connected: true,
      // Two subscribers, one socket. A connection count that grows with the
      // render tree is the failure the ref counting exists to prevent, and this
      // is where it would be visible.
      refs: 2,
      opens: 1,
      reconnecting: false,
    });
    expect(open[0]?.channels).toEqual([{ channel: '/ws/orders', handlers: 2 }]);

    first();
    second();
    live.release.flush();

    expect(devtools.sockets()).toEqual([]);

    devtools.dispose();
  });

  it('finds the manager through the binder, with nothing to configure', async () => {
    const h = harness();
    const live = wire(h.cache);
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.watchLive(ops.orderList);
    await h.settle();

    expect(devtools.sockets().map((socket) => socket.endpoint)).toEqual(['/ws/orders']);
    // Explicitly passed, for an application that built a manager without a
    // binder: the same answer.
    const explicit = attach(h.cache, { now: counter(), manager: live.manager });

    expect(explicit.sockets()).toEqual(devtools.sockets());

    explicit.dispose();
    stop();
    devtools.dispose();
  });
});
