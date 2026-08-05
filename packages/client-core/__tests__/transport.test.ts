import { describe, expect, it } from 'vitest';

import { manualClock, operationUrl, RestTransport, retryable, statusOf } from '../src/transport';
import type { OperationMeta } from '../src/transport';
import { fakeClient, HttpFailure } from './harness';

const list: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const create: OperationMeta = {
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  provides: [],
  invalidates: ['Order[]'],
};

const detail: OperationMeta = {
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  invalidates: [],
};

/** A transport whose backoff is a fixed multiple, so a test can name the delay. */
function transport(client: ReturnType<typeof fakeClient>, clock = manualClock()) {
  return {
    clock,
    transport: new RestTransport({
      client,
      sleep: clock.sleep,
      random: () => 0,
      retry: { attempts: 3, baseDelay: 100 },
    }),
  };
}

describe('operationUrl', () => {
  it('substitutes path parameters and percent-encodes them', () => {
    expect(operationUrl('/orders/{id}/items/{sku}', { path: { id: 7, sku: 'a/b' } })).toBe(
      '/orders/7/items/a%2Fb',
    );
  });

  it('leaves a placeholder with no argument intact rather than emptying it', () => {
    // `/orders/` is a different, plausibly-successful resource. Failing loudly
    // beats fetching the collection when the detail page was asked for.
    expect(operationUrl('/orders/{id}', {})).toBe('/orders/{id}');
  });

  it('renders query parameters in sorted key order, skipping nullish', () => {
    const url = operationUrl('/orders', {
      query: { status: 'open', after: '2026-01-01', cursor: undefined, owner: null },
    });

    expect(url).toBe('/orders?after=2026-01-01&status=open');
  });

  it('repeats an array parameter and appends to an existing query string', () => {
    expect(operationUrl('/orders?fixed=1', { query: { tag: ['a', 'b'] } })).toBe(
      '/orders?fixed=1&tag=a&tag=b',
    );
  });
});

describe('retry classification', () => {
  it('retries a failure with no status, and never an abort', () => {
    expect(retryable(new Error('network down'))).toBe(true);

    const aborted = new Error('aborted');
    aborted.name = 'AbortError';

    expect(retryable(aborted)).toBe(false);
  });

  it('retries 5xx, 408 and 429 but no other 4xx', () => {
    expect(retryable(new HttpFailure(500))).toBe(true);
    expect(retryable(new HttpFailure(503))).toBe(true);
    expect(retryable(new HttpFailure(408))).toBe(true);
    expect(retryable(new HttpFailure(429))).toBe(true);
    expect(retryable(new HttpFailure(400))).toBe(false);
    expect(retryable(new HttpFailure(401))).toBe(false);
    expect(retryable(new HttpFailure(404))).toBe(false);
    expect(retryable(new HttpFailure(422))).toBe(false);
  });

  it('reads either statusCode or status', () => {
    expect(statusOf(new HttpFailure(429))).toBe(429);
    expect(statusOf({ status: 503 })).toBe(503);
    expect(statusOf('not an object')).toBeUndefined();
  });
});

