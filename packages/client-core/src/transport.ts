import type { TagContext } from './tags.js';

/**
 * One operation, exactly as the generated `ops.ts` declares it.
 *
 * Structurally the generated `OperationMeta`, plus `security` -- which the
 * generator does not emit yet. It is declared here because the credential
 * attach below is specified as being *per the endpoint's declared scheme*, and
 * an `AuthProvider` that receives the meta can dispatch on it the day the
 * generator starts emitting it, with no change to this package.
 */
export interface OperationMeta {
  readonly method: string;
  readonly path: string;
  /** The entity this operation's cache contract is about. */
  readonly entity?: string;
  /**
   * The typename of the response DOCUMENT -- of its elements when the response
   * is a bare array. This is what indexes the entities table when normalizing
   * a response, and it is not interchangeable with `entity`.
   *
   * They coincide for `GET /orders/{id}` and for a bare `[]Order`, and they
   * differ for every enveloped read: `PageOrder{items: [Order], total}` has
   * `entity: 'Order'` and `rootType: 'PageOrder'`. Normalizing that response
   * against 'Order' reads Order's field edges -- customer, items -- against an
   * envelope whose properties are items and total, matches nothing, and stores
   * nothing.
   *
   * Optional because a response whose root has no row in the entities table
   * needs no typename, and because a manifest generated before this field
   * existed does not carry it. `entity` is the fallback in both cases, which
   * is exactly right when the two coincide and no worse than the old behaviour
   * when they do not.
   */
  readonly rootType?: string;
  readonly provides: readonly string[];
  readonly invalidates: readonly string[];
  readonly security?: readonly string[];
  /**
   * Schema id (a key into the generated `src/codecs.ts`) that renames this
   * operation's JSON request body from its TypeScript shape to the wire shape,
   * and its JSON response back again.
   *
   * These exist because this package drives the generated `HTTPClient#request`
   * rather than the typed per-endpoint methods (see `RestClientLike`), and
   * `request` applies a codec only when the config it is handed names one. The
   * typed methods name them from their own generated call sites; a generic
   * caller can only name them from the manifest, so the generator emits them
   * here too, resolved by the same Go pass. Without that, a client generated
   * under `--field-naming camel` returns camelCase through its typed methods
   * and WIRE-cased through this transport -- one package contradicting its own
   * TypeScript types.
   *
   * Absent when the operation has no JSON body/response resolving to a named
   * component schema, and absent from every manifest generated for a client
   * that renames nothing at all. `request` treats an absent codec as a
   * passthrough, which is exactly right in both cases.
   */
  readonly bodyCodec?: string;
  /** See `bodyCodec`. */
  readonly responseCodec?: string;
}

/** One operation invocation, as the query cache hands it to a transport. */
export interface TransportRequest {
  readonly meta: OperationMeta;
  /** Path, query and body, in the vocabulary the tag resolver already speaks. */
  readonly args: TagContext;
  readonly headers?: Readonly<Record<string, string>> | undefined;
  readonly signal?: AbortSignal | undefined;
}

/**
 * What the cache needs of a wire protocol.
 *
 * One method, because the cache's only question is "run this operation and
 * give me the decoded response". WebSocket and SSE transports in a later chunk
 * implement the same interface for their request/response operations.
 */
export interface Transport {
  execute(request: TransportRequest): Promise<unknown>;
}

/** The request the generated `HTTPClient.request` accepts. */
export interface RestRequestConfig {
  method: string;
  url: string;
  headers?: Record<string, string> | undefined;
  body?: unknown;
  signal?: AbortSignal | undefined;
  retry?: { maxAttempts?: number } | undefined;
  /**
   * Codec ids forwarded straight from `OperationMeta`. Declared here because
   * `HTTPClient#request` applies `encode`/`decode` only for the fields its
   * config carries -- a config that omits them is a request that silently
   * ships wire-cased and comes back un-decoded. The generated `RequestConfig`
   * declares the same two fields (under the same condition), so a generated
   * client satisfies this structurally either way.
   */
  bodyCodec?: string | undefined;
  responseCodec?: string | undefined;
  /**
   * The fetch credentials mode, forwarded from `RestTransportOptions`.
   *
   * Declared here for the reason the codec fields are: `HTTPClient#request`
   * applies only what its config carries, so a config that omits this is a
   * request that silently does not send cookies cross-origin.
   */
  credentials?: RequestCredentials | undefined;
}

