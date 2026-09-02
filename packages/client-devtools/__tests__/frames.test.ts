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
