import { describe, expect, it, vi } from 'vitest';

import { OverlayStack } from '../src/overlay';
import type { EntityPatch } from '../src/overlay';
import { makeRef } from '../src/ref';
import { EntityStore } from '../src/store';
import type { EntityKey } from '../src/types';

function host(): { store: EntityStore; stack: OverlayStack } {
  const store = new EntityStore();
  const stack = new OverlayStack(store);

  store.overlays = stack;

  return { store, stack };
}

function merge(fields: Record<string, unknown>): EntityPatch {
  return { kind: 'merge', source: fields };
}

function compute(fn: (prev: Record<string, unknown>) => Record<string, unknown>): EntityPatch {
  return { kind: 'merge', source: fn };
}

function patches(entries: [EntityKey, EntityPatch][]): Map<EntityKey, EntityPatch> {
  return new Map(entries);
}

function order(store: EntityStore, key: EntityKey): Record<string, unknown> | undefined {
  return store.read<Record<string, unknown> | undefined>(makeRef(key));
}

describe('the entity plane', () => {
  it('shows a merged patch over the base record', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open', total: 99 });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));

    // The record is still overlaid at read time, so it carries the OPTIMISTIC
    // symbol -- see store.ts. toEqual DOES compare symbol-keyed properties, so
    // an overlaid record is asserted with objectContaining rather than against
    // a plain literal.
    expect(order(store, 'Order:7')).toEqual(
      expect.objectContaining({ id: 7, status: 'shipped', total: 99 }),
    );
  });

  it('rebases: dropping the FIRST of two overlays keeps the second', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open', note: '' });

    const first = stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    stack.add(patches([['Order:7', merge({ note: 'gift' })]]));

    stack.take(first);

    // The second overlay is still live, so this is still an overlaid read.
    expect(order(store, 'Order:7')).toEqual(
      expect.objectContaining({ id: 7, status: 'open', note: 'gift' }),
    );
  });

  it('composes computed patches, and recomputes them on a refold', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, likes: 0 });

    const first = stack.add(
      patches([['Order:7', compute((prev) => ({ likes: (prev.likes as number) + 1 }))]]),
    );
    stack.add(patches([['Order:7', compute((prev) => ({ likes: (prev.likes as number) + 1 }))]]));

    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, likes: 2 }));

    stack.take(first);

    // Re-run against the reverted base, NOT rolled back by one. The second
    // overlay is still live, so this remains an overlaid read.
    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, likes: 1 }));
  });

  it('refolds over a base write, so a stream frame lands underneath the patch', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open', total: 99 });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    store.put('Order:7', { total: 120 }, 1);

    expect(order(store, 'Order:7')).toEqual(
      expect.objectContaining({ id: 7, status: 'shipped', total: 120 }),
    );
  });

  it('is a no-op over a record an evicting frame removed', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open' });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    store.evict('Order:7', 1);

    expect(order(store, 'Order:7')).toBeUndefined();
  });

  it('deletes, and restores on drop', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7 });

    const id = stack.add(patches([['Order:7', { kind: 'delete' }]]));
    expect(order(store, 'Order:7')).toBeUndefined();

    stack.take(id);
    expect(order(store, 'Order:7')).toEqual({ id: 7 });
  });

  it('creates a record that base never held', () => {
    const { store, stack } = host();

    stack.add(patches([['Order:~opt1', { kind: 'create', fields: { id: '~opt1', total: 99 } }]]));

    expect(order(store, 'Order:~opt1')).toEqual(
      expect.objectContaining({ id: '~opt1', total: 99 }),
    );
    expect(store.has('Order:~opt1')).toBe(false);
  });

  it('keeps the identity of records no overlay touches', () => {
    const { store, stack } = host();
    const { skeleton } = store.write(
      [{ id: 7 }, { id: 8 }],
      { Order: { idField: 'id' } },
      'Order',
    );
    const before = store.read<unknown[]>(skeleton);

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    const after = store.read<unknown[]>(skeleton);

    expect(after[1]).toBe(before[1]);
    expect(after[0]).not.toBe(before[0]);
  });

  it('reports a throwing compute patch and treats it as no change', () => {
    const report = vi.fn();
    const store = new EntityStore();
    const stack = new OverlayStack(store, report);
    store.overlays = stack;
    store.put('Order:7', { id: 7, status: 'open' });

    stack.add(
      patches([
        [
          'Order:7',
          compute(() => {
            throw new Error('boom');
          }),
        ],
      ]),
    );

    // Still overlaid -- the throwing patch is a no-op on the DATA, not a
    // removal of the overlay itself -- so this is still an overlaid read.
    expect(order(store, 'Order:7')).toEqual(expect.objectContaining({ id: 7, status: 'open' }));
    expect(report).toHaveBeenCalledWith(expect.any(Error), 'optimistic');
  });

  it('promote writes merges into base and reports delete targets', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open' });
    store.put('Order:8', { id: 8 });

    const id = stack.add(
      patches([
        ['Order:7', merge({ status: 'shipped' })],
        ['Order:8', { kind: 'delete' }],
        ['Order:~opt1', { kind: 'create', fields: { id: '~opt1' } }],
      ]),
    );

    const buried = stack.promote(stack.take(id) as never);

    expect(store.getRecord('Order:7')?.data).toEqual({ id: 7, status: 'shipped' });
    expect(store.has('Order:8')).toBe(false);
    // A create is never promoted: the real entity arrives in the response.
    expect(store.has('Order:~opt1')).toBe(false);
    expect(buried).toEqual(['Order:8']);
  });

  it('clear drops every overlay', () => {
    const { store, stack } = host();
    store.put('Order:7', { id: 7, status: 'open' });

    stack.add(patches([['Order:7', merge({ status: 'shipped' })]]));
    stack.clear();

    expect(stack.empty).toBe(true);
    expect(order(store, 'Order:7')).toEqual({ id: 7, status: 'open' });
  });
});
