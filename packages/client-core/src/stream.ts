import { microtaskScheduler } from './invalidate.js';
import type { Scheduler } from './invalidate.js';
import { realSleep } from './transport.js';
import type { Sleep } from './transport.js';

/** What a stream message does to the cache. The IR's `StreamIntent`. */
export type StreamIntent = 'upsert' | 'patch' | 'evict';

/**
 * One row of the generated manifest's `streams` table.
 *
 * Emitted by `writeStreams` in `internal/client/generators/typescript/opsmanifest.go`,
 * one per `(channel, message)` pair, and structurally what that generator
 * produces: the `as const` literal types it emits are assignable to these.
 *
 * `intent` is a build-time fact -- the manifest and the client are generated
 * together -- but it is still validated at runtime, because a hand-written or
 * hand-edited manifest is the case where a typo would otherwise apply the
 * wrong operation to a real entity.
 */
export interface StreamBinding {
  /** The endpoint path the channel is served on, e.g. `/ws/orders`. */
  readonly channel: string;
  /** The message name, e.g. `order.created`. */
  readonly message: string;
  /** The typename the payload carries, e.g. `Order`. */
  readonly entity: string;
  readonly intent: StreamIntent;
  /** Tag templates this message invalidates, unresolved. */
  readonly invalidates: readonly string[];
}

/**
 * One socket, as the subscription manager drives it.
 *
 * Deliberately three methods, all of which the generated `websocket.ts` and
 * `sse.ts` clients already have under other names -- their `onMessage`,
 * `onClose` and `disconnect`. Adapting either is a four-line object literal,
 * which is the point: this package does not open sockets, and nothing in it
 * imports `WebSocket` or `EventSource`.
 *
 * The handlers are registered exactly once, immediately after the connection is
 * created, and are never removed. A connection is used until it closes and then
 * discarded; reconnection produces a *new* connection rather than reviving this
 * one. That is what lets the manager ignore a late frame from a socket it has
 * already replaced, by identity rather than by a flag the connection would have
 * to maintain.
 */
export interface StreamConnection {
  /** Deliver a decoded-or-raw message. Whatever the transport hands over. */
  onMessage(handler: (message: unknown) => void): void;
  /** The socket went away. The manager decides whether to reopen. */
  onClose(handler: (reason?: unknown) => void): void;
  /** Transport-level failure. Reported, never fatal. */
  onError?(handler: (error: unknown) => void): void;
  /**
   * Send one message back up the socket, if this transport can.
   *
   * Optional because most of what the runtime does is one-directional and a
   * receive-only transport is a legitimate thing to hand the manager. It is
   * here for `keepalive`: the streaming extension judges a connection's
   * liveness purely by inbound traffic, so a client that only ever listens has
   * to say something or be closed. See `forgeKeepalive`.
   */
  send?(message: unknown): void;
  /** Close for good. The manager will not use this connection again. */
  close(): void;
}

/**
 * Decide whether an inbound message is a keepalive, and what to answer it with.
 *
 * Returning `undefined` means "not a keepalive", which is the answer for
 * virtually every frame. Anything else is sent straight back up the socket.
 */
export type Keepalive = (message: unknown) => unknown;

/**
 * The default: answer the streaming extension's ping.
 *
 * `extensions/streaming` runs a heartbeat per connection that closes it once
 * `time.Since(GetLastActivity()) > PingInterval + PongTimeout`, 40 seconds at
 * the defaults. `UpdateActivity` is called in exactly one place, the read loop,
 * so only a message from the client moves it. The ping the server sends is an
 * ordinary JSON application message rather than a WebSocket control frame, and
 * that distinction is the whole problem: browsers answer control pings
 * automatically and expose no API to send one, so nothing about a browser
 * subscription is visible to that heartbeat unless the application answers.
 *
 * Without this, a page that subscribes and then only listens is disconnected
 * every forty seconds, reconnected by this manager, and recovered by
 * `StreamBinder` with a refetch. It looks like it works. It is polling.
 */