/**
 * The generated client, narrowed to the one member this drives.
 *
 * `RESTClient extends HTTPClient` satisfies this structurally, so an
 * application passes its generated client straight in. The typed per-endpoint
 * methods are not driven: their parameters are positional and per-endpoint
 * (`(orderId, body, options)`), which no generic caller holding only an
 * `OperationMeta` can fill. `request` is the seam that is generic, and it is
 * the same one every generated method funnels into -- so path building,
 * content-type dispatch, codecs and binary bodies below it are unchanged.
 */
export interface RestClientLike {
  request<T>(config: RestRequestConfig): Promise<T>;
}

/** How a delay is taken. Injected so retry backoff is testable without timers. */
export type Sleep = (ms: number) => Promise<void>;

/** The default: a real timer. */
export const realSleep: Sleep = (ms) =>
  new Promise<void>((resolve) => {
    setTimeout(resolve, ms);
  });

/** A clock a test drives by hand. */
export interface ManualClock {
  readonly sleep: Sleep;
  /** Milliseconds elapsed. */
  now(): number;
  /** How many sleeps are waiting. */
  pending(): number;
  /**
   * Move time forward, wake every sleep that came due, and drain the microtask
   * queue on both sides so the code under test has actually reacted by the
   * time this resolves.
   */
  advance(ms: number): Promise<void>;
}

/**
 * A clock that moves only when asked.
 *
 * The alternative -- a real timer and a test that sleeps long enough for the
 * retry to have happened -- passes locally, flakes on a loaded CI box, and is
 * deleted the week after it is written. Nothing in this package's tests waits
 * on wall-clock time.
 */
export function manualClock(): ManualClock {
  let now = 0;
  let waiting: { at: number; wake: () => void }[] = [];

  const drain = async (): Promise<void> => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  };

  return {
    sleep: (ms) =>
      new Promise<void>((wake) => {
        waiting.push({ at: now + (ms > 0 ? ms : 0), wake });
      }),
    now: () => now,
    pending: () => waiting.length,
    async advance(ms) {
      await drain();

      now += ms;

      const due = waiting.filter((entry) => entry.at <= now);
      waiting = waiting.filter((entry) => entry.at > now);

      for (const entry of due) entry.wake();

      await drain();
    },
  };
}

/** Where credentials come from, and how they are renewed. */
export interface AuthProvider {
  /**
   * Headers to attach to this operation.
   *
   * Receives the operation, not just the request, so a provider can attach per
   * the endpoint's declared scheme -- a bearer token for one, an API key
   * header for another, nothing at all for a public endpoint.
   */
  credentials?(
    meta: OperationMeta,
  ): PromiseLike<Record<string, string> | undefined> | Record<string, string> | undefined;
  /**
   * Renew the credential. Called at most once per stampede: see
   * `RestTransport`.
   *
   * Resolving means "a new credential is now available from `credentials`".
   * Rejecting means the 401 stands, and the original error -- not the refresh
   * failure -- is what the caller sees.
   */
  refresh?(): PromiseLike<unknown> | unknown;
}

/**
 * Retry policy. Applied to idempotent methods only; see `RestTransport`.
 *
 * `attempts` counts the first try, so 3 means one request and two retries.
 */
export interface RetryPolicy {
  readonly attempts?: number;
  readonly baseDelay?: number;
  readonly maxDelay?: number;
}

export interface RestTransportOptions {
  readonly client: RestClientLike;
  readonly auth?: AuthProvider;
  readonly retry?: RetryPolicy;
  /** Defaults to a real timer. Tests pass `manualClock().sleep`. */
  readonly sleep?: Sleep;
  /** Jitter source. Defaults to `Math.random`. */
  readonly random?: () => number;
  /**
   * Prefixed onto every URL this builds. The generated client carries a base
   * URL of its own; set one or the other, not both.
   */
  readonly baseUrl?: string;
  /**
   * The fetch credentials mode for every request this transport builds.
   *
   * Unset by default, which preserves today's behaviour exactly -- a browser
   * already attaches same-origin cookies to `fetch` on its own. `'include'`
   * is the opt-in a cross-origin cookie session needs; nothing else in this
   * runtime sets it.
   */
  readonly credentials?: RequestCredentials;
}

