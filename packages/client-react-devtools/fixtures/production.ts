import { configureClient, mutation, query, RestTransport } from '@forge-go/client-core';

/**
 * An application that uses the runtime and never mentions the devtools.
 *
 * The control in this package's own zero-cost experiment, copied in shape
 * from `client-devtools/fixtures/production.ts`. `react-guarded.tsx` builds
 * on top of this, so a failure here is a failure of the control, not of the
 * React entry point it exists to isolate.
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
