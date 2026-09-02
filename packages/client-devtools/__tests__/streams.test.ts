import { StreamBinder, SubscriptionManager } from '@forge-go/client-core';
import type { StreamConnection, StreamConnectContext } from '@forge-go/client-core';
import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import { counter, harness, ops } from './harness';

const bindings = [
  {
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: 'upsert' as const,
    invalidates: ['Order[]'],
  },
];

/**
 * A connection that never delivers anything.
 *
 * The binder only has to hold it. The manager's `connect` option expects a
 * function taking `StreamConnectContext` and returning a `StreamConnection`.
 */
function connect(): StreamConnection {
  return {
    onMessage: () => undefined,
    onClose: () => undefined,
    close: () => undefined,
  };
}

describe('streams', () => {
  it('reports the bindings and the mounted live queries', async () => {
    const h = harness();
    const manager = new SubscriptionManager({ connect });
    const binder = new StreamBinder({ cache: h.cache, streams: bindings, manager });
    const devtools = attach(h.cache, { now: counter(), binder });

    const release = binder.subscribe(ops.orderList, undefined);
    await h.settle();

    const view = devtools.streams();

    expect(view?.channels[0]?.channel).toBe('/ws/orders');
    expect(view?.channels[0]?.bindings[0]?.message).toBe('order.updated');
    expect(view?.live[0]?.key).toBe(h.cache.key(ops.orderList));
    expect(view?.recovering).toEqual([]);

    release();
    devtools.dispose();
  });

  it('answers undefined when no stream runtime is wired', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    expect(devtools.streams()).toBeUndefined();

    devtools.dispose();
  });

  it('does not move the store', async () => {
    const h = harness();
    const manager = new SubscriptionManager({ connect });
    const binder = new StreamBinder({ cache: h.cache, streams: bindings, manager });
    const devtools = attach(h.cache, { now: counter(), binder });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const version = devtools.store().version;
    const records = devtools.store().records;

    devtools.streams();
    devtools.sockets();

    expect(devtools.store().version).toBe(version);
    expect(devtools.store().records).toBe(records);

    stop();
    devtools.dispose();
  });
});
