import { describe, expect, it } from 'vitest';

import { RestTransport } from '../src/transport';
import type { AuthProvider, OperationMeta, RequestEvent } from '../src/transport';
import { fakeClient, HttpFailure } from './harness';

const list: OperationMeta = {
  method: 'GET',
  path: '/orders',
  provides: ['Order[]'],
  invalidates: [],
  security: ['bearer'],
};

const create: OperationMeta = {
  method: 'POST',
  path: '/orders',
  provides: [],
  invalidates: ['Order[]'],
};

/** Collect what the observer saw, as `type` strings plus the payloads. */
function recorder(): { events: RequestEvent[]; ids: number[]; observe: (r: { id: number }, e: RequestEvent) => void } {
  const events: RequestEvent[] = [];
  const ids: number[] = [];

  return {
    events,
    ids,
    observe(report, event) {
      ids.push(report.id);
      events.push(event);
    },
  };
}

/**
 * The transport already decides all of this and decides it silently. A
 * decorator around `execute` cannot see any of it: retries, the backoff and
 * the credential refresh all happen inside one call, so from the outside a
 * request that was retried twice and one that was not are the same event.
 */
describe('watching what the transport did', () => {
  it('reports each attempt and the backoff between them', async () => {
    const seen = recorder();
    const client = fakeClient((_config, attempt) => {
      if (attempt === 0) throw new HttpFailure(503);

      return { ok: true };
    });
    const rest = new RestTransport({
      client,
      sleep: () => Promise.resolve(),
      random: () => 0,
      observer: seen.observe,
    });

    await rest.execute({ meta: list, args: {} });

    expect(seen.events.map((event) => event.type)).toEqual([
      'start',
      'attempt',
      'retry',
      'attempt',
      'settled',
    ]);

    const retry = seen.events.find((event) => event.type === 'retry');

    expect(retry).toMatchObject({ type: 'retry', status: 503 });
    expect((retry as { delay: number }).delay).toBeGreaterThan(0);
    // One request, so one id across every event of it.
    expect(new Set(seen.ids).size).toBe(1);
  });

  /**
   * The distinction the mock's network view exists to draw. A failed POST and
   * a failed GET look identical in a browser network tab; here the `limit` on
   * `start` says a retry was never on the table for this method.
   */
  it('says a retry was never available for a method that is not idempotent', async () => {
    const seen = recorder();
    const client = fakeClient(() => {
      throw new HttpFailure(500);
    });
    const rest = new RestTransport({ client, sleep: () => Promise.resolve(), observer: seen.observe });

    await expect(rest.execute({ meta: create, args: { body: {} } })).rejects.toThrow();

    expect(seen.events[0]).toEqual({ type: 'start', limit: 1 });
    expect(seen.events.filter((event) => event.type === 'attempt')).toHaveLength(1);
    expect(seen.events.at(-1)).toMatchObject({ type: 'settled', ok: false, status: 500 });
  });

  it('reports a retryable status that still ran out of attempts', async () => {
    const seen = recorder();
    const client = fakeClient(() => {
      throw new HttpFailure(503);
    });
    const rest = new RestTransport({
      client,
      retry: { attempts: 2 },
      sleep: () => Promise.resolve(),
      observer: seen.observe,
    });

    await expect(rest.execute({ meta: list, args: {} })).rejects.toThrow();

    expect(seen.events[0]).toEqual({ type: 'start', limit: 2 });
    expect(seen.events.filter((event) => event.type === 'attempt')).toHaveLength(2);
  });

  /**
   * The single flight, which is the one thing a per-request view cannot infer:
   * three 401s produce one refresh, and only the transport knows which of them
   * asked for it and which waited on somebody else's.
   */
  it('marks the credential refresh, and which requests only waited on it', async () => {
    const seen = recorder();
    let refreshes = 0;
    let token = 't0';
    const client = fakeClient((config) => {
      if (config.headers?.['Authorization'] === 'Bearer t0') throw new HttpFailure(401);

      return { ok: true };
    });
    const auth: AuthProvider = {
      credentials: () => ({ Authorization: `Bearer ${token}` }),
      refresh: async () => {
        refreshes += 1;
        token = 't1';

        return Promise.resolve();
      },
    };
    const rest = new RestTransport({ client, auth, sleep: () => Promise.resolve(), observer: seen.observe });

    await Promise.all([
      rest.execute({ meta: list, args: {} }),
      rest.execute({ meta: list, args: {} }),
      rest.execute({ meta: list, args: {} }),
    ]);

    expect(refreshes).toBe(1);

    const refreshed = seen.events.filter((event) => event.type === 'refresh');

    expect(refreshed).toHaveLength(3);
    // Exactly one of them asked; the other two joined the flight in progress.
    expect(refreshed.filter((event) => !(event as { joined: boolean }).joined)).toHaveLength(1);
  });

  /**
   * The refresh has a duration and nothing reported it. Without a closing
   * event the waterfall can say a request hit the credential refresh but not
   * how long it sat there, which is the only number that makes an auth stall
   * distinguishable from a slow server.
   */
  it('closes the refresh so its duration is measurable', async () => {
    const seen = recorder();
    let token = 't0';
    const client = fakeClient((config) => {
      if (config.headers?.['Authorization'] === 'Bearer t0') throw new HttpFailure(401);

      return { ok: true };
    });
    const auth: AuthProvider = {
      credentials: () => ({ Authorization: `Bearer ${token}` }),
      refresh: () => {
        token = 't1';

        return Promise.resolve();
      },
    };
    const rest = new RestTransport({ client, auth, sleep: () => Promise.resolve(), observer: seen.observe });

    await rest.execute({ meta: list, args: {} });

    const kinds = seen.events.map((event) => event.type);

    expect(kinds).toContain('refresh');
    expect(kinds).toContain('refreshed');
    expect(kinds.indexOf('refreshed')).toBeGreaterThan(kinds.indexOf('refresh'));
  });

  it('closes the refresh even when it fails', async () => {
    const seen = recorder();
    const client = fakeClient(() => {
      throw new HttpFailure(401);
    });
    const auth: AuthProvider = {
      credentials: () => ({ Authorization: 'Bearer t0' }),
      refresh: () => Promise.reject(new Error('refresh is down')),
    };
    const rest = new RestTransport({ client, auth, sleep: () => Promise.resolve(), observer: seen.observe });

    await expect(rest.execute({ meta: list, args: {} })).rejects.toThrow();

    expect(seen.events.map((event) => event.type)).toContain('refreshed');
  });

  it('costs an unwatched transport nothing but an undefined field', async () => {
    const client = fakeClient(() => ({ ok: true }));
    const rest = new RestTransport({ client });

    await expect(rest.execute({ meta: list, args: {} })).resolves.toEqual({ ok: true });
  });
});
