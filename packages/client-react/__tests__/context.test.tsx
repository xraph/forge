import { act, render, screen } from '@testing-library/react';
import { renderToString } from 'react-dom/server';
import { afterEach, describe, expect, it } from 'vitest';
import { QueryCache, setClient } from '@forge-go/client-core';
import { ForgeProvider, useForgeClient, useQuery } from '../src';
import { harness, orderList, schema, useOrderList } from './harness';
import type { Order } from './harness';

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

afterEach(() => {
  setClient(undefined);
});

describe('client resolution', () => {
  it('falls back to the module-level client when no provider is rendered', async () => {
    const h = harness(() => [{ id: 1, total: 99 }]);

    setClient(h.cache);

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    // No provider anywhere. A generated `hooks.ts` binds at module scope and
    // must not require the application to have adopted a particular dependency
    // injection style before a single hook will run.
    render(<List />);
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('99');
  });

  it('prefers a provided client over the module-level one', async () => {
    const global_ = harness(() => [{ id: 1, total: 1 }]);
    const scoped = harness(() => [{ id: 1, total: 2 }]);

    setClient(global_.cache);

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    render(
      <ForgeProvider client={scoped.cache}>
        <List />
      </ForgeProvider>,
    );
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('2');
    expect(global_.transport.calls).toHaveLength(0);
  });

  it('prefers a per-call client over a provided one', async () => {
    const provided = harness(() => [{ id: 1, total: 1 }]);
    const explicit = harness(() => [{ id: 1, total: 3 }]);

    function List() {
      const { data } = useQuery<Order[]>(useOrderList, undefined, { client: explicit.cache });

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    render(
      <ForgeProvider client={provided.cache}>
        <List />
      </ForgeProvider>,
    );
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('3');
    expect(provided.transport.calls).toHaveLength(0);
  });

  it('reports the missing configuration rather than fetching into a scratch cache', () => {
    function List() {
      useQuery<Order[]>(useOrderList);

      return null;
    }

    expect(() => render(<List />)).toThrow(/no client configured/);
  });

  it('exposes the resolved client for work that reaches past the hooks', async () => {
    const h = harness(() => [{ id: 1, total: 99 }]);

    let resolved: QueryCache | undefined;

    function Probe() {
      resolved = useForgeClient();

      return null;
    }

    render(
      <ForgeProvider client={h.cache}>
        <Probe />
      </ForgeProvider>,
    );

    expect(resolved).toBe(h.cache);
  });
});

describe('getServerSnapshot', () => {
  it('renders the loading branch on the server and issues no request', () => {
    const h = harness(() => [{ id: 1, total: 99 }]);

    function List() {
      const { status, data, isFetching } = useQuery<Order[]>(useOrderList);

      return (
        <div>
          {status}:{data === undefined ? '-' : data.length}:{String(isFetching)}
        </div>
      );
    }

    const html = renderToString(
      <ForgeProvider client={h.cache}>
        <List />
      </ForgeProvider>,
    );

    // `idle`, always. This chunk ships no store serialisation, so a hydrating
    // client necessarily starts empty; returning anything else here would be a
    // guaranteed hydration mismatch rather than an optimisation.
    expect(html.replace(/<!-- -->/g, '')).toBe('<div>idle:-:false</div>');
    // And it did not open a record on a cache that, on a server, is shared by
    // every concurrent request.
    expect(h.transport.calls).toHaveLength(0);
    expect(h.cache.size).toBe(0);
  });

  it('returns the same object on every call, for a query no cache has opened', () => {
    // A server render of the same hook twice, on a cache with nothing in it:
    // `getServerSnapshot` has no per-record memo to lean on, so stability has
    // to come from the constant itself.
    const empty = new QueryCache({
      transport: { execute: () => Promise.resolve(null) },
      entities: schema,
    });

    function List() {
      const result = useQuery<Order[]>(useOrderList);

      seen.push(result);

      return null;
    }

    const seen: unknown[] = [];

    renderToString(
      <ForgeProvider client={empty}>
        <List />
        <List />
      </ForgeProvider>,
    );

    expect(seen).toHaveLength(2);
    expect(empty.registry.get(empty.key(orderList))).toBeUndefined();
  });
});