export const forgeKeepalive: Keepalive = (message) => {
  if (typeof message !== 'object' || message === null) return undefined;

  const frame = message as { type?: unknown; event?: unknown };

  if (frame.type !== 'system' || frame.event !== 'ping') return undefined;

  return { type: 'system', event: 'pong' };
};

/**
 * The part of `EventTarget` the manager uses to hear about the network.
 *
 * Structural rather than the DOM type so a test can pass a plain object, and so
 * importing this module on a server does not depend on `EventTarget` existing.
 */
export interface EventTargetLike {
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
}

/**
 * An open WebTransport session, narrowed to what the adapter below reads.
 *
 * Structural rather than the DOM's `WebTransport`, for the reason every other
 * seam in this package is structural: naming the global would put a `lib.dom`
 * dependency in a package that also runs on a server, and the adapter needs
 * three members out of that class either way.
 */
export interface WebTransportLike {
  readonly datagrams: { readonly readable: ReadableStream<Uint8Array> };
  /** Resolves when the session ends, from either side. */
  readonly closed: Promise<unknown>;
  close(): void;
}

export interface WebTransportConnectionOptions {
  /**
   * Turn one datagram into a message. UTF-8 JSON by default, which is what
   * the streaming extension sends.
   *
   * Replace it for a binary envelope. A throw is reported through `onError`
   * and the datagram is dropped; see the read loop.
   */
  readonly parse?: (bytes: Uint8Array) => unknown;
}

/**
 * Built on first use, never at module scope.
 *
 * `dev-guard.test.ts` evaluates this bundle in a realm with no `process` and
 * no `TextDecoder`, which is the check that keeps the module importable
 * anywhere. A decoder constructed at module scope throws on import for every
 * consumer, including the ones that never touch WebTransport.
 */
let datagramText: TextDecoder | undefined;

function decodeDatagramJSON(bytes: Uint8Array): unknown {
  datagramText ??= new TextDecoder();

  return JSON.parse(datagramText.decode(bytes));
}

/**
 * Adapt an already-open WebTransport session to a `StreamConnection`.
 *
 * The session is passed in rather than opened here, exactly as
 * `SubscriptionManagerOptions.connect` is handed an already-open socket, so
 * this stays on the right side of "nothing in this package opens a
 * connection".
 *
 * A WebSocket adapter is the four-line object literal `StreamConnection`
 * describes, because `onMessage` is already a callback. A datagram source is
 * not: it is a pull loop over a `ReadableStream`, with a reader to release and
 * a decode that can fail per datagram. That asymmetry is the whole reason this
 * is written once here instead of left to each caller.
 *
 * ```ts
 * const manager = new SubscriptionManager({
 *   connect: () => {
 *     const wt = new WebTransport(`${baseURL}/wt/orders`);
 *     return webTransportConnection(wt);
 *   },
 * });
 * ```
 *
 * `new WebTransport(...)` is usable before `ready` settles, so the caller does
 * not have to await it: datagrams simply do not arrive until the handshake
 * completes, and a handshake that fails rejects `closed`, which this reports
 * and then treats as a drop like any other.
 */
