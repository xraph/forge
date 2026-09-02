import {
  QueryCache,
  StreamBinder,
  SubscriptionManager,
  manualScheduler,
  mutation,
  query,
} from '@forge-go/client-core';
import type {
  EntitySchema,
  ManualScheduler,
  OperationMeta,
  StreamBinding,
  StreamConnection,
  Transport,
  TransportRequest,
} from '@forge-go/client-core';

/** The two entities every test in this package renders. */
export const schema: EntitySchema = {
  Order: {
    idField: 'id',
    fields: { customer: 'Customer' },
  },
  Customer: {
    idField: 'id',
  },
};

export const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

export const orderGet: OperationMeta = {
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  invalidates: [],
};

export const orderPatch: OperationMeta = {
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  invalidates: ['Order[]', 'Order:{id}'],
};

export const orderCreate: OperationMeta = {
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  provides: [],
  invalidates: ['Order[]'],
};

/**
 * A read that declares **no tags at all**, which is the shape roughly four out
 * of five generated reads actually have today.
 *
 * It still acquires the entity keys its response normalizes to, so a write to
 * `Order:1` reaches it. What cannot reach it is a list-level invalidation: a
 * create declaring `Order[]` refreshes `orderList` and leaves this one showing
 * a result set the row it just created belongs in. That gap is the reason the
 * invalidation binding addresses queries by operation rather than by tag.
 */
export const orderSearch: OperationMeta = {
  method: 'GET',
  path: '/orders/search',
  entity: 'Order',
  provides: [],
  invalidates: [],
};

export interface Order {
  readonly id: number;
  readonly total: number;
}

/**
 * The bindings, exactly as a generated `hooks.ts` declares them: module
 * scope, one line each, no logic.
 */
export const useOrderList = query<Order[]>(orderList);
export const useOrderSearch = query<Order[]>(orderSearch);
export const useOrderGet = query<Order>(orderGet);
export const useOrderPatch = mutation<Order>(orderPatch);
export const useOrderCreate = mutation<Order>(orderCreate);

export interface FakeTransport extends Transport {
  readonly calls: TransportRequest[];
  /** How many times this operation has been requested. */
  countOf(meta: OperationMeta): number;
}

/**
 * A transport under the test's control. No HTTP, no timers, no sleeps: every
 * response resolves on the microtask queue, which `act()` drains.
 */
export function fakeTransport(
  handler: (request: TransportRequest, call: number) => unknown,
): FakeTransport {
  const calls: TransportRequest[] = [];

  return {
    calls,
    countOf: (meta) => calls.filter((c) => c.meta === meta).length,
    execute(request: TransportRequest): Promise<unknown> {
      const call = calls.length;
      calls.push(request);

      return Promise.resolve().then(() => handler(request, call));
    },
  };
}

export interface Harness {
  readonly cache: QueryCache;
  readonly transport: FakeTransport;
  readonly scheduler: ManualScheduler;
}

/**
 * A cache wired to a hand-driven scheduler.
 *
 * The scheduler is manual so that "a mutation refetches the queries it
 * invalidated" is a statement about a batch the test flushes, not about a
 * microtask that may or may not have run by the time an assertion reads.
 */
export function harness(handler: (request: TransportRequest, call: number) => unknown): Harness {
  const transport = fakeTransport(handler);
  const scheduler = manualScheduler();
  const cache = new QueryCache({ transport, entities: schema, scheduler: scheduler.schedule });

  return { cache, transport, scheduler };
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
 * The generated manifest's `streams` table, for the one channel these tests
 * push on.
 *
 * Deliberately bound to `Order` only. Which *set* of channels a query resolves
 * to -- the closure over the result type's entity graph -- is the core's rule
 * and is tested there, against a schema built for it. What these tests are
 * about is the adapter's half: that the subscription is acquired once, shared,
 * and released on exactly the right teardown.
 */
export const streams: readonly StreamBinding[] = [
  {
    channel: '/ws/orders',
    message: 'order.created',
    entity: 'Order',
    intent: 'upsert',
    invalidates: ['Order[]'],
  },
  {
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: 'patch',
    invalidates: [],
  },
];

/**
 * A socket the test drives by hand.
 *
 * No `WebSocket`, no `EventSource`, no server, no timers: every frame in these
 * tests is a method call.
 */
export interface FakeConnection extends StreamConnection {
  /** The channels multiplexed over it when it was opened. */
  readonly channels: readonly string[];
  /** Push one message to the binder. */
  deliver(message: unknown): void;
  readonly closed: boolean;
}

export interface LiveHarness extends Harness {
  readonly manager: SubscriptionManager;
  readonly binder: StreamBinder;
  /** When a batch of frames commits. Manual, so a frame lands where the test says. */
  readonly frames: ManualScheduler;
  /**
   * When a socket nobody is subscribed to is actually closed.
   *
   * Manual, and that is the whole of the StrictMode story: the deferred close
   * is what makes a phantom unmount free, so a test that asserts a release has
   * to be the thing that decides the deferral has elapsed.
   */
  readonly closes: ManualScheduler;
  /** Every socket ever opened, in order. */
  readonly opened: FakeConnection[];
  /** How many are still open. */
  live(): number;
  /** Push one message onto every open socket, and commit the batch. */
  emit(message: unknown): void;
}

/**
 * The `harness` above, plus a subscription manager and a bound stream runtime.
 *
 * The binder registers itself on the cache in its constructor, which is how
 * `{live: true}` finds it: the adapter resolves a cache and reads the runtime
 * off it. Nothing here hands the binder to a component.
 */
export function liveHarness(
  handler: (request: TransportRequest, call: number) => unknown,
  bindings: readonly StreamBinding[] = streams,
): LiveHarness {
  const base = harness(handler);
  const opened: FakeConnection[] = [];
  const frames = manualScheduler();
  const closes = manualScheduler();

  const manager = new SubscriptionManager({
    connect: (context) => {
      let deliver: ((message: unknown) => void) | undefined;
      let closed = false;

      const connection: FakeConnection = {
        channels: [...context.channels],
        get closed() {
          return closed;
        },
        onMessage(next) {
          deliver = next;
        },
        onClose() {
          // Nothing in these tests drops a socket; reconnect is the core's.
        },
        close() {
          closed = true;
        },
        deliver(message) {
          deliver?.(message);
        },
      };

      opened.push(connection);

      return connection;
    },
    release: closes.schedule,
    principal: () => base.cache.owner,
  });

  const binder = new StreamBinder({
    cache: base.cache,
    streams: bindings,
    manager,
    scheduler: frames.schedule,
  });

  return {
    ...base,
    manager,
    binder,
    frames,
    closes,
    opened,
    live: () => opened.filter((connection) => !connection.closed).length,
    emit(message: unknown) {
      for (const connection of opened) {
        if (!connection.closed) connection.deliver(message);
      }

      frames.flush();
    },
  };
}
