import { QueryCache, manualScheduler, mutation, query } from '@forge-go/client-core';
import type {
  EntitySchema,
  ManualScheduler,
  OperationMeta,
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

export interface Order {
  readonly id: number;
  readonly total: number;
}

/**
 * The four bindings, exactly as a generated `hooks.ts` declares them: module
 * scope, one line each, no logic.
 */
export const useOrderList = query<Order[]>(orderList);
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