export function webTransportConnection(
  transport: WebTransportLike,
  options: WebTransportConnectionOptions = {},
): StreamConnection {
  const parse = options.parse ?? decodeDatagramJSON;

  let message: ((value: unknown) => void) | undefined;
  let closed: ((reason?: unknown) => void) | undefined;
  let failed: ((error: unknown) => void) | undefined;

  // Both a deliberate `close()` and a peer disconnect resolve `transport.
  // closed`, and the read loop ends on either as well, so "the session is
  // over" arrives from two places for one event. Exactly one of them gets to
  // report it.
  let over = false;

  const finish = (reason?: unknown): void => {
    if (over) return;

    over = true;
    closed?.(reason);
  };

  transport.closed.then(
    (reason) => finish(reason),
    (error) => {
      failed?.(error);
      finish(error);
    },
  );

  const read = async (): Promise<void> => {
    const reader = transport.datagrams.readable.getReader();

    try {
      for (;;) {
        const next = await reader.read();

        if (next.done) break;
        if (next.value === undefined) continue;

        // A datagram that does not parse is one bad packet, not the end of
        // the session. Reporting and continuing is what keeps a single
        // malformed frame from taking the subscription down with it.
        try {
          message?.(parse(next.value));
        } catch (error) {
          failed?.(error);
        }
      }
    } catch (error) {
      failed?.(error);
    } finally {
      reader.releaseLock();
      finish();
    }
  };

  return {
    onMessage(handler) {
      message = handler;

      // Started here rather than at construction: the manager registers its
      // handlers synchronously after `connect` returns, and a loop that ran
      // first could read a datagram with nowhere to put it.
      void read();
    },
    onClose(handler) {
      closed = handler;
    },
    onError(handler) {
      failed = handler;
    },
    close() {
      // Suppressed rather than reported. The manager already ignores a close
      // it asked for (`onClose` checks `socket.disposed`), but a
      // `StreamConnection` is a general contract and a caller that reconnects
      // on every `onClose` would otherwise reopen the socket it just shut.
      over = true;
      transport.close();
    },
  };
}

/** Everything the factory is told about the socket it is being asked for. */
export interface StreamConnectContext {
  /** The endpoint this socket serves. */
  readonly endpoint: string;
  /** The channels multiplexed over it at the moment it is opened. */
  readonly channels: readonly string[];
  /** Who it belongs to. See `SubscriptionManagerOptions.principal`. */
  readonly principal: unknown;
  /** How many times this socket has been opened. 0 is the first open. */
  readonly attempt: number;
}

/** Open one socket. Called again, with a higher `attempt`, on reconnect. */
export type StreamConnect = (context: StreamConnectContext) => StreamConnection;

/** How far apart reconnect attempts are, and how many there are. */
export interface BackoffPolicy {
  /** Total attempts after a drop before the manager gives up. */
  readonly attempts?: number;
  readonly baseDelay?: number;
  readonly maxDelay?: number;
}

/**
 * Work out what to listen on for the network coming back.
 *
 * `false` is off. An explicit target is used as given. Anything else looks at
 * `globalThis`, which has `addEventListener` in a browser and in a worker, and
 * does not during a server render -- so importing this module on a server
 * registers nothing and a Node test sees no global listener it did not ask for.
 */
function resolveReviveTarget(
  revive: EventTargetLike | false | undefined,
): EventTargetLike | undefined {
  if (revive === false) return undefined;
  if (revive !== undefined) return revive;

  const candidate = globalThis as unknown as Partial<EventTargetLike>;

  return typeof candidate.addEventListener === 'function' &&
    typeof candidate.removeEventListener === 'function'
    ? (candidate as EventTargetLike)
    : undefined;
}

