import { RestTransport } from '@forge-go/client-core';
import type { OperationMeta, RestClientLike, RestRequestConfig } from '@forge-go/client-core';
import { describe, expect, it } from 'vitest';
import { RequestLog } from '../src/requests';
import { counter } from './harness';

const list: OperationMeta = {
  method: 'GET',
  path: '/orders',
  provides: ['Order[]'],
  invalidates: [],
};

const create: OperationMeta = {
  method: 'POST',
  path: '/orders',
  provides: [],
  invalidates: ['Order[]'],
};

class Failure extends Error {
  readonly statusCode: number;

  constructor(statusCode: number) {
    super(`HTTP ${statusCode}`);
    this.name = 'HTTPError';
    this.statusCode = statusCode;
  }
}

/** A generated client, faked down to the one method the transport drives. */
function client(handler: (config: RestRequestConfig, call: number) => unknown): RestClientLike {
  let calls = 0;

  return {
    request<T>(config: RestRequestConfig): Promise<T> {
      const call = calls++;

      return Promise.resolve().then(() => handler(config, call) as T);
    },
  };
}

function wired(handler: (config: RestRequestConfig, call: number) => unknown): {
  log: RequestLog;
  rest: RestTransport;
} {
  const log = new RequestLog(50, counter());
  const rest = new RestTransport({
    client: client(handler),
    sleep: () => Promise.resolve(),
    random: () => 0,
    observer: log.observer,
  });

  return { log, rest };
}

describe('the request log', () => {
  it('records one settled request with its operation and its arguments', async () => {
    const { log, rest } = wired(() => ({ ok: true }));

    await rest.execute({ meta: list, args: { query: { page: 1 } } });

    const [entry] = log.entries();

    expect(log.entries()).toHaveLength(1);
    expect(entry?.operation).toBe('GET /orders');
    expect(entry?.args).toContain('page');
    expect(entry?.outcome).toBe('ok');
    expect(entry?.attempts).toBe(1);
  });

  it('keeps the ledger of attempts and the backoff between them', async () => {
    const { log, rest } = wired((_config, call) => {
      if (call === 0) throw new Failure(503);

      return { ok: true };
    });

    await rest.execute({ meta: list, args: {} });

    const [entry] = log.entries();

    expect(entry?.attempts).toBe(2);
    expect(entry?.retries).toHaveLength(1);
    expect(entry?.retries[0]?.status).toBe(503);
    expect(entry?.retries[0]?.delay).toBeGreaterThan(0);
    expect(entry?.outcome).toBe('ok');
  });

  /**
   * The row the whole view exists for. A browser network tab shows this and a
   * retried GET as the same thing: one request, one failure.
   */
  it('records that a failed POST was never eligible for a retry', async () => {
    const { log, rest } = wired(() => {
      throw new Failure(500);
    });

    await expect(rest.execute({ meta: create, args: { body: {} } })).rejects.toThrow();

    const [entry] = log.entries();

    expect(entry?.outcome).toBe('failed');
    expect(entry?.status).toBe(500);
    expect(entry?.attempts).toBe(1);
    expect(entry?.limit).toBe(1);
  });

  it('records a 4xx that the policy declined to retry despite the budget', async () => {
    const { log, rest } = wired(() => {
      throw new Failure(403);
    });

    await expect(rest.execute({ meta: list, args: {} })).rejects.toThrow();

    const [entry] = log.entries();

    expect(entry?.attempts).toBe(1);
    // The budget was there and went unused, which is what says the status was
    // the reason rather than the method.
    expect(entry?.limit).toBeGreaterThan(1);
    expect(entry?.status).toBe(403);
  });

  it('shows a request while it is still in flight', async () => {
    let release = (): void => undefined;
    const { log, rest } = wired(
      () =>
        new Promise((resolve) => {
          release = () => {
            resolve({ ok: true });
          };
        }),
    );

    const pending = rest.execute({ meta: list, args: {} });

    // `execute` awaits the credential attach before it ever reaches the
    // client, so one microtask is not enough to get the request out the door.
    for (let i = 0; i < 8; i++) await Promise.resolve();

    expect(log.entries()[0]?.outcome).toBe('pending');
    expect(log.entries()[0]?.duration).toBeUndefined();

    release();
    await pending;

    expect(log.entries()[0]?.outcome).toBe('ok');
    expect(log.entries()[0]?.duration).toBeGreaterThanOrEqual(0);
  });

  it('overwrites the oldest request once the ring is full, and says how many', async () => {
    const log = new RequestLog(2, counter());
    const rest = new RestTransport({ client: client(() => ({ ok: true })), observer: log.observer });

    await rest.execute({ meta: list, args: {} });
    await rest.execute({ meta: list, args: {} });
    await rest.execute({ meta: list, args: {} });

    expect(log.entries()).toHaveLength(2);
    expect(log.dropped).toBe(1);
  });
});
