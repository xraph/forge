import type {
  StreamConnect,
  StreamConnectContext,
  StreamConnection,
} from '../src/stream';
import type {
  RestClientLike,
  RestRequestConfig,
  Transport,
  TransportRequest,
} from '../src/transport';

/**
 * A failure shaped like the generated `HTTPError`.
 *
 * `statusCode` rather than `status` deliberately: that is the property the
 * generated client uses, and the retry classifier has to read it.
 */
export class HttpFailure extends Error {
  readonly statusCode: number;

  constructor(statusCode: number) {
    super(`HTTP ${statusCode}`);
    this.name = 'HTTPError';
    this.statusCode = statusCode;
  }
}

export interface FakeRestClient extends RestClientLike {
  readonly calls: RestRequestConfig[];
}

/** A stand-in for the generated `RESTClient`, recording what it was asked for. */
export function fakeClient(
  handler: (config: RestRequestConfig, attempt: number) => unknown,
): FakeRestClient {
  const calls: RestRequestConfig[] = [];

  return {
    calls,
    request<T>(config: RestRequestConfig): Promise<T> {
      const attempt = calls.length;
      calls.push({ ...config, headers: config.headers ? { ...config.headers } : undefined });

      return Promise.resolve()
        .then(() => handler(config, attempt))
        .then((value) => value as T);
    },
  };
}

export interface FakeTransport extends Transport {
  readonly calls: TransportRequest[];
}

/** A transport under the test's control, with no HTTP anywhere near it. */
export function fakeTransport(
  handler: (request: TransportRequest, call: number) => unknown,
): FakeTransport {
  const calls: TransportRequest[] = [];

  return {
    calls,
    execute(request: TransportRequest): Promise<unknown> {
      const call = calls.length;
      calls.push(request);

      return Promise.resolve().then(() => handler(request, call));
    },
  };
}

/**
 * A socket the test drives by hand.
 *
 * No `WebSocket`, no `EventSource`, no server. Every message and every drop in
 * these tests is a method call, which is the only way a reconnect test is
 * anything other than a sleep with an assertion after it.
 */
export interface FakeConnection extends StreamConnection {
  readonly context: StreamConnectContext;
  /** Push one message to whoever subscribed. */
  deliver(message: unknown): void;
  /** The socket went away without being asked to. */
  drop(reason?: unknown): void;
  /** A transport-level error, which is not a close. */
  fail(error: unknown): void;
  /** Whether `close` has been called on it. */
  readonly closed: boolean;
  /** Everything the manager sent back up this connection, in order. */
  readonly sent: readonly unknown[];
}

export interface FakeSockets {
  readonly connect: StreamConnect;
  /** Every connection ever opened, in order. */
  readonly opened: FakeConnection[];
  /** The most recently opened connection, optionally for one endpoint. */
  last(endpoint?: string): FakeConnection;
  /** How many are still open. */
  live(): number;
}

export function fakeSockets(onConnect?: (context: StreamConnectContext) => void): FakeSockets {
  const opened: FakeConnection[] = [];

  const connect: StreamConnect = (context) => {
    onConnect?.(context);

    let messages: ((message: unknown) => void) | undefined;
    let closes: ((reason?: unknown) => void) | undefined;
    let errors: ((error: unknown) => void) | undefined;
    let closed = false;
    const sent: unknown[] = [];

    const connection: FakeConnection = {
      context,
      get closed() {
        return closed;
      },
      get sent() {
        return sent;
      },
      send(message) {
        sent.push(message);
      },
      onMessage(handler) {
        messages = handler;
      },
      onClose(handler) {
        closes = handler;
      },
      onError(handler) {
        errors = handler;
      },
      close() {
        closed = true;
      },
      deliver(message) {
        messages?.(message);
      },
      drop(reason) {
        closed = true;
        closes?.(reason);
      },
      fail(error) {
        errors?.(error);
      },
    };

    opened.push(connection);

    return connection;
  };

  return {
    connect,
    opened,
    last(endpoint) {
      const matching =
        endpoint === undefined
          ? opened
          : opened.filter((connection) => connection.context.endpoint === endpoint);
      const connection = matching[matching.length - 1];

      if (connection === undefined) throw new Error(`no connection opened for ${endpoint ?? 'any'}`);

      return connection;
    },
    live() {
      return opened.filter((connection) => !connection.closed).length;
    },
  };
}

/** A promise the test resolves when it chooses. */
export interface Deferred<T> {
  readonly promise: Promise<T>;
  resolve(value: T): void;
  reject(error: unknown): void;
}

export function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;

  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });

  return { promise, resolve, reject };
}

/**
 * Let every already-queued microtask run.
 *
 * Not a sleep: this yields to the microtask queue a bounded number of times
 * and returns. Nothing in these tests waits on wall-clock time.
 */
export async function settleMicrotasks(): Promise<void> {
  for (let i = 0; i < 8; i++) await Promise.resolve();
}