export interface SubscriptionManagerOptions {
  readonly connect: StreamConnect;
  /**
   * Which socket a channel rides on.
   *
   * The default is one socket per channel, which is what the generated clients
   * do -- a `WebSocketClient` is constructed per path. An application whose
   * server multiplexes several channels over one endpoint returns the same
   * string for all of them, and they share a connection and a ref count.
   */
  readonly endpointOf?: (channel: string) => string;
  /**
   * Who the sockets belong to, read at open time.
   *
   * A socket outliving an identity change would push the previous principal's
   * entities into the new session's store, which is the same defect the entity
   * store is partitioned to prevent and is not made less real by arriving over
   * a socket. `repartition` is how the change is acted on.
   */
  readonly principal?: () => unknown;
  /**
   * A socket that had dropped is open again, and frames were missed while it
   * was down. Assigned by `StreamBinder`; see its `recover`.
   */
  onReconnect?: (endpoint: string, channels: readonly string[]) => void;
  /** Defaults to a real timer. Tests pass `manualClock().sleep`. */
  readonly sleep?: Sleep;
  /** Jitter source. Defaults to `Math.random`. */
  readonly random?: () => number;
  readonly backoff?: BackoffPolicy;
  /**
   * How to answer a server keepalive, or `false` to answer nothing.
   *
   * Defaults to `forgeKeepalive`, which answers the streaming extension's ping
   * and ignores everything else. A replacement replaces it rather than adding
   * to it: an application that speaks a different keepalive owns the whole
   * decision.
   *
   * The reply goes out through `StreamConnection.send`, so a receive-only
   * transport silently answers nothing however this is set.
   */
  readonly keepalive?: Keepalive | false;
  /**
   * Where to listen for the network coming back, or `false` for nowhere.
   *
   * Defaults to `globalThis` when it looks like an event target, which is a
   * browser and not a server render. The reconnect budget is finite, so an
   * outage longer than it abandons the socket for the life of the page unless
   * something notices the machine came back. `online` is that something.
   */
  readonly revive?: EventTargetLike | false;
  /**
   * When a socket nobody is subscribed to is actually closed.
   *
   * Not immediately, and this is the whole of the StrictMode fix. React
   * development double-invokes effects, so every subscriber is torn down and
   * put back before the browser does anything else; a manager that closed on
   * the count reaching zero would close the socket and open a second one on
   * every mount, and -- worse, because it only reproduces in development -- a
   * `live` query whose remount raced the close would silently stop updating.
   * Deferring the close by one turn makes the phantom unmount free.
   */
  readonly release?: Scheduler;
  readonly onError?: (error: unknown, context: string) => void;
}

/** What a subscriber is handed. `channel` is which of a socket's it came on. */
export type FrameHandler = (message: unknown, channel: string) => void;

/** One multiplexed channel on a socket, and how many subscribers it has. */
export interface ChannelSnapshot {
  readonly channel: string;
  /** Ref count for this channel alone. The socket's `refs` is their sum. */
  readonly handlers: number;
}

/**
 * One socket, copied out for an inspector.
 *
 * A copy rather than the live `Socket`, so reading it cannot reach the
 * connection, the handler sets or the reconnect flags -- an inspector that
 * could `close()` what it is looking at is not an inspector.
 *
 * "Which sockets are open, for which channels, with what ref count" is the
 * question a connection panel exists to answer, and it is otherwise
 * unanswerable from outside: `size` counts them and `connected` tests one
 * endpoint, and neither can enumerate.
 */
export interface SocketSnapshot {
  readonly endpoint: string;
  /** The identity this socket was opened for. See `repartition`. */
  readonly principal: unknown;
  /** False between a drop and a reopen, and before the first open. */
  readonly connected: boolean;
  /** Outstanding subscriptions across every channel. */
  readonly refs: number;
  /** How many times this socket has been opened. Above 1 means it dropped. */
  readonly opens: number;
  /** Consecutive failed reconnects. Reset by any traffic at all. */
  readonly attempt: number;
  readonly reconnecting: boolean;
  /** Its last subscriber went away and the deferred close is pending. */
  readonly closing: boolean;
  readonly channels: readonly ChannelSnapshot[];
}

/** One socket the manager is holding open. */
interface Socket {
  readonly endpoint: string;
  /** The identity this socket was opened for. */
  principal: unknown;
  /** The live connection, or undefined between a drop and a reopen. */
  connection: StreamConnection | undefined;
  /** Subscribers, by channel. A channel with no handlers is deleted. */
  readonly channels: Map<string, Set<FrameHandler>>;
  /** How many subscriptions are outstanding across every channel. */
  refs: number;
  /** Consecutive failed reconnects. Reset by any sign of life. */
  attempt: number;
  /** How many times this socket has been opened. */
  opens: number;
  /** Queued for a deferred close, and cancelled by a subscriber arriving. */
  closing: boolean;
  /** Closed for good. Frames and close events from it are ignored. */
  disposed: boolean;
  /** A reconnect is waiting on the clock. */
  reconnecting: boolean;
}

