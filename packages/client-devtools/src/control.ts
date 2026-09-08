import type { Sleep, Transport, TransportRequest } from '@forge-go/client-core';

/**
 * The conditions half of the panel, and the reason it is a decorator rather
 * than an observer.
 *
 * `RequestLog` watches; this one intervenes. Offline has to fail *before* the
 * inner transport rather than after, or the request still reaches the server
 * and the one thing offline is meant to prove is exactly the thing it did not
 * prove. So this wraps `Transport` and the log observes `RestTransport`, and
 * the two seams are separate on purpose: neither can be built out of the other.
 *
 * Wrapped *outside* the retry loop, which is the right place for all three.
 * These simulate what the application sees, so a latency is a delay on the
 * operation and not on each attempt of it, and going offline fails the whole
 * operation rather than one attempt that then gets retried into a second
 * failure.
 */
export type NetworkMode = 'online' | 'slow' | 'offline';

/** How much a slow connection adds, per request, before anything else. */
const SLOW = 400;

export interface TransportControlsOptions {
  /** Injected so a test does not sleep. Defaults to a real timer. */
  readonly sleep?: Sleep;
  /** What a slow connection costs. Defaults to 400ms. */
  readonly slow?: number;
}

/**
 * A synthetic failure, shaped like the generated client's.
 *
 * `statusCode` and the `HTTPError` name are what `statusOf` and `retryable`
 * read, so an armed failure is subject to the same retry policy a real one
 * would be. A synthetic error the policy could not classify would make the
 * network tab tell a story about this request that no real request tells.
 */
export class SyntheticFailure extends Error {
  readonly statusCode: number;

  constructor(statusCode: number) {
    super(`[forge] synthetic failure ${String(statusCode)}, armed from the devtools panel`);
    this.name = 'HTTPError';
    this.statusCode = statusCode;
  }
}

export class TransportControls {
  /** Read and written by the panel. */
  mode: NetworkMode = 'online';
  /** Extra milliseconds before every request. */
  latency = 0;

  private readonly sleep: Sleep;
  private readonly slow: number;
  private next: number | undefined;

  constructor(options: TransportControlsOptions = {}) {
    this.sleep = options.sleep ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
    this.slow = options.slow ?? SLOW;
  }

  /** Whether a synthetic failure is waiting to be spent. */
  get armed(): boolean {
    return this.next !== undefined;
  }

  /**
   * Fail the next request, once.
   *
   * Disarmed on use rather than left on. A toggle that stayed armed would make
   * every subsequent request fail, which is indistinguishable from the bug you
   * armed it to reproduce.
   */
  failNext(status = 500): void {
    this.next = status;
  }

  /** Give up on the armed failure without spending it. */
  disarm(): void {
    this.next = undefined;
  }

  /** Wrap the real transport. Pass the result to `QueryCache`. */
  wrap(inner: Transport): Transport {
    return {
      execute: async (request: TransportRequest): Promise<unknown> => {
        if (this.mode === 'offline') {
          throw new Error('[forge] offline, switched on from the devtools panel');
        }

        const armed = this.next;

        if (armed !== undefined) {
          this.next = undefined;

          throw new SyntheticFailure(armed);
        }

        const delay = this.latency + (this.mode === 'slow' ? this.slow : 0);

        if (delay > 0) await this.sleep(delay);

        return inner.execute(request);
      },
    };
  }
}

/** The three sources of a refetch nobody asked for. */
export type RevalidationSource = 'focus' | 'reconnect' | 'poll';

/**
 * How to start one revalidation source. Returns how to stop it again.
 *
 * Exactly the shape `revalidateOnFocus`, `revalidateOnReconnect` and `poll`
 * already have, so registering one is a thunk around the call the application
 * was making anyway.
 */
export type StartRevalidation = () => () => void;

/**
 * The revalidation toggles.
 *
 * These are wired once at setup and then invisible, and half of "why did this
 * refetch" is one of them. The panel cannot toggle a subscription it was never
 * handed, and it must not install one behind the application's back either --
 * a panel that started polling an endpoint because somebody pressed a button
 * would be generating the traffic it is supposed to be explaining. So the
 * application registers how to start each source it actually uses, and this
 * holds the stop.
 *
 * Registering does not start anything. The application decides the initial
 * state by calling `toggle` for whatever it already had running.
 */
export class Revalidation {
  private readonly sources: Partial<Record<RevalidationSource, StartRevalidation>>;
  private readonly running = new Map<RevalidationSource, () => void>();

  constructor(sources: Partial<Record<RevalidationSource, StartRevalidation>>) {
    this.sources = sources;
  }

  /** Whether the application wired this source at all. */
  registered(source: RevalidationSource): boolean {
    return this.sources[source] !== undefined;
  }

  enabled(source: RevalidationSource): boolean {
    return this.running.has(source);
  }

  /** Start it if it is off, stop it if it is on. A no-op if unregistered. */
  toggle(source: RevalidationSource): void {
    const stop = this.running.get(source);

    if (stop !== undefined) {
      this.running.delete(source);
      stop();

      return;
    }

    const start = this.sources[source];

    if (start === undefined) return;

    this.running.set(source, start());
  }

  /** Stop everything this started. Nothing it did not start is touched. */
  dispose(): void {
    for (const stop of this.running.values()) stop();

    this.running.clear();
  }
}
