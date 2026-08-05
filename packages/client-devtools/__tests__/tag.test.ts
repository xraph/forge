import { describe, expect, it } from 'vitest';
import { nearMisses, parseTag } from '../src/tag';

describe('parsing a tag', () => {
  it('recognises collections, instances and bare names', () => {
    expect(parseTag('Order[]')).toMatchObject({ type: 'Order', collection: true, scope: '' });
    expect(parseTag('Order:7')).toMatchObject({ type: 'Order', id: '7', collection: false });
    expect(parseTag('Order[]:archived')).toMatchObject({
      type: 'Order',
      collection: true,
      scope: ':archived',
    });
    // Not everything is structured, and an application is free to invalidate
    // whatever it likes. Reported as a typename rather than refused.
    expect(parseTag('everything')).toMatchObject({
      type: 'everything',
      id: undefined,
      collection: false,
    });
  });

  it('keeps a composite id whole rather than splitting on the first colon twice', () => {
    expect(parseTag('Order:tenant:7')).toMatchObject({ type: 'Order', id: 'tenant:7' });
  });
});

describe('naming the near miss', () => {
  it('reports each relation, most suspicious first', () => {
    const found = nearMisses(
      ['Order:7', 'order[]', 'Customer[]', 'Invoice[]'],
      ['Order[]', 'Order[]:archived', 'Customer:3', 'Invoice[]'],
    );

    const pairs = found.map((miss) => `${miss.invalidated}|${miss.carried}|${miss.relation}`);

    expect(pairs[0]).toBe('Order:7|Order[]|instance-vs-collection');
    expect(pairs).toContain('order[]|Order[]|case');
    expect(pairs).toContain('Customer[]|Customer:3|collection-vs-instance');
    expect(pairs).toContain('Order:7|Order[]:archived|instance-vs-collection');

    // An exact match is not a near miss: those tags met.
    expect(pairs.some((pair) => pair.startsWith('Invoice[]|Invoice[]'))).toBe(false);
  });

  it('spots a wrong id, which is a wrong placeholder in a template', () => {
    const [miss] = nearMisses(['Order:7'], ['Order:8']);

    expect(miss?.relation).toBe('different-instance');
    expect(miss?.hint).toContain('different ones');
  });

  it('spots a scope that only one side carries', () => {
    const [miss] = nearMisses(['Order[]'], ['Order[]:archived']);

    expect(miss?.relation).toBe('scoped');
  });

  it('says nothing about tags that are simply unrelated', () => {
    expect(nearMisses(['Shipment:1'], ['Order[]', 'Customer:3'])).toEqual([]);
  });

  it('caps its output, so a huge dependency set does not produce a wall of text', () => {
    const carried = Array.from({ length: 400 }, (_, i) => `Order:${String(i)}`);

    expect(nearMisses(['Order:9000'], carried)).toHaveLength(8);
    expect(nearMisses(['Order:9000'], carried, 3)).toHaveLength(3);
  });
});
