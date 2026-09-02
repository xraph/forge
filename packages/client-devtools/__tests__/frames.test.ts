import { applyFrames } from '@forge-go/client-core';
import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import { counter, harness } from './harness';

const binding = {
  channel: '/ws/orders',
  message: 'order.updated',
  entity: 'Order',
  intent: 'upsert' as const,
  invalidates: ['Order[]'],
};

describe('frame capture', () => {
  it('reads a bare `frames: {}` as on, not as a limit of zero', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), frames: {} });

    applyFrames(h.cache, [{ binding, payload: { id: 1, total: 5 } }]);

    expect(devtools.capturing).toBe(true);
    expect(devtools.frames()).toHaveLength(1);

    devtools.dispose();
  });

  it('is off by default and captures nothing', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    applyFrames(h.cache, [{ binding, payload: { id: 1, total: 5 } }]);

    expect(devtools.capturing).toBe(false);
    expect(devtools.frames()).toEqual([]);

    devtools.dispose();
  });

  it('records the channel, the message and the payload when asked', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), frames: { limit: 10 } });

    applyFrames(h.cache, [{ binding, payload: { id: 1, total: 5 } }]);

    const captured = devtools.frames();

    expect(captured).toHaveLength(1);
    expect(captured[0]?.channel).toBe('/ws/orders');
    expect(captured[0]?.message).toBe('order.updated');
    expect(captured[0]?.intent).toBe('upsert');
    expect(captured[0]?.entity).toBe('Order');
    expect(captured[0]?.payload).toEqual({ id: 1, total: 5 });

    devtools.dispose();
  });

  it('copies the payload, so nothing it holds can move the store', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), frames: { limit: 10 } });
    const payload = { id: 1, total: 5 };

    applyFrames(h.cache, [{ binding, payload }]);

    expect(devtools.frames()[0]?.payload).not.toBe(payload);

    devtools.dispose();
  });

  it('truncates a deep payload, and says where it stopped', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), frames: { limit: 10 } });

    // Eight levels against a depth cap of six.
    let payload: Record<string, unknown> = { id: 1, bottom: true };

    for (let i = 0; i < 8; i++) payload = { id: 1, nested: payload };

    applyFrames(h.cache, [{ binding, payload }]);

    let node = devtools.frames()[0]?.payload as Record<string, unknown> | string;
    let depth = 0;

    while (typeof node === 'object' && node['nested'] !== undefined) {
      node = node['nested'] as Record<string, unknown> | string;
      depth++;
    }

    expect(node).toBe('[deeper]');
    expect(depth).toBeLessThanOrEqual(7);

    devtools.dispose();
  });

  it('truncates a wide array and a wide object alike', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), frames: { limit: 10 } });

    // The width cap is 50 in both directions. An object of 60 fields used to
    // be copied whole, because only the array branch capped anything -- which
    // made the "depth and width capped" promise false for exactly the payload
    // shape that costs the most to get wrong, a wide flat one.
    const wide: Record<string, unknown> = { id: 1 };

    for (let i = 0; i < 60; i++) wide[`field${String(i)}`] = i;

    applyFrames(h.cache, [{ binding, payload: { id: 1, rows: Array.from({ length: 60 }, (_, i) => i), wide } }]);

    const captured = devtools.frames()[0]?.payload as {
      rows: unknown[];
      wide: Record<string, unknown>;
    };

    // 50 elements plus the marker.
    expect(captured.rows).toHaveLength(51);
    expect(captured.rows[50]).toBe('[10 more]');

    // 61 keys in, 50 kept plus one marker key out.
    expect(Object.keys(captured.wide)).toHaveLength(51);
    expect(captured.wide['[more]']).toBe('[11 more]');
    expect(captured.wide['field49']).toBeUndefined();

    devtools.dispose();
  });

  it('is a bounded ring, oldest first, dropping the excess', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), frames: { limit: 2 } });

    for (const id of [1, 2, 3]) {
      applyFrames(h.cache, [{ binding, payload: { id, total: id } }]);
    }

    const captured = devtools.frames();

    expect(captured).toHaveLength(2);
    expect((captured[0]?.payload as { id: number }).id).toBe(2);
    expect((captured[1]?.payload as { id: number }).id).toBe(3);

    devtools.dispose();
  });
});
