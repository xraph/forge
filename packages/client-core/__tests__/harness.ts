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
