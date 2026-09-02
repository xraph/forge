/**
 * Type-level assertions for the mutation boundary.
 *
 * There is nothing to run here and Vitest never loads this file -- its name
 * does not match the `*.test.ts` include -- but `npm run typecheck` does,
 * because `tsconfig.test.json` covers `__tests__`. A `@ts-expect-error` that
 * stops erroring is itself a compile error, which is what makes each one below
 * an assertion rather than a comment.
 *
 * What is pinned is that a generated binding's entity type reaches the
 * `optimistic` option. No behavioural test can stand in for this: erasing
 * `TEntity` changes nothing at runtime. The patch still dispatches, the request
 * still goes out, and a misspelled field simply never appears -- which is the
 * failure the typing exists to turn into a red squiggle instead of a bug
 * report.
 */

import { mutation, query } from '@forge-go/client-core';
import type { OperationMeta } from '@forge-go/client-core';
import { useMutation } from '../src/useMutation';
import { useInvalidate } from '../src/useInvalidate';

interface Order {
  id: string;
  status: string;
  total: number;
}

const meta: OperationMeta = {
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  provides: [],
  invalidates: ['Order:{id}'],
};

/** What the generator emits when the endpoint declares both type names. */
const updateOrder = mutation<Order, Order>(meta);

/** What it emits when it cannot: the entity falls back to `unknown`. */
const untypedUpdate = mutation<Order>(meta);

export function entityTypeReachesTheOptimisticOption(): void {
  const update = useMutation(updateOrder);

  void update.mutate({ path: { id: '7' } }, { optimistic: { status: 'shipped' } });
  void update.mutate({ path: { id: '7' } }, { optimistic: (prev) => ({ total: prev.total + 1 }) });
  void update.mutate({ path: { id: '7' } }, { optimistic: 'delete' });

  // The same patch with the field misspelled. Against `unknown` this compiles,
  // dispatches, and silently changes nothing.
  // @ts-expect-error `stauts` is not a field of Order
  void update.mutate({ path: { id: '7' } }, { optimistic: { stauts: 'shipped' } });

  // @ts-expect-error Order.total is a number
  void update.mutateAsync({ path: { id: '7' } }, { optimistic: { total: 'lots' } });

  // Hook-level options are checked too, not only per-call ones.
  // @ts-expect-error `stauts` is not a field of Order
  useMutation(updateOrder, { optimistic: { stauts: 'shipped' } });
}

/** A binding with no entity type still compiles exactly as it did. */
export function anUntypedBindingIsUnaffected(): void {
  void useMutation(untypedUpdate).mutate({}, { optimistic: { whatever: true } });
}

/**
 * `useInvalidate` takes reads, and only reads.
 *
 * Worth pinning because the mistake is so easy to make and so quiet when it
 * lands: `invalidate(useOrderCreate)` reads as "refresh what this write
 * changed", which is the one thing it cannot mean. A mutation binding names no
 * cached query, so every call matches nothing, refetches nothing, and reports
 * nothing. `QueryBinding` in the signature is what turns that into a compile
 * error instead of a list that never updates.
 */
export function invalidateRejectsAMutationBinding(): void {
  const orderList = query<Order[]>({
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    provides: [],
    invalidates: [],
  });

  const invalidate = useInvalidate();

  invalidate(orderList);
  invalidate(orderList, { query: { status: 'open' } });
  void invalidate.refetch(orderList);
  invalidate.tags(['Order[]']);

  // @ts-expect-error a mutation names no cached query
  invalidate(updateOrder);

  // @ts-expect-error same, for the awaitable spelling
  void invalidate.refetch(updateOrder);
}
