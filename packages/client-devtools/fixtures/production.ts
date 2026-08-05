import { configureClient, mutation, query, RestTransport } from '@forge-go/client-core';

/**
 * An application that uses the runtime and never imports the devtools.
 *
 * The control in the zero-cost experiment. Deliberately exercises the code
 * paths the observation seam is threaded through -- a mounted query, a settle,
 * a mutation, an invalidation -- so that if any of the emit sites dragged the
 * devtools in behind it, this bundle would contain it.
 */
const ops = {
  orderList: { method: 'GET', path: '/orders', entity: 'Order', provides: ['Order[]'], invalidates: [] },
  orderCreate: { method: 'POST', path: '/orders', entity: 'Order', provides: [], invalidates: ['Order[]'] },
} as const;

export const client = configureClient({
  transport: new RestTransport({
    client: { request: <T,>() => Promise.resolve([] as unknown as T) },
  }),
  entities: { Order: { idField: 'id' } },
});

export const useOrders = query(ops.orderList);
export const createOrder = mutation(ops.orderCreate);

export function run(): void {
  const handle = useOrders();
  const release = handle.subscribe(() => undefined);

  void handle.fetch();
  void createOrder({ body: {} });
  void client.getState(ops.orderList);

  release();
}
