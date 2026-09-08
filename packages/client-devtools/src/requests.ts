import type { RequestEvent, RequestObserver, RequestReport } from '@forge-go/client-core';
import { argsKey } from './explain.js';

/**
 * What one request did, reduced to what is free to keep.
 *
 * No headers and no bodies. This is the one view in the package that is about
 * the wire, and it would be the easiest place to start retaining payloads --
 * which is exactly why it does not. What makes a network view worth having
 * here is not the bytes, which a browser already shows better than this ever
 * will. It is the policy those bytes were subject to: the attempt that was
 * never made, the backoff nobody saw, the refresh three requests shared.
 */
export interface RequestSnapshot {
  readonly id: number;
  /** `METHOD path`, as the manifest names the operation. */
  readonly operation: string;
  readonly method: string;
  /** The arguments, as a truncated key. Never the body itself. */
  readonly args: string;
  /** On the injected clock, at dispatch. */
  readonly at: number;
  /** Undefined while still in flight. */
  readonly duration: number | undefined;
  /** How many attempts were actually made. */
  readonly attempts: number;
  /**
   * How many this method was allowed at all.
   *
   * One for a method that is not idempotent, which is the fact that makes a
   * failed `POST` and a failed `GET` different and is invisible everywhere
   * else. A budget that went unused says the status was the reason instead.
   */
  readonly limit: number;
  readonly status: number | undefined;
  readonly outcome: 'pending' | 'ok' | 'failed';
  /** Each retry that was taken, with the delay it waited. */
  readonly retries: readonly {
    readonly attempt: number;
    readonly delay: number;
    readonly status: number | undefined;
  }[];
  /** How many times a 401 sent this request to the credential refresh. */
  readonly refreshes: number;
  /**
   * Whether it only ever waited on a refresh somebody else had started.
   *
   * The single flight, which no per-request view can infer: three 401s produce
   * one refresh, and only the transport knows which of them asked for it.
   */
  readonly joined: boolean;
  /**
   * Milliseconds spent waiting on the credential refresh.
   *
   * Zero for a request that never met a 401. This is what tells an auth stall
   * apart from a slow server: both look like one long request from outside,
   * and only this says which half of it was queueing behind a token.
   */
  readonly authMs: number;
}

/** The mutable half, while a request is still running. */
interface Live {
  readonly id: number;
  readonly operation: string;
  readonly method: string;
  readonly args: string;
  readonly at: number;
  duration: number | undefined;
  attempts: number;
  limit: number;
  status: number | undefined;
  outcome: 'pending' | 'ok' | 'failed';
  retries: { attempt: number; delay: number; status: number | undefined }[];
  refreshes: number;
  joined: boolean;
  authMs: number;
  /** When the refresh this request is waiting on began. */
  authAt: number | undefined;
}

/**
 * A bounded ring of requests, fed by the transport's observer.
 *
 * The same shape as `EventLog`: a fixed ring, an overwrite counter, and an
 * injected clock. A debugging tool that grows without bound in a long session
 * is a memory leak wearing a badge, and this one is fed by every request the
 * application makes.
 *
 * Entries are kept in dispatch order and a request is mutated in place as its
 * events arrive, so a row appears the moment a request goes out rather than
 * when it comes back. A request that never settles stays `pending` forever,
 * which is correct: that is what you are looking at the panel to find out.
 */
export class RequestLog {
  readonly capacity: number;

  private readonly ring: (Live | undefined)[];
  private readonly byId = new Map<number, Live>();
  private readonly now: () => number;
  private cursor = 0;
  private filled = 0;
  private overwritten = 0;

  constructor(capacity = 200, now: () => number = Date.now) {
    this.capacity = Math.max(1, Math.floor(capacity));
    this.ring = new Array<Live | undefined>(this.capacity);
    this.now = now;
  }

  /** How many requests have been overwritten by newer ones. */
  get dropped(): number {
    return this.overwritten;
  }

  /**
   * Hand this to `RestTransport`'s `observer` option.
   *
   * A bound property rather than a method so it survives being passed as a
   * value, which is the only way it is ever used.
   */
  readonly observer: RequestObserver = (report, event) => {
    this.record(report, event);
  };

  /** In dispatch order, oldest first. */
  entries(): readonly RequestSnapshot[] {
    const out: RequestSnapshot[] = [];

    for (let i = 0; i < this.filled; i++) {
      const live = this.ring[(this.cursor + this.capacity - this.filled + i) % this.capacity];

      if (live !== undefined) {
        const { authAt: _at, ...snapshot } = live;

        out.push({ ...snapshot, retries: [...live.retries] });
      }
    }

    return out;
  }

  clear(): void {
    this.ring.fill(undefined);
    this.byId.clear();
    this.cursor = 0;
    this.filled = 0;
    this.overwritten = 0;
  }

  private record(report: RequestReport, event: RequestEvent): void {
    if (event.type === 'start') {
      this.push({
        id: report.id,
        operation: `${report.meta.method.toUpperCase()} ${report.meta.path}`,
        method: report.meta.method.toUpperCase(),
        args: argsKey(report.args),
        at: this.now(),
        duration: undefined,
        attempts: 0,
        limit: event.limit,
        status: undefined,
        outcome: 'pending',
        retries: [],
        refreshes: 0,
        joined: false,
        authMs: 0,
        authAt: undefined,
      });

      return;
    }

    const live = this.byId.get(report.id);

    // Its slot has already been overwritten by newer traffic. Nothing to
    // update, and inventing a row for a request whose start is gone would put
    // an entry with no dispatch time in the middle of the list.
    if (live === undefined) return;

    switch (event.type) {
      case 'attempt':
        live.attempts = event.attempt + 1;
        break;

      case 'retry':
        live.retries.push({ attempt: event.attempt, delay: event.delay, status: event.status });
        break;

      case 'refresh':
        live.refreshes += 1;
        live.authAt = this.now();
        // Only ever waited: one request that started a flight is not a joiner,
        // even if a later 401 on the same request joined one.
        if (live.refreshes === 1) live.joined = event.joined;
        else if (!event.joined) live.joined = false;
        break;

      case 'refreshed':
        if (live.authAt !== undefined) {
          live.authMs += this.now() - live.authAt;
          live.authAt = undefined;
        }

        break;

      case 'settled':
        live.outcome = event.ok ? 'ok' : 'failed';
        live.status = event.status;
        live.duration = this.now() - live.at;
        this.byId.delete(report.id);
        break;
    }
  }

  private push(live: Live): void {
    const evicted = this.ring[this.cursor];

    if (evicted !== undefined) this.byId.delete(evicted.id);
    if (this.filled === this.capacity) this.overwritten++;
    else this.filled++;

    this.ring[this.cursor] = live;
    this.byId.set(live.id, live);
    this.cursor = (this.cursor + 1) % this.capacity;
  }
}
