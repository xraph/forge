import { describe, expect, it } from 'vitest';

import { RestTransport } from '../src/transport';
import type { AuthProvider, OperationMeta } from '../src/transport';
import { deferred, fakeClient, HttpFailure, settleMicrotasks } from './harness';

const list: OperationMeta = {
  method: 'GET',
  path: '/orders',
  provides: ['Order[]'],
  invalidates: [],
  security: ['bearer'],
};

const metrics: OperationMeta = {
  method: 'GET',
  path: '/metrics',
  provides: [],
  invalidates: [],
  security: ['apiKey'],
};

const health: OperationMeta = {
  method: 'GET',
  path: '/health',
  provides: [],
  invalidates: [],
};

describe('credential attach', () => {
  it('attaches per the endpoint’s declared scheme, and nothing to a public one', async () => {
    const client = fakeClient(() => ({}));
    const auth: AuthProvider = {
      credentials: (meta): Record<string, string> | undefined => {
        if (meta.security?.includes('bearer')) return { Authorization: 'Bearer t0' };
        if (meta.security?.includes('apiKey')) return { 'X-Api-Key': 'k0' };

        return undefined;
      },
    };
    const rest = new RestTransport({ client, auth });

    await rest.execute({ meta: list, args: {} });
    await rest.execute({ meta: metrics, args: {} });
    await rest.execute({ meta: health, args: {} });

    expect(client.calls[0]?.headers).toEqual({ Authorization: 'Bearer t0' });
    expect(client.calls[1]?.headers).toEqual({ 'X-Api-Key': 'k0' });
    expect(client.calls[2]?.headers).toBeUndefined();
  });

  it('merges credentials over the caller’s own headers', async () => {
    const client = fakeClient(() => ({}));
    const rest = new RestTransport({
      client,
      auth: { credentials: () => ({ Authorization: 'Bearer t0' }) },
    });

    await rest.execute({ meta: list, args: {}, headers: { 'X-Trace': 'abc' } });

    expect(client.calls[0]?.headers).toEqual({ 'X-Trace': 'abc', Authorization: 'Bearer t0' });
  });
});

/**
 * A provider whose refresh the test releases by hand.
 *
 * The whole point of the stampede test is that two requests are inside the
 * refresh window at the same time, and a window that closes on its own is a
 * race. Here it closes when the test says so.
 */
function gatedAuth() {
  const gate = deferred<void>();
  const state = { token: 't0', refreshes: 0 };

  const auth: AuthProvider = {
    credentials: () => ({ Authorization: `Bearer ${state.token}` }),
    refresh: async () => {
      state.refreshes++;
      await gate.promise;
      state.token = 't1';
    },
  };

  return { auth, gate, state };
}

/** Rejects anything not bearing the current token. */
function guarded(valid: () => string) {
  return fakeClient((config) => {
    if (config.headers?.['Authorization'] !== `Bearer ${valid()}`) throw new HttpFailure(401);

    return { ok: true };
  });
}

describe('single-flight refresh', () => {
  it('turns two concurrent 401s into one refresh, and retries both', async () => {
    const { auth, gate, state } = gatedAuth();
    const client = guarded(() => 't1');
    const rest = new RestTransport({ client, auth, sleep: () => Promise.resolve() });

    const first = rest.execute({ meta: list, args: {} });
    const second = rest.execute({ meta: list, args: { query: { status: 'open' } } });

    // Both requests are now inside the refresh window, deterministically:
    // the gate has not been released, so neither can have got past it.
    await settleMicrotasks();
    expect(state.refreshes).toBe(1);
    expect(client.calls).toHaveLength(2);

    gate.resolve();

    await expect(first).resolves.toEqual({ ok: true });
    await expect(second).resolves.toEqual({ ok: true });

    // One refresh, and neither request retried against the token that was
    // already known to be dead.
    expect(state.refreshes).toBe(1);
    expect(client.calls).toHaveLength(4);
    expect(client.calls[2]?.headers).toEqual({ Authorization: 'Bearer t1' });
    expect(client.calls[3]?.headers).toEqual({ Authorization: 'Bearer t1' });
  });

  it('retries exactly once: a 401 against a fresh credential is an answer', async () => {
    const { auth, gate, state } = gatedAuth();
    // Never satisfied, so the retry 401s too.
    const client = guarded(() => 'never');
    const rest = new RestTransport({ client, auth });

    const running = rest.execute({ meta: list, args: {} });

    await settleMicrotasks();
    gate.resolve();

    await expect(running).rejects.toThrow('HTTP 401');
    expect(state.refreshes).toBe(1);
    expect(client.calls).toHaveLength(2);
  });

  it('surfaces the 401, not the refresh failure, when the refresh fails', async () => {
    const client = guarded(() => 'never');
    const rest = new RestTransport({
      client,
      auth: {
        credentials: () => ({ Authorization: 'Bearer t0' }),
        refresh: () => Promise.reject(new Error('refresh token expired')),
      },
    });

    // The caller asked whether it was authorized. "Your refresh token is
    // expired" answers a question it did not ask.
    await expect(rest.execute({ meta: list, args: {} })).rejects.toThrow('HTTP 401');
    expect(client.calls).toHaveLength(1);
  });

  it('starts a new refresh for a 401 that arrives after the previous one landed', async () => {
    const state = { token: 't0', refreshes: 0 };
    const auth: AuthProvider = {
      credentials: () => ({ Authorization: `Bearer ${state.token}` }),
      refresh: () => {
        state.refreshes++;
        state.token = `t${state.refreshes}`;

        return Promise.resolve();
      },
    };
    // A credential that expires between operations: the first attempt of each
    // 401s, the retry after the refresh succeeds.
    const client = fakeClient((_config, attempt) => {
      if (attempt % 2 === 0) throw new HttpFailure(401);

      return { ok: true };
    });
    const rest = new RestTransport({ client, auth });

    await rest.execute({ meta: list, args: {} });
    expect(state.refreshes).toBe(1);
    expect(client.calls[1]?.headers).toEqual({ Authorization: 'Bearer t1' });

    await rest.execute({ meta: metrics, args: {} });

    // The in-flight refresh is cleared once it lands, so the second stampede
    // is its own -- otherwise every later 401 would adopt a settled promise
    // and retry against a credential that is still dead.
    expect(state.refreshes).toBe(2);
  });

  it('leaves a 401 alone when no refresh is configured', async () => {
    const client = guarded(() => 'never');
    const rest = new RestTransport({
      client,
      auth: { credentials: () => ({ Authorization: 'Bearer t0' }) },
    });

    await expect(rest.execute({ meta: list, args: {} })).rejects.toThrow('HTTP 401');
    expect(client.calls).toHaveLength(1);
  });
});