const IDEMPOTENT = new Set(['GET', 'HEAD', 'PUT', 'DELETE']);

/**
 * Drives the generated REST client, with the retry policy the design
 * specifies and single-flight credential refresh.
 *
 * **Retries are for idempotent methods only** (`GET`, `HEAD`, `PUT`,
 * `DELETE`), never on a 4xx except 408 and 429, with exponential backoff and
 * jitter. Retrying a `POST` that timed out is how duplicate orders happen: the
 * client cannot distinguish a request the server never saw from one it
 * processed and failed to acknowledge, and only the idempotent methods make
 * that distinction not matter.
 *
 * The generated client has a retry loop of its own, which retries any method
 * on 408/429/5xx. Every request built here sets `retry: { maxAttempts: 1 }` to
 * switch it off, so exactly one policy governs -- this one.
 */
export class RestTransport implements Transport {
  private readonly client: RestClientLike;
  private readonly auth: AuthProvider | undefined;
  private readonly sleep: Sleep;
  private readonly random: () => number;
  private readonly attempts: number;
  private readonly baseDelay: number;
  private readonly maxDelay: number;
  private readonly baseUrl: string;
  private readonly credentials: RequestCredentials | undefined;

  /** The refresh everyone stampeding a 401 waits on, while it is running. */
  private refreshing: Promise<void> | undefined;
  /**
   * Bumped by each completed refresh.
   *
   * A request that 401s holds the reading its credentials were taken at. If
   * that reading is behind the current one, some other request already
   * refreshed since, so this one retries against the new credential without
   * asking for another refresh. That is the difference between one refresh per
   * stampede and one refresh per request in it.
   */
  private generation = 0;

  constructor(options: RestTransportOptions) {
    this.client = options.client;
    this.auth = options.auth;
    this.sleep = options.sleep ?? realSleep;
    this.random = options.random ?? Math.random;
    this.attempts = options.retry?.attempts ?? 3;
    this.baseDelay = options.retry?.baseDelay ?? 300;
    this.maxDelay = options.retry?.maxDelay ?? 30000;
    this.baseUrl = options.baseUrl ?? '';
    this.credentials = options.credentials;
  }

  async execute(request: TransportRequest): Promise<unknown> {
    const method = request.meta.method.toUpperCase();
    const config: RestRequestConfig = {
      method,
      // The operation name is threaded through so a missing path parameter
      // reports as `GET /orders/{id}` -- what the manifest calls this
      // operation -- rather than as a bare path.
      url:
        this.baseUrl +
        operationUrl(request.meta.path, request.args, `${method} ${request.meta.path}`),
      body: request.args.body,
      signal: request.signal,
      // The generated client's own retry loop, disabled. See the class comment.
      retry: { maxAttempts: 1 },
      ...(this.credentials === undefined ? {} : { credentials: this.credentials }),
    };

    if (request.headers !== undefined) config.headers = { ...request.headers };

    // Assigned only when present, rather than always assigned as possibly
    // undefined, so this compiles under `exactOptionalPropertyTypes` -- which
    // the generated clients are already built to satisfy.
    if (request.meta.bodyCodec !== undefined) config.bodyCodec = request.meta.bodyCodec;
    if (request.meta.responseCodec !== undefined) {
      config.responseCodec = request.meta.responseCodec;
    }

    const limit = IDEMPOTENT.has(method) ? this.attempts : 1;

    for (let attempt = 0; ; attempt++) {
      try {
        return await this.send(config, request.meta);
      } catch (error) {
        if (attempt + 1 >= limit || !retryable(error)) throw error;

        await this.sleep(this.backoff(attempt));
      }
    }
  }

