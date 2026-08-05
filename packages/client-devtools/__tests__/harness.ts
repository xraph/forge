import { manualScheduler, QueryCache } from '@forge-go/client-core';
import type {
  EntitySchema,
  ManualScheduler,
  OperationMeta,
  Transport,
  TransportRequest,
} from '@forge-go/client-core';

/**
 * A cache with no network, no timers and no framework.
 *
 * Every schedule in these tests is driven by hand: the invalidation batch runs
 * when `flush()` says so, and nothing anywhere waits on wall-clock time.
 */
export const schema: EntitySchema = {
  Order: { idField: 'id', fields: { customer: 'Customer', items: 'LineItem' } },
  Customer: { idField: 'id' },
  LineItem: { idField: 'sku' },
};

/**
 * Four operations, chosen so that one of them reproduces the defect this whole
 * package exists to explain.
 *
 * `orderCreate` invalidates `Order:{res.id}` and nothing else. That is a
 * perfectly plausible declaration, it is wrong, and it is wrong *silently*: the
 * newly created order's own tag reaches no list, because a list only carries
 * `Order:9` after a response has actually put order 9 in it -- which for a
 * create it never has. The list keeps showing the old rows and nothing reports
 * anything.
 */
export const ops = {
  orderList: {
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    provides: ['Order[]'],
    invalidates: [],
  },
  orderGet: {
    method: 'GET',
    path: '/orders/{id}',
    entity: 'Order',
    provides: ['Order:{id}'],
    invalidates: [],
  },
  orderCreate: {
    method: 'POST',
    path: '/orders',
    entity: 'Order',
    provides: [],
    invalidates: ['Order:{res.id}'],
  },
  orderUpdate: {
    method: 'PATCH',
    path: '/orders/{id}',
    entity: 'Order',
    provides: [],
    invalidates: ['Order:{id}', 'Order[]'],
  },
  /** Declares a template that cannot resolve unless the response carries `ref`. */
  orderArchive: {
    method: 'POST',
    path: '/orders/{id}/archive',
    entity: 'Order',
    provides: [],
    invalidates: ['Order[]:{res.ref}'],
  },
} satisfies Record<string, OperationMeta>;

export interface Harness {
  readonly cache: QueryCache;
  readonly scheduler: ManualScheduler;
  readonly calls: TransportRequest[];
  /** Run the pending invalidation batch. */
  flush(): void;
  /** Let queued microtasks run. Not a sleep. */
  settle(): Promise<void>;
  /** What the transport answers with, by `METHOD path`. */
  reply(operation: string, value: unknown): void;
}

export function harness(): Harness {
  const scheduler = manualScheduler();
  const calls: TransportRequest[] = [];
  const replies = new Map<string, unknown>([
    [
      'GET /orders',
      [
        { id: 1, total: 10, customer: { id: 'c1', name: 'Ada' } },
        { id: 2, total: 20, customer: { id: 'c1', name: 'Ada' } },
      ],
    ],
    ['GET /orders/{id}', { id: 1, total: 10 }],
    ['POST /orders', { id: 9, total: 30 }],
    // Deliberately identical to what the list already holds, so a test can
    // show that a write of unchanged data bumps no version.
    ['PATCH /orders/{id}', { id: 1, total: 10 }],
    ['POST /orders/{id}/archive', { id: 1, archived: true }],
  ]);

  const transport: Transport = {
    execute(request) {
      calls.push(request);

      return Promise.resolve().then(() =>
        clone(replies.get(`${request.meta.method} ${request.meta.path}`)),
      );
    },
  };

  const cache = new QueryCache({ transport, entities: schema, scheduler: scheduler.schedule });

  return {
    cache,
    scheduler,
    calls,
    flush: () => {
      scheduler.flush();
    },
    async settle() {
      for (let i = 0; i < 8; i++) await Promise.resolve();
    },
    reply(operation, value) {
      replies.set(operation, value);
    },
  };
}

/**
 * A fresh object graph per request.
 *
 * A transport that hands back the same object twice would let the store's
 * identity checks compare a value against itself, which is exactly the case a
 * real response never presents.
 */
function clone<T>(value: T): T {
  return value === undefined ? value : (JSON.parse(JSON.stringify(value)) as T);
}

/** A clock that only moves when a test asks it to. */
export function counter(): () => number {
  let at = 0;

  return () => ++at;
}
