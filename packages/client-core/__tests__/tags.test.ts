import { describe, expect, it } from 'vitest';

import { queryKey, resolveTag, resolveTags } from '../src/tags';

const ctx = {
  path: { id: 7, customerId: 'from-path' },
  query: { status: 'open', customerId: 'from-query' },
  body: { customerId: 'from-body', nested: { id: 'b-1' }, note: null },
  response: { id: 9, customerId: 'from-response', customer: { id: 'c-3' } },
};

describe('resolveTag', () => {
  it('returns a template with no placeholder unchanged', () => {
    expect(resolveTag('Order[]', {})).toBe('Order[]');
  });

  it('resolves from the path', () => {
    expect(resolveTag('Order:{id}', { path: { id: 7 } })).toBe('Order:7');
  });

  it('resolves from the query string', () => {
    expect(resolveTag('Order:{cursor}', { query: { cursor: 'abc' } })).toBe('Order:abc');
  });

  it('resolves from the request body', () => {
    expect(resolveTag('Customer:{customerId}', { body: { customerId: 'c-3' } })).toBe(
      'Customer:c-3',
    );
  });

  it('resolves from the response', () => {
    expect(resolveTag('Order:{id}', { response: { id: 9 } })).toBe('Order:9');
  });

  it('resolves an explicit {req.x} against the request only', () => {
    // The response also has a customerId, and it is not the one that wins.
    expect(resolveTag('Customer:{req.customerId}', ctx)).toBe('Customer:from-path');
    expect(resolveTag('Customer:{req.customerId}', { response: { customerId: 'r' } })).toBe(
      undefined,
    );
  });

  it('resolves an explicit {res.a.b} against the response only', () => {
    expect(resolveTag('Customer:{res.customer.id}', ctx)).toBe('Customer:c-3');
    expect(resolveTag('Customer:{res.customerId}', { body: { customerId: 'b' } })).toBe(undefined);
  });

  it('walks a dotted path into the request body', () => {
    expect(resolveTag('Order:{req.nested.id}', ctx)).toBe('Order:b-1');
  });

  // Path, then query, then body, then response -- first match wins.
  it.each([
    ['Customer:from-path', ctx],
    ['Customer:from-query', { ...ctx, path: {} }],
    ['Customer:from-body', { ...ctx, path: {}, query: {} }],
    ['Customer:from-response', { ...ctx, path: {}, query: {}, body: {} }],
  ])('resolves a bare placeholder to %s', (expected, context) => {
    expect(resolveTag('Customer:{customerId}', context)).toBe(expected);
  });

  it('stops at a source that holds null rather than falling through', () => {
    // The body answered the question. Reading the response instead would
    // invalidate some other record's list on a value nobody supplied.
    expect(resolveTag('Note:{note}', { body: { note: null }, response: { note: 'n-1' } })).toBe(
      undefined,
    );
  });

  it.each([
    ['nothing anywhere', 'Customer:{customerId}', {}],
    ['an empty string', 'Customer:{customerId}', { path: { customerId: '' } }],
    ['NaN', 'Order:{id}', { query: { id: Number.NaN } }],
    ['an object', 'Order:{id}', { body: { id: { nested: true } } }],
    ['an unknown explicit source', 'Order:{ctx.id}', { path: { id: 7 } }],
  ])('resolves to undefined, never the empty string, for %s', (_name, template, context) => {
    expect(resolveTag(template, context)).toBe(undefined);
  });

  it('fails the whole template when one of several placeholders is missing', () => {
    expect(resolveTag('Order:{id}:{missing}', { path: { id: 7 } })).toBe(undefined);
  });

  it('substitutes every placeholder in a multi-part template', () => {
    expect(resolveTag('Order:{id}:{req.status}', { path: { id: 7 }, query: { status: 'open' } })).toBe(
      'Order:7:open',
    );
  });

  it('accepts numbers, bigints and booleans as values', () => {
    expect(resolveTag('A:{a}', { path: { a: 0 } })).toBe('A:0');
    expect(resolveTag('A:{a}', { path: { a: 10n } })).toBe('A:10');
    expect(resolveTag('A:{a}', { path: { a: false } })).toBe('A:false');
  });
});

describe('resolveTags', () => {
  it('separates what resolved from what did not, and deduplicates', () => {
    const { tags, unresolved } = resolveTags(
      ['Order[]', 'Order:{id}', 'Order:{id}', 'Customer:{missing}'],
      { path: { id: 7 } },
    );

    // One bad declaration does not cost the caller the tags that did resolve.
    expect(tags).toEqual(['Order[]', 'Order:7']);
    expect(unresolved).toEqual(['Customer:{missing}']);
  });
});

describe('queryKey', () => {
  it('is stable under key order', () => {
    expect(queryKey('orderList', { query: { a: 1, b: 2 } })).toBe(
      queryKey('orderList', { query: { b: 2, a: 1 } }),
    );
  });

  it('treats an absent argument and an undefined one as the same request', () => {
    expect(queryKey('orderList', { query: { a: 1, b: undefined } })).toBe(
      queryKey('orderList', { query: { a: 1 } }),
    );
  });

  it('separates different arguments and different operations', () => {
    expect(queryKey('orderList', { query: { page: 1 } })).not.toBe(
      queryKey('orderList', { query: { page: 2 } }),
    );
    expect(queryKey('orderList')).not.toBe(queryKey('orderCount'));
  });

  it('keeps array order significant', () => {
    expect(queryKey('op', { query: { ids: [1, 2] } })).not.toBe(
      queryKey('op', { query: { ids: [2, 1] } }),
    );
  });
});