describe('RestTransport retries', () => {
  it('retries a GET that failed with a 500 and resolves from the retry', async () => {
    const client = fakeClient((_config, attempt) => {
      if (attempt === 0) throw new HttpFailure(500);

      return [{ id: 7 }];
    });
    const { transport: rest, clock } = transport(client);

    const running = rest.execute({ meta: list, args: {} });

    await clock.advance(0);
    expect(client.calls).toHaveLength(1);
    expect(clock.pending()).toBe(1);

    await clock.advance(100);

    await expect(running).resolves.toEqual([{ id: 7 }]);
    expect(client.calls).toHaveLength(2);
  });

  it('never retries a POST, however transient the failure looks', async () => {
    const client = fakeClient(() => {
      throw new HttpFailure(500);
    });
    const { transport: rest, clock } = transport(client);

    await expect(rest.execute({ meta: create, args: { body: { total: 99 } } })).rejects.toThrow(
      'HTTP 500',
    );

    // Not one retry, and not one sleep either: a duplicate order is worse than
    // a failed one.
    expect(client.calls).toHaveLength(1);
    expect(clock.pending()).toBe(0);
  });

  it('does not retry a GET that got a 400, but does retry one that got a 429', async () => {
    const rejecting = fakeClient(() => {
      throw new HttpFailure(400);
    });
    const first = transport(rejecting);

    await expect(first.transport.execute({ meta: list, args: {} })).rejects.toThrow('HTTP 400');
    expect(rejecting.calls).toHaveLength(1);

    const throttled = fakeClient((_config, attempt) => {
      if (attempt === 0) throw new HttpFailure(429);

      return [];
    });
    const second = transport(throttled);
    const running = second.transport.execute({ meta: list, args: {} });

    await second.clock.advance(100);

    await expect(running).resolves.toEqual([]);
    expect(throttled.calls).toHaveLength(2);
  });

  it('gives up after the configured number of attempts', async () => {
    const client = fakeClient(() => {
      throw new HttpFailure(503);
    });
    const { transport: rest, clock } = transport(client);
    const running = rest.execute({ meta: list, args: {} });
    const settled = running.catch((error: unknown) => error);

    await clock.advance(100);
    await clock.advance(200);

    expect(await settled).toBeInstanceOf(HttpFailure);
    expect(client.calls).toHaveLength(3);
  });

  it('backs off exponentially, and jitters within the window', async () => {
    const delays: number[] = [];
    const client = fakeClient(() => {
      throw new HttpFailure(500);
    });
    const rest = new RestTransport({
      client,
      // Records rather than waits: the delay is the observable, not the wait.
      sleep: (ms) => {
        delays.push(ms);

        return Promise.resolve();
      },
      random: () => 1,
      retry: { attempts: 4, baseDelay: 100, maxDelay: 250 },
    });

    await expect(rest.execute({ meta: list, args: {} })).rejects.toThrow();

    // 100, 200, then capped at 250; random() === 1 puts each at the top of its
    // window, and random() === 0 would put it at the bottom.
    expect(delays).toEqual([100, 200, 250]);

    const floor: number[] = [];
    const low = new RestTransport({
      client: fakeClient(() => {
        throw new HttpFailure(500);
      }),
      sleep: (ms) => {
        floor.push(ms);

        return Promise.resolve();
      },
      random: () => 0,
      retry: { attempts: 3, baseDelay: 100 },
    });

    await expect(low.execute({ meta: list, args: {} })).rejects.toThrow();
    expect(floor).toEqual([50, 100]);
  });

  it('switches the generated client’s own retry loop off', async () => {
    const client = fakeClient(() => ({ id: 7 }));
    const { transport: rest } = transport(client);

    await rest.execute({ meta: detail, args: { path: { id: 7 } } });

    // Two retry policies would compound: three attempts each is nine requests.
    expect(client.calls[0]?.retry).toEqual({ maxAttempts: 1 });
    expect(client.calls[0]?.url).toBe('/orders/7');
    expect(client.calls[0]?.method).toBe('GET');
  });

  it('sends the body and the base URL it was configured with', async () => {
    const client = fakeClient(() => ({ id: 8 }));
    const rest = new RestTransport({ client, baseUrl: 'https://api.example.com' });

    await rest.execute({
      meta: create,
      args: { body: { total: 99 } },
      headers: { 'X-Trace': 'abc' },
    });

    expect(client.calls[0]?.url).toBe('https://api.example.com/orders');
    expect(client.calls[0]?.body).toEqual({ total: 99 });
    expect(client.calls[0]?.headers).toEqual({ 'X-Trace': 'abc' });
  });
});

/**
 * The generated base client's own declarations, copied out of
 * `internal/client/generators/typescript/fetch_client.go`.
 *
 * This is a compile-time assertion, not a runtime one: if `RestClientLike`
 * drifts from what the generator emits, a consumer discovers it as a type
 * error in their repository rather than here. Optional properties are widened
 * with `| undefined` there because a generated client must survive
 * `exactOptionalPropertyTypes`, and the transport has to accept that shape.
 */