/**
 * Ref-counted sockets: one per `(endpoint, principal)`, multiplexed by channel,
 * closed on the last release.
 *
 * Everything above the transport -- decoding, binding lookup, what a frame does
 * to the cache -- is `StreamBinder`'s. This class knows only that somebody
 * wants messages from a channel, and it owns exactly three things that are easy
 * to get wrong:
 *
 * - **Sharing.** Ten components subscribing to `/ws/orders` are one socket. A
 *   connection count that grows with the render tree is the failure mode this
 *   exists to prevent.
 * - **Surviving a phantom unmount.** See `release`.
 * - **Reconnecting on a schedule, and saying so.** A reconnect is not just a
 *   new socket: the client missed frames while it was down and is now wrong in
 *   a way nothing about it looks wrong. `onReconnect` is how that is reported,
 *   and it fires after a *reopen*, never after the first open.
 */
export class SubscriptionManager {
  /** Assigned by the binder. See `SubscriptionManagerOptions.onReconnect`. */
  onReconnect: ((endpoint: string, channels: readonly string[]) => void) | undefined;

  private readonly sockets = new Map<string, Socket>();
  private readonly connect: StreamConnect;
  private readonly endpointOf: (channel: string) => string;
  private readonly principal: () => unknown;
  private readonly sleep: Sleep;
  private readonly random: () => number;
  private readonly attempts: number;
  private readonly baseDelay: number;
  private readonly maxDelay: number;
  private readonly release: Scheduler;
  private readonly onError: ((error: unknown, context: string) => void) | undefined;
  private readonly keepalive: Keepalive | undefined;
  private readonly reviveTarget: EventTargetLike | undefined;
  private readonly onOnline: (() => void) | undefined;

  /**
   * Sockets whose last subscriber went away, awaiting the deferred close.
   *
   * A set plus one scheduled callback rather than one callback per socket: the
   * injected `Scheduler` is a single slot (see `manualScheduler`), so N
   * independent schedules would lose all but the last.
   */
  private readonly releasing = new Set<Socket>();
  private releaseScheduled = false;

  constructor(options: SubscriptionManagerOptions) {
    this.connect = options.connect;
    this.endpointOf = options.endpointOf ?? ((channel) => channel);
    this.principal = options.principal ?? (() => undefined);
    this.sleep = options.sleep ?? realSleep;
    this.random = options.random ?? Math.random;
    this.attempts = options.backoff?.attempts ?? 10;
    this.baseDelay = options.backoff?.baseDelay ?? 500;
    this.maxDelay = options.backoff?.maxDelay ?? 30000;
    this.release = options.release ?? microtaskScheduler;
    this.onError = options.onError;
    this.onReconnect = options.onReconnect;
    this.keepalive = options.keepalive === false ? undefined : (options.keepalive ?? forgeKeepalive);

    this.reviveTarget = resolveReviveTarget(options.revive);
    this.onOnline =
      this.reviveTarget === undefined
        ? undefined
        : () => {
            this.retry();
          };

    if (this.reviveTarget !== undefined && this.onOnline !== undefined) {
      this.reviveTarget.addEventListener('online', this.onOnline);
    }
  }

  /** How many sockets are held, open or reconnecting. */
  get size(): number {
    return this.sockets.size;
  }

  /** Whether this endpoint currently has an open connection. */
  connected(endpoint: string): boolean {
    return this.sockets.get(endpoint)?.connection !== undefined;
  }

