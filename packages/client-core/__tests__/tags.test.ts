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

  // A template names the wire property, because that is what the route can
  // see. The body and response it resolves against carry the client-side
  // names. The generator renames what it has a schema for; the runtime
  // accepts the wire spelling for the document it did not.
  it('accepts a wire-spelled placeholder against a client-cased payload', () => {
    expect(resolveTag('Customer:{req.customer_id}', { body: { customerId: 'c-3' } })).toBe(
      'Customer:c-3',
    );
    expect(
      resolveTag('Ledger:{res.customer.external_id}', {
        response: { customer: { externalId: 'x-9' } },
      }),
    ).toBe('Ledger:x-9');
    expect(resolveTag('Customer:{customerId}', { body: { customer_id: 'c-4' } })).toBe(
      'Customer:c-4',
    );
  });

  it('reads an exact key as written when both spellings are present', () => {
    expect(
      resolveTag('Customer:{req.customer_id}', {
        body: { customer_id: 'wire', customerId: 'client' },
      }),
    ).toBe('Customer:wire');
  });

  it('still resolves to nothing when no spelling matches', () => {
    expect(resolveTag('Customer:{req.customer_id}', { body: { id: 1 } })).toBe(undefined);
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

  // A collection read declares `Order:{id}`: it provides one tag per record it
  // returns, not one tag for the array. Resolving against the array as a
  // whole finds no `id` and would report the healthiest query in the
  // application as a declaration that resolved to nothing.
  it('resolves a response template once per element of an array response', () => {
    const { tags, unresolved } = resolveTags(['Order:{id}', 'Order[]'], {
      response: [{ id: 1 }, { id: 2 }, { id: 1 }],
    });

    expect(tags).toEqual(['Order:1', 'Order:2', 'Order[]']);
    expect(unresolved).toEqual([]);
  });

  it('treats an empty array response as providing nothing, not as unresolved', () => {
    const { tags, unresolved } = resolveTags(['Order:{res.id}'], {
      response: [],
    });

    expect(tags).toEqual([]);
    expect(unresolved).toEqual([]);
  });

  it('still reports a template no element of the array can answer', () => {
    const { unresolved } = resolveTags(['Customer:{res.customerId}'], {
      response: [{ id: 1 }],
    });

    expect(unresolved).toEqual(['Customer:{res.customerId}']);
  });

  it('does not let an array response answer a request-only template', () => {
    const { unresolved } = resolveTags(['Customer:{req.customerId}'], {
      response: [{ customerId: 'r' }],
    });

    expect(unresolved).toEqual(['Customer:{req.customerId}']);
  });

  it('prefers the request over the array response for a bare placeholder', () => {
    const { tags } = resolveTags(['Customer:{customerId}'], {
      query: { customerId: 'q' },
      response: [{ customerId: 'r' }],
    });

    expect(tags).toEqual(['Customer:q']);
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