  /**
   * One attempt, including the 401 path.
   *
   * The refresh retry is deliberately *inside* the retry loop rather than
   * layered over it, and applies to every method including `POST`. A 401 says
   * the server rejected the request before acting on it, so re-sending is safe
   * in a way a timeout is not. It is also strictly one retry: a second 401
   * against a freshly refreshed credential is an authorization answer, not a
   * transient failure, and looping on it is how a client hammers a login
   * endpoint.
   */
  private async send(config: RestRequestConfig, meta: OperationMeta): Promise<unknown> {
    // Read *after* the credentials, not before. `credentials` may await, and a
    // refresh landing inside that window would make this request look older
    // than the credential it is actually carrying -- costing one spurious
    // refresh on a 401 that the current reading already explains.
    const authorized = await this.authorize(config, meta);
    const generation = this.generation;

    try {
      return await this.client.request(authorized);
    } catch (error) {
      if (this.auth?.refresh === undefined || statusOf(error) !== 401) throw error;

      // Behind the current reading: someone else's refresh already landed, so
      // retry against it rather than asking for another one.
      if (generation === this.generation) {
        try {
          await this.refresh();
        } catch {
          // The refresh failed, so the 401 stands. Reporting the refresh's own
          // error here would replace "you are not authorized" with whatever
          // the token endpoint said, which is not what the caller asked for.
          throw error;
        }
      }

      return this.client.request(await this.authorize(config, meta));
    }
  }

  /** One refresh, however many callers arrive while it runs. */
  private refresh(): Promise<void> {
    if (this.refreshing !== undefined) return this.refreshing;

    const provider = this.auth as AuthProvider;
    const running = Promise.resolve()
      .then(() => provider.refresh?.())
      .then(() => {
        this.generation++;
      });

    // Cleared before any waiter resumes, so a 401 arriving after this refresh
    // has landed starts a new one rather than adopting a settled promise.
    this.refreshing = running.then(
      () => {
        this.refreshing = undefined;
      },
      (error) => {
        this.refreshing = undefined;

        throw error;
      },
    );

    return this.refreshing;
  }

  private async authorize(
    config: RestRequestConfig,
    meta: OperationMeta,
  ): Promise<RestRequestConfig> {
    const credentials = await this.auth?.credentials?.(meta);

    if (credentials === undefined) return config;

    return { ...config, headers: { ...config.headers, ...credentials } };
  }

  /**
   * Exponential backoff with jitter.
   *
   * Jitter is not decoration. Fifty clients that lost the same connection
   * retry on the same schedule without it, and arrive together -- the
   * thundering herd that turns a blip into an outage. Half the delay is fixed
   * so a retry still backs off, half is random so the herd disperses.
   */
  private backoff(attempt: number): number {
    const delay = Math.min(this.maxDelay, this.baseDelay * 2 ** attempt);

    return delay / 2 + this.random() * (delay / 2);
  }
}

/**
 * Whether a failure is worth another attempt, given the method already is.
 *
 * No status at all means the request never got an answer -- DNS, a dropped
 * connection, a parse failure -- which is the case retries exist for. A 4xx is
 * the server saying the request is wrong, and repeating it verbatim will get
 * the same answer; 408 and 429 are the two that explicitly ask for a retry.
 */
export function retryable(error: unknown): boolean {
  if (error !== null && typeof error === 'object' && (error as Error).name === 'AbortError') {
    return false;
  }

  const status = statusOf(error);

  if (status === undefined) return true;

  if (status < 400) return false;

  if (status < 500) return status === 408 || status === 429;

  return true;
}

/**
 * The HTTP status an error carries, if any.
 *
 * `statusCode` is what the generated `HTTPError` uses; `status` is what most
 * other client libraries use, and reading both costs nothing.
 */
export function statusOf(error: unknown): number | undefined {
  if (error === null || typeof error !== 'object') return undefined;

  const carrier = error as { statusCode?: unknown; status?: unknown };
  const code = typeof carrier.statusCode === 'number' ? carrier.statusCode : carrier.status;

  return typeof code === 'number' ? code : undefined;
}

const PLACEHOLDER = /\{([^{}]*)\}/g;

/**
 * A path placeholder had no usable argument, so no request was made.
 *
 * This is the one failure in this package that is a **bug in the calling
 * code** rather than an answer from a server, and it is a class rather than a
 * bare `Error` so that a caller can tell those two apart. A global `onError`
 * that reports every rejection to a crash tracker wants to page someone about
 * this one and not about the 404s; a migration harness wants to fail its
 * suite on it. Neither can key off a message.
 *
 * `operation` and `params` are carried as fields as well as being formatted
 * into the message, because a reporter that groups by message needs the
 * message to be short and a reporter that filters by parameter needs the
 * parameter to be a value.
 *
 * Deliberately carries **no** `status`/`statusCode`, so `statusOf` reads
 * `undefined` for it and it can never be mistaken for a server response.
 */