  /**
   * Which endpoint a channel resolves to under this manager's `endpointOf`.
   *
   * Exposed because `StreamBinder` has to settle a deferred recovery against
   * the same endpoint `onReconnect` reported it under, and a control frame
   * only carries the channel it arrived on. Channel and endpoint coincide
   * under the default `endpointOf`, which is the identity function, but not
   * under one that multiplexes several channels onto a single socket -- and
   * guessing they are interchangeable there is exactly the assumption that
   * would settle the wrong endpoint's recovery.
   */
  endpointFor(channel: string): string {
    return this.endpointOf(channel);
  }

  /**
   * Subscribe to a channel. The returned function is the release.
   *
   * Idempotent in the direction that matters: releasing twice decrements once.
   */
  subscribe(channel: string, handler: FrameHandler): () => void {
    const endpoint = this.endpointOf(channel);
    const socket = this.socketFor(endpoint);

    // Cancels a deferred close. The socket the phantom unmount was about to
    // close is the one this mount wants, and it is still open.
    socket.closing = false;
    this.releasing.delete(socket);

    let handlers = socket.channels.get(channel);

    if (handlers === undefined) {
      handlers = new Set<FrameHandler>();
      socket.channels.set(channel, handlers);
    }

    handlers.add(handler);
    socket.refs++;

    if (socket.connection === undefined && !socket.reconnecting) this.open(socket);

    let released = false;

    return () => {
      if (released) return;

      released = true;
      socket.refs--;

      const current = socket.channels.get(channel);

      if (current !== undefined) {
        current.delete(handler);

        if (current.size === 0) socket.channels.delete(channel);
      }

      if (socket.refs > 0 || socket.disposed) return;

      socket.closing = true;
      this.releasing.add(socket);
      this.scheduleRelease();
    };
  }

  /**
   * Close every socket that no longer belongs to the current principal, and
   * reopen the ones that still have subscribers.
   *
   * Called after an identity change. Reopening rather than merely closing is
   * deliberate: a `live` query mounted across a login change is still mounted,
   * and dropping its socket would leave it looking connected and receiving
   * nothing. The reopen goes through the same path as a reconnect, so gap
   * recovery fires and the query refetches under the new identity.
   */
  repartition(): void {
    const principal = this.principal();

    for (const socket of [...this.sockets.values()]) {
      if (socket.principal === principal) continue;

      const channels = [...socket.channels.keys()];
      const handlers = new Map(
        [...socket.channels].map(([channel, set]) => [channel, new Set(set)] as const),
      );
      const refs = socket.refs;

      this.dispose(socket);

      if (refs === 0) continue;

      const replacement = this.socketFor(socket.endpoint);
      replacement.refs = refs;

      for (const [channel, set] of handlers) replacement.channels.set(channel, set);

      this.open(replacement);
      this.onReconnect?.(replacement.endpoint, channels);
    }
  }

  /** Close everything, now. Subscriptions are not restored by a later call. */
  closeAll(): void {
    for (const socket of [...this.sockets.values()]) this.dispose(socket);

    this.releasing.clear();

    if (this.reviveTarget !== undefined && this.onOnline !== undefined) {
      this.reviveTarget.removeEventListener('online', this.onOnline);
    }
  }

  /** Run the deferred closes now, whatever the scheduler had planned. */
  flushReleases(): void {
    this.releaseScheduled = false;

    for (const socket of [...this.releasing]) {
      if (socket.closing && socket.refs === 0) this.dispose(socket);
    }

    this.releasing.clear();
  }

  private socketFor(endpoint: string): Socket {
    const principal = this.principal();
    const existing = this.sockets.get(endpoint);

    if (existing !== undefined) {
      // A socket opened for somebody else is not this subscriber's socket, and
      // adopting it would deliver the previous session's frames into the new
      // one's store.
      if (existing.principal === principal) return existing;

      this.dispose(existing);
    }

    const socket: Socket = {
      endpoint,
      principal,
      connection: undefined,
      channels: new Map(),
      refs: 0,
      attempt: 0,
      opens: 0,
      closing: false,
      disposed: false,
      reconnecting: false,
    };

    this.sockets.set(endpoint, socket);

    return socket;
  }

