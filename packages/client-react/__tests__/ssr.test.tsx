import { Component, StrictMode } from 'react';
import type { ReactElement, ReactNode } from 'react';
import { renderToString } from 'react-dom/server';
import { act, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { dehydrate } from '@forge-go/client-core';
import type { DehydratedState } from '@forge-go/client-core';
import { HydrationBoundary, ClientProvider, useQuery } from '../src';
import { harness, orderList, useOrderList } from './harness';
import type { Harness, Order } from './harness';

const ops = { orderList };

function Orders(): ReactElement {
  const { status, data } = useQuery<Order[]>(useOrderList);

  return (
    <ul data-testid="orders" data-status={status}>
      {(data ?? []).map((order) => (
        <li key={order.id}>{order.total}</li>
      ))}
    </ul>
  );
}

/** A cache whose transport must never be reached. */
function offline(): Harness {
  return harness(() => {
    throw new Error('a hydrated query must not fetch');
  });
}

/**
 * One server render: its own cache, prefetched, dehydrated, and the payload put
 * through JSON exactly as an HTML response would.
 */
async function serverRender(): Promise<{ html: string; state: DehydratedState }> {
  const server = harness(() => [{ id: 7, total: 99 }]);

  await server.cache.fetch(orderList);

  const state = dehydrate(server.cache, { principal: undefined });
  const html = renderToString(
    <ClientProvider client={server.cache}>
      <HydrationBoundary state={state} ops={ops}>
        <Orders />
      </HydrationBoundary>
    </ClientProvider>,
  );

  return { html, state: JSON.parse(JSON.stringify(state)) as DehydratedState };
}

/**
 * The nearest error boundary, as the hydration boundary's docblock names it.
 *
 * A class component because that is still the only way to catch a render throw;
 * there is no hook equivalent.
 */
class Catch extends Component<{ children?: ReactNode }, { message: string | undefined }> {
  override state = { message: undefined as string | undefined };

  static getDerivedStateFromError(error: unknown): { message: string } {
    return { message: error instanceof Error ? error.message : String(error) };
  }

  override render(): ReactNode {
    if (this.state.message === undefined) return this.props.children;

    return <p data-testid="caught">{this.state.message}</p>;
  }
}

/** Render a tree into the DOM inside `act`, so effects and microtasks settle. */
async function mount(tree: ReactElement): Promise<void> {
  await act(async () => {
    render(tree);
  });
}

describe('server rendering', () => {
  it('emits the data rather than the loading branch', async () => {
    const { html } = await serverRender();

    expect(html).toContain('data-status="success"');
    expect(html).toContain('99');
  });
});

describe('hydrating', () => {
  it('renders the server data on the first pass and issues no request', async () => {
    const { state } = await serverRender();
    const client = offline();

    await mount(
      <ClientProvider client={client.cache}>
        <HydrationBoundary state={state} ops={ops}>
          <Orders />
        </HydrationBoundary>
      </ClientProvider>,
    );

    expect(screen.getByTestId('orders').dataset.status).toBe('success');
    expect(screen.getByText('99')).toBeTruthy();
    expect(client.transport.calls).toHaveLength(0);
  });

  it('hydrates once under StrictMode, whose renders are double-invoked', async () => {
    const { state } = await serverRender();
    const client = offline();

    await mount(
      <StrictMode>
        <ClientProvider client={client.cache}>
          <HydrationBoundary state={state} ops={ops}>
            <Orders />
          </HydrationBoundary>
        </ClientProvider>
      </StrictMode>,
    );

    expect(client.cache.store.getRecord('Order:7')?.version).toBe(1);
  });

  it('refetches on mount when hydrated stale', async () => {
    const { state } = await serverRender();
    const client = harness(() => [{ id: 7, total: 120 }]);

    await mount(
      <ClientProvider client={client.cache}>
        <HydrationBoundary state={state} ops={ops} stale>
          <Orders />
        </HydrationBoundary>
      </ClientProvider>,
    );

    await act(async () => {
      client.scheduler.flush();
    });

    expect(client.transport.calls).toHaveLength(1);
  });

  it('renders children unchanged when there is nothing to hydrate', async () => {
    const client = harness(() => [{ id: 7, total: 99 }]);

    await mount(
      <ClientProvider client={client.cache}>
        <HydrationBoundary state={undefined} ops={ops}>
          <Orders />
        </HydrationBoundary>
      </ClientProvider>,
    );

    expect(screen.getByTestId('orders')).toBeTruthy();
  });

  it('adds no element of its own, so the server and client DOM agree', async () => {
    const { state } = await serverRender();
    const client = offline();

    await mount(
      <ClientProvider client={client.cache}>
        <HydrationBoundary state={state} ops={ops}>
          <Orders />
        </HydrationBoundary>
      </ClientProvider>,
    );

    expect(document.body.firstElementChild?.firstElementChild?.tagName).toBe('UL');
  });
});

describe('when hydrate refuses the payload', () => {
  it('rethrows a principal mismatch, so an error boundary catches it', async () => {
    const { state } = await serverRender();
    const client = offline();

    client.cache.setPrincipal('someone-else');

    // React logs a caught render error whatever the boundary does with it.
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => undefined);

    await mount(
      <Catch>
        <ClientProvider client={client.cache}>
          <HydrationBoundary state={state} ops={ops}>
            <Orders />
          </HydrationBoundary>
        </ClientProvider>
      </Catch>,
    );

    quiet.mockRestore();

    expect(screen.getByTestId('caught').textContent).toMatch(/belongs to a different principal/);
    // The subtree did not mount, which is the whole point of rethrowing.
    expect(screen.queryByTestId('orders')).toBeNull();
  });

  it('reports and renders on when the payload is from a newer client', async () => {
    const reported: unknown[] = [];
    const client = harness(() => [{ id: 7, total: 99 }]);

    client.cache.report = (error) => reported.push(error);

    await mount(
      <ClientProvider client={client.cache}>
        <HydrationBoundary state={{ v: 9 } as never} ops={ops}>
          <Orders />
        </HydrationBoundary>
      </ClientProvider>,
    );

    // The subtree mounted and fetched for itself rather than blanking.
    expect(screen.getByTestId('orders')).toBeTruthy();
    expect(reported).toHaveLength(1);
    expect(client.transport.calls).toHaveLength(1);
  });
});