interface GeneratedRetryConfig {
  maxAttempts?: number;
  delay?: number;
  maxDelay?: number;
  retryableStatusCodes?: number[];
}

interface GeneratedRequestConfig {
  method: string;
  url: string;
  headers?: Record<string, string> | undefined;
  body?: any | undefined;
  signal?: AbortSignal | undefined;
  retry?: GeneratedRetryConfig | undefined;
  allowEmptyBody?: boolean;
  bodyCodec?: string;
  responseCodec?: string;
}

declare class GeneratedHTTPClient {
  request<T>(config: GeneratedRequestConfig): Promise<T>;
}

declare class GeneratedRESTClient extends GeneratedHTTPClient {
  readonly orders: { list: (options?: { signal?: AbortSignal }) => Promise<unknown> };
}

describe('the generated client fits the transport', () => {
  it('accepts a RESTClient with no adapter in between', () => {
    const accepts = (client: import('../src/transport').RestClientLike): boolean =>
      typeof client.request === 'function';

    // The assertion is that this call compiles: `GeneratedRESTClient` has to
    // be assignable to `RestClientLike`. The stub stands in for an instance,
    // since constructing one would need the whole generated module.
    const stub = {
      request: () => Promise.resolve(undefined),
    } as unknown as GeneratedRESTClient;

    expect(accepts(stub)).toBe(true);
  });
});

/**
 * The transport half of codec parity.
 *
 * `HTTPClient#request` applies `encode`/`decode` only for the codec ids the
 * config it is handed names -- and this transport, not a typed per-endpoint
 * method, is what builds that config for every cached read and write. Without
 * the forward below, a client generated under `--field-naming camel` hands
 * back camelCase through `client.orders.get(id)` and WIRE-cased through the
 * hooks, from the same package, contradicting its own generated types.
 *
 * The Go half -- that the generator actually emits these ids into `ops.ts` --
 * is TestEntitiesTableIsRenamedToMatchTheDecodedPayload, and the two together
 * are TestCodecParityEndToEndThroughTheRuntime, in
 * internal/client/generators/typescript/e2e_codec_parity_test.go.
 */
describe('codec ids reach the generated client', () => {
  const coded: OperationMeta = {
    method: 'POST',
    path: '/orders',
    entity: 'Order',
    provides: [],
    invalidates: ['Order[]'],
    bodyCodec: 'Order',
    responseCodec: 'Order',
  };

  it('forwards both codec ids from the operation onto the request config', async () => {
    const client = fakeClient(() => ({ ok: true }));

    await new RestTransport({ client }).execute({ meta: coded, args: { body: { id: 1 } } });

    expect(client.calls[0]?.bodyCodec).toBe('Order');
    expect(client.calls[0]?.responseCodec).toBe('Order');
  });

  it('omits them entirely for an operation that declares none', async () => {
    const client = fakeClient(() => ({ ok: true }));

    await new RestTransport({ client }).execute({ meta: create, args: { body: { id: 1 } } });

    // `undefined` is not enough: an operation with no codec must leave the
    // keys ABSENT, so a generated client compiled with
    // exactOptionalPropertyTypes is handed a config it actually accepts.
    expect('bodyCodec' in (client.calls[0] as object)).toBe(false);
    expect('responseCodec' in (client.calls[0] as object)).toBe(false);
  });

  it('keeps them across the credential refresh retry', async () => {
    // `authorize` rebuilds the config as a spread; a codec dropped there would
    // decode the first attempt and not the second, which is the worst of both.
    let attempts = 0;
    const client = fakeClient(() => {
      attempts++;

      if (attempts === 1) throw new HttpFailure(401);

      return { ok: true };
    });

    await new RestTransport({
      client,
      auth: { credentials: () => ({ authorization: 'Bearer t' }), refresh: () => undefined },
    }).execute({ meta: coded, args: { body: { id: 1 } } });

    expect(client.calls).toHaveLength(2);
    expect(client.calls[1]?.responseCodec).toBe('Order');
  });
});