export class MissingPathParamsError extends Error {
  /** `GET /orders/{id}`, or the bare path template when the method is unknown. */
  readonly operation: string;
  /** Every placeholder that resolved to nothing, in the order they appear. */
  readonly params: readonly string[];

  constructor(operation: string, params: readonly string[]) {
    super(
      `${operation}: no value for path parameter${params.length > 1 ? 's' : ''} ` +
        `${params.map((name) => `{${name}}`).join(', ')}. No request was sent.`,
    );

    this.name = 'MissingPathParamsError';
    this.operation = operation;
    this.params = params;
  }
}

/**
 * Build one operation's URL from its path template and arguments.
 *
 * Query parameters are emitted in sorted key order so two calls with the same
 * arguments produce byte-identical URLs -- which is what makes an HTTP cache,
 * a service worker or a request log treat them as the same request. The
 * in-memory cache key does not depend on this (see `queryKey`, which sorts for
 * the same reason), but everything downstream of the wire does.
 *
 * **A path placeholder with no usable argument throws**, and the throw happens
 * before this returns anything at all -- so no partially substituted URL is
 * ever handed to a transport, a cache or a log.
 *
 * The previous behaviour left the placeholder in the URL, on the reasoning
 * that `/orders/{id}` reaching the wire "fails loudly" where `/orders/` would
 * quietly fetch the collection. Only half of that is true. It does not fail
 * loudly; it fails *remotely*, as a 403 or a 404 from a URL that cannot exist,
 * arriving as an ordinary server error that a component renders as an empty
 * state. The bug was found by diffing recorded request logs, which is what it
 * takes to see it, and the call site that caused it -- a path parameter
 * arriving from React context one render late -- is well typed and looks
 * correct. TypeScript cannot see it either: `{path: {orgId}}` type-checks
 * whatever `orgId` holds at runtime.
 *
 * Compare the query parameter loop below, which skips a nullish value. That
 * asymmetry is the argument for this being a bug rather than a preference: a
 * dropped filter degrades a result set, while a dropped path segment addresses
 * a different resource, and the careless handling was on the dangerous one.
 *
 * A parameter is unusable when it renders to no characters -- `undefined`,
 * `null`, `''`, and `[]`, which `String` also flattens to nothing. That is the
 * same rule `isIdentity` applies to an entity id and `usable` applies to a
 * tag: this package already refuses to build a key out of an empty string, and
 * a URL is a key.
 *
 * `operation` names the failure. It defaults to the path template, which is
 * specific enough to find the call site; `RestTransport` passes the method
 * too, because `GET /orders/{id}` is what the manifest calls this operation
 * and what a reader will grep for.
 *
 * @throws {MissingPathParamsError} when any placeholder resolves to nothing.
 */
export function operationUrl(path: string, args: TagContext, operation: string = path): string {
  const params = args.path;
  const missing: string[] = [];

  // Every missing parameter is collected and reported together. A path with
  // two placeholders that is called with neither should say so once, rather
  // than sending the caller round the loop to discover the second one after
  // fixing the first.
  let url = path.replace(PLACEHOLDER, (_match, name: string) => {
    const key = name.trim();
    const value = params?.[key];
    // Rendered first, then tested, so that one check covers every value that
    // contributes no path segment. Testing the input instead would need a
    // list of the shapes that stringify to nothing and would still miss one.
    const rendered =
      value === undefined || value === null ? '' : encodeURIComponent(String(value));

    if (rendered === '') missing.push(key);

    return rendered;
  });

  // Thrown after the substitution pass rather than before it, which is what
  // lets the message name every missing parameter. `url` holds a partial
  // string at this point and is unreachable from here: nothing is returned,
  // and the query string below is never built.
  if (missing.length > 0) throw new MissingPathParamsError(operation, missing);

  const query = args.query;

  if (query === undefined) return url;

  const search = new URLSearchParams();

  for (const key of Object.keys(query).sort()) {
    const value = query[key];

    if (value === undefined || value === null) continue;

    if (Array.isArray(value)) {
      for (const item of value) {
        if (item !== undefined && item !== null) search.append(key, String(item));
      }

      continue;
    }

    search.append(key, String(value));
  }

  const rendered = search.toString();

  if (rendered !== '') url += (url.includes('?') ? '&' : '?') + rendered;

  return url;
}