  private open(socket: Socket): void {
    if (socket.disposed) return;

    socket.principal = this.principal();
    socket.reconnecting = false;

    let connection: StreamConnection;

    try {
      connection = this.connect({
        endpoint: socket.endpoint,
        channels: [...socket.channels.keys()],
        principal: socket.principal,
        attempt: socket.opens,
      });
    } catch (error) {
      this.onError?.(error, `stream connect ${socket.endpoint}`);
      // A factory that throws is a drop that happened before the socket
      // existed, and is retried on the same schedule rather than abandoning
      // the subscription.
      void this.reconnect(socket);

      return;
    }

    socket.connection = connection;
    socket.opens++;

    connection.onMessage((message) => {
      // A frame from a connection this socket has already replaced, or from one
      // whose socket is gone. Late and belonging to nothing.
      if (socket.disposed || socket.connection !== connection) return;

      // Any traffic at all is proof the endpoint is healthy, so the next drop
      // starts its backoff from the beginning rather than from wherever the
      // previous outage left it.
      socket.attempt = 0;

      this.answer(connection, message);

      this.deliver(socket, message);
    });

    connection.onClose(() => {
      if (socket.disposed || socket.connection !== connection) return;

      socket.connection = undefined;

      if (socket.refs === 0) return;

      void this.reconnect(socket);
    });

    connection.onError?.((error) => {
      this.onError?.(error, `stream ${socket.endpoint}`);
    });
  }

  /**
   * Wait, then reopen, then report the gap.
   *
   * The delay is taken through the injected `Sleep`, so a test drives it with
   * `manualClock()` and no reconnect test ever sleeps. `onReconnect` fires
   * after the reopen and only here, which is what makes "first connect" and
   * "reconnect after a drop" distinguishable -- the first has no gap to recover
   * from, and invalidating the channel's tags on it would refetch every live
   * query a moment after it loaded.
   */
  private async reconnect(socket: Socket): Promise<void> {
    if (socket.disposed || socket.reconnecting) return;

    socket.reconnecting = true;

    while (!socket.disposed && socket.refs > 0) {
      if (socket.attempt >= this.attempts) {
        socket.reconnecting = false;
        this.onError?.(
          new Error(
            `[forge] gave up reconnecting to ${socket.endpoint} after ${String(this.attempts)} attempts`,
          ),
          `stream ${socket.endpoint}`,
        );

        return;
      }

      const delay = this.backoff(socket.attempt);
      socket.attempt++;

      await this.sleep(delay);

      if (socket.disposed || socket.refs === 0) {
        socket.reconnecting = false;

        return;
      }

      const channels = [...socket.channels.keys()];

      this.open(socket);

      if (socket.connection === undefined) continue;

      this.onReconnect?.(socket.endpoint, channels);

      return;
    }

    socket.reconnecting = false;
  }

  /**
   * Answer a keepalive, if this message is one and this transport can reply.
   *
   * Answering happens before delivery and never instead of it. A keepalive is
   * still a frame the application is entitled to see, and a decoder that
   * returns `undefined` for it already drops it without a warning.
   */
  private answer(connection: StreamConnection, message: unknown): void {
    if (this.keepalive === undefined || connection.send === undefined) return;

    try {
      const reply = this.keepalive(message);

      if (reply !== undefined) connection.send(reply);
    } catch (error) {
      // Both the policy and the send are the application's code, and either
      // throwing means the same thing here: no answer went out. A failed send
      // is the socket going away, which the close handler already owns.
      // Reported, never rethrown, because this runs inside the transport's own
      // message callback.
      this.onError?.(error, 'stream keepalive');
    }
  }

