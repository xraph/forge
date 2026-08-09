import { describe, expect, it } from 'vitest';

import { isRef, isRewritten, makeRef } from '../src/ref';
import { assertAcyclic, encode, revive } from '../src/wire';

const where = { query: 'GET /orders({})' };

describe('encode', () => {
  it('emits a reference as a plain marker object and reports its key', () => {
    const { value, refs } = encode({ order: makeRef('Order:7') }, where);

    expect(value).toEqual({ order: { __ref: 'Order:7' } });
    expect(refs).toEqual(['Order:7']);
  });

  it('escapes response data that is shaped exactly like a reference', () => {
    const { value, refs } = encode({ meta: { __ref: 'not a reference' } }, where);

    expect(value).toEqual({ meta: { ___ref: 'not a reference' } });
    expect(refs).toEqual([]);
  });

  it('escapes an already-escape-shaped key, so the scheme nests', () => {
    expect(encode({ ___ref: 1, ____ref: 2 }, where).value).toEqual({ ____ref: 1, _____ref: 2 });
  });

  it('leaves every other key alone', () => {
    expect(encode({ __refs: 1, ref: 2, _ref: 3 }, where).value).toEqual({
      __refs: 1,
      ref: 2,
      _ref: 3,
    });
  });

  it('reports references found at any depth, deduplication left to the caller', () => {
    const { refs } = encode(
      { rows: [{ o: makeRef('Order:1') }, { o: makeRef('Order:2') }, makeRef('Order:1')] },
      where,
    );

    expect(refs).toEqual(['Order:1', 'Order:2', 'Order:1']);
  });

  it('allows the same object twice through different branches -- a DAG is not a cycle', () => {
    const shared = { n: 1 };

    expect(encode({ a: shared, b: shared }, where).value).toEqual({ a: { n: 1 }, b: { n: 1 } });
  });

  it('throws on a cycle, naming the query and the path', () => {
    const node: Record<string, unknown> = { id: 7 };
    node.self = node;

    expect(() => encode(node, where)).toThrow(/cyclic value/);
    expect(() => encode(node, where)).toThrow(/skeleton\.self/);
  });

  it('names the record when one is being encoded', () => {
    const node: Record<string, unknown> = {};
    node.meta = { self: node };

    const record = { query: 'GET /orders({})', entity: 'Order:7' };

    expect(() => encode(node, record)).toThrow(/entity {2}Order:7/);
    expect(() => encode(node, record)).toThrow(/data\.meta\.self/);
  });

  it('reports an array index in the path', () => {
    const row: Record<string, unknown> = {};
    row.rows = [row];

    expect(() => encode(row, where)).toThrow(/skeleton\.rows\[0\]/);
  });
});

describe('assertAcyclic', () => {
  it('accepts an acyclic value', () => {
    expect(() => assertAcyclic({ a: [1, { b: 2 }] }, where)).not.toThrow();
  });

  it('accepts a DAG', () => {
    const shared = { n: 1 };

    expect(() => assertAcyclic({ a: shared, b: shared }, where)).not.toThrow();
  });

  it('throws on a cycle', () => {
    const node: Record<string, unknown> = {};
    node.self = node;

    expect(() => assertAcyclic(node, where)).toThrow(/cyclic value/);
  });
});

describe('revive', () => {
  it('mints a genuine reference the runtime recognises', () => {
    const revived = revive({ order: { __ref: 'Order:7' } }) as { order: unknown };

    expect(isRef(revived.order)).toBe(true);
  });

  it('unescapes data that was shaped like a reference, and does not mint one', () => {
    const revived = revive({ meta: { ___ref: 'not a reference' } }) as { meta: unknown };

    expect(revived.meta).toEqual({ __ref: 'not a reference' });
    expect(isRef(revived.meta)).toBe(false);
  });

  it('marks a container that has a reference beneath it', () => {
    const revived = revive({ rows: [{ __ref: 'Order:7' }] }) as { rows: object };

    expect(isRewritten(revived.rows)).toBe(true);
    expect(isRewritten(revived)).toBe(true);
  });

  it('leaves a container with no reference beneath it unmarked and by identity', () => {
    const input = { totals: { open: 3 } };
    const revived = revive(input) as { totals: object };

    expect(revived).toBe(input);
    expect(isRewritten(revived.totals)).toBe(false);
  });

  it('does not mark a container that only needed unescaping', () => {
    const revived = revive({ meta: { ___ref: 'x' } }) as object;

    expect(isRewritten(revived)).toBe(false);
  });

  it('ignores a marker-shaped object carrying anything but a lone string', () => {
    expect(isRef(revive({ __ref: 7 }))).toBe(false);
    expect(isRef(revive({ __ref: 'Order:7', extra: 1 }))).toBe(false);
  });

  it('round-trips through JSON', () => {
    const encoded = encode({ rows: [makeRef('Order:7'), { __ref: 'data' }] }, where);
    const revived = revive(JSON.parse(JSON.stringify(encoded.value))) as { rows: unknown[] };

    expect(isRef(revived.rows[0])).toBe(true);
    expect(revived.rows[1]).toEqual({ __ref: 'data' });
    expect(isRef(revived.rows[1])).toBe(false);
  });
});