  /**
   * Reopen anything that gave up reconnecting.
   *
   * The backoff budget is finite on purpose, so a server that is genuinely gone
   * stops being hammered. But "gave up" was permanent, and the common reason to
   * exhaust ten attempts is a laptop that slept or a tunnel that dropped, both
   * of which end. This is how they end: the attempt counter is cleared and the
   * socket gets a full budget again.
   *
   * A socket that is connected, still counting down, or has no subscribers left
   * is not touched, so calling this on every `online` event costs nothing.
   */
  retry(): void {
    for (const socket of [...this.sockets.values()]) {
      if (socket.disposed || socket.reconnecting) continue;
      if (socket.connection !== undefined) continue;
      if (socket.refs === 0) continue;

      socket.attempt = 0;
      void this.reconnect(socket);
    }
  }

  private deliver(socket: Socket, message: unknown): void {
    // Copied before iterating: a handler releasing its own subscription -- a
    // component unmounting in response to a frame -- would otherwise mutate the
    // set being walked.
    for (const [channel, handlers] of [...socket.channels]) {
      for (const handler of [...handlers]) {
        try {
          handler(message, channel);
        } catch (error) {
          // One subscriber must not cost the others their frame.
          this.onError?.(error, `stream handler ${channel}`);
        }
      }
    }
  }

  private dispose(socket: Socket): void {
    socket.disposed = true;
    socket.closing = false;
    socket.reconnecting = false;
    this.releasing.delete(socket);

    const connection = socket.connection;
    socket.connection = undefined;

    if (this.sockets.get(socket.endpoint) === socket) this.sockets.delete(socket.endpoint);

    if (connection === undefined) return;

    try {
      connection.close();
    } catch (error) {
      this.onError?.(error, `stream close ${socket.endpoint}`);
    }
  }

  private scheduleRelease(): void {
    if (this.releaseScheduled) return;

    this.releaseScheduled = true;
    this.release(() => {
      if (this.releaseScheduled) this.flushReleases();
    });
  }

  /**
   * Exponential backoff with jitter, the same shape `RestTransport` uses.
   *
   * Jitter matters more here than there: a server restart drops every client at
   * the same instant, and an undithered schedule brings all of them back
   * together on the first attempt and again on each retry.
   */
  private backoff(attempt: number): number {
    const delay = Math.min(this.maxDelay, this.baseDelay * 2 ** attempt);

    return delay / 2 + this.random() * (delay / 2);
  }
}

/**
 * Every socket a manager holds, copied out. See `SocketSnapshot`.
 *
 * Pure: it opens no connection, closes none, and cancels no deferred release.
 * Calling it in a render loop is wasteful and not wrong.
 *
 * A free function rather than a method on `SubscriptionManager`, and the reason
 * is measured rather than stylistic. A class method is part of the class and
 * cannot be tree-shaken, so as a method this cost 120 B gzipped inside the
 * streams layer -- paid by every live application, for a connection panel
 * almost none of them will ever open. As a free function it is paid for by the
 * bundles that import it and by nothing else.
 *
 * That is also why it reaches `sockets` through a cast. The field stays
 * private; this function is declared in the file that owns it, so the cast is
 * confined to the one module that already knows the shape and no consumer gains
 * access to anything. The alternative -- making the field public so a function
 * three lines below could read it -- would hand every caller a mutable map of
 * live connections in exchange for nothing.
 */
export function socketSnapshot(manager: SubscriptionManager): readonly SocketSnapshot[] {
  const held = (manager as unknown as { readonly sockets: ReadonlyMap<string, Socket> }).sockets;
  const out: SocketSnapshot[] = [];

  for (const socket of held.values()) {
    const channels: ChannelSnapshot[] = [];

    for (const [channel, handlers] of socket.channels) {
      channels.push({ channel, handlers: handlers.size });
    }

    out.push({
      endpoint: socket.endpoint,
      principal: socket.principal,
      connected: socket.connection !== undefined,
      refs: socket.refs,
      opens: socket.opens,
      attempt: socket.attempt,
      reconnecting: socket.reconnecting,
      closing: socket.closing,
      channels,
    });
  }

  return out;
}
